/**
 * Start the OpenTelemetry SDK from OpenTelemetry's own environment.
 *
 * There is deliberately nothing to configure here. Decision 0006: the
 * specification defines the variables, every language's SDK reads them,
 * and a second vocabulary in a configuration file would be a ceiling on
 * what an operator can set and a precedence question at three in the
 * morning.
 *
 * NO ENDPOINT MEANS EXPORT NOTHING. `OTEL_TRACES_EXPORTER=none` is a
 * value the specification defines, and a deployment with no endpoint
 * sets it — so a laptop, a test and a cluster with no collector all do
 * nothing, without this module or its callers carrying an enable flag.
 *
 * That flag is the failure decision 0006 records, and it happened in a
 * Node service: it exported only when an environment name matched
 * `production`, nothing set that variable in any deployment, and so it
 * ran with a console exporter — writing spans onto the same stream as
 * its logs — in every environment including the one it was written for.
 *
 * The exporters read `OTEL_EXPORTER_OTLP_*` themselves, so nothing here
 * parses an endpoint or a protocol.
 */

// Type-only, so it is erased before this ever runs: the package is an
// OPTIONAL peer, and a build that exports nothing must not need it.
import type { Resource } from "@opentelemetry/resources";

/** Flush what is buffered and release the exporters. */
export type Shutdown = () => Promise<void>;

/**
 * `none` is the only value that turns a signal off. Absent means the
 * SDK's own default, which is to export.
 */
function disabled(variable: string): boolean {
  return (process.env[variable] ?? "").trim().toLowerCase() === "none";
}

/**
 * The resource this process reports itself as, from OpenTelemetry's own
 * environment: OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES.
 *
 * A provider built with no resource does NOT read them. The Node SDK's
 * default names the service `unknown_service:node`, and the variable a
 * platform sets to say who this is is ignored without a word -- the spans
 * arrive, the store accepts them, every dashboard reports the pipeline
 * healthy, and the service is filed under a name nobody set and nobody
 * searches for. Found exactly that way: a service missing from the store's
 * list of services, and a stranger in it.
 *
 * Exported so the behaviour can be asserted without standing up an
 * exporter.
 */
export async function resourceFromEnvironment(): Promise<Resource> {
  const { detectResources, envDetector } = await import("@opentelemetry/resources");
  return detectResources({ detectors: [envDetector] });
}

/**
 * Install the global tracer and meter providers.
 *
 * Returns a function that flushes them. A caller that skips it loses
 * whatever had not been sent, which on a short-lived process is usually
 * everything.
 *
 * The SDK is imported INSIDE the branch, so a build that exports nothing
 * does not carry the exporter's cost at start-up — and a bundle that is
 * never going to export does not have to resolve it at all.
 */
export async function start(): Promise<Shutdown> {
  const stops: Shutdown[] = [];

  if (!disabled("OTEL_TRACES_EXPORTER")) {
    const { NodeTracerProvider, BatchSpanProcessor } = await import("@opentelemetry/sdk-trace-node");
    const { OTLPTraceExporter } = await import("@opentelemetry/exporter-trace-otlp-http");

    const provider = new NodeTracerProvider({
      resource: await resourceFromEnvironment(),
      spanProcessors: [new BatchSpanProcessor(new OTLPTraceExporter())],
    });
    provider.register();
    stops.push(() => provider.shutdown());
  }

  if (!disabled("OTEL_METRICS_EXPORTER")) {
    const { MeterProvider, PeriodicExportingMetricReader } = await import("@opentelemetry/sdk-metrics");
    const { OTLPMetricExporter } = await import("@opentelemetry/exporter-metrics-otlp-http");
    const { metrics } = await import("@opentelemetry/api");

    const provider = new MeterProvider({
      resource: await resourceFromEnvironment(),
      readers: [new PeriodicExportingMetricReader({ exporter: new OTLPMetricExporter() })],
    });
    metrics.setGlobalMeterProvider(provider);
    stops.push(() => provider.shutdown());
  }

  // Logs are not exported: stdout is collected already, and a log that
  // exists only over OTLP vanishes exactly when the exporter is what
  // broke.

  return async () => {
    await Promise.all(stops.map((stop) => stop()));
  };
}
