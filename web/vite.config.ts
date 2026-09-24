import { copyFileSync } from "node:fs";

import react from "@vitejs/plugin-react-swc";
import { defineConfig } from "vite";

// SWC emits, and the type checker is a separate command — see
// docs/canon/toolchain.md. Emit was never the compiler's job, which is also
// why it was never the slow part.
export default defineConfig({
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
