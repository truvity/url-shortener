import { afterEach, describe, expect, it, vi } from "vitest";
import { createUrl, deleteUrl, listUrls } from "./api.ts";

function answer(body: unknown, init: { status?: number } = {}): Response {
  return new Response(JSON.stringify(body), {
    status: init.status ?? 200,
    headers: { "content-type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the page's view of the back end", () => {
  it("asks this server, never the service that owns the table", async () => {
    const fetched: string[] = [];
    vi.stubGlobal("fetch", (input: string) => {
      fetched.push(String(input));
      return Promise.resolve(answer({ urls: [] }));
    });

    await listUrls();

    // Relative, so it goes to whatever served the page. An absolute
    // address here would be this front end claiming to know where the
    // service lives, which is the seam the example exists to keep shut.
    expect(fetched).toEqual(["/api/urls"]);
  });

  it("carries the SERVICE's refusal, not a generic one", async () => {
    vi.stubGlobal("fetch", () =>
      Promise.resolve(answer({ error: "that key is already taken" }, { status: 400 })),
    );

    // The message a caller sees has to be the one the service wrote. A
    // client that replaced it with "request failed" would leave the user
    // with nothing to act on and the operator with nothing to search for.
    await expect(createUrl("https://example.test")).rejects.toThrow("that key is already taken");
  });

  it("does not turn an unreadable body into a second failure", async () => {
    vi.stubGlobal("fetch", () => Promise.resolve(new Response("<html>gateway</html>", { status: 502 })));

    // A proxy answering HTML is a real case, and JSON.parse throwing
    // inside the error path would replace a 502 with a parse error that
    // names nothing.
    await expect(listUrls()).rejects.toThrow("502");
  });

  it("escapes the key it puts in a path", async () => {
    const fetched: string[] = [];
    vi.stubGlobal("fetch", (input: string) => {
      fetched.push(String(input));
      return Promise.resolve(new Response(null, { status: 204 }));
    });

    await deleteUrl("a/b?c");

    // Unescaped, `a/b?c` is a different route and a query string: the
    // request succeeds against the wrong thing, which is worse than
    // failing.
    expect(fetched).toEqual(["/api/urls/a%2Fb%3Fc"]);
  });
});
