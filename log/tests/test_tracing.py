"""The archive's spans keep the trace whole across the broker and the batch."""

from __future__ import annotations

import boto3
from botocore.stub import Stubber
from opentelemetry import propagate, trace
from opentelemetry.instrumentation.botocore import BotocoreInstrumentor
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import SpanKind, StatusCode
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from url_shortener_log import tracing


def _provider() -> tuple[TracerProvider, InMemorySpanExporter]:
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    return provider, exporter


def _publisher_headers(provider: TracerProvider, header_name: str) -> tuple[dict[str, str], int]:
    span = provider.get_tracer("publisher").start_span("redirect")
    carrier: dict[str, str] = {}
    with trace.use_span(span):
        TraceContextTextMapPropagator().inject(carrier)
    span.end()
    return {header_name: carrier["traceparent"]}, span.get_span_context().trace_id


def test_a_received_message_is_a_child_of_its_publisher() -> None:
    provider, exporter = _provider()
    propagate.set_global_textmap(TraceContextTextMapPropagator())
    headers, trace_id = _publisher_headers(provider, "traceparent")

    span = tracing.receive(provider.get_tracer("log"), "events.redirect", headers)
    span.end()

    done = [s for s in exporter.get_finished_spans() if s.kind == SpanKind.CONSUMER]
    assert len(done) == 1
    assert done[0].context.trace_id == trace_id
    assert done[0].parent is not None
    assert done[0].name == "events.redirect receive"


def test_a_publisher_that_capitalised_the_header_is_still_found() -> None:
    provider, exporter = _provider()
    propagate.set_global_textmap(TraceContextTextMapPropagator())
    headers, trace_id = _publisher_headers(provider, "Traceparent")

    tracing.receive(provider.get_tracer("log"), "events.redirect", headers).end()

    (span,) = [s for s in exporter.get_finished_spans() if s.kind == SpanKind.CONSUMER]
    assert span.context.trace_id == trace_id


def test_a_message_without_context_still_gets_a_span() -> None:
    provider, exporter = _provider()
    propagate.set_global_textmap(TraceContextTextMapPropagator())

    tracing.receive(provider.get_tracer("log"), "events.redirect", None).end()

    assert len(exporter.get_finished_spans()) == 1


def test_a_write_links_every_message_it_carries_up_to_the_cap() -> None:
    provider, _ = _provider()
    tracer = provider.get_tracer("log")
    spans = [tracer.start_span(f"m{i}") for i in range(tracing.MAX_LINKS + 5)]

    links = tracing.links(spans)

    assert len(links) == tracing.MAX_LINKS
    assert links[0].context.span_id == spans[0].get_span_context().span_id


def test_an_s3_write_is_a_span_under_the_flush() -> None:
    provider, exporter = _provider()
    instrumentor = BotocoreInstrumentor()  # type: ignore[no-untyped-call]
    instrumentor.instrument(tracer_provider=provider)
    try:
        s3 = boto3.client(
            "s3",
            region_name="us-east-1",
            aws_access_key_id="x",
            aws_secret_access_key="x",
        )
        with Stubber(s3) as stub:
            stub.add_response("put_object", {})
            with provider.get_tracer("log").start_as_current_span("archive.flush") as flush:
                s3.put_object(Bucket="b", Key="k", Body=b"x")
    finally:
        instrumentor.uninstrument()

    (put,) = [s for s in exporter.get_finished_spans() if s.kind == SpanKind.CLIENT]
    assert put.parent is not None
    assert put.parent.span_id == flush.get_span_context().span_id
    assert put.attributes is not None
    assert put.attributes.get("rpc.method") == "PutObject"


def test_a_messages_trace_shows_the_write_and_links_to_the_real_one() -> None:
    provider, exporter = _provider()
    propagate.set_global_textmap(TraceContextTextMapPropagator())
    tracer = provider.get_tracer("log")
    headers, trace_id = _publisher_headers(provider, "traceparent")
    message = tracing.receive(tracer, "events.redirect", headers)
    flush = tracer.start_span("archive.flush")

    tracing.record_write(tracer, message, flush.get_span_context(), 1_000, 5_000, key="a/b.ndjson")
    message.end()
    flush.end()

    (write,) = [s for s in exporter.get_finished_spans() if s.name == "archive.write"]
    # In the REQUEST's trace, under the message, timed to the write...
    assert write.context.trace_id == trace_id
    assert write.parent is not None
    assert write.parent.span_id == message.get_span_context().span_id
    assert (write.start_time, write.end_time) == (1_000, 5_000)
    # ...and a way into the real one, which is in a different trace.
    assert [link.context.span_id for link in write.links] == [flush.get_span_context().span_id]
    assert write.attributes is not None
    assert write.attributes["archive.key"] == "a/b.ndjson"


def test_a_failed_write_is_an_error_in_the_messages_trace() -> None:
    provider, exporter = _provider()
    propagate.set_global_textmap(TraceContextTextMapPropagator())
    tracer = provider.get_tracer("log")
    message = tracing.receive(tracer, "events.redirect", None)

    tracing.record_write(
        tracer, message, message.get_span_context(), 1, 2, key=None, error="denied"
    )

    (write,) = [s for s in exporter.get_finished_spans() if s.name == "archive.write"]
    assert write.status.status_code == StatusCode.ERROR
