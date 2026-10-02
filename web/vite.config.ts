import { copyFileSync } from "node:fs";

import react from "@vitejs/plugin-react-swc";
import { defineConfig } from "vite";

// The release version, baked in at build time and never read at run time:
// the browser reports it as Faro's app.release, and it must be the SAME string
// the source maps are pushed under. The release passes it as VITE_APP_VERSION
// (GoReleaser's {{ .Version }}); a build without it reports "dev".
const appVersion = process.env.VITE_APP_VERSION || "dev";

// SWC emits, and the type checker is a separate command — see
// docs/canon/toolchain.md. Emit was never the compiler's job, which is also
// why it was never the slow part.
export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(appVersion) },
  plugins: [
    react(),
    {
      // The schema is COPIED beside the server, never committed twice. The
      // source of truth is ../schemas/web.json, where the chart's tests read
      // it; a second committed copy is a copy that drifts. This one is for
      // running from a checkout — the bundle gets its own, in scripts/.
      name: "carry-the-schema",
      buildStart() {
        copyFileSync("../schemas/web.json", "src/server/web.schema.json");
      },
    },
  ],
  build: {
    outDir: "dist/assets",
    emptyOutDir: true,
    // "hidden": maps are written but the bundles carry no sourceMappingURL
    // comment (Alloy asks the map server by the script's own path).
    // scripts/sourcemaps.ts moves them out of dist/ before the image is built.
    sourcemap: "hidden",
  },
});
