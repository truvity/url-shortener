package redirect_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/neilotoole/slogt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/truvity/policy/examples/url-shortener/internal/business/redirect"
	"github.com/truvity/policy/examples/url-shortener/internal/events"
)

type (
	// stubResolver answers lookups from a fixed map; a missing key returns the
	// empty string, which is how the real store reports "not found".
	stubResolver struct {
		urls map[string]string
		err  error
	}

	// capturingPublisher records every event handed to it, in order.
	capturingPublisher struct {
		published []events.Event
		calls     int
		err       error
	}
)

func (s stubResolver) GetURLString(_ context.Context, urlKey string) (string, error) {
	if s.err != nil {
		return "", s.err
	}

	return s.urls[urlKey], nil
}

func (p *capturingPublisher) PublishEvent(ctx context.Context, event events.Event) error {
	return p.PublishEvents(ctx, []events.Event{event})
}

func (p *capturingPublisher) PublishEvents(_ context.Context, evts []events.Event) error {
	p.calls++
	if p.err != nil {
		return p.err
	}

	p.published = append(p.published, evts...)

	return nil
}

func (p *capturingPublisher) Shutdown(context.Context) error    { return nil }
func (p *capturingPublisher) HealthCheck(context.Context) error { return nil }

// TestRedirectWithInfoPublishesOnlyDecodableRedirectEvents is the regression
// test for the double-emit bug.
//
// The assertion is deliberately made at the WIRE level rather than by counting
// events: nats-utils marshals Event.Detail alone and drops Source/DetailType
// (publisher.go), and both events used to go to the same subject with an empty
// Subject field. So the stat consumer could not tell them apart — it decoded
// the URLRequest payload as a URLRedirectEvent, got an empty long_url, and
// counted a click for "". What must hold is therefore not "one event" but
// "every payload on this stream decodes into a usable URLRedirectEvent".
//
// Against the old two-event emitter this fails on the URLRequest payload.
func TestRedirectWithInfoPublishesOnlyDecodableRedirectEvents(t *testing.T) {
	publisher := &capturingPublisher{}
	manager := redirect.NewManager(
		slogt.New(t),
		stubResolver{urls: map[string]string{"abcd1234": "https://example.com/target"}},
		publisher,
	)

	longURL, err := manager.RedirectWithInfo(context.Background(), "abcd1234", redirect.RequestInfo{
		ClientIP:  "203.0.113.5",
		UserAgent: "test-agent",
		Referer:   "https://referrer.example",
		StartTime: time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/target", longURL)

	require.NotEmpty(t, publisher.published, "the redirect must emit an event")

	for i, evt := range publisher.published {
		// Reproduce exactly what JetStreamPublisher puts on the wire.
		payload, marshalErr := json.Marshal(evt.Detail)
		require.NoError(t, marshalErr)

		var decoded events.URLRedirectEvent
		require.NoError(t, json.Unmarshal(payload, &decoded),
			"payload %d does not decode as a URLRedirectEvent", i)

		assert.NotEmpty(t, decoded.LongURL,
			"payload %d decodes with an empty long_url — the stat consumer would count a click for \"\"", i)
		assert.Equal(t, "abcd1234", decoded.URLKey)
	}

	// Having established what the stream carries, pin the producer's shape too:
	// one event, correctly typed, so a future second emitter is caught here.
	require.Len(t, publisher.published, 1)
	assert.Equal(t, events.EventDetailTypeURLRedirect, publisher.published[0].DetailType)
}

// TestRedirectWithInfoCarriesRequestInfo checks that the client IP resolved at
// the route (api.ClientIP) reaches the event payload rather than being dropped
// somewhere in between — the reason the XFF handling exists at all.
func TestRedirectWithInfoCarriesRequestInfo(t *testing.T) {
	publisher := &capturingPublisher{}
	manager := redirect.NewManager(
		slogt.New(t),
		stubResolver{urls: map[string]string{"abcd1234": "https://example.com/target"}},
		publisher,
	)

	_, err := manager.RedirectWithInfo(context.Background(), "abcd1234", redirect.RequestInfo{
		ClientIP:  "203.0.113.5",
		UserAgent: "test-agent",
		Referer:   "https://referrer.example",
		StartTime: time.Now(),
	})
	require.NoError(t, err)
	require.Len(t, publisher.published, 1)

	detail, ok := publisher.published[0].Detail.(events.URLRedirectEventDetail)
	require.True(t, ok, "detail is %T", publisher.published[0].Detail)

	assert.Equal(t, "203.0.113.5", detail.Request.ClientIP)
	assert.Equal(t, "test-agent", detail.Request.UserAgent)
	assert.Equal(t, "https://referrer.example", detail.Request.Referer)
	assert.Equal(t, 302, detail.Response.StatusCode)
}

// TestRedirectWithInfoUnknownKey: a miss must not emit anything — an event with
// an empty long_url is exactly what the stat consumer cannot handle.
func TestRedirectWithInfoUnknownKey(t *testing.T) {
	publisher := &capturingPublisher{}
	manager := redirect.NewManager(slogt.New(t), stubResolver{urls: map[string]string{}}, publisher)

	_, err := manager.RedirectWithInfo(context.Background(), "missing0", redirect.RequestInfo{})
	require.Error(t, err)
	assert.Empty(t, publisher.published)
}

// TestRedirectWithInfoSurvivesPublishFailure: events are best-effort telemetry,
// so a publish error must not turn a working redirect into a 404.
func TestRedirectWithInfoSurvivesPublishFailure(t *testing.T) {
	publisher := &capturingPublisher{err: errors.New("jetstream unavailable")}
	manager := redirect.NewManager(
		slogt.New(t),
		stubResolver{urls: map[string]string{"abcd1234": "https://example.com/target"}},
		publisher,
	)

	longURL, err := manager.RedirectWithInfo(context.Background(), "abcd1234", redirect.RequestInfo{})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/target", longURL)
	assert.Equal(t, 1, publisher.calls, "the redirect still attempts the emission")
}
