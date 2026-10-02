/**
 * Keep source maps OUT of the image, and file them where `smctl push` reads.
 *
 * `vite build` emits `*.map` files beside the bundles (`build.sourcemap:
 * "hidden"`, so the bundles do not point at them). The image copies all of
 * `dist/` and the server serves any file under it, so a map left there is a
 * map published to every visitor. This moves them to `dist-sourcemaps/` at
 * the repository root, each at the path the script is served under
 * (`dist-sourcemaps/assets/index-<hash>.js.map`), which is the layout the
 * release's `smctl push --maps` expects, and then fails the build if ANY
 * `*.map` is still under `dist/`. The failure is the point: otherwise a stray
 * map is silent.
 */
import { mkdirSync, readdirSync, renameSync, rmSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { pathToFileURL } from "node:url";

/** Every `*.map` under `dir`, as paths. */
export function mapsUnder(dir: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) found.push(...mapsUnder(path));
    else if (entry.name.endsWith(".map")) found.push(path);
  }
  return found;
}

/** Throws, naming each file, when `dist` still holds a source map. */
export function assertNoMaps(dist: string): void {
  const left = mapsUnder(dist);
  if (left.length > 0) {
    throw new Error(`source maps would ship in the image:\n  ${left.join("\n  ")}`);
  }
}

/**
 * Moves every map from `served` (the directory the page is served from) to
 * `out`, keeping its path relative to `served`. `out` is emptied first.
 * Returns the count.
 */
export function moveMaps(served: string, out: string): number {
  const maps = mapsUnder(served);
  rmSync(out, { recursive: true, force: true });
  for (const map of maps) {
    const to = join(out, relative(served, map));
    mkdirSync(dirname(to), { recursive: true });
    renameSync(map, to);
  }
  return maps.length;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? "").href) {
  // Run from web/ by `yarn build`; the repository root is three levels up,
  // which is where the release (run from the root) looks for dist-sourcemaps.
  const out = process.env.SOURCEMAPS_DIR ?? "../../../dist-sourcemaps";
  const moved = moveMaps("dist/assets", out);
  assertNoMaps("dist");
  process.stderr.write(`moved ${moved} source maps to ${out}; dist/ has none\n`);
}
