import { describe, expect, it } from "vitest";

import { collectorOrigin, publicFaro, withTelemetryConfig } from "./page.ts";

const HTML =
  '<html><head><title>t</title></head><body><script type="module" src="/a.js"></script></body></html>';
const faro = {
  enabled: true,
  collectorUrl: "https://collector.example/collect",
  apiKey: "pub-key",
  environment: "devel",
};

describe("withTelemetryConfig", () => {
  it("leaves the page untouched when telemetry is absent or off", () => {
    expect(withTelemetryConfig(HTML, undefined)).toBe(HTML);
    expect(withTelemetryConfig(HTML, { enabled: false, collectorUrl: faro.collectorUrl })).toBe(HTML);
  });

  it("writes the public block before the first script", () => {
    const out = withTelemetryConfig(HTML, faro);
    expect(out.indexOf('id="faro-config"')).toBeLessThan(out.indexOf('src="/a.js"'));
    const json = /id="faro-config">(.*?)<\/script>/.exec(out)?.[1] ?? "";
    expect(JSON.parse(json)).toEqual({
      enabled: true,
      collectorUrl: faro.collectorUrl,
      apiKey: "pub-key",
      appName: "url-shortener-web",
      environment: "devel",
      sampleRate: 1,
    });
  });

  it("cannot be broken out of by a value", () => {
    const out = withTelemetryConfig(HTML, { ...faro, environment: "</script><script>alert(1)</script>" });
    expect(out).not.toContain("</script><script>alert");
  });

  it("ships only the listed fields", () => {
    expect(Object.keys(publicFaro({ ...faro, extra: "x" } as never) ?? {}).sort()).toEqual([
      "apiKey",
      "appName",
      "collectorUrl",
      "enabled",
      "environment",
      "sampleRate",
    ]);
  });
});

describe("collectorOrigin", () => {
  it("is the origin of an absolute collector, or nothing when off", () => {
    expect(collectorOrigin(faro)).toBe("https://collector.example");
    expect(collectorOrigin(undefined)).toBeUndefined();
  });

  it("is nothing for the same-origin default, which 'self' already covers", () => {
    expect(collectorOrigin({ enabled: true })).toBeUndefined();
    expect(publicFaro({ enabled: true })?.collectorUrl).toBe("/faro/collect");
  });
});
