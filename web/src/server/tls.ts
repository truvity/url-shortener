/**
 * The identity this front end presents to the URL service, and the trust it
 * holds in the answer.
 *
 * The same contract the Go components hold through the repository's transport
 * package, written for Node's TLS stack: the certificate and key are presented
 * as the client identity, the platform's trust bundle (`caFile`) is the only
 * authority the answer is verified against, and the answering peer is checked
 * by its IDENTITY — a SPIFFE URI in the trust domain, naming one of the
 * configured peer accounts — rather than by its name. A workload certificate
 * carries an identity and no name, so the library's own name check is replaced
 * and the chain check is kept.
 *
 * Without this, a `tls` block in the configuration was read and ignored: Node
 * trusts its built-in roots only, so a call to a service that answers with a
 * platform-issued certificate failed with "unable to get local issuer
 * certificate", and a service that demands a client certificate refused it.
 */
import { readFileSync } from "node:fs";
import type { PeerCertificate } from "node:tls";

import type { Tls } from "./config.ts";

/** The options handed to the HTTP/2 connection, or undefined while the transport is off. */
export interface TlsOptions {
  ca: Buffer;
  cert: Buffer;
  key: Buffer;
  minVersion: "TLSv1.3";
  checkServerIdentity: (host: string, cert: PeerCertificate) => Error | undefined;
}

/**
 * Node options for a call authenticated by `tls`. `ca`, `cert` and `key` are
 * getters that read the mounted files each time a connection is made, so a
 * certificate the platform has rotated is picked up at the next connect and a
 * connection already made is never dropped.
 */
export function tlsOptions(tls: Tls | undefined): TlsOptions | undefined {
  if (!tls?.mode || tls.mode === "off") return undefined;
  const { caFile, certFile, keyFile } = tls;
  if (!caFile || !certFile || !keyFile) {
    throw new Error("tls.caFile, tls.certFile and tls.keyFile are required once tls.mode is not off");
  }
  return {
    get ca() {
      return readFileSync(caFile);
    },
    get cert() {
      return readFileSync(certFile);
    },
    get key() {
      return readFileSync(keyFile);
    },
    minVersion: "TLSv1.3",
    checkServerIdentity: (_host, cert) => verifyPeer(cert, tls),
  };
}

/**
 * Whether the certificate the server answered with names an admitted account.
 * Runs after Node has built the chain against `ca`: only the NAME check is
 * replaced, never the chain check.
 */
export function verifyPeer(cert: PeerCertificate, tls: Tls): Error | undefined {
  const names = (cert.subjectaltname ?? "").split(",").map((s) => s.trim());
  const uris = names.filter((n) => n.startsWith("URI:")).map((n) => n.slice("URI:".length));
  if (uris.length !== 1) {
    return new Error(
      `the peer's certificate carries ${uris.length} URI names, and an identity is exactly one`,
    );
  }
  const uri = uris[0] ?? "";
  let id: URL;
  try {
    id = new URL(uri);
  } catch {
    return new Error(`the peer's identity ${uri} is not a URI`);
  }
  if (id.protocol !== "spiffe:") {
    return new Error(`the peer's certificate carries no identity: ${uri} is not a spiffe URI`);
  }
  if (id.hostname !== tls.trustDomain) {
    return new Error(`the peer's identity belongs to trust domain ${id.hostname}, not ${tls.trustDomain}`);
  }
  const parts = id.pathname.replace(/^\//, "").split("/");
  if (parts.length !== 4 || parts[0] !== "ns" || parts[2] !== "sa" || !parts[1] || !parts[3]) {
    return new Error(`the peer's identity ${uri} does not name a namespace and an account`);
  }
  const [namespace, serviceAccount] = [parts[1], parts[3]];
  const admitted = (tls.peers ?? []).some(
    (p) => p.namespace === namespace && p.serviceAccount === serviceAccount,
  );
  return admitted
    ? undefined
    : new Error(`${namespace}/${serviceAccount} is not a peer this service admits`);
}
