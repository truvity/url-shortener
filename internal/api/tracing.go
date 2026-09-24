package api

import (
	"github.com/gofiber/fiber/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
)

// Tracing starts a span per request and continues an incoming trace.
//
// Written here rather than taken from a library because the published
// middleware for this router targets its previous major version, and a
// span per request is twenty lines. What it must get right is small and
// worth stating:
//
//   - The INCOMING context is extracted before the span starts. Without
//     that every service begins its own trace and the store fills with
//     one-span traces that look like success while telling you nothing
//     about which hop was slow.
//   - The span name is the ROUTE, never the path. A path carries the
//     short key, the id, the customer — and a span name is a dimension,
//     so naming spans by path is how a trace store runs out of memory.
//   - A 5xx marks the span as an error. Without it, a trace that failed
//     looks exactly like one that did not, and the only way to find the
//     failures is to read them all.
//
// It exports nothing by itself: with no endpoint configured the global
// provider is a no-op and this costs an allocation and a map lookup.
func Tracing() fiber.Handler {
	tracer := otel.Tracer("github.com/truvity/policy/examples/url-shortener")
	propagator := otel.GetTextMapPropagator()

	return func(c fiber.Ctx) error {
		ctx := propagator.Extract(c.Context(), propagation.HeaderCarrier(headersOf(c)))

		route := c.Route().Path
		if route == "" {
			route = "unmatched"
		}

		ctx, span := tracer.Start(ctx, c.Method()+" "+route,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Method()),
				semconv.HTTPRouteKey.String(route),
			),
		)
		defer span.End()

		c.SetContext(ctx)

		err := c.Next()

		status := c.Response().StatusCode()
		span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(status))
		if status >= fiber.StatusInternalServerError {
			span.SetStatus(codes.Error, "")
		}
		if err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.Bool("error", true))
		}

		return err
	}
}

// headersOf copies the request headers into the shape the propagator
// reads. Fiber's own header type is not a http.Header.
func headersOf(c fiber.Ctx) map[string][]string {
	out := map[string][]string{}
	for key, values := range c.Request().Header.All() {
		out[string(key)] = append(out[string(key)], string(values))
	}

	return out
}
