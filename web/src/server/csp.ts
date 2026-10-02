/**
 * The Content-Security-Policy this server sends.
 *
 * Report-only by default: a browser tells its console (and `reportUri`, when
 * set) what the policy WOULD have blocked and blocks nothing, so the first
 * deployment of a policy cannot break a page. Moving to `enforce` is a
 * configuration change once the reports are quiet.
 *
 * Pure functions, no I/O, so the whole policy is testable without a server.
 */

export type CspMode = "off" | "report-only" | "enforce";

export interface Csp {
  mode?: CspMode;
  connectSrc?: string[];
  reportUri?: string;
}

/**
 * The directives. MUI/emotion injects <style> elements at run time, which is
 * why style-src carries 'unsafe-inline' and script-src does not: scripts are
 * only ever the page's own bundle.
 */
export function policy(csp: Csp | undefined, extraConnect: string[] = []): string {
  const connect = ["'self'", ...(csp?.connectSrc ?? []), ...extraConnect];
  const directives = [
    "default-src 'self'",
    "script-src 'self'",
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data:",
    "font-src 'self' data:",
    `connect-src ${[...new Set(connect)].join(" ")}`,
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ];
  if (csp?.reportUri) {
    directives.push(`report-uri ${csp.reportUri}`);
  }
  return directives.join("; ");
}

/** The header name and value to send, or undefined when the mode is `off`. */
export function header(
  csp: Csp | undefined,
  extraConnect: string[] = [],
): { name: string; value: string } | undefined {
  const mode = csp?.mode ?? "report-only";
  if (mode === "off") {
    return undefined;
  }
  return {
    name: mode === "enforce" ? "content-security-policy" : "content-security-policy-report-only",
    value: policy(csp, extraConnect),
  };
}
