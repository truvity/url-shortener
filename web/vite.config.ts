import { execSync } from "node:child_process";
import { copyFileSync } from "node:fs";

import react from "@vitejs/plugin-react-swc";
import { defineConfig } from "vite";

// The build id: the commit this bundle was built from, or BUILD_ID when the
// build runs somewhere with no checkout. It is baked in, never read at run
// time, because it must be the SAME string the source maps are stored under.
function buildId(): string {
  if (process.env.BUILD_ID) return process.env.BUILD_ID;
  try {
    return execSync("git rev-parse --short=12 HEAD", { stdio: ["ignore", "pipe", "ignore"] })
      .toString()
      .trim();
  } catch {
    return "unknown";
  }
}

// SWC emits, and the type checker is a separate command — see
// docs/canon/toolchain.md. Emit was never the compiler's job, which is also
// why it was never the slow part.
export default defineConfig({
  define: { __BUILD_ID__: JSON.stringify(buildId()) },
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
  build: { outDir: "dist/assets", emptyOutDir: true },
});
