/**
 * The client for the service that owns the tables.
 *
 * ONE plugin generated the descriptors this imports, and the client is built
 * from them — there is no hand-written request shape anywhere, which is the
 * point of a schema living in the repository with a build configuration
 * beside it.
 */
import { createClient, type Client, type Interceptor } from "@connectrpc/connect";
import { context, propagation, SpanKind, SpanStatusCode, trace } from "@opentelemetry/api";
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
  return createClient(
    UrlsService,
    createGrpcTransport({ baseUrl: address, interceptors: [tracing] }),
  );
}

/**
 * One CLIENT span per call, and the trace context on the wire.
 *
 * Without the header the callee starts a trace of its own: the store fills
 * with two half-traces per request, each looking complete, and nothing shows
 * that one caused the other. The span is named for the procedure, never the
 * arguments, because a span name is a dimension.
 */
export const tracing: Interceptor = (next) => async (req) => {
  const tracer = trace.getTracer("url-shortener-web");
  const span = tracer.startSpan(
    `${req.service.typeName}/${req.method.name}`,
    {
      kind: SpanKind.CLIENT,
      attributes: {
        "rpc.system": "grpc",
        "rpc.service": req.service.typeName,
        "rpc.method": req.method.name,
      },
    },
    context.active(),
  );
  propagation.inject(trace.setSpan(context.active(), span), req.header, {
    set: (carrier, key, value) => carrier.set(key, value),
  });
  try {
    return await context.with(trace.setSpan(context.active(), span), () => next(req));
  } catch (error) {
    span.recordException(error as Error);
    span.setStatus({ code: SpanStatusCode.ERROR });
    throw error;
  } finally {
    span.end();
  }
};
