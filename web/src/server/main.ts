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

import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { readFileSync, existsSync } from "node:fs";
import { extname, join, normalize } from "node:path";

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

function logLine(level: string, message: string, rest: Record<string, unknown> = {}): void {
  // JSON, to stderr, at one level. stdout is the program's product and
  // stderr is its commentary; this program's product is a web page, so it
  // writes nothing to stdout at all.
  process.stderr.write(
    `${JSON.stringify({ time: new Date().toISOString(), level, msg: message, ...rest })}\n`,
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
    void serve(req, res, cfg.assets.directory, urls);
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

// The entry is async because telemetry starts before anything serves: a
// span lost during start-up is one describing the start-up.
void main();
