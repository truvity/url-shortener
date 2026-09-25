"""log <-> trace correlation: trace_id/span_id for the current span, or nothing."""

from __future__ import annotations

from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider

from truvity_policy.telemetry import trace_context


def test_a_record_inside_a_span_carries_its_ids() -> None:
    provider = TracerProvider()
    tracer = provider.get_tracer("test")

    with tracer.start_as_current_span("request") as span:
        event = trace_context(None, "info", {"event": "handled"})

    span_context = span.get_span_context()
    assert event["trace_id"] == format(span_context.trace_id, "032x")
    assert event["span_id"] == format(span_context.span_id, "016x")


def test_a_record_outside_any_span_carries_neither_field() -> None:
    # No span started at all, in a fresh (default, never-configured) tracer
    # provider -- the ordinary state for a process that never called
    # telemetry.start().
    event = trace_context(None, "info", {"event": "handled"})

    assert "trace_id" not in event
    assert "span_id" not in event


def test_a_record_between_two_spans_carries_neither_field() -> None:
    provider = TracerProvider()
    tracer = provider.get_tracer("test")

    with tracer.start_as_current_span("first"):
        pass

    event = trace_context(None, "info", {"event": "handled"})

    assert "trace_id" not in event
    assert "span_id" not in event
    # Sanity: the span really did end, rather than this asserting something
    # trivially true because no provider was ever installed.
    assert trace.get_current_span().get_span_context().is_valid is False
