/**
 * Browser telemetry, off unless the server says otherwise.
 *
 * The configuration is RUNTIME: one build is promoted unchanged from
 * environment to environment, so the collector address cannot be baked in.
 * The server writes the public part of its `faro` block into the page as a
 * `<script type="application/json" id="faro-config">` element (a data block,
 * so it is not script and a Content-Security-Policy does not touch it).
 * Without that element, or with `enabled: false`, this module does nothing
 * and the Faro libraries are never even downloaded: they live behind a
 * dynamic import.
 *
 * The `apiKey` is a PUBLIC identifier of this app at the collector, shipped to
 * every browser by design. It is not a credential and is not secret.
 */
import { scrubItem } from "./scrub.ts";

export interface FaroConfig {
  enabled: boolean;
  /** A path on this origin (the default) or an absolute HTTPS URL. */
  collectorUrl?: string;
  apiKey?: string;
  appName: string;
  environment?: string;
  sampleRate?: number;
}

declare const __APP_VERSION__: string;

/** The release this bundle was built for: Faro's app.release, and the tag its source maps are pushed under. */
export const appVersion: string = typeof __APP_VERSION__ === "undefined" ? "dev" : __APP_VERSION__;

/** Parses the text of the config element. Anything unusable is "off", never an exception. */
export function parseConfig(text: string | null | undefined): FaroConfig | undefined {
  if (!text) return undefined;
  try {
    const cfg = JSON.parse(text) as Partial<FaroConfig>;
    if (cfg.enabled !== true) return undefined;
    if (
      cfg.collectorUrl !== undefined &&
      (typeof cfg.collectorUrl !== "string" || cfg.collectorUrl === "")
    ) {
      return undefined;
    }
    if (typeof cfg.appName !== "string" || cfg.appName === "") return undefined;
    return cfg as FaroConfig;
  } catch {
    return undefined;
  }
}

/** The config the server embedded in this document, if any. */
export function readConfig(doc: Pick<Document, "getElementById"> = document): FaroConfig | undefined {
  return parseConfig(doc.getElementById("faro-config")?.textContent);
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Starts Faro when `cfg` is present. Resolves to whether it did.
 *
 * `origin` is this page's own origin: trace headers are sent to it and to no
 * other, so a request to a third party never carries a trace id.
 */
export async function initTelemetry(
  cfg: FaroConfig | undefined,
  origin: string = window.location.origin,
): Promise<boolean> {
  if (cfg === undefined) return false;

  const [sdk, tracing] = await Promise.all([
    import("@grafana/faro-web-sdk"),
    import("@grafana/faro-web-tracing"),
  ]);

  // Relative to this page by default; the absolute form is what the transport
  // and the ignore list below must both see.
  const collector = new URL(cfg.collectorUrl ?? "/faro/collect", origin).href;
  const rate = Math.min(1, Math.max(0, cfg.sampleRate ?? 1));

  sdk.initializeFaro({
    url: collector,
    apiKey: cfg.apiKey,
    app: { name: cfg.appName, version: appVersion, release: appVersion, environment: cfg.environment },
    // An anonymous, random, per-tab session id, kept in memory only: no cookie
    // and no storage, so a closed tab is a finished session. samplingRate
    // keeps or drops whole sessions.
    sessionTracking: { enabled: true, persistent: false, samplingRate: rate },
    instrumentations: [
      // Errors, web vitals, page views and performance; console capture is
      // configured below so that only console.error is kept.
      ...sdk.getWebInstrumentations({
        captureConsole: true,
        enablePerformanceInstrumentation: true,
        // Report-only CSP violations land in the same stream, which is what
        // the report-only header is for.
        enableContentSecurityPolicyInstrumentation: true,
      }),
      new tracing.TracingInstrumentation({
        instrumentationOptions: {
          // This page's own origin and nothing else. (Same-origin requests
          // get the header anyway; stating it keeps a future cross-origin
          // fetch from leaking a trace id to a third party.)
          propagateTraceHeaderCorsUrls: [new RegExp(`^${escapeRegExp(origin)}(/|$)`)],
        },
        // An unsampled session sends no traceparent, so the server takes its
        // own sampling decision instead of inheriting "do not record".
        omitTraceContextForUnsampledSessions: true,
      }),
    ],
    consoleInstrumentation: {
      disabledLevels: [
        sdk.LogLevel.TRACE,
        sdk.LogLevel.DEBUG,
        sdk.LogLevel.LOG,
        sdk.LogLevel.INFO,
        sdk.LogLevel.WARN,
      ],
    },
    // The collector's own address is never traced or reported on.
    ignoreUrls: [collector],
    beforeSend: (item) => scrubItem(item, origin),
  });
  return true;
}
