package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/truvity/policy/examples/url-shortener/internal/api"
)

func TestSpanIsNamedForTheRouteThatServedTheRequest(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(trace.NewTracerProvider(trace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	app := fiber.New()
	app.Use(api.Tracing())
	app.Get("/r/:key", func(c fiber.Ctx) error { return c.SendStatus(http.StatusFound) })

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/r/abc123", nil))
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, response.StatusCode)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	// The ROUTE, not "GET /" (the middleware's own route, which is all a
	// span named at the start can know) and not the path, which carries
	// the key.
	require.Equal(t, "GET /r/:key", spans[0].Name())
}
