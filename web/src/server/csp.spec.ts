import { describe, expect, it } from "vitest";

import { header, policy } from "./csp.ts";

describe("header", () => {
  it("is report-only when nothing is configured", () => {
    expect(header(undefined)?.name).toBe("content-security-policy-report-only");
  });

  it("enforces only when asked", () => {
    expect(header({ mode: "enforce" })?.name).toBe("content-security-policy");
  });

  it("sends nothing when off", () => {
    expect(header({ mode: "off" })).toBeUndefined();
  });
});

describe("policy", () => {
  it("allows the page's own origin and nothing else by default", () => {
    const p = policy(undefined);
    expect(p).toContain("default-src 'self'");
    expect(p).toContain("script-src 'self';");
    expect(p).toContain("connect-src 'self';");
    expect(p).toContain("frame-ancestors 'none'");
    expect(p).not.toContain("report-uri");
  });

  it("adds configured origins to connect-src, once", () => {
    const p = policy({ connectSrc: ["https://collector.example"] }, ["https://collector.example"]);
    expect(p).toContain("connect-src 'self' https://collector.example;");
    expect(p.match(/collector\.example/g)).toHaveLength(1);
  });

  it("never loosens script-src", () => {
    const p = policy({ connectSrc: ["https://collector.example"] });
    expect(p).toContain("script-src 'self';");
    expect(p).not.toContain("unsafe-eval");
  });

  it("adds report-uri when set", () => {
    expect(policy({ reportUri: "/csp-report" })).toMatch(/; report-uri \/csp-report$/);
  });
});
