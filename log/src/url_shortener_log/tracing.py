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

from opentelemetry import propagate
from opentelemetry.trace import Link, Span, SpanKind, Tracer

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
