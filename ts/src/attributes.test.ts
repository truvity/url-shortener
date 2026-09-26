import {
  BasicTracerProvider,
  InMemorySpanExporter,
  SimpleSpanProcessor,
} from "@opentelemetry/sdk-trace-node";
import { describe, expect, it } from "vitest";
import { DEFAULT_ALLOWED_ATTRIBUTES, filteringSpanExporter } from "./attributes.js";

async function exportOneSpan(
  allowed: ReadonlySet<string>,
  attributes: Record<string, string>,
): Promise<Record<string, unknown>> {
  const inner = new InMemorySpanExporter();
  const provider = new BasicTracerProvider({
    spanProcessors: [new SimpleSpanProcessor(filteringSpanExporter(inner, allowed))],
  });

  const tracer = provider.getTracer("test");
  const span = tracer.startSpan("span");
  span.setAttributes(attributes);
  span.end();
  // forceFlush BEFORE shutdown: InMemorySpanExporter.shutdown() clears
  // whatever it holds, so reading after shutdown always sees nothing.
  await provider.forceFlush();

  const [exported] = inner.getFinishedSpans();
  if (!exported) throw new Error("no span exported");
  const attrs = exported.attributes;
  await provider.shutdown();
  return attrs;
}

describe("the span-attribute allow-list", () => {
  it("drops an attribute nobody listed", async () => {
    const got = await exportOneSpan(DEFAULT_ALLOWED_ATTRIBUTES, { "url.path": "/users/42" });

    expect(got["url.path"]).toBeUndefined();
  });

  it("keeps a default allowed attribute", async () => {
    const got = await exportOneSpan(DEFAULT_ALLOWED_ATTRIBUTES, { "http.route": "/users/:id" });

    expect(got["http.route"]).toBe("/users/:id");
  });

  it("keeps a caller's extension, without giving up a default", async () => {
    const allowed = new Set([...DEFAULT_ALLOWED_ATTRIBUTES, "archive.key"]);

    const got = await exportOneSpan(allowed, {
      "http.route": "/users/:id",
      "archive.key": "2026/09/26/00001.ndjson",
      "url.path": "/should/not/survive",
    });

    expect(got["http.route"]).toBe("/users/:id");
    expect(got["archive.key"]).toBe("2026/09/26/00001.ndjson");
    expect(got["url.path"]).toBeUndefined();
  });

  it("still lets the underlying exporter read the span through spanContext()", async () => {
    // spanContext() is a PROTOTYPE method reading private fields -- the
    // regression an object-spread wrapper would introduce silently, by
    // dropping it rather than throwing.
    const inner = new InMemorySpanExporter();
    const provider = new BasicTracerProvider({
      spanProcessors: [new SimpleSpanProcessor(filteringSpanExporter(inner, DEFAULT_ALLOWED_ATTRIBUTES))],
    });
    const tracer = provider.getTracer("test");
    const span = tracer.startSpan("span");
    const traceId = span.spanContext().traceId;
    span.end();
    await provider.forceFlush();

    const [exported] = inner.getFinishedSpans();
    if (!exported) throw new Error("no span exported");
    expect(exported.spanContext().traceId).toBe(traceId);
    await provider.shutdown();
  });
});
