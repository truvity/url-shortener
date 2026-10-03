/**
 * The identity this front end presents to the URL service, and the trust it
 * holds in the answer.
 *
 * The same contract the Go components hold through the repository's transport
 * package, written for Node's TLS stack: the certificate and key are presented
 * as the client identity, the platform's trust bundle (`caFile`) is the only
 * authority the answer is verified against, and the answering peer is checked
 * by its IDENTITY (a SPIFFE URI in the trust domain naming one of the
 * configured peer accounts) rather than by its name.
 *
 * A trust bundle is a set of certificates the platform trusts, and Go treats
 * every one of them as an anchor, including an issuing authority that is not
 * self-signed. OpenSSL by default accepts only a self-signed root, so against
 * such a bundle it fails with "unable to get issuer certificate". The secure
 * context option `allowPartialTrustChain` (OpenSSL's partial-chain flag) gives
 * Go's rule: any certificate in `ca` is an anchor. The chain is still checked
 * by OpenSSL, and certificate validation stays on.
 */
import { readFileSync } from "node:fs";
import type { SecureClientSessionOptions } from "node:http2";
import { type ConnectionOptions, connect as tlsConnect, type PeerCertificate, type TLSSocket } from "node:tls";

import type { Tls } from "./config.ts";

/** What http2.connect takes, or undefined while the transport is off. */
export interface TlsOptions {
  createConnection: (authority: URL, options: SecureClientSessionOptions) => TLSSocket;
}

/** Called with the reason when the answering peer is refused, so it can be logged. */
export type Report = (reason: string) => void;

/**
 * Node options for a call authenticated by `tls`. The certificate, key and
 * bundle are read from the mounted files each time a connection is made, so a
 * certificate the platform has rotated is picked up at the next connect and a
 * connection already made is never dropped.
 */
export function tlsOptions(tls: Tls | undefined, report: Report = () => {}): TlsOptions | undefined {
  if (!tls?.mode || tls.mode === "off") return undefined;
  const { caFile, certFile, keyFile } = tls;
  if (!caFile || !certFile || !keyFile) {
    throw new Error("tls.caFile, tls.certFile and tls.keyFile are required once tls.mode is not off");
  }
  return {
    createConnection: (authority, options) => {
      const socket = tlsConnect({
        host: authority.hostname,
        port: Number(authority.port) || 443,
        servername: options.servername,
        ALPNProtocols: ["h2"],
        minVersion: "TLSv1.3",
        cert: readFileSync(certFile),
        key: readFileSync(keyFile),
        ca: readFileSync(caFile),
        // Any certificate in the bundle is a trust anchor, as in Go.
        allowPartialTrustChain: true,
        // Runs after OpenSSL has verified the chain: the answer must be a
        // workload identity admitted by the allow-list.
        checkServerIdentity: (_host, cert) => verifyPeer(cert, tls),
      } as ConnectionOptions);
      // A refused answer (chain or identity) destroys the socket with the
      // reason; log it so a 502 is never silent.
      socket.once("error", (error) => report(error.message));
      return socket;
    },
  };
}

/**
 * Whether the certificate the server answered with names an admitted account:
 * exactly one URI name, a SPIFFE one in the trust domain, in the form
 * spiffe://<trust domain>/ns/<namespace>/sa/<account>, on the allow-list.
 */
export function verifyPeer(cert: Pick<PeerCertificate, "subjectaltname">, tls: Tls): Error | undefined {
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
