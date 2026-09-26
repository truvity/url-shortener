package suite

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1"
)

// TestRedirectAnswers302 proves the redirect service answers with the long
// URL it was given, through its own Service — the question
// hack/smoke.sh asked with curl, asked here with a Go client that refuses
// to follow the redirect so it can inspect it.
func TestRedirectAnswers302(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	longURL := testLongURL(t)
	created, err := urlsClient(ctx, t).Create(ctx, connect.NewRequest(&v1.CreateRequest{LongUrl: longURL}))
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create the URL under test", err))
	}
	key := created.Msg.GetUrl().GetKey()

	location := followRedirect(ctx, t, key)
	if location != longURL {
		t.Fatalf("redirected to %q, wanted %q", location, longURL)
	}
}

// noRedirectClient never follows a redirect, so a 302's own status and
// Location header are what the caller sees — following it would answer
// "what is at the long URL", which is a different question from "did the
// redirect service answer 302 with the right Location".
var noRedirectClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// followRedirect asks the redirect Service for key and returns the
// Location header of the 302 it must answer with.
func followRedirect(ctx context.Context, t *testing.T, key string) string {
	t.Helper()

	base := serviceURL(ctx, t, componentRedirect, httpPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/r/"+key, http.NoBody)
	if err != nil {
		t.Fatalf("build the redirect request: %v", err)
	}

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("%s", errString(componentRedirect, "GET /r/"+key, err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("the redirect answered %d, wanted %d", resp.StatusCode, http.StatusFound)
	}

	return resp.Header.Get("Location")
}
