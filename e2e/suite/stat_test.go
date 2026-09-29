package suite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/truvity/policy/examples/url-shortener/e2e/journey"
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
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	client := urlsClient(ctx, t)
	longURL := testLongURL(t)
	created, err := journey.CreateURL(ctx, client, longURL)
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create the URL under test", err))
	}
	key := created.GetKey()

	// Two redirects, not one: the assertion below is an exact count, which
	// is the difference between "the counter counts" and "a row exists".
	const redirects = 2
	for range redirects {
		_ = followRedirect(ctx, t, key)
	}

	// want is read by BOTH the comparison and the failure message below, so
	// the two cannot drift apart — a literal repeated at each site is a
	// message that can go on stating the old expectation after the
	// comparison changes, which is exactly the bug a mutation check here
	// once caught: "click_count is 2, want exactly 2" for a test that was
	// failing because 2 was not what it wanted.
	want := int64(redirects)
	eventually(t, 60*time.Second, func() error {
		got, err := journey.ClickCount(ctx, client, key)
		if err != nil {
			return errString(componentURLs, "get the click count", err)
		}
		if got != want {
			return fmt.Errorf("click_count is %d, want exactly %d", got, want)
		}
		return nil
	})

	// Reading the wanted number once proves the counter moves; it cannot
	// prove each click was counted ONCE. The consumer acknowledges a
	// message only after a successful call, and an unacknowledged one is
	// redelivered after its ack wait (30s) and counted again — so a click
	// counted twice reads right now and wrong half a minute later. Hold the
	// count for twice the ack wait and require it not to move.
	const settle = 60 * time.Second
	for stop := time.Now().Add(settle); time.Now().Before(stop); time.Sleep(time.Second) {
		got, err := journey.ClickCount(ctx, client, key)
		if err != nil {
			continue
		}
		if got != want {
			t.Fatalf("click_count moved to %d within %s of reading %d: a click was counted more than once (a redelivery)", got, settle, want)
		}
	}
}
