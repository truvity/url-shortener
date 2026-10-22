package suite

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/truvity/url-shortener/e2e/journey"
)

// TestCreateGeneratesAKey: a caller who omits a key gets one back, in the
// shape the front end and the redirect route both hold a key to.
func TestCreateGeneratesAKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key, err := journey.CreateViaWeb(ctx, httpClient(true), shared.base, testLongURL(t))
	if err != nil {
		t.Fatalf("create with no key: %v", err)
	}

	if len(key) != 8 {
		t.Fatalf("a generated key was %q (%d characters), wanted exactly 8", key, len(key))
	}
}

// TestCreateIsIdempotentOnTheSameURL: shortening the SAME long URL twice is
// not an error, it is the same link, answered both times with the same key.
func TestCreateIsIdempotentOnTheSameURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := httpClient(true)
	longURL := testLongURL(t)

	first, err := journey.CreateViaWeb(ctx, client, shared.base, longURL)
	if err != nil {
		t.Fatalf("create (first): %v", err)
	}

	second, err := journey.CreateViaWeb(ctx, client, shared.base, longURL)
	if err != nil {
		t.Fatalf("create (second, same URL): %v", err)
	}

	if first != second {
		t.Fatalf("shortening the same URL twice returned %q then %q, wanted the same key both times", first, second)
	}
}

// TestRedirectAnswers302 proves the redirect service answers with the long
// URL it was given.
func TestRedirectAnswers302(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	longURL := testLongURL(t)

	key, err := journey.CreateViaWeb(ctx, httpClient(true), shared.base, longURL)
	if err != nil {
		t.Fatalf("create the URL under test: %v", err)
	}

	if location := followRedirect(ctx, t, key); location != longURL {
		t.Fatalf("redirected to %q, wanted %q", location, longURL)
	}
}

// followRedirect asks the redirect service for key and returns the Location
// of the 302 it must answer with.
func followRedirect(ctx context.Context, t *testing.T, key string) string {
	t.Helper()

	location, status, err := journey.Resolve(ctx, httpClient(false), shared.base, key)
	if err != nil {
		t.Fatalf("GET /r/%s: %v", key, err)
	}

	if status != http.StatusFound {
		t.Fatalf("the redirect answered %d, wanted %d", status, http.StatusFound)
	}

	return location
}

// TestStatMovesTheCounter proves the part the cluster exists for: a redirect
// publishes an event, the broker keeps it, stat's consumer is bound to the
// right subject, and it counts by calling UrlsService/RecordClick rather than
// writing the table itself. Read through the front end, which asks the same
// service the counter does; stat has no endpoint of its own.
func TestStatMovesTheCounter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	client := httpClient(true)

	key, err := journey.CreateViaWeb(ctx, client, shared.base, testLongURL(t))
	if err != nil {
		t.Fatalf("create the URL under test: %v", err)
	}

	// Two redirects, not one: the assertion is an exact count, which is the
	// difference between "the counter counts" and "a row exists".
	const redirects = 2

	for range redirects {
		_ = followRedirect(ctx, t, key)
	}

	want := int64(redirects)

	eventually(t, 90*time.Second, func() error {
		got, err := journey.ClickCountViaWeb(ctx, client, shared.base, key)
		if err != nil {
			return err
		}

		if got != want {
			return fmt.Errorf("click_count is %d, want exactly %d", got, want)
		}

		return nil
	})

	// Reading the wanted number once proves the counter moves; it cannot
	// prove each click was counted ONCE. The consumer acknowledges a message
	// only after a successful call, and an unacknowledged one is redelivered
	// after its ack wait (30s) and counted again. Hold the count for twice the
	// ack wait and require it not to move.
	const settle = 60 * time.Second

	for stop := time.Now().Add(settle); time.Now().Before(stop); time.Sleep(time.Second) {
		got, err := journey.ClickCountViaWeb(ctx, client, shared.base, key)
		if err != nil {
			continue
		}

		if got != want {
			t.Fatalf("click_count moved to %d within %s of reading %d: a click was counted more than once (a redelivery)", got, settle, want)
		}
	}
}

// TestWebListsThroughTheURLService proves the front end's own path to the URL
// service: a 502 here is a front end that cannot reach, or be verified by, its
// backend, which every journey that calls the URL service from outside would
// miss. It is not an assertion about WHICH links the listing holds: a shared
// namespace is not empty and the listing is a page.
func TestWebListsThroughTheURLService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := httpClient(true)

	if _, err := journey.CreateViaWeb(ctx, client, shared.base, testLongURL(t)); err != nil {
		t.Fatalf("create a link for the front end to list: %v", err)
	}

	keys, err := journey.ListViaWeb(ctx, client, shared.base)
	if err != nil {
		t.Fatalf("GET /api/urls: %v", err)
	}

	if len(keys) == 0 {
		t.Fatal("the front end listed no links although one was just created")
	}
}

// TestTheGatewayRefusesAnUnauthenticatedCall: the release's host is gated.
// Only meaningful once the installs gate is on, so it asks for it:
// E2E_EXPECT_GATE=true (the workflow sets it from the repository variable of
// the same name once the gate is switched on).
func TestTheGatewayRefusesAnUnauthenticatedCall(t *testing.T) {
	if !expectGate() {
		t.Skip("E2E_EXPECT_GATE is not true: the installs gate is not switched on yet")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shared.base+"/r/_health_", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a call with no token answered %d, wanted 401 or 403", resp.StatusCode)
	}
}
