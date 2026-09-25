"""Keep a trace whole across the broker and the batch.

Two facts shape this. The publisher puts the trace context in the MESSAGE,
and nothing else carries it across a broker. And this component does not
handle one message at a time: it holds hundreds and writes one object, so
the write cannot be the child of any one of them.

The answer is one short span per message, a child of its publisher, so the
request that caused an event shows the archive receiving it; and one span
for the write, LINKED to those, so the write is reachable from every trace
it served without pretending to belong to just one.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

from opentelemetry import propagate, trace
from opentelemetry.trace import Link, Span, SpanContext, SpanKind, StatusCode, Tracer

if TYPE_CHECKING:
    from collections.abc import Iterable, Mapping

# A span holds 128 links by default and drops the rest silently. Stating the
# number here keeps a full batch from looking like a complete one.
MAX_LINKS = 128


def receive(tracer: Tracer, subject: str, headers: Mapping[str, str] | None) -> Span:
    """Start the span for one received message, continuing its publisher's trace.

    The header names are lower-cased first: NATS header names are
    case-sensitive and the W3C ones are specified in lower case, but a
    publisher that went through an HTTP-style carrier would have written
    `Traceparent`, and a lookup that missed it would silently start a trace.
    """
    carrier = {name.lower(): value for name, value in (headers or {}).items()}
    return tracer.start_span(
        f"{subject} receive",
        context=propagate.extract(carrier),
        kind=SpanKind.CONSUMER,
        attributes={
            "messaging.system": "nats",
            "messaging.destination.name": subject,
            "messaging.operation.type": "receive",
        },
    )


def links(spans: Iterable[Span]) -> list[Link]:
    """Link a write to each received message it carries, up to the cap."""
    found = [Link(span.get_span_context()) for span in spans]
    return found[:MAX_LINKS]


def record_write(  # noqa: PLR0913 — one call per message, and each argument is a different fact
    tracer: Tracer,
    message: Span,
    flush: SpanContext,
    start_ns: int,
    end_ns: int,
    *,
    key: str | None,
    error: str | None = None,
) -> None:
    """Show the write inside the trace of a message it carried.

    The write itself — and the object-store call beneath it — is ONE span in
    ONE trace, because a batch of hundreds cannot be the child of any single
    request. Left at that, a request's trace ends at "received" and the
    question "did my event reach the archive, and how long did that take?"
    has to be answered by finding a different trace.

    So each message also gets a short child, timed to the write and LINKED to
    the real one: the request's trace shows the write and its duration, and
    the link is the way into the flush and the store call. It costs a span
    per sampled message, which is what the per-message span already costs.
    """
    child = tracer.start_span(
        "archive.write",
        context=trace.set_span_in_context(message),
        kind=SpanKind.PRODUCER,
        start_time=start_ns,
        links=[Link(flush)],
        attributes={"archive.key": key} if key else None,
    )
    if error is not None:
        child.set_status(StatusCode.ERROR, error)
    child.end(end_time=end_ns)
