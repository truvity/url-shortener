import { existsSync, mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { assertNoMaps, mapsUnder, moveMaps } from "./sourcemaps.ts";

function fixture(): { dist: string; served: string; out: string } {
  const root = mkdtempSync(join(tmpdir(), "maps-"));
  const served = join(root, "dist", "assets");
  mkdirSync(join(served, "assets"), { recursive: true });
  mkdirSync(join(root, "dist", "server"), { recursive: true });
  writeFileSync(join(served, "index.html"), "<html></html>");
  writeFileSync(join(served, "assets", "index-abc.js"), "x");
  writeFileSync(join(served, "assets", "index-abc.js.map"), "{}");
  return { dist: join(root, "dist"), served, out: join(root, "dist-sourcemaps") };
}

describe("source maps", () => {
  it("are found wherever they sit", () => {
    expect(mapsUnder(fixture().dist)).toHaveLength(1);
  });

  it("fail the check while any remains under dist", () => {
    expect(() => assertNoMaps(fixture().dist)).toThrow(/index-abc\.js\.map/);
  });

  it("are moved keeping the served path and the check then passes", () => {
    const f = fixture();
    expect(moveMaps(f.served, f.out)).toBe(1);
    expect(existsSync(join(f.out, "assets", "index-abc.js.map"))).toBe(true);
    expect(() => assertNoMaps(f.dist)).not.toThrow();
    expect(existsSync(join(f.served, "assets", "index-abc.js"))).toBe(true);
  });

  it("are caught in the server directory too", () => {
    const f = fixture();
    moveMaps(f.served, f.out);
    writeFileSync(join(f.dist, "server", "main.js.map"), "{}");
    expect(() => assertNoMaps(f.dist)).toThrow(/main\.js\.map/);
  });
});
