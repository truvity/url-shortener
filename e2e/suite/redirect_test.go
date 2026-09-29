package suite

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/truvity/policy/examples/url-shortener/e2e/journey"
)

// TestRedirectAnswers302 proves the redirect service answers with the long
// URL it was given, through its own Service — the question
// hack/smoke.sh asked with curl, asked here with a Go client that refuses
// to follow the redirect so it can inspect it.
func TestRedirectAnswers302(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	longURL := testLongURL(t)
	created, err := journey.CreateURL(ctx, urlsClient(ctx, t), longURL)
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create the URL under test", err))
	}
	key := created.GetKey()

	location := followRedirect(ctx, t, key)
	if location != longURL {
		t.Fatalf("redirected to %q, wanted %q", location, longURL)
	}
}

// noRedirectClient never follows a redirect, so a 302's own status and
// Location header are what the caller sees — following it would answer
// "what is at the long URL", which is a different question from "did the
// redirect service answer 302 with the right Location". A function, not a
// variable: it carries the identity, which is loaded after package
// initialisation (see httpClient).
func noRedirectClient() *http.Client { return httpClient(false) }

// followRedirect asks the redirect Service for key and returns the
// Location header of the 302 it must answer with — the "redirect" journey,
// shared with examples/url-shortener/e2e/cmd/prober via
// examples/url-shortener/e2e/journey.
func followRedirect(ctx context.Context, t *testing.T, key string) string {
	t.Helper()

	base := serviceURL(ctx, t, componentRedirect)
	location, status, err := journey.Resolve(ctx, noRedirectClient(), base, key)
	if err != nil {
		t.Fatalf("%s", errString(componentRedirect, "GET /r/"+key, err))
	}

	if status != http.StatusFound {
		t.Fatalf("the redirect answered %d, wanted %d", status, http.StatusFound)
	}

	return location
}
