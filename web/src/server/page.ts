/**
 * The page the server hands out, with the runtime telemetry configuration
 * written into it.
 *
 * The build is promoted unchanged from environment to environment, so the
 * collector address cannot live in the bundle. The server owns the
 * configuration file and the page, so it puts the PUBLIC part of the `faro`
 * block into the HTML as a JSON data block the bundle reads at start-up.
 */
import type { Faro } from "./config.ts";

/** Same-origin by default: the gateway routes this path to the collector. */
export const DEFAULT_COLLECTOR = "/faro/collect";

/** What the browser is told. A fixed list: a key added to the schema is not shipped by accident. */
export function publicFaro(faro: Faro | undefined): Record<string, unknown> | undefined {
  if (faro?.enabled !== true) return undefined;
  return {
    enabled: true,
    collectorUrl: faro.collectorUrl ?? DEFAULT_COLLECTOR,
    apiKey: faro.apiKey,
    appName: faro.appName ?? "url-shortener-web",
    environment: faro.environment,
    sampleRate: faro.sampleRate ?? 1,
  };
}

/**
 * The collector's origin when it is ANOTHER origin, for connect-src. A path
 * on this page's own origin (the default) needs nothing: `'self'` covers it.
 */
export function collectorOrigin(faro: Faro | undefined): string | undefined {
  const shown = publicFaro(faro);
  const url = shown?.collectorUrl as string | undefined;
  return url?.startsWith("https://") ? new URL(url).origin : undefined;
}

// `<` is escaped so no value can close the element or open a comment, and
// U+2028/2029 so the block is valid wherever it is embedded.
function safeJson(value: unknown): string {
  return JSON.stringify(value)
    .replaceAll("<", "\\u003c")
    .replaceAll(String.fromCharCode(0x2028), "\\u2028")
    .replaceAll(String.fromCharCode(0x2029), "\\u2029");
}

/** The HTML with the config block added before the first module script; unchanged when telemetry is off. */
export function withTelemetryConfig(html: string, faro: Faro | undefined): string {
  const shown = publicFaro(faro);
  if (!shown) return html;
  const block = `<script type="application/json" id="faro-config">${safeJson(shown)}</script>`;
  const at = html.indexOf("<script");
  return at === -1
    ? html.replace("</head>", `${block}</head>`)
    : `${html.slice(0, at)}${block}\n    ${html.slice(at)}`;
}
