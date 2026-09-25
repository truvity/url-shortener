package events_test

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/truvity/policy/examples/url-shortener/internal/events"
)

type capture struct {
	jetstream.JetStream
	sent []*nats.Msg
}

func (c *capture) PublishMsg(_ context.Context, m *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	c.sent = append(c.sent, m)
	return &jetstream.PubAck{}, nil
}

func TestPublishedEventsCarryTheRequestsTrace(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previous) })

	provider := trace.NewTracerProvider()
	ctx, span := provider.Tracer("test").Start(context.Background(), "request")
	defer span.End()

	js := &capture{}
	err := events.NewJetStream(js).PublishEvents(ctx, []events.Event{
		{Subject: "s", DetailType: "T", Detail: map[string]string{"k": "v"}},
	})
	require.NoError(t, err)
	require.Len(t, js.sent, 1)

	// The name a Kotlin or Python consumer reads, exactly: NATS header
	// names are case-sensitive, so `Traceparent` would be invisible to it.
	require.Contains(t, js.sent[0].Header, "traceparent")

	// What a consumer would extract: the publisher's request, as the parent.
	carrier := propagation.MapCarrier{}
	for name, values := range js.sent[0].Header {
		carrier[name] = values[0]
	}
	got := propagation.TraceContext{}.Extract(context.Background(), carrier)
	require.Equal(t, span.SpanContext().TraceID(), oteltrace.SpanContextFromContext(got).TraceID())
	require.Equal(t, span.SpanContext().SpanID(), oteltrace.SpanContextFromContext(got).SpanID())
	require.Equal(t, "T", js.sent[0].Header.Get(events.HeaderDetailType), "the existing headers must survive")
}
