"""What the archiver decides: when a batch is due, what it is called, what it holds."""

from __future__ import annotations

import json
from datetime import UTC, datetime, timedelta
from typing import Any

import pytest

from url_shortener_log.archive import Record, Writer, key_for, render

START = datetime(2026, 9, 24, 11, 30, 5, tzinfo=UTC)


def record(sequence: int, *, at: datetime | None = None, body: bytes = b'{"url":"abc"}') -> Record:
    """One message, as the broker would have handed it over."""
    return Record(
        sequence=sequence,
        received=at or START,
        subject="url-shortener.log",
        headers={"X-Detail-Type": "URLRequest"},
        body=body,
    )


class FakeS3:
    """Enough of a store to record what it was asked to hold.

    Five lines rather than a mock, which is what the one-method protocol in
    archive.py buys.
    """

    def __init__(self) -> None:
        """Start empty, and willing."""
        self.puts: list[dict[str, Any]] = []
        self.fail = False

    def put_object(self, **kwargs: Any) -> None:  # noqa: ANN401
        """Record the call, or refuse it."""
        if self.fail:
            raise RuntimeError("the store said no")
        self.puts.append(kwargs)


def test_a_record_becomes_one_json_line() -> None:
    line = record(7).as_line()
    assert "\n" not in line
    decoded = json.loads(line)
    assert decoded["sequence"] == 7
    assert decoded["subject"] == "url-shortener.log"
    assert decoded["detail"] == {"url": "abc"}


def test_the_headers_stay_beside_the_body_rather_than_inside_it() -> None:
    # The publisher puts the event's type in a header precisely so two kinds
    # of event on one subject stay apart. Folding it into the body would be
    # the same bug one layer down.
    decoded = json.loads(record(1).as_line())
    assert decoded["headers"]["X-Detail-Type"] == "URLRequest"
    assert "X-Detail-Type" not in decoded["detail"]


def test_a_body_that_is_not_json_is_kept_under_a_different_key() -> None:
    decoded = json.loads(record(1, body=b"\xff not json").as_line())
    assert "detail" not in decoded
    assert "not json" in decoded["raw"]


def test_ndjson_is_one_line_per_record_and_ends_with_a_newline() -> None:
    out = render([record(1), record(2), record(3)])
    assert out.endswith(b"\n")
    assert len(out.splitlines()) == 3


def test_the_key_is_the_first_sequence_so_a_retry_overwrites_its_own_attempt() -> None:
    # A batch is acknowledged only after its object is written, so a failed
    # write means the SAME records are redelivered. They must land on the
    # same key, or a retry leaves a second copy beside the first.
    first = key_for("p", [record(41), record(42), record(43)])
    retry = key_for("p", [record(41), record(42)])
    assert first == retry
    assert first == "p/2026/09/24/00000000000000000041.ndjson"


def test_the_prefix_is_honoured_because_a_bucket_is_usually_shared() -> None:
    assert key_for("shortener/requests", [record(1)]).startswith("shortener/requests/")
    assert key_for("/leading/and/trailing/", [record(1)]).startswith("leading/and/trailing/2026/")


def test_a_batch_is_due_on_count() -> None:
    writer = Writer(FakeS3(), "b", max_records=3, max_seconds=3600)
    for i in range(2):
        writer.add(record(i))
    assert not writer.due(START)
    writer.add(record(2))
    assert writer.due(START)


def test_a_batch_is_due_on_age_so_a_quiet_hour_is_still_archived() -> None:
    writer = Writer(FakeS3(), "b", max_records=1000, max_seconds=60)
    writer.add(record(1, at=START))
    assert not writer.due(START + timedelta(seconds=59))
    assert writer.due(START + timedelta(seconds=60))


def test_an_empty_batch_is_never_due_and_writes_nothing() -> None:
    s3 = FakeS3()
    writer = Writer(s3, "b", max_seconds=1)
    assert not writer.due(START + timedelta(days=1))
    assert writer.flush() is None
    assert s3.puts == []


def test_a_flush_writes_one_object_and_empties_the_batch() -> None:
    s3 = FakeS3()
    writer = Writer(s3, "archive", prefix="p")
    writer.add(record(5))
    writer.add(record(6))

    key = writer.flush()

    assert len(writer) == 0
    assert len(s3.puts) == 1
    put = s3.puts[0]
    assert put["Bucket"] == "archive"
    assert put["Key"] == key
    assert put["ContentType"] == "application/x-ndjson"
    assert len(put["Body"].splitlines()) == 2


def test_a_failed_write_keeps_the_batch_so_nothing_is_acknowledged_unstored() -> None:
    # This is the whole ordering rule. If a failure emptied the batch, the
    # caller would acknowledge records that were never stored, and the
    # broker would never send them again.
    s3 = FakeS3()
    s3.fail = True
    writer = Writer(s3, "b")
    writer.add(record(1))

    with pytest.raises(RuntimeError):
        writer.flush()

    assert len(writer) == 1


def test_capacity_shrinks_as_the_batch_fills_and_never_reaches_zero() -> None:
    # It is the size of the next fetch. Asking a broker for zero messages is
    # an error, and asking for more than the batch will take makes the
    # count limit meaningless.
    writer = Writer(FakeS3(), "b", max_records=3)
    assert writer.capacity == 3
    writer.add(record(1))
    assert writer.capacity == 2
    for i in range(5):
        writer.add(record(i))
    assert writer.capacity == 1
