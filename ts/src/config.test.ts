/**
 * The same fixtures the Go loader is tested against, in the directory the Go
 * tests read them from.
 *
 * That is the point of this file, not an economy: two loaders that claim to
 * implement one contract must refuse the same documents and say something a
 * person can act on when they do. A fixture that only one of them sees is a
 * contract that exists twice.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import { ConfigError, load, secret, validate } from "./config.js";

const fixtures = fileURLToPath(new URL("../../config/testdata/", import.meta.url));
const schema = JSON.parse(readFileSync(`${fixtures}shortener.schema.json`, "utf8")) as object;
const fixture = (name: string) => `${fixtures}${name}`;

interface Shortener {
  probes: { address: string };
  baseURL: string;
  database: { url: string; passwordEnv?: string; maxConnections?: number };
}

describe("load", () => {
  it("accepts a valid file and returns it", () => {
    const cfg = load<Shortener>(fixture("valid.yaml"), schema);
    expect(cfg.probes.address).toBe(":7070");
    expect(cfg.database.maxConnections).toBe(20);
    expect(cfg.baseURL).toBe("https://example.com");
  });

  // The failure the whole contract exists to prevent: a key that means
  // nothing, set by a deployment, doing nothing, with no signal but
  // behaviour.
  it("refuses an unknown key and names it", () => {
    expect(() => load(fixture("unknown-key.yaml"), schema)).toThrow(/databse/);
  });

  it("refuses a missing required key and names it", () => {
    expect(() => load(fixture("missing-required.yaml"), schema)).toThrow(/database/);
  });

  // "invalid config" is not an error message. The person reading it is
  // looking at a file and needs the path.
  it("names the path when a type is wrong", () => {
    expect(() => load(fixture("wrong-type.yaml"), schema)).toThrow(/database\.maxConnections/);
  });

  it("refuses a password written into the file, and does not echo it", () => {
    let thrown: unknown;
    try {
      load(fixture("secret-in-file.yaml"), schema);
    } catch (error) {
      thrown = error;
    }
    expect(thrown).toBeInstanceOf(ConfigError);
    const message = (thrown as Error).message;
    expect(message).toMatch(/password/);
    expect(message).not.toMatch(/hunter2/);
  });

  it("says which file is missing", () => {
    expect(() => load(fixture("there-is-no-such-file.yaml"), schema)).toThrow(/there-is-no-such-file\.yaml/);
  });

  it("refuses an empty file", () => {
    // An empty file would otherwise start a service on defaults nobody chose.
    expect(() => load(fixture("empty.yaml"), schema)).toThrow(/is empty/);
  });

  // The wording is part of the contract: the same misconfiguration must read
  // the same way whichever runtime refused it.
  it("reports an unknown key the way the Go loader does", () => {
    let message = "";
    try {
      load(fixture("unknown-key.yaml"), schema);
    } catch (error) {
      message = (error as Error).message;
    }
    expect(message).toContain("databse: not a key this service reads");
  });
});

describe("validate", () => {
  it("accepts a document that satisfies the schema", () => {
    expect(() =>
      validate(
        {
          listen: { address: ":8080" },
          probes: { address: ":7070" },
          baseURL: "https://example.com",
          database: { url: "postgres://shortener@db:5432/shortener" },
        },
        schema,
      ),
    ).not.toThrow();
  });

  it("reports every failure at once, not one per run", () => {
    let failures: readonly string[] = [];
    try {
      validate({ baseURL: 7, database: { url: "not-a-postgres-url" } }, schema);
    } catch (error) {
      failures = (error as ConfigError).failures;
    }
    expect(failures.length).toBeGreaterThan(1);
  });
});

describe("secret", () => {
  it("reads the variable a configuration names", () => {
    process.env["EXAMPLE_PASSWORD"] = "value";
    expect(secret("EXAMPLE_PASSWORD")).toBe("value");
    delete process.env["EXAMPLE_PASSWORD"];
  });

  it("names the variable when it is unset", () => {
    expect(() => secret("EXAMPLE_PASSWORD_THAT_IS_NOT_SET")).toThrow(/EXAMPLE_PASSWORD_THAT_IS_NOT_SET/);
  });

  it("treats an empty variable as unset", () => {
    // An empty password is a misconfiguration, not a password.
    process.env["EXAMPLE_EMPTY"] = "";
    expect(() => secret("EXAMPLE_EMPTY")).toThrow(/is empty/);
    delete process.env["EXAMPLE_EMPTY"];
  });

  it("refuses a secret with no variable named", () => {
    expect(() => secret("")).toThrow();
  });
});
