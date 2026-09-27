package suite

import (
	"context"
	"testing"
	"time"

	"github.com/truvity/policy/examples/url-shortener/e2e/journey"
)

// TestUrlsCreateGeneratesAKey proves what hack/smoke.sh never asked: that a
// caller who omits a key gets one back, and that it is the shape the front
// end and the redirect route both hold a key to (KeyLength, urls.KeyLength).
func TestUrlsCreateGeneratesAKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	longURL := testLongURL(t)

	url, err := journey.CreateURL(ctx, urlsClient(ctx, t), longURL)
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create with no key", err))
	}

	key := url.GetKey()
	if len(key) != 8 {
		t.Fatalf("a generated key was %q (%d characters), wanted exactly 8", key, len(key))
	}
	if url.GetLongUrl() != longURL {
		t.Fatalf("Create echoed long_url %q, wanted %q", url.GetLongUrl(), longURL)
	}
}

// TestUrlsCreateIsIdempotentOnTheSameURL proves the rule
// internal/business/urls/handler.go's decideCreate states: shortening the
// SAME long URL twice is not an error, it is the same link, answered both
// times with the same key.
func TestUrlsCreateIsIdempotentOnTheSameURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := urlsClient(ctx, t)
	longURL := testLongURL(t)

	first, err := journey.CreateURL(ctx, client, longURL)
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create (first)", err))
	}

	second, err := journey.CreateURL(ctx, client, longURL)
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create (second, same URL)", err))
	}

	if first.GetKey() != second.GetKey() {
		t.Fatalf("shortening the same URL twice returned %q then %q, wanted the same key both times",
			first.GetKey(), second.GetKey())
	}
}
