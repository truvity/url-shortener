import { context, trace } from "@opentelemetry/api";
import {
  InMemorySpanExporter,
  NodeTracerProvider,
  SimpleSpanProcessor,
} from "@opentelemetry/sdk-trace-node";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";

import { spanCorrelation } from "./main.ts";

const exporter = new InMemorySpanExporter();
const provider = new NodeTracerProvider({ spanProcessors: [new SimpleSpanProcessor(exporter)] });

beforeAll(() => provider.register());
afterAll(() => provider.shutdown());
beforeEach(() => exporter.reset());

describe("spanCorrelation", () => {
  it("carries the current span's ids while a span is active", () => {
    const tracer = trace.getTracer("test");
    const span = tracer.startSpan("request");

    const correlation = context.with(trace.setSpan(context.active(), span), spanCorrelation);
    span.end();

    expect(correlation).toEqual({
      trace_id: span.spanContext().traceId,
      span_id: span.spanContext().spanId,
    });
  });

  it("is empty when no span is current", () => {
    expect(spanCorrelation()).toEqual({});
  });

  it("is empty once the span that was current has ended", () => {
    const tracer = trace.getTracer("test");
    const span = tracer.startSpan("request");
    context.with(trace.setSpan(context.active(), span), () => undefined);
    span.end();

    // Outside the context.with callback: no span is active here, even
    // though one was current a moment ago in this same test.
    expect(spanCorrelation()).toEqual({});
  });
});
