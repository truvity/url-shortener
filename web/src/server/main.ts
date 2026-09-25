/**
 * The composition root.
 *
 * Read this file top to bottom and you know what the process is made of and
 * what it talks to. There is no container to ask and no registration in a
 * package far away — the same rule every other component here follows.
 *
 * This is a front end, so it does two things: serve the built assets, and
 * ask the service that owns the tables. It writes nothing itself, and holds
 * no database credential — the whole of the ownership rule, seen from the
 * consuming side.
 */
import { start as startTelemetry } from "@truvity/policy/telemetry";
import { context, propagation, SpanKind, SpanStatusCode, trace } from "@opentelemetry/api";

import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { readFileSync, existsSync } from "node:fs";
import { extname, join, normalize } from "node:path";
import { pathToFileURL } from "node:url";

import { read } from "./config.ts";
import { urlsClient } from "./urls.ts";

const LIVE = "/health/live";
const READY = "/health/ready";

const TYPES: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".svg": "image/svg+xml",
  ".json": "application/json",
};

// spanCorrelation reads the span active when a log line is written and
// returns trace_id/span_id for it -- the names the OpenTelemetry
// specification recommends for trace context in a log format that is not
// OTLP, lower-case hex, the W3C forms. An empty object when no span is
// current, so the fields are ABSENT from the line rather than empty
// strings.
export function spanCorrelation(): Record<string, string> {
  const spanContext = trace.getSpan(context.active())?.spanContext();
  if (!spanContext || !trace.isSpanContextValid(spanContext)) {
    return {};
  }
  return { trace_id: spanContext.traceId, span_id: spanContext.spanId };
}

function logLine(level: string, message: string, rest: Record<string, unknown> = {}): void {
  // JSON, to stderr, at one level. stdout is the program's product and
  // stderr is its commentary; this program's product is a web page, so it
  // writes nothing to stdout at all.
  process.stderr.write(
    `${JSON.stringify({
      time: new Date().toISOString(),
      level,
      msg: message,
      ...spanCorrelation(),
      ...rest,
    })}\n`,
  );
}

async function main(): Promise<void> {
  const path = argument() ?? process.env["CONFIG_FILE"];
  if (!path) {
    process.stderr.write("no configuration file: pass -config or set CONFIG_FILE\n");
    process.exit(1);
  }

  let cfg;
  try {
    cfg = read(path);
  } catch (error) {
    process.stderr.write(`${(error as Error).message}\n`);
    process.exit(1);
  }

  logLine("info", "starting", { component: "web", version: process.env["VERSION"] ?? "unknown" });

  // Telemetry, from OpenTelemetry's own environment (decision 0006).
  // With no endpoint configured the chart sets the exporters to `none`
  // and this installs nothing, so a laptop and a cluster run the same
  // code down the same path — which is the failure 0006 records, and it
  // happened in a Node service exactly like this one.
  const stopTelemetry = await startTelemetry();

  const urls = urlsClient(cfg.urls.address);

  const app = createServer((req, res) => {
    // The probe listener below is deliberately NOT traced: a readiness
    // check every few seconds is not a request anybody is debugging, and
    // it would be most of what the store holds.
    void traced(req, res, () => serve(req, res, cfg.assets.directory, urls));
  });

  // Probes on their OWN listener. The port that serves the page is the port
  // a browser can reach, and a readiness endpoint reachable from outside is
  // one anybody can use to take a service out of rotation.
  const probes = createServer((req, res) => {
    if (req.url === LIVE) {
      // Checks nothing. A liveness probe that checks a dependency restarts
      // a healthy process because something else is down.
      res.writeHead(200).end("ok");
      return;
    }
    if (req.url === READY) {
      // The assets have to be there to serve. The URL service is NOT
      // checked: a service that is down is not this component's outage to
      // report, and a probe that checked it would take the front end out of
      // rotation for somebody else's problem.
      if (existsSync(join(cfg.assets.directory, "index.html"))) {
        res.writeHead(200).end("ok");
      } else {
        res.writeHead(503).end("the assets are not where the configuration says");
      }
      return;
    }
    res.writeHead(404).end("not found");
  });

  app.listen(port(cfg.listen.address), () => logLine("info", "listening", { address: cfg.listen.address }));
  probes.listen(port(cfg.probes.address), () => logLine("info", "serving probes", { address: cfg.probes.address }));

  const drain = (cfg.drain?.seconds ?? 20) * 1000;
  for (const signal of ["SIGTERM", "SIGINT"] as const) {
    process.on(signal, () => {
      logLine("info", "draining", { seconds: drain / 1000 });
      // Both servers, and a deadline. An orchestrator keeps sending traffic
      // for a moment after the signal, and a server that stopped accepting
      // immediately would drop requests already in flight.
      const done = setTimeout(() => process.exit(0), drain);
      let left = 2;
      const finish = (): void => {
        if (--left === 0) {
          clearTimeout(done);
          // Flush before the process goes. Spans describing a shutdown
          // are the ones somebody is looking for when they ask why it
          // shut down, and they are the first to be lost.
          void stopTelemetry().finally(() => process.exit(0));
        }
      };
      app.close(finish);
      probes.close(finish);
    });
  }
}

function argument(): string | undefined {
  const at = process.argv.findIndex((a) => a === "-config" || a === "--config");
  return at >= 0 ? process.argv[at + 1] : undefined;
}

function port(address: string): number {
  return Number.parseInt(address.replace(/^.*:/, ""), 10);
}

// present is the one shape this front end sees, and it is NOT the protobuf
// message. Timestamps become strings and a 64-bit count becomes a number,
// because JSON has no Timestamp and JavaScript has no int64 — handing the
// wire type straight to the page gives it a BigInt that JSON.stringify
// refuses, which fails at the boundary rather than where it was decided.
function present(url:
  | { key?: string; longUrl?: string; clickCount?: bigint; createdAt?: { seconds?: bigint }; deletedAt?: { seconds?: bigint } }
  | undefined): { key: string; longUrl: string; clicks: number; createdAt: string | null; deleted: boolean } {
  const stamp = (t?: { seconds?: bigint }): string | null =>
    t?.seconds === undefined ? null : new Date(Number(t.seconds) * 1000).toISOString();
  return {
    key: url?.key ?? "",
    longUrl: url?.longUrl ?? "",
    clicks: Number(url?.clickCount ?? 0),
    createdAt: stamp(url?.createdAt),
    deleted: url?.deletedAt !== undefined,
  };
}

// readBody collects a request body with a CEILING on it. Without one a
// single request can make this process hold as much memory as somebody
// cares to send it.
async function readBody(req: IncomingMessage): Promise<string> {
  const LIMIT = 64 * 1024;
  let size = 0;
  const chunks: Buffer[] = [];
  for await (const chunk of req) {
    size += (chunk as Buffer).length;
    if (size > LIMIT) {
      throw new Error("the request body is larger than this endpoint accepts");
    }
    chunks.push(chunk as Buffer);
  }
  return Buffer.concat(chunks).toString("utf8");
}

// traced wraps a request in a SERVER span.
//
// Installing exporters is not instrumentation. A provider with nothing
// creating spans exports nothing, and the only symptom is a service that
// is missing from the trace store while every dashboard says the pipeline
// is healthy — which is exactly how this was found, after forty requests
// produced no trace at all.
//
// The incoming context is extracted BEFORE the span starts, so a request
// that arrives with a traceparent continues that trace instead of
// beginning an orphan one.
async function traced(
  req: IncomingMessage,
  res: ServerResponse,
  run: () => Promise<void>,
): Promise<void> {
  const tracer = trace.getTracer("url-shortener-web");
  const incoming = propagation.extract(context.active(), req.headers);
  // Named for the ROUTE, never the path: `/api/urls/abc12345` as a span
  // name makes one span per key, and a trace store groups by name.
  const route = routeOf(req);

  await context.with(incoming, async () => {
    const span = tracer.startSpan(`${req.method ?? "GET"} ${route}`, {
      kind: SpanKind.SERVER,
      attributes: { "http.request.method": req.method ?? "GET", "http.route": route },
    });
    try {
      await context.with(trace.setSpan(context.active(), span), run);
      span.setAttribute("http.response.status_code", res.statusCode);
      // Only 5xx is this service's failure. A 404 is an answer.
      if (res.statusCode >= 500) {
        span.setStatus({ code: SpanStatusCode.ERROR });
      }
    } catch (error) {
      span.setStatus({ code: SpanStatusCode.ERROR, message: (error as Error).message });
      span.recordException(error as Error);
      throw error;
    } finally {
      span.end();
    }
  });
}

/** The low-cardinality name for a path. */
function routeOf(req: IncomingMessage): string {
  const path = new URL(req.url ?? "/", "http://localhost").pathname;
  if (path.startsWith("/api/urls/")) return "/api/urls/:key";
  if (path === "/api/urls" || path === "/api/url") return path;
  return path === "/" ? "/" : "/*";
}

async function serve(
  req: IncomingMessage,
  res: ServerResponse,
  assets: string,
  urls: ReturnType<typeof urlsClient>,
): Promise<void> {
  const url = new URL(req.url ?? "/", "http://localhost");

  // The one call this front end makes server-side. A browser could call the
  // same boundary directly through the gateway with a Connect transport;
  // doing it here keeps the example's route surface to one host.
  if (url.pathname === "/api/url" && req.method === "GET") {
    const key = url.searchParams.get("key") ?? "";
    try {
      const answer = await urls.get({ key });
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ key: answer.url?.key, longUrl: answer.url?.longUrl, clicks: Number(answer.url?.clickCount ?? 0) }));
    } catch (error) {
      logLine("error", "the URL service refused", { detail: (error as Error).message });
      res.writeHead(502, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: "the URL service could not answer" }));
    }
    return;
  }

  // The listing, the create and the retire. Together with the lookup above
  // they are the whole surface this page needs, and each is one call to the
  // service that owns the table — this server holds no database credential
  // and could not read it directly if it wanted to.
  if (url.pathname === "/api/urls" && req.method === "GET") {
    try {
      const answer = await urls.list({
        pageSize: Number(url.searchParams.get("pageSize") ?? 0),
        pageToken: url.searchParams.get("pageToken") ?? "",
        includeDeleted: url.searchParams.get("includeDeleted") === "true",
      });
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ urls: answer.urls.map(present), nextPageToken: answer.nextPageToken }));
    } catch (error) {
      logLine("error", "the URL service refused a listing", { detail: (error as Error).message });
      res.writeHead(502, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: "the URL service could not answer" }));
    }
    return;
  }

  if (url.pathname === "/api/urls" && req.method === "POST") {
    try {
      const body = JSON.parse(await readBody(req)) as { key?: string; longUrl?: string };
      const answer = await urls.create({ key: body.key ?? "", longUrl: body.longUrl ?? "" });
      res.writeHead(201, { "content-type": "application/json" });
      res.end(JSON.stringify(present(answer.url)));
    } catch (error) {
      // The service's refusals are the caller's to see — a key already
      // taken, a URL that is not one — so the message goes back rather
      // than being flattened into "could not answer".
      logLine("warn", "the URL service refused a create", { detail: (error as Error).message });
      res.writeHead(400, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: (error as Error).message }));
    }
    return;
  }

  if (url.pathname.startsWith("/api/urls/") && req.method === "DELETE") {
    const key = decodeURIComponent(url.pathname.slice("/api/urls/".length));
    try {
      await urls.delete({ key });
      res.writeHead(204).end();
    } catch (error) {
      logLine("warn", "the URL service refused a delete", { detail: (error as Error).message });
      res.writeHead(400, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: (error as Error).message }));
    }
    return;
  }

  // Static assets, and anything else is the single page.
  const wanted = url.pathname === "/" ? "/index.html" : url.pathname;
  const file = join(assets, normalize(wanted).replace(/^(\.\.[/\\])+/, ""));
  if (existsSync(file)) {
    res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" });
    res.end(readFileSync(file));
    return;
  }
  res.writeHead(404, { "content-type": "text/plain" }).end("not found");
}

// Guarded so importing this module (a test, importing spanCorrelation) does
// not also run it: only run when this file is the one `node` was started
// on, the same file esbuild bundles to `dist/server/main.js` and the image
// runs.
if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  // The entry is async because telemetry starts before anything serves: a
  // span lost during start-up is one describing the start-up.
  void main();
}
