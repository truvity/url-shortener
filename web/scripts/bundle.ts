/**
 * Bundle the server into one file, so the runtime image needs no
 * `node_modules` at all.
 *
 * That is what makes the image a COPY and nothing else — service.md §7 —
 * and it is also what makes the in-repository dependency on this
 * repository's own loader work in an image: it is a symlink in a checkout,
 * and a symlink is not a thing you can copy into a container.
 *
 * The schema goes next to the bundle for the same reason it goes inside the
 * Python package and the Kotlin jar: the binary validates against the
 * schema it shipped with, not one a deployment happened to mount beside it.
 */
import { copyFileSync, mkdirSync } from "node:fs";

import { build } from "esbuild";

await build({
  entryPoints: ["src/server/main.ts"],
  bundle: true,
  platform: "node",
  format: "esm",
  // The line the IMAGE runs, which is not the line this script runs on.
  // `yarn <script>` executes under yarn's own Node — the package set pulls
  // one in beside the declared one — so the target is stated here rather
  // than inherited from whatever is executing. It is the same distinction
  // as a build tool's own runtime versus the one it compiles for, met for
  // the third time in a third ecosystem.
  target: "node26",
  outfile: "dist/server/main.js",
  // Bundled, not marked external: the point is an image with nothing in it
  // but the artifact.
  packages: "bundle",
  banner: {
    // A dependency reached through its CommonJS build calls `require` for a
    // Node builtin, and an ES module has none — so the bundle starts with
    // one. Without it the image builds, starts, and dies on the first line
    // that touches YAML: `Dynamic require of "process" is not supported`,
    // which names neither the dependency nor the format that caused it.
    js: [
      "import { createRequire as __nodeRequire } from 'node:module';",
      "const require = __nodeRequire(import.meta.url);",
    ].join("\n"),
  },
});

mkdirSync("dist/server", { recursive: true });
copyFileSync("../schemas/web.json", "dist/server/web.schema.json");

process.stderr.write("bundled the server, and carried its schema beside it\n");
