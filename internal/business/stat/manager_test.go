package stat_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/neilotoole/slogt/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/truvity/policy/examples/url-shortener/internal/business/stat"
	"github.com/truvity/policy/examples/url-shortener/internal/events"
)

type (
	// recordingCounter records every long_url a click was counted against.
	recordingCounter struct {
		counted []string
		err     error
	}
)

func (r *recordingCounter) IncrementClickCount(_ context.Context, longURL string) error {
	r.counted = append(r.counted, longURL)

	return r.err
}

func TestProcessURLRedirectEvent(t *testing.T) {
	tests := []struct {
		name        string
		event       events.URLRedirectEvent
		wantCounted []string
	}{
		{
			name: "a real redirect counts one click against its long_url",
			event: events.URLRedirectEvent{
				URLKey:    "abcd1234",
				LongURL:   "https://example.com/target",
				Timestamp: time.Now(),
			},
			wantCounted: []string{"https://example.com/target"},
		},
		{
			// The guard exists because clicks are keyed by long_url: counting
			// one for "" creates a bogus stat row that belongs to no URL.
			name: "an empty long_url is skipped, not counted",
			event: events.URLRedirectEvent{
				URLKey:    "abcd1234",
				Timestamp: time.Now(),
			},
			wantCounted: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := &recordingCounter{}
			manager := stat.NewManager(context.Background(), slogt.New(t), counter)

			require.NoError(t, manager.ProcessURLRedirectEvent(context.Background(), tt.event))
			assert.Equal(t, tt.wantCounted, counter.counted)
		})
	}
}

// TestProcessURLRedirectEventRejectsForeignPayload feeds the manager the exact
// payload that produced the bug: a URLRequest event body decoded as a
// URLRedirectEvent.
//
// nats-utils marshals Event.Detail alone and drops DetailType, so anything
// landing on the redirect subject decodes into a zero-valued URLRedirectEvent
// — url_key survives (both types carry it), long_url does not. The producer no
// longer emits it, and this guard is the second line of defense for anything
// else that reaches the stream.
func TestProcessURLRedirectEventRejectsForeignPayload(t *testing.T) {
	payload, err := json.Marshal(events.URLRequestEventDetail{
		RequestID:   "req-abcd1234-1",
		RequestType: "redirect",
		URLKey:      "abcd1234",
		Timestamp:   time.Now(),
	})
	require.NoError(t, err)

	var decoded events.URLRedirectEvent
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.Empty(t, decoded.LongURL, "precondition: a URLRequest body carries no long_url")

	counter := &recordingCounter{}
	manager := stat.NewManager(context.Background(), slogt.New(t), counter)

	require.NoError(t, manager.ProcessURLRedirectEvent(context.Background(), decoded))
	assert.Empty(t, counter.counted, "a foreign payload must never increment a click")
}

// TestProcessURLRedirectEventPropagatesStoreError: a genuine store failure has
// to surface, so NATS redelivers rather than silently losing the click.
func TestProcessURLRedirectEventPropagatesStoreError(t *testing.T) {
	counter := &recordingCounter{err: errors.New("connection refused")}
	manager := stat.NewManager(context.Background(), slogt.New(t), counter)

	err := manager.ProcessURLRedirectEvent(context.Background(), events.URLRedirectEvent{
		URLKey:  "abcd1234",
		LongURL: "https://example.com/target",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "increment click count")
}
