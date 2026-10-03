import { context, metrics, trace } from "@opentelemetry/api";
import {
  InMemoryMetricExporter,
  MeterProvider,
  PeriodicExportingMetricReader,
  AggregationTemporality,
} from "@opentelemetry/sdk-metrics";
import {
  InMemorySpanExporter,
  NodeTracerProvider,
  SimpleSpanProcessor,
} from "@opentelemetry/sdk-trace-node";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";

import { REQUEST_DURATION, spanCorrelation, traced } from "./main.ts";

const exporter = new InMemorySpanExporter();
const provider = new NodeTracerProvider({ spanProcessors: [new SimpleSpanProcessor(exporter)] });
const metricExporter = new InMemoryMetricExporter(AggregationTemporality.DELTA);
const reader = new PeriodicExportingMetricReader({
  exporter: metricExporter,
  exportIntervalMillis: 3_600_000,
});
const meterProvider = new MeterProvider({ readers: [reader] });

beforeAll(() => {
  provider.register();
  metrics.setGlobalMeterProvider(meterProvider);
});
afterAll(async () => {
  await provider.shutdown();
  await meterProvider.shutdown();
});
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

describe("traced", () => {
  it("continues the trace a browser started", async () => {
    const traceId = "0af7651916cd43dd8448eb211c80319c";
    const parentId = "b7ad6b7169203331";
    const req = {
      method: "POST",
      url: "/api/urls",
      headers: { traceparent: `00-${traceId}-${parentId}-01` },
    } as unknown as Parameters<typeof traced>[0];
    const res = { statusCode: 201 } as Parameters<typeof traced>[1];

    await traced(req, res, async () => undefined);

    const [span] = exporter.getFinishedSpans();
    expect(span?.spanContext().traceId).toBe(traceId);
    expect(span?.parentSpanContext?.spanId).toBe(parentId);
  });

  it("records http.server.request.duration with the route template, never the path", async () => {
    // Drop what the other tests recorded: delta temporality, so what follows
    // is only this test's four requests.
    await reader.forceFlush();
    metricExporter.reset();
    const run = (method: string, url: string, statusCode: number) =>
      traced(
        { method, url, headers: {} } as unknown as Parameters<typeof traced>[0],
        { statusCode } as Parameters<typeof traced>[1],
        async () => undefined,
      );

    await run("GET", "/api/urls/abc12345", 200);
    await run("GET", "/api/urls/zzz99999", 200);
    await run("POST", "/api/urls", 502);
    await run("GET", "/some/unknown/path", 404);
    await reader.forceFlush();

    const histogram = metricExporter
      .getMetrics()
      .flatMap((m) => m.scopeMetrics)
      .flatMap((s) => s.metrics)
      .find((m) => m.descriptor.name === REQUEST_DURATION);
    expect(histogram?.descriptor.unit).toBe("s");

    const series = Object.fromEntries(
      (histogram?.dataPoints ?? []).map((p) => [
        `${p.attributes["http.request.method"]} ${p.attributes["http.route"]} ${p.attributes["http.response.status_code"]}`,
        (p.value as { count: number }).count,
      ]),
    );
    expect(series).toEqual({
      "GET /api/urls/:key 200": 2,
      "POST /api/urls 502": 1,
      "GET /* 404": 1,
    });
    // Three attributes and no more: nothing that carries a key or a path.
    for (const p of histogram?.dataPoints ?? []) {
      expect(Object.keys(p.attributes).sort()).toEqual([
        "http.request.method",
        "http.response.status_code",
        "http.route",
      ]);
    }
  });
});
