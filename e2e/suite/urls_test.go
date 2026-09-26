package suite

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1"
)

// TestUrlsCreateGeneratesAKey proves what hack/smoke.sh never asked: that a
// caller who omits a key gets one back, and that it is the shape the front
// end and the redirect route both hold a key to (KeyLength, urls.KeyLength).
func TestUrlsCreateGeneratesAKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := urlsClient(ctx, t)
	longURL := testLongURL(t)

	resp, err := client.Create(ctx, connect.NewRequest(&v1.CreateRequest{LongUrl: longURL}))
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create with no key", err))
	}

	key := resp.Msg.GetUrl().GetKey()
	if len(key) != 8 {
		t.Fatalf("a generated key was %q (%d characters), wanted exactly 8", key, len(key))
	}
	if resp.Msg.GetUrl().GetLongUrl() != longURL {
		t.Fatalf("Create echoed long_url %q, wanted %q", resp.Msg.GetUrl().GetLongUrl(), longURL)
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

	first, err := client.Create(ctx, connect.NewRequest(&v1.CreateRequest{LongUrl: longURL}))
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create (first)", err))
	}

	second, err := client.Create(ctx, connect.NewRequest(&v1.CreateRequest{LongUrl: longURL}))
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create (second, same URL)", err))
	}

	if first.Msg.GetUrl().GetKey() != second.Msg.GetUrl().GetKey() {
		t.Fatalf("shortening the same URL twice returned %q then %q, wanted the same key both times",
			first.Msg.GetUrl().GetKey(), second.Msg.GetUrl().GetKey())
	}
}
