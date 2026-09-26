"""Start the OpenTelemetry SDK from OpenTelemetry's own environment.

There is deliberately nothing to configure here. Decision 0006: the
specification defines the variables, every language's SDK reads them, and
a second vocabulary in a configuration file would be a ceiling on what an
operator can set and a precedence question at three in the morning.

NO ENDPOINT MEANS EXPORT NOTHING. ``OTEL_TRACES_EXPORTER=none`` is a value
the specification defines, and a deployment with no endpoint sets it — so
a laptop, a test and a cluster with no collector all do nothing, without
this module or its callers carrying an enable flag. That flag is the
failure decision 0006 records: a service that exported to a console in
production because nothing set the environment name its code tested.

The exporters read ``OTEL_EXPORTER_OTLP_*`` themselves, so nothing here
parses an endpoint or a protocol.
"""

from __future__ import annotations

import os
from typing import TYPE_CHECKING, Any, Protocol

if TYPE_CHECKING:
    from collections.abc import Iterable, MutableMapping

    from opentelemetry.sdk.trace.export import SpanExporter

    # A processor's third argument, spelled out rather than imported from
    # structlog: that package is the ARCHIVER's dependency, not this
    # module's, and importing its types here would make a consumer who
    # never took structlog carry it just to type-check.
    EventDict = MutableMapping[str, Any]

# The names the OpenTelemetry specification recommends for trace context in
# a log format that is not OTLP: lower-case hex, the W3C forms. Every
# language in this repository uses exactly these two names.
_FIELD_TRACE_ID = "trace_id"
_FIELD_SPAN_ID = "span_id"

# The span attribute keys exported by default: OpenTelemetry semantic-
# convention keys that describe a call's shape rather than its content. An
# attribute nobody thought about is ABSENT, not exported because some
# instrumentation library happened to add it -- so this list is short on
# purpose and never grows to accommodate one caller. A caller with more of
# its own passes `extra_attributes` to `start`.
#
# Deliberately NOT here: url.path, url.query, url.full (the request line
# itself -- ids, search terms, tokens), any header, any database or
# messaging PAYLOAD, and any peer address. Those are exactly the attributes
# an instrumentation library adds on its own, which is why this is an ALLOW
# list rather than a set of things to strip.
DEFAULT_ALLOWED_ATTRIBUTES: frozenset[str] = frozenset(
    {
        "http.request.method",
        "http.route",
        "http.response.status_code",
        "rpc.system",
        "rpc.service",
        "rpc.method",
        "rpc.grpc.status_code",
        "rpc.connect_rpc.error_code",
        "db.system",
        "db.system.name",
        "db.operation",
        "db.operation.name",
        "db.query.text",
        "db.response.returned_rows",
        "messaging.system",
        "messaging.destination.name",
        "messaging.operation",
        "messaging.operation.type",
        "server.port",
        "error.type",
        "otel.status_code",
        "otel.status_description",
    },
)


def _filtering_span_exporter(exporter: SpanExporter, allowed: frozenset[str]) -> SpanExporter:
    """Wrap a span exporter so no attribute outside ``allowed`` reaches it.

    The SDK hands a processor's ``on_end`` a :class:`ReadableSpan` -- already
    a read-only snapshot -- so there is no processor hook that lets code
    remove an attribute after instrumentation added it. This wraps the
    EXPORTER instead: the last point before spans leave the process where
    the shape is still ours to change, by building a new ``ReadableSpan``
    that copies everything but the attributes.
    """
    from opentelemetry.sdk.trace import ReadableSpan  # noqa: PLC0415
    from opentelemetry.sdk.trace.export import SpanExporter as _SpanExporter  # noqa: PLC0415

    class FilteringSpanExporter(_SpanExporter):
        def export(self, spans: Any) -> Any:  # noqa: ANN401 — matches the SDK's own untyped signature
            kept = [
                ReadableSpan(
                    name=span.name,
                    context=span.context,
                    parent=span.parent,
                    resource=span.resource,
                    attributes={
                        key: value
                        for key, value in (span.attributes or {}).items()
                        if key in allowed
                    },
                    events=span.events,
                    links=span.links,
                    kind=span.kind,
                    status=span.status,
                    start_time=span.start_time,
                    end_time=span.end_time,
                    instrumentation_scope=span.instrumentation_scope,
                )
                for span in spans
            ]
            return exporter.export(kept)

        def shutdown(self) -> None:
            exporter.shutdown()

        def force_flush(self, timeout_millis: int = 30000) -> bool:
            return exporter.force_flush(timeout_millis)

    return FilteringSpanExporter()


class Shutdown(Protocol):
    """Flush what is buffered and release the exporters."""

    def __call__(self) -> None:
        """Flush and release. Safe to call once."""


def _disabled(variable: str) -> bool:
    # Absent means the SDK's own default, which is to export. Only an
    # explicit `none` turns a signal off, and that is what the chart sets
    # where no endpoint is configured.
    return os.environ.get(variable, "").strip().lower() == "none"


def start(*, extra_attributes: Iterable[str] = ()) -> Shutdown:
    """Install the global tracer and meter providers.

    Returns a callable that flushes them. A caller that skips it loses
    whatever had not been sent, which on a short-lived process is usually
    everything.

    Every exported span passes through an allow-list first: an attribute
    nobody thought about is ABSENT, not exported because an instrumentation
    library happened to add it. ``DEFAULT_ALLOWED_ATTRIBUTES`` covers the
    semantic-convention keys that carry no request data; ``extra_attributes``
    extends it for keys this service's own code adds. There is no
    configuration key or environment variable for this -- decision 0006 is
    about the SDK's own environment, not this list.
    """
    allowed = DEFAULT_ALLOWED_ATTRIBUTES | frozenset(extra_attributes)
    shutdowns: list[Shutdown] = []

    if not _disabled("OTEL_TRACES_EXPORTER"):
        # Imported HERE, not at the top of the file. Telemetry is an
        # optional extra, so a consumer that installed the loader alone
        # must still be able to import this module -- and a deployment
        # that exports nothing should not pay to load an SDK it will not
        # use. noqa is the honest answer rather than a lint exception
        # nobody can explain later.
        from opentelemetry import trace  # noqa: PLC0415
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import (  # noqa: PLC0415
            OTLPSpanExporter,
        )
        from opentelemetry.sdk.trace import TracerProvider  # noqa: PLC0415
        from opentelemetry.sdk.trace.export import BatchSpanProcessor  # noqa: PLC0415

        tracer_provider = TracerProvider()
        exporter = _filtering_span_exporter(OTLPSpanExporter(), allowed)
        tracer_provider.add_span_processor(BatchSpanProcessor(exporter))
        trace.set_tracer_provider(tracer_provider)
        shutdowns.append(tracer_provider.shutdown)

    if not _disabled("OTEL_METRICS_EXPORTER"):
        from opentelemetry import metrics  # noqa: PLC0415
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import (  # noqa: PLC0415
            OTLPMetricExporter,
        )
        from opentelemetry.sdk.metrics import MeterProvider  # noqa: PLC0415
        from opentelemetry.sdk.metrics.export import (  # noqa: PLC0415
            PeriodicExportingMetricReader,
        )

        reader = PeriodicExportingMetricReader(OTLPMetricExporter())
        meter_provider = MeterProvider(metric_readers=[reader])
        metrics.set_meter_provider(meter_provider)
        shutdowns.append(meter_provider.shutdown)

    # W3C propagation is the SDK's default, so a trace survives a hop
    # without being asked for. Logs are not exported: stdout is collected
    # already, and a log that exists only over OTLP vanishes exactly when
    # the exporter is what broke.

    def shutdown() -> None:
        for each in shutdowns:
            each()

    return shutdown


def trace_context(_logger: object, _method_name: str, event_dict: EventDict) -> EventDict:
    """Add trace_id and span_id for the current span, as a structlog processor.

    Imported here, not at module load, for the same reason `start` delays
    its own import: telemetry is an optional extra, and a consumer that
    took only the loader must still be able to wire this processor into a
    logger without carrying the SDK — it degrades to a no-op rather than
    an import error, whether or not `start` was ever called.

    Absent, not empty: when no span is current, or the SDK was never
    installed, neither key is added at all.
    """
    try:
        from opentelemetry import trace  # noqa: PLC0415
    except ImportError:
        return event_dict

    span_context = trace.get_current_span().get_span_context()
    if not span_context.is_valid:
        return event_dict

    event_dict[_FIELD_TRACE_ID] = format(span_context.trace_id, "032x")
    event_dict[_FIELD_SPAN_ID] = format(span_context.span_id, "016x")
    return event_dict
