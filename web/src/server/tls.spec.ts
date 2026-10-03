import { execFileSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { type Http2SecureServer, connect, createSecureServer } from "node:http2";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterAll, beforeAll, describe, expect, it } from "vitest";

import type { Tls } from "./config.ts";
import { tlsOptions, verifyPeer } from "./tls.ts";
import { urlsClient } from "./urls.ts";

const DOMAIN = "example.invalid";
const dir = mkdtempSync(join(tmpdir(), "web-tls-"));

function openssl(...args: string[]): void {
  execFileSync("openssl", args, { stdio: "ignore", cwd: dir });
}

const EC = ["-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes"];

function sign(name: string, parent: string, ext: string): void {
  writeFileSync(join(dir, `${name}.ext`), ext);
  openssl(
    "x509",
    "-req",
    "-in",
    `${name}.csr`,
    "-CA",
    `${parent}.crt`,
    "-CAkey",
    `${parent}.key`,
    "-CAcreateserial",
    "-days",
    "1",
    "-extfile",
    `${name}.ext`,
    "-out",
    `${name}.pem`,
  );
}

// A workload certificate the way the platform issues one: an identity (a
// critical SPIFFE URI) and NO subject and NO name, signed by an issuing
// authority, and delivered with its chain. Its absence of a DNS name is why
// the library's name check cannot be the one used.
function leaf(name: string, uri: string, ca: string): void {
  openssl("req", ...EC, "-keyout", `${name}.key`, "-out", `${name}.csr`, "-subj", "/");
  sign(
    name,
    ca,
    `subjectAltName=critical,URI:${uri}\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth,clientAuth\nbasicConstraints=critical,CA:FALSE\n`,
  );
  writeFileSync(
    join(dir, `${name}.crt`),
    readFileSync(join(dir, `${name}.pem`), "utf8") + readFileSync(join(dir, `${ca}.crt`), "utf8"),
  );
}

// An issuing authority UNDER a root: not self-signed, which is what a
// platform's trust bundle holds.
function issuer(name: string, parent: string): void {
  openssl("req", ...EC, "-keyout", `${name}.key`, "-out", `${name}.csr`, "-subj", `/CN=${name}`);
  sign(name, parent, "basicConstraints=critical,CA:TRUE\nkeyUsage=critical,keyCertSign,cRLSign\n");
  copyFileSync(join(dir, `${name}.pem`), join(dir, `${name}.crt`));
}

function authority(name: string): void {
  openssl(
    "req",
    "-x509",
    ...EC,
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
  caFile: join(dir, "bundle-root.crt"),
  trustDomain: DOMAIN,
  peers: [{ namespace: "ns", serviceAccount: "urls" }],
};

beforeAll(async () => {
  mkdirSync(dir, { recursive: true });
  authority("root");
  authority("other");
  issuer("ca", "root");
  leaf("urls", `spiffe://${DOMAIN}/ns/ns/sa/urls`, "ca");
  leaf("web", `spiffe://${DOMAIN}/ns/ns/sa/web`, "ca");
  // The bundles a platform may distribute: the root, the issuing authority
  // alone (not self-signed), and both.
  copyFileSync(join(dir, "root.crt"), join(dir, "bundle-root.crt"));
  copyFileSync(join(dir, "ca.crt"), join(dir, "bundle-issuer.crt"));
  writeFileSync(
    join(dir, "bundle-both.crt"),
    readFileSync(join(dir, "root.crt"), "utf8") + readFileSync(join(dir, "ca.crt"), "utf8"),
  );

  const read = (f: string) => readFileSync(join(dir, f));
  const serve = (name: string) => {
    server = createSecureServer({
      key: read(`${name}.key`),
      cert: read(`${name}.crt`),
      ca: [read("root.crt"), read("ca.crt")],
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

  // The regression: a platform bundle that holds the ISSUING authority and no
  // self-signed root. Go accepts it as an anchor; Node's own check does not.
  it("accepts a bundle that holds only the issuing authority", async () => {
    await expect(call({ ...client, caFile: join(dir, "bundle-issuer.crt") })).resolves.toBe("ok");
  });

  it("accepts a bundle that holds the root and the issuing authority", async () => {
    await expect(call({ ...client, caFile: join(dir, "bundle-both.crt") })).resolves.toBe("ok");
  });

  it("refuses a server whose chain is not the trust bundle's, and reports why", async () => {
    const reasons: string[] = [];
    const options = tlsOptions({ ...client, caFile: join(dir, "other.crt") }, (r) => reasons.push(r));
    const session = connect(`https://localhost:${port}`, options);
    await expect(
      new Promise((_, reject) => {
        session.on("error", reject);
        session.request({ ":path": "/" }).on("error", reject);
      }),
    ).rejects.toThrow(/issuer certificate|self-signed|unable to verify/);
    expect(reasons).toEqual([expect.stringMatching(/issuer certificate|self-signed|unable to verify/)]);
    session.destroy();
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

describe("the front end's own client", () => {
  // Through the real gRPC client the front end builds, not a bare HTTP/2
  // session: the server here is no gRPC server, so the call fails, but it must
  // fail PAST the handshake (a protocol answer), and never on trust.
  it("gets through the handshake against a platform-shaped bundle", async () => {
    const client = urlsClient(`https://localhost:${port}`, {
      ...tls0(),
      caFile: join(dir, "bundle-issuer.crt"),
    });
    const error = await client.get({ key: "k" }).then(
      () => undefined,
      (e: Error) => e,
    );
    expect(error?.message ?? "").not.toMatch(/certificate|chain|self-signed|admits/);
  });

  it("is refused on trust, with the reason, when the bundle is not the server's", async () => {
    const reasons: string[] = [];
    const client = urlsClient(
      `https://localhost:${port}`,
      { ...tls0(), caFile: join(dir, "other.crt") },
      (r) => reasons.push(r),
    );
    await expect(client.get({ key: "k" })).rejects.toThrow(/issuer certificate|self-signed|unable to verify/);
    expect(reasons).toHaveLength(1);
  });
});

function tls0(): Tls {
  return client;
}

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
