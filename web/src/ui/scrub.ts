/**
 * What leaves the browser, and what is taken off it first.
 *
 * Pure functions with no Faro import, so the privacy rules are testable (and
 * readable) on their own. The rules, from the telemetry decision:
 *
 *   - no user identity: `meta.user` is dropped, and so is any attribute that
 *     names a user, an e-mail address or an account;
 *   - no query string and no fragment on ANY URL, wherever it appears: a
 *     page URL, a stack frame, a span attribute, a message;
 *   - in a log message or an exception's text, a URL that is not this page's
 *     own origin is replaced outright, because this page echoes what the
 *     visitor typed (a refused create names the long URL it was given).
 */

const URL_IN_TEXT = /\bhttps?:\/\/[^\s"'<>)\]}]+/gi;

// Keys that identify a person. Matched on the whole key, case-insensitively,
// split on `.`/`_`/`-`, so `user.id` and `enduser_email` go and `username_hint`
// is judged by its parts.
const IDENTITY = /(^|[._-])(user|users|enduser|email|e-?mail|account|login|username|userid)([._-]|$)/i;

/** Strips the query string and the fragment from one URL; other text is returned as is. */
export function scrubUrl(value: string): string {
  try {
    const u = new URL(value, "http://relative.invalid");
    if (u.origin === "http://relative.invalid" && !/^https?:\/\//i.test(value)) {
      // A relative reference: cut it by hand, keep what is left as written.
      return value.replace(/[?#].*$/s, "");
    }
    return `${u.origin}${u.pathname}`;
  } catch {
    return value.replace(/[?#].*$/s, "");
  }
}

/** Every URL inside free text, scrubbed; foreign ones replaced when `selfOrigin` is given. */
export function scrubText(text: string, selfOrigin?: string): string {
  return text.replace(URL_IN_TEXT, (match) => {
    const clean = scrubUrl(match);
    if (selfOrigin === undefined) return clean;
    return clean.startsWith(`${selfOrigin}/`) || clean === selfOrigin ? clean : "[url]";
  });
}

type Json = unknown;

const FREE_TEXT_KEYS = new Set(["value", "message"]);

function walk(node: Json, selfOrigin: string | undefined, key: string | undefined): Json {
  if (typeof node === "string") {
    // Whole-string URLs (page.url, stack filenames, span `http.url`) and URLs
    // embedded in prose both go through the same cut; only free-text fields
    // also lose foreign URLs.
    const freeText = key !== undefined && FREE_TEXT_KEYS.has(key);
    return scrubText(node, freeText ? selfOrigin : undefined);
  }
  if (Array.isArray(node)) {
    return node.map((item) => walk(item, selfOrigin, key));
  }
  if (node !== null && typeof node === "object") {
    const out: Record<string, Json> = {};
    for (const [k, v] of Object.entries(node as Record<string, Json>)) {
      if (IDENTITY.test(k)) continue;
      out[k] = walk(v, selfOrigin, k);
    }
    return out;
  }
  return node;
}

/**
 * The beforeSend hook. Returns a scrubbed copy; never mutates its input, and
 * never returns null (dropping is the sampler's job, not the scrubber's).
 */
export function scrubItem<T extends { payload: unknown; meta?: unknown }>(item: T, selfOrigin?: string): T {
  const meta = walk(item.meta ?? {}, undefined, "meta") as Record<string, Json>;
  // Faro attaches `meta.user` only when somebody calls setUser; this page
  // never does, and the hook makes sure nothing else can.
  delete meta.user;
  return { ...item, payload: walk(item.payload, selfOrigin, undefined), meta };
}
