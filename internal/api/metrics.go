package api

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

// RequestDurationMetric is the instrument name the OpenTelemetry HTTP
// semantic conventions give a server's request duration. A Prometheus-
// compatible store sees it as http_server_request_duration_seconds_*.
const RequestDurationMetric = "http.server.request.duration"

// requestDurationBuckets are the boundaries the semantic conventions advise
// for this histogram, in seconds. They are set here because the default
// boundaries are sized for milliseconds and put every request in one bucket.
var requestDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

// RequestMetrics records the duration of every request as the semantic
// convention histogram `http.server.request.duration`, with the method, the
// ROUTE TEMPLATE and the response status code.
//
// It is the metric twin of Tracing, and it has the same rule about names: the
// route is the registered template ("/r/:key"), never the path, because a
// metric attribute is a dimension and a path carries the short key. A request
// no route matched is "unmatched", one series however many paths are probed.
//
// The Go HTTP-handler instrumentation (otelhttp) wraps a net/http handler;
// this router is not one, so the same instrument is recorded here. Like
// Tracing it costs almost nothing with no endpoint configured: the global
// meter provider is then a no-op.
func RequestMetrics() fiber.Handler {
	return requestMetrics(otel.GetMeterProvider())
}

func requestMetrics(provider metric.MeterProvider) fiber.Handler {
	meter := provider.Meter("github.com/truvity/policy/examples/url-shortener")
	duration, err := meter.Float64Histogram(RequestDurationMetric,
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests."),
		metric.WithExplicitBucketBoundaries(requestDurationBuckets...),
	)
	if err != nil {
		// Instrument creation fails only on a malformed definition, which is
		// a bug in the constants above, not a condition to serve through.
		panic("api: request duration instrument: " + err.Error())
	}

	return func(c fiber.Ctx) error {
		start := time.Now()
		own := c.Route()
		err := c.Next()

		// An error returned from a handler has not been turned into a
		// response yet (the router's error handler runs after this chain),
		// so the status is read from the error when there is one.
		status := c.Response().StatusCode()
		if err != nil {
			status = fiber.StatusInternalServerError
			var fe *fiber.Error
			if errors.As(err, &fe) {
				status = fe.Code
			}
		}

		duration.Record(c.Context(), time.Since(start).Seconds(), metric.WithAttributes(
			semconv.HTTPRequestMethodKey.String(c.Method()),
			semconv.HTTPRouteKey.String(routeOf(c, own)),
			semconv.HTTPResponseStatusCodeKey.Int(status),
		))

		return err
	}
}

// routeOf is the template of the route that served the request, or
// "unmatched". Read after the handler has run: before it, the only route the
// router has matched is the middleware's own, which is passed as own. A request
// no route matched ends on that same route, and naming it would call every 404
// after the root.
func routeOf(c fiber.Ctx, own *fiber.Route) string {
	if route := c.Route(); route != own && route.Path != "" {
		return route.Path
	}

	return "unmatched"
}
