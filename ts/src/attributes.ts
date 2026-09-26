/**
 * The span-attribute allow-list. An attribute nobody thought about is
 * ABSENT, not exported because some instrumentation library happened to add
 * it — so this is an ALLOW list, not a set of things to strip, and it stays
 * short.
 *
 * The JS SDK hands a {@link https://github.com/open-telemetry/opentelemetry-js/blob/main/packages/opentelemetry-sdk-trace-base/src/SpanProcessor.ts | SpanProcessor}'s
 * `onEnd` a `ReadableSpan` that is already the read-only shape about to be
 * exported — there is no processor hook that lets code remove an attribute
 * instrumentation added earlier in the span's life. So the filter wraps the
 * EXPORTER instead: the last point before spans leave the process where the
 * shape is still ours to change.
 */

// Type-only, so it is erased before this ever runs: the package is an
// OPTIONAL peer, and a build that exports nothing must not need it.
import type { ReadableSpan, SpanExporter } from "@opentelemetry/sdk-trace-base";

/**
 * OpenTelemetry semantic-convention keys that describe a call's shape
 * rather than its content. Never grows to accommodate one caller — a
 * caller with more of its own passes `extraAttributes` to `start`.
 *
 * Deliberately NOT here: url.path, url.query, url.full (the request line
 * itself — ids, search terms, tokens), any header, any database or
 * messaging PAYLOAD, and any peer address. Those are exactly the
 * attributes an instrumentation library adds on its own.
 */
export const DEFAULT_ALLOWED_ATTRIBUTES: ReadonlySet<string> = new Set([
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
]);

/**
 * A `ReadableSpan` with its attributes replaced, everything else read
 * through to the original. A `Proxy` rather than an object spread: the
 * SDK's own span implementation exposes several of these members (notably
 * `spanContext()`) as prototype methods reading private fields, and a
 * spread copies only OWN enumerable properties — it silently drops them.
 */
function withFilteredAttributes(span: ReadableSpan, allowed: ReadonlySet<string>): ReadableSpan {
  const attributes = Object.fromEntries(
    Object.entries(span.attributes).filter(([key]) => allowed.has(key)),
  );

  return new Proxy(span, {
    get(target, prop, _receiver) {
      if (prop === "attributes") return attributes;
      const value = Reflect.get(target, prop, target) as unknown;
      // Methods (spanContext()) must run with the REAL span as `this`,
      // never the proxy: they read private fields the proxy does not have.
      return typeof value === "function" ? value.bind(target) : value;
    },
  }) as ReadableSpan;
}

/**
 * Wrap a span exporter so no attribute outside `allowed` reaches it.
 */
export function filteringSpanExporter(exporter: SpanExporter, allowed: ReadonlySet<string>): SpanExporter {
  const wrapped: SpanExporter = {
    export(spans, resultCallback) {
      exporter.export(
        spans.map((span) => withFilteredAttributes(span, allowed)),
        resultCallback,
      );
    },
    shutdown: () => exporter.shutdown(),
  };

  if (exporter.forceFlush) {
    wrapped.forceFlush = () => exporter.forceFlush!();
  }

  return wrapped;
}
