"""The span-attribute allow-list: an attribute nobody listed is ABSENT."""

from __future__ import annotations

from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from truvity_policy.telemetry import DEFAULT_ALLOWED_ATTRIBUTES, _filtering_span_exporter


def _export(allowed: frozenset[str], attributes: dict[str, str]) -> dict[str, object]:
    inner = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(_filtering_span_exporter(inner, allowed)))

    tracer = provider.get_tracer("test")
    with tracer.start_as_current_span("span", attributes=attributes):
        pass

    (span,) = inner.get_finished_spans()
    return dict(span.attributes or {})


def test_an_unlisted_attribute_is_absent_from_what_exports() -> None:
    got = _export(DEFAULT_ALLOWED_ATTRIBUTES, {"url.path": "/users/42"})

    assert "url.path" not in got


def test_a_default_allowed_attribute_survives() -> None:
    got = _export(DEFAULT_ALLOWED_ATTRIBUTES, {"http.route": "/users/:id"})

    assert got["http.route"] == "/users/:id"


def test_a_caller_extension_survives() -> None:
    allowed = DEFAULT_ALLOWED_ATTRIBUTES | frozenset({"archive.key"})
    got = _export(allowed, {"archive.key": "2026/09/26/00001.ndjson", "url.path": "/no"})

    assert got["archive.key"] == "2026/09/26/00001.ndjson"
    assert "url.path" not in got


def test_extending_the_list_never_removes_a_default() -> None:
    allowed = DEFAULT_ALLOWED_ATTRIBUTES | frozenset({"archive.key"})
    got = _export(allowed, {"http.route": "/users/:id", "archive.key": "k"})

    assert got["http.route"] == "/users/:id"
    assert got["archive.key"] == "k"
