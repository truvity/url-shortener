package suite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1"
)

// TestStatMovesTheCounter proves the part hack/smoke.sh existed to prove:
// a redirect publishes an event, the broker keeps it, stat's consumer is
// bound to the right subject, and it counts by calling UrlsService/
// RecordClick RPC rather than writing the table itself — the counter holds
// no database credential any more, so the ONLY way this number moves is
// through the boundary urls owns.
//
// Read through the SAME Service the counter itself asks — stat has none of
// its own; this is its effect, not its endpoint.
func TestStatMovesTheCounter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client := urlsClient(ctx, t)
	longURL := testLongURL(t)
	created, err := client.Create(ctx, connect.NewRequest(&v1.CreateRequest{LongUrl: longURL}))
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create the URL under test", err))
	}
	key := created.Msg.GetUrl().GetKey()

	// Two redirects, not one: the assertion below is an exact count, which
	// is the difference between "the counter counts" and "a row exists".
	const wantClicks = 2
	for range wantClicks {
		_ = followRedirect(ctx, t, key)
	}

	eventually(t, 60*time.Second, func() error {
		resp, err := client.Get(ctx, connect.NewRequest(&v1.GetRequest{Key: key}))
		if err != nil {
			return errString(componentURLs, "get the click count", err)
		}
		if got := resp.Msg.GetUrl().GetClickCount(); got != wantClicks {
			return fmt.Errorf("click_count is %d, want exactly %d", got, wantClicks)
		}
		return nil
	})
}
