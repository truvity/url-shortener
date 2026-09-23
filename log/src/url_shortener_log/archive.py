"""Turn messages into objects: batch them, render NDJSON, write one object.

The only interesting decisions in here are the key and the ordering, and
both are about what happens when a write fails.
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any, Protocol

import boto3
from botocore.config import Config as BotoConfig

if TYPE_CHECKING:
    from mypy_boto3_s3.client import S3Client

    from .config import Bucket


class ObjectStore(Protocol):
    """The one call this component makes against a store.

    One method, deliberately. A narrow interface is not tidiness: it is what
    lets a test substitute five lines instead of a mock, and what stops a
    caller reaching for something — a delete, a lifecycle rule, a bucket
    creation — that belongs to whoever provisioned the store. The vendor's
    own client satisfies this without knowing about it.
    """

    # The parameter names are the S3 API's, not ours: a protocol that renamed
    # them would describe a client nobody has.
    def put_object(
        self,
        *,
        Bucket: str,  # noqa: N803
        Key: str,  # noqa: N803
        Body: bytes,  # noqa: N803
        ContentType: str,  # noqa: N803
    ) -> object:
        """Store one object."""
        ...


DEFAULT_PREFIX = "url-shortener/requests"
DEFAULT_MAX_RECORDS = 500
DEFAULT_MAX_SECONDS = 60


@dataclass(frozen=True, slots=True)
class Record:
    """One message, as it will be written.

    The headers are kept beside the body rather than merged into it. The
    publisher puts the event's TYPE in a header precisely so that two kinds
    of event on one subject stay distinguishable, and an archive that folded
    the header into the body would be the same bug one layer down.
    """

    sequence: int
    received: datetime
    subject: str
    headers: dict[str, str]
    body: bytes

    def as_line(self) -> str:
        """Render one NDJSON line."""
        detail: Any
        try:
            detail = json.loads(self.body)
            key = "detail"
        except ValueError, UnicodeDecodeError:
            # An archive does not get to drop what it cannot parse. A body
            # that is not JSON is recorded as text under a DIFFERENT key, so
            # a reader can tell "this event had no detail" from "this event
            # was not JSON" without guessing.
            detail = self.body.decode("utf-8", errors="replace")
            key = "raw"
        return json.dumps(
            {
                "sequence": self.sequence,
                "time": self.received.isoformat(),
                "subject": self.subject,
                "headers": self.headers,
                key: detail,
            },
            separators=(",", ":"),
            sort_keys=True,
        )


def render(records: list[Record]) -> bytes:
    """Render a batch as NDJSON: one record per line, newline-terminated."""
    return ("".join(record.as_line() + "\n" for record in records)).encode()


def key_for(prefix: str, records: list[Record]) -> str:
    """Name the object this batch becomes.

    Keyed on the FIRST stream sequence in the batch, and nothing else that
    varies. A batch is acknowledged only after its object is written, so a
    failed write means the whole batch is redelivered — and a redelivery
    begins at the same sequence and produces the same key, overwriting the
    partial attempt instead of leaving a second copy beside it. Putting the
    count or the wall clock in the key would lose that.

    The date path is the reader's convenience: an archive is read by
    prefix, and a day is the prefix people ask for.
    """
    first = records[0]
    day = first.received.strftime("%Y/%m/%d")
    return f"{prefix.strip('/')}/{day}/{first.sequence:020d}.ndjson"


def client(bucket: Bucket) -> S3Client:
    """Build the S3 client this configuration describes.

    The vendor is not in here, and that is the platform contract's rule about
    stores: an endpoint, a region and a path-style flag reach a cloud
    service, an in-cluster store or a test double, and nothing in this file
    knows which it got.
    """
    kwargs: dict[str, Any] = {
        "config": BotoConfig(
            s3={"addressing_style": "path" if bucket.get("pathStyle") else "auto"},
            retries={"mode": "standard", "max_attempts": 3},
        ),
    }
    if region := bucket.get("region"):
        kwargs["region_name"] = region
    if endpoint := bucket.get("endpoint"):
        kwargs["endpoint_url"] = endpoint
    if ca := bucket.get("ca"):
        # A store inside somebody's own network is the ordinary case, not the
        # exotic one, so a bundle the platform mounts is a supported input
        # rather than a reason to disable verification.
        kwargs["verify"] = ca
    if names := bucket.get("credentialsEnv"):
        # The configuration carries the NAMES. Reading them here, at the one
        # point they are needed, keeps the values out of everything that
        # renders, logs or commits the configuration.
        kwargs["aws_access_key_id"] = _secret(names["accessKeyID"])
        kwargs["aws_secret_access_key"] = _secret(names["secretAccessKey"])
    return boto3.client("s3", **kwargs)


def _secret(name: str) -> str:
    """Read a variable the configuration named, or say which one is missing."""
    value = os.environ.get(name)
    if not value:
        raise ValueError(f"environment variable {name} is not set")
    return value


class Writer:
    """Holds a batch, decides when it is finished, and writes it."""

    def __init__(
        self,
        s3: ObjectStore,
        bucket: str,
        prefix: str = DEFAULT_PREFIX,
        max_records: int = DEFAULT_MAX_RECORDS,
        max_seconds: int = DEFAULT_MAX_SECONDS,
    ) -> None:
        """Take the client and the limits. Both limits apply; either closes."""
        self._s3 = s3
        self._bucket = bucket
        self._prefix = prefix
        self._max_records = max_records
        self._max_seconds = max_seconds
        self._batch: list[Record] = []

    def __len__(self) -> int:
        """How many records are held but not yet written."""
        return len(self._batch)

    @property
    def capacity(self) -> int:
        """How many more records this batch will take before it is due."""
        return max(1, self._max_records - len(self._batch))

    def add(self, record: Record) -> None:
        """Hold a record for the next write."""
        self._batch.append(record)

    def due(self, now: datetime) -> bool:
        """Whether the held batch should be written now.

        Both limits, because either alone has a failure nobody notices: a
        size-only rule never archives a quiet hour, and a time-only rule
        archives a busy one in objects too small to be worth reading.
        """
        if not self._batch:
            return False
        if len(self._batch) >= self._max_records:
            return True
        return (now - self._batch[0].received).total_seconds() >= self._max_seconds

    def flush(self) -> str | None:
        """Write the held batch as one object and return its key.

        Returns None when there was nothing to write. Raises if the write
        failed, and the batch is KEPT — the caller acknowledges nothing it
        did not manage to store.
        """
        if not self._batch:
            return None
        key = key_for(self._prefix, self._batch)
        self._s3.put_object(
            Bucket=self._bucket,
            Key=key,
            Body=render(self._batch),
            ContentType="application/x-ndjson",
        )
        self._batch = []
        return key


def now() -> datetime:
    """Read the clock, in one place, so a test can hold it still."""
    return datetime.now(UTC)
