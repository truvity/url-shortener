import { context, trace } from "@opentelemetry/api";
import {
  InMemorySpanExporter,
  NodeTracerProvider,
  SimpleSpanProcessor,
} from "@opentelemetry/sdk-trace-node";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";

import { tracing } from "./urls.ts";

const exporter = new InMemorySpanExporter();
const provider = new NodeTracerProvider({ spanProcessors: [new SimpleSpanProcessor(exporter)] });

beforeAll(() => provider.register());
afterAll(() => provider.shutdown());
beforeEach(() => exporter.reset());

function call(header: Headers, fail?: Error) {
  const req = {
    service: { typeName: "urlshortener.v1.UrlsService" },
    method: { name: "GetURL" },
    header,
  };
  const next = () => (fail ? Promise.reject(fail) : Promise.resolve({}));
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return tracing(next as any)(req as any);
}

describe("the outbound call", () => {
  it("puts the trace context on the wire, naming the client span as the parent", async () => {
    const tracer = trace.getTracer("test");
    const server = tracer.startSpan("GET /api/url");
    const header = new Headers();

    await context.with(trace.setSpan(context.active(), server), () => call(header));
    server.end();

    const client = exporter.getFinishedSpans().find((s) => s.kind === 2)!;
    expect(client.name).toBe("urlshortener.v1.UrlsService/GetURL");
    expect(client.parentSpanContext?.spanId).toBe(server.spanContext().spanId);
    // The callee reads THIS header to become the client's child; a wrong
    // span id here would attach it to the request instead.
    expect(header.get("traceparent")).toContain(
      `${client.spanContext().traceId}-${client.spanContext().spanId}`,
    );
  });

  it("marks the span failed and lets the error through", async () => {
    await expect(call(new Headers(), new Error("refused"))).rejects.toThrow("refused");

    const client = exporter.getFinishedSpans()[0]!;
    expect(client.status.code).toBe(2);
    expect(client.events.map((e) => e.name)).toContain("exception");
  });
});
