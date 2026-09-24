/**
 * The client for the service that owns the tables.
 *
 * ONE plugin generated the descriptors this imports, and the client is built
 * from them — there is no hand-written request shape anywhere, which is the
 * point of a schema living in the repository with a build configuration
 * beside it.
 */
import { createClient, type Client } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";

import { UrlsService } from "../gen/urlshortener/v1/urls_pb.ts";

/**
 * gRPC over HTTP/2.
 *
 * This transport is HTTP/2 only — there is no version to choose — and the
 * URL's SCHEME decides whether it is cleartext or TLS. So `http://` here is
 * cleartext HTTP/2, which is what a gRPC call in the cluster needs when the
 * transport identity is off, and `https://` is the same call once it is on.
 * The address carries that decision because the chart renders the address.
 */
export function urlsClient(address: string): Client<typeof UrlsService> {
  return createClient(UrlsService, createGrpcTransport({ baseUrl: address }));
}
