import { trace } from "@opentelemetry/api";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it } from "vitest";
import { resourceFromEnvironment, start } from "./telemetry.ts";

const KEYS = [
  "OTEL_SERVICE_NAME",
  "OTEL_RESOURCE_ATTRIBUTES",
  "OTEL_TRACES_EXPORTER",
  "OTEL_METRICS_EXPORTER",
  "OTEL_EXPORTER_OTLP_ENDPOINT",
] as const;
const saved: Record<string, string | undefined> = {};
for (const key of KEYS) saved[key] = process.env[key];

afterEach(() => {
  for (const key of KEYS) {
    if (saved[key] === undefined) delete process.env[key];
    else process.env[key] = saved[key];
  }
});

describe("the resource a process reports itself as", () => {
  it("is named by OTEL_SERVICE_NAME", async () => {
    process.env.OTEL_SERVICE_NAME = "example-web";

    const resource = await resourceFromEnvironment();

    // A provider built with no resource ignores this variable and files
    // the service under `unknown_service:node`. Nothing errors and every
    // span is accepted, so the only symptom is a service missing from the
    // store's list and a stranger in it -- which is how this was found.
    expect(resource.attributes["service.name"]).toBe("example-web");
  });

  it("carries OTEL_RESOURCE_ATTRIBUTES beside it", async () => {
    process.env.OTEL_SERVICE_NAME = "example-web";
    process.env.OTEL_RESOURCE_ATTRIBUTES = "deployment.example.tier=primary";

    const resource = await resourceFromEnvironment();

    expect(resource.attributes["deployment.example.tier"]).toBe("primary");
  });

  it("does not invent a name when none was given", async () => {
    delete process.env.OTEL_SERVICE_NAME;

    const resource = await resourceFromEnvironment();

    // Absent stays absent HERE. The SDK's own fallback is applied by the
    // provider, and inventing one in this function would make an unset
    // variable look like a deliberate choice.
    expect(resource.attributes["service.name"]).toBeUndefined();
  });
});

describe("start()", () => {
  it("puts the service name ON THE WIRE, from the provider that made the span", async () => {
    const bodies: string[] = [];
    const server = createServer((req, res) => {
      const chunks: Buffer[] = [];
      req.on("data", (c: Buffer) => chunks.push(c));
      req.on("end", () => {
        bodies.push(Buffer.concat(chunks).toString("utf8"));
        res.writeHead(200, { "content-type": "application/json" }).end("{}");
      });
    });
    await new Promise<void>((done) => server.listen(0, "127.0.0.1", done));
    const { port } = server.address() as AddressInfo;

    process.env.OTEL_SERVICE_NAME = "example-web";
    process.env.OTEL_TRACES_EXPORTER = "otlp";
    process.env.OTEL_METRICS_EXPORTER = "none";
    process.env.OTEL_EXPORTER_OTLP_ENDPOINT = `http://127.0.0.1:${port}`;

    const stop = await start();
    try {
      trace.getTracer("test").startSpan("probe").end();
    } finally {
      await stop();
      await new Promise<void>((done) => server.close(() => done()));
    }

    // The helper above can be right while the provider is built without
    // it -- which is the actual failure, and the one a test of the helper
    // alone cannot see. So this asks the thing that goes on the wire: the
    // payload a collector would receive, not what was meant to be in it.
    expect(bodies.join("\n")).toContain("example-web");
    expect(bodies.join("\n")).not.toContain("unknown_service");
  });
});
