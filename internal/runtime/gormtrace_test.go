package runtime_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/truvity/policy/examples/url-shortener/internal/runtime"
)

type row struct {
	ID      string
	LongURL string
}

func TestQueriesAreChildrenOfTheRequestAndCarryNoValues(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	// DryRun: no server is needed to prove what a query's span looks like.
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1 dbname=x"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)
	require.NoError(t, runtime.TraceDatabase(db))

	ctx, request := provider.Tracer("test").Start(context.Background(), "request")
	var found row
	_ = db.WithContext(ctx).Where("id = ?", "secret-long-url").First(&found).Error
	request.End()

	var query trace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() != "request" {
			query = s
		}
	}
	require.NotNil(t, query, "the query made no span at all")
	require.Equal(t, request.SpanContext().SpanID(), query.Parent().SpanID(),
		"the query span is not a child of the request")
	var text string
	for _, attribute := range query.Attributes() {
		if attribute.Key == "db.query.text" {
			text = attribute.Value.AsString()
		}
	}
	require.Contains(t, text, "SELECT", "the statement text is what says which query was slow")
	for _, attribute := range query.Attributes() {
		require.NotContains(t, attribute.Value.AsString(), "secret-long-url",
			"query value %s leaked into a span", attribute.Key)
	}
}
