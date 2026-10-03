import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { type Http2SecureServer, connect, createSecureServer } from "node:http2";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterAll, beforeAll, describe, expect, it } from "vitest";

import type { Tls } from "./config.ts";
import { tlsOptions, verifyPeer } from "./tls.ts";

const DOMAIN = "example.invalid";
const dir = mkdtempSync(join(tmpdir(), "web-tls-"));

function openssl(...args: string[]): void {
  execFileSync("openssl", args, { stdio: "ignore", cwd: dir });
}

// A workload certificate the way the platform issues one: an identity (a
// SPIFFE URI) and NO name, signed by an authority the client holds as its
// trust bundle. Its absence of a DNS name is the whole reason the library's
// name check cannot be the one used.
function leaf(name: string, uri: string, ca: string): void {
  writeFileSync(
    join(dir, `${name}.ext`),
    `subjectAltName=URI:${uri}\nextendedKeyUsage=serverAuth,clientAuth\n`,
  );
  openssl(
    "req",
    "-newkey",
    "ec",
    "-pkeyopt",
    "ec_paramgen_curve:prime256v1",
    "-nodes",
    "-keyout",
    `${name}.key`,
    "-out",
    `${name}.csr`,
    "-subj",
    `/CN=${name}`,
  );
  openssl(
    "x509",
    "-req",
    "-in",
    `${name}.csr`,
    "-CA",
    `${ca}.crt`,
    "-CAkey",
    `${ca}.key`,
    "-CAcreateserial",
    "-days",
    "1",
    "-extfile",
    `${name}.ext`,
    "-out",
    `${name}.crt`,
  );
}

function authority(name: string): void {
  openssl(
    "req",
    "-x509",
    "-newkey",
    "ec",
    "-pkeyopt",
    "ec_paramgen_curve:prime256v1",
    "-nodes",
    "-keyout",
    `${name}.key`,
    "-out",
    `${name}.crt`,
    "-subj",
    `/CN=${name}`,
    "-days",
    "1",
  );
}

let server: Http2SecureServer;
let port = 0;

const client: Tls = {
  mode: "strict",
  certFile: join(dir, "web.crt"),
  keyFile: join(dir, "web.key"),
  caFile: join(dir, "ca.crt"),
  trustDomain: DOMAIN,
  peers: [{ namespace: "ns", serviceAccount: "urls" }],
};

beforeAll(async () => {
  mkdirSync(dir, { recursive: true });
  authority("ca");
  authority("other");
  leaf("urls", `spiffe://${DOMAIN}/ns/ns/sa/urls`, "ca");
  leaf("web", `spiffe://${DOMAIN}/ns/ns/sa/web`, "ca");
  leaf("intruder", `spiffe://${DOMAIN}/ns/ns/sa/intruder`, "ca");
  leaf("foreign", `spiffe://${DOMAIN}/ns/ns/sa/urls`, "other");

  const read = (f: string) => execFileSync("cat", [join(dir, f)]);
  const serve = (name: string) => {
    server = createSecureServer({
      key: read(`${name}.key`),
      cert: read(`${name}.crt`),
      ca: read("ca.crt"),
      requestCert: true,
      rejectUnauthorized: true,
      minVersion: "TLSv1.3",
    });
    server.on("request", (_req, res) => res.end("ok"));
  };
  serve("urls");
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  port = (server.address() as AddressInfo).port;
});

afterAll(() => {
  server.close();
  rmSync(dir, { recursive: true, force: true });
});

// The call the way connect-node makes it: an HTTP/2 connection opened with
// the options this module returns.
function call(tls: Tls): Promise<string> {
  return new Promise((resolve, reject) => {
    const options = tlsOptions(tls);
    const session = connect(`https://localhost:${port}`, options);
    session.on("error", reject);
    const req = session.request({ ":path": "/" });
    let body = "";
    req.on("data", (c) => {
      body += c;
    });
    req.on("end", () => {
      session.close();
      resolve(body);
    });
    req.on("error", reject);
    req.end();
  });
}

describe("the call to the URL service", () => {
  it("trusts the platform's bundle and presents the mounted identity", async () => {
    await expect(call(client)).resolves.toBe("ok");
  });

  it("refuses an answer from a peer outside the allow-list, though its chain is good", async () => {
    await expect(
      call({ ...client, peers: [{ namespace: "ns", serviceAccount: "someone-else" }] }),
    ).rejects.toThrow(/not a peer this service admits/);
  });

  it("refuses an answer from another trust domain", async () => {
    await expect(call({ ...client, trustDomain: "elsewhere.invalid" })).rejects.toThrow(/trust domain/);
  });

  it("refuses a server whose chain is not the trust bundle's", async () => {
    await expect(call({ ...client, caFile: join(dir, "other.crt") })).rejects.toThrow(
      /unable to get local issuer certificate|self.signed/,
    );
  });

  it("fails on Node's own roots, as the field did, without the bundle", async () => {
    // Node's own roots: the symptom this module exists to remove.
    const session = connect(`https://localhost:${port}`, { minVersion: "TLSv1.3" });
    await expect(
      new Promise((_, reject) => {
        session.on("error", reject);
        session.request({ ":path": "/" }).on("error", reject);
      }),
    ).rejects.toThrow(/certificate/);
    session.destroy();
  });
});

describe("tlsOptions", () => {
  it("is nothing while the transport is off or absent", () => {
    expect(tlsOptions(undefined)).toBeUndefined();
    expect(tlsOptions({ mode: "off" })).toBeUndefined();
  });

  it("refuses a configuration that names no files once it is on", () => {
    expect(() => tlsOptions({ mode: "strict" })).toThrow(/required/);
  });
});

describe("verifyPeer", () => {
  const cert = (san: string | undefined) => ({ subjectaltname: san }) as never;
  const tls: Tls = { trustDomain: DOMAIN, peers: [{ namespace: "ns", serviceAccount: "urls" }] };

  it("admits exactly the configured account", () => {
    expect(verifyPeer(cert(`URI:spiffe://${DOMAIN}/ns/ns/sa/urls`), tls)).toBeUndefined();
  });

  it("refuses no identity, two identities, a foreign scheme and a malformed path", () => {
    expect(verifyPeer(cert(undefined), tls)?.message).toMatch(/0 URI/);
    expect(
      verifyPeer(cert(`URI:spiffe://${DOMAIN}/ns/ns/sa/urls, URI:spiffe://${DOMAIN}/ns/ns/sa/x`), tls)
        ?.message,
    ).toMatch(/2 URI/);
    expect(verifyPeer(cert("URI:https://example.invalid/ns/ns/sa/urls"), tls)?.message).toMatch(
      /not a spiffe/,
    );
    expect(verifyPeer(cert(`URI:spiffe://${DOMAIN}/ns/ns`), tls)?.message).toMatch(
      /namespace and an account/,
    );
  });
});
