import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App.tsx";
import { initTelemetry, readConfig } from "./telemetry.ts";

const root = document.getElementById("root");
if (root === null) throw new Error("the page has no root element");

// Fire and forget, and never in the way of the page: telemetry that fails to
// start is a page with no telemetry, not a page that does not render.
void initTelemetry(readConfig()).catch(() => undefined);

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
