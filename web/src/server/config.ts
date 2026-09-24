/**
 * What this binary reads, and the shape it reads it into.
 *
 * One file, one schema, validated before anything is constructed — the same
 * rule every other component in this example follows, using this
 * repository's own TypeScript loader. The type below is hand-written beside
 * the schema rather than generated from it: two descriptions that a test
 * holds together beat one that a generator makes untouchable.
 */
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { load } from "@truvity/policy";

export interface Web {
  listen: { address: string };
  probes: { address: string };
  log?: { level: string };
  drain?: { seconds: number };
  urls: { address: string };
  assets: { directory: string };
}

// Carried beside the code, so the binary validates against the schema it
// SHIPPED with rather than one a deployment happens to have mounted beside
// it. The source of truth is ../schemas/web.json and the build copies it —
// see vite.config.ts.
const here = dirname(fileURLToPath(import.meta.url));

export function schema(): object {
  return JSON.parse(readFileSync(join(here, "web.schema.json"), "utf8")) as object;
}

export function read(path: string): Web {
  return load<Web>(path, schema());
}
