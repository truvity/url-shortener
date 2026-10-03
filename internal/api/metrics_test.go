package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func collect(t *testing.T, reader *sdkmetric.ManualReader) metricdata.Histogram[float64] {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == RequestDurationMetric {
				require.Equal(t, "s", m.Unit)
				h, ok := m.Data.(metricdata.Histogram[float64])
				require.True(t, ok)

				return h
			}
		}
	}
	t.Fatalf("no %s recorded", RequestDurationMetric)

	return metricdata.Histogram[float64]{}
}

func attrs(h metricdata.Histogram[float64]) []attribute.Set {
	out := make([]attribute.Set, 0, len(h.DataPoints))
	for _, dp := range h.DataPoints {
		out = append(out, dp.Attributes)
	}

	return out
}

func TestRequestMetricsRecordsTheRouteTemplateAndStatus(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	app := fiber.New()
	app.Use(requestMetrics(provider, "redirect"))
	app.Get("/r/:key", func(c fiber.Ctx) error { return c.SendStatus(http.StatusFound) })
	app.Get("/boom", func(c fiber.Ctx) error { return errors.New("broken") })

	for _, path := range []string{"/r/abc123", "/r/def456", "/boom", "/nowhere"} {
		response, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
	}

	h := collect(t, reader)
	want := map[string]int{"/r/:key|302": 2, "/boom|500": 1, "unmatched|404": 1}
	got := map[string]int{}
	for _, dp := range h.DataPoints {
		method, _ := dp.Attributes.Value("http.request.method")
		require.Equal(t, "GET", method.AsString())
		route, _ := dp.Attributes.Value("http.route")
		status, _ := dp.Attributes.Value("http.response.status_code")
		got[route.AsString()+"|"+status.String()] = int(dp.Count)
		component, _ := dp.Attributes.Value("url_shortener.component")
		require.Equal(t, "redirect", component.AsString())
		// Exactly four attributes: nothing that carries a key or a path.
		require.Equal(t, 4, dp.Attributes.Len())
		require.Equal(t, requestDurationBuckets, dp.Bounds)
	}
	require.Equal(t, want, got)
	require.NotEmpty(t, attrs(h))
}
