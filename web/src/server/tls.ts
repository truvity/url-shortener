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
 * Why the chain is verified HERE and not by Node. A trust bundle is a set of
 * certificates the platform trusts, and Go treats every one of them as an
 * anchor — including an issuing authority that is not self-signed, which is
 * what a platform's bundle usually holds. OpenSSL, under Node, accepts only a
 * self-signed root as an anchor and offers no way to relax that, so against
 * such a bundle it fails with "unable to get issuer certificate" for a chain
 * the Go callers accept. So the connection is opened with Node's own chain
 * check off, and the chain is built and verified below against the bundle
 * (anchor = any certificate in it), BEFORE the HTTP/2 session exists: the
 * socket's `secureConnect` announcement is held until the peer is verified, so
 * a refused answer never gets a request.
 */
import { X509Certificate } from "node:crypto";
import { readFileSync } from "node:fs";
import type { SecureClientSessionOptions } from "node:http2";
import {
  connect as tlsConnect,
  type DetailedPeerCertificate,
  type PeerCertificate,
  type TLSSocket,
} from "node:tls";

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
        // The certificate IS checked, by verifyAnswer when the handshake completes, before the
        // session exists: the chain against the trust bundle, then the
        // peer's identity. Only OpenSSL's own anchor rule is off, because it
        // refuses an issuing authority that the platform's bundle holds.
        // Tests hold: a wrong bundle and a peer outside the allow-list are
        // refused.
        // codeql[js/disabling-certificate-validation] verified by verifyAnswer
        rejectUnauthorized: false,
      });
      // The HTTP/2 session sets itself up when the socket announces
      // `secureConnect`, and reports `connect` (which is when a request is
      // written) after that. So the announcement is HELD until the peer has
      // been verified: on a refusal it is never made, the session never
      // exists, and no request is written. Destroying the socket from a
      // listener instead would race the session's own and crash the process.
      const announce = socket.emit.bind(socket);
      socket.emit = ((event: string | symbol, ...args: unknown[]) => {
        if (event === "secureConnect") {
          let reason: Error | undefined;
          try {
            reason = verifyAnswer(socket.getPeerCertificate(true), readFileSync(caFile, "utf8"), tls);
          } catch (error) {
            reason = error as Error;
          }
          if (reason) {
            report(reason.message);
            socket.destroy(reason);
            return false;
          }
        }
        return announce(event, ...args);
      }) as typeof socket.emit;
      return socket;
    },
  };
}

/** Whether the peer's chain reaches the bundle and its identity is admitted. */
export function verifyAnswer(
  peer: DetailedPeerCertificate,
  bundle: string,
  tls: Tls,
  now = new Date(),
): Error | undefined {
  if (!peer?.raw) return new Error("the peer presented no certificate");
  const chain: X509Certificate[] = [];
  for (let c: DetailedPeerCertificate | undefined = peer; c?.raw; c = c.issuerCertificate) {
    const cert = new X509Certificate(c.raw);
    if (chain.some((x) => x.fingerprint256 === cert.fingerprint256)) break;
    chain.push(cert);
  }
  const anchors = (bundle.match(/-----BEGIN CERTIFICATE-----[\s\S]*?-----END CERTIFICATE-----/g) ?? []).map(
    (pem) => new X509Certificate(pem),
  );
  if (anchors.length === 0) return new Error("the trust bundle holds no certificate");
  const chainError = verifyChain(chain, anchors, now);
  if (chainError) return chainError;
  return verifyPeer(peer, tls);
}

function verifyChain(chain: X509Certificate[], anchors: X509Certificate[], now: Date): Error | undefined {
  const [leaf] = chain;
  if (!leaf) return new Error("the peer presented no certificate");
  if (leaf.ca) {
    return new Error("the peer's certificate is a certificate authority, not a workload identity");
  }
  const candidates = [...anchors, ...chain.slice(1)];
  const trusted = (c: X509Certificate) => anchors.some((a) => a.fingerprint256 === c.fingerprint256);
  const live = (c: X509Certificate) => new Date(c.validFrom) <= now && now <= new Date(c.validTo);
  let current = leaf;
  // A bounded walk: leaf, the presented intermediates and the anchor.
  for (let depth = 0; depth <= chain.length + 1; depth++) {
    if (!live(current))
      return new Error(
        `a certificate in the peer's chain is outside its validity (${current.subject || "no subject"})`,
      );
    if (trusted(current)) return undefined;
    const issuer = candidates.find(
      (c) => c.ca && live(c) && current.checkIssued(c) && current.verify(c.publicKey),
    );
    if (!issuer) {
      return new Error("the peer's certificate does not chain to the trust bundle");
    }
    current = issuer;
  }
  return new Error("the peer's certificate does not chain to the trust bundle");
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
