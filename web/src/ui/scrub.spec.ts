import { describe, expect, it } from "vitest";

import { scrubItem, scrubText, scrubUrl } from "./scrub.ts";

const SELF = "https://app.example";

describe("scrubUrl", () => {
  it("drops the query string and the fragment", () => {
    expect(scrubUrl("https://app.example/r/abc?token=s3cret&x=1#frag")).toBe("https://app.example/r/abc");
  });

  it("handles a relative reference", () => {
    expect(scrubUrl("/api/urls?pageToken=abc#x")).toBe("/api/urls");
  });

  it("leaves a clean URL alone", () => {
    expect(scrubUrl("https://app.example/a/b")).toBe("https://app.example/a/b");
  });
});

describe("scrubText", () => {
  it("cuts every URL inside prose", () => {
    expect(scrubText("see https://app.example/x?a=1 and https://other.example/y#z")).toBe(
      "see https://app.example/x and https://other.example/y",
    );
  });

  it("replaces a foreign URL when asked to, and keeps one of this origin", () => {
    expect(
      scrubText("long https://victim.example/secret/path?q=1 here https://app.example/p?q=2", SELF),
    ).toBe("long [url] here https://app.example/p");
  });
});

describe("scrubItem", () => {
  const item = {
    type: "exception",
    payload: {
      type: "Error",
      value: "the service refused https://typed.example/very/long?token=abc",
      stacktrace: { frames: [{ filename: "https://app.example/assets/index-x.js?v=2#l", lineno: 1 }] },
      context: { "user.id": "u-1", email: "a@b.example", ok: "kept" },
    },
    meta: {
      page: { url: "https://app.example/list?search=private#section" },
      user: { id: "u-1", email: "a@b.example" },
      session: { id: "anon-123" },
    },
  };

  it("scrubs every URL, wherever it sits", () => {
    const out = scrubItem(item, SELF) as typeof item;
    expect(out.meta.page.url).toBe("https://app.example/list");
    expect(out.payload.stacktrace.frames[0]?.filename).toBe("https://app.example/assets/index-x.js");
    expect(out.payload.value).toBe("the service refused [url]");
    expect(JSON.stringify(out)).not.toMatch(/[?#]/);
  });

  it("drops the user and anything that names one", () => {
    const out = scrubItem(item, SELF) as typeof item;
    expect(out.meta).not.toHaveProperty("user");
    expect(out.payload.context).toEqual({ ok: "kept" });
    expect(JSON.stringify(out)).not.toContain("a@b.example");
  });

  it("keeps the anonymous session id and does not mutate its input", () => {
    const before = JSON.stringify(item);
    const out = scrubItem(item, SELF) as typeof item;
    expect(out.meta.session.id).toBe("anon-123");
    expect(JSON.stringify(item)).toBe(before);
  });

  it("scrubs span attributes of a trace", () => {
    const trace = {
      type: "trace",
      payload: {
        resourceSpans: [
          {
            scopeSpans: [
              {
                spans: [
                  {
                    attributes: [
                      {
                        key: "http.url",
                        value: { stringValue: "https://app.example/api/urls?pageToken=zz" },
                      },
                      { key: "enduser.id", value: { stringValue: "u-1" } },
                    ],
                  },
                ],
              },
            ],
          },
        ],
      },
      meta: {},
    };
    const out = JSON.stringify(scrubItem(trace, SELF));
    expect(out).toContain("https://app.example/api/urls");
    expect(out).not.toContain("pageToken");
  });
});
