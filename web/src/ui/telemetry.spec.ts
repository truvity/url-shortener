import { describe, expect, it } from "vitest";

import { initTelemetry, parseConfig, readConfig } from "./telemetry.ts";

const on = JSON.stringify({
  enabled: true,
  collectorUrl: "https://collector.example/collect",
  appName: "app",
});

describe("parseConfig", () => {
  it("is off with no element, empty text, bad JSON or enabled false", () => {
    expect(parseConfig(null)).toBeUndefined();
    expect(parseConfig("")).toBeUndefined();
    expect(parseConfig("{nope")).toBeUndefined();
    expect(
      parseConfig(JSON.stringify({ enabled: false, collectorUrl: "https://c.example", appName: "a" })),
    ).toBeUndefined();
  });

  it("is off when enabled but unusable", () => {
    expect(parseConfig(JSON.stringify({ enabled: true, appName: "a", collectorUrl: "" }))).toBeUndefined();
    expect(
      parseConfig(JSON.stringify({ enabled: true, collectorUrl: "https://c.example" })),
    ).toBeUndefined();
  });

  it("takes the same-origin default when no collector is named", () => {
    expect(parseConfig(JSON.stringify({ enabled: true, appName: "a" }))?.collectorUrl).toBeUndefined();
  });

  it("parses a usable block", () => {
    expect(parseConfig(on)?.collectorUrl).toBe("https://collector.example/collect");
  });
});

describe("readConfig", () => {
  it("reads the element the server wrote", () => {
    expect(readConfig({ getElementById: () => ({ textContent: on }) as HTMLElement })?.appName).toBe("app");
  });

  it("is off when the page has no such element", () => {
    expect(readConfig({ getElementById: () => null })).toBeUndefined();
  });
});

describe("initTelemetry", () => {
  it("does nothing, and loads nothing, when there is no config", async () => {
    expect(await initTelemetry(undefined, "https://app.example")).toBe(false);
  });
});
