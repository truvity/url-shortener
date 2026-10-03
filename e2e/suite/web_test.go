package suite

import (
	"context"
	"testing"
	"time"

	"github.com/truvity/policy/examples/url-shortener/e2e/journey"
)

// TestWebListsThroughTheURLService proves the front end's own path to the URL
// service: web is asked for the listing the page shows, and web itself calls
// the URL service — over the authenticated transport when the release runs
// one — to answer.
//
// It is the only test that goes through web's OWN client. Every other journey
// calls the URL service from here, with this suite's own identity, so none of
// them can see a front end that cannot verify the service's certificate or
// present its own: a release where that was true passed every journey and
// answered 502 to every listing a person asked for. A 502 here is exactly that
// failure, so it fails the suite and the release verification with it.
//
// Deliberately not an assertion about WHICH links the listing holds: a shared
// install is not empty, and the listing is a page. The question is whether the
// front end could ask at all, which is answered by a 200 with a decodable
// body after a link this test created exists to be listed.
func TestWebListsThroughTheURLService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := journey.CreateURL(ctx, urlsClient(ctx, t), testLongURL(t)); err != nil {
		t.Fatalf("%s", errString(componentURLs, "create a link for web to list", err))
	}

	keys, err := journey.ListViaWeb(ctx, httpClient(true), serviceURL(ctx, t, componentWeb))
	if err != nil {
		t.Fatalf("%s", errString(componentWeb, "GET /api/urls", err))
	}
	if len(keys) == 0 {
		t.Fatalf("web listed no links although one was just created: it reached the URL service and found nothing")
	}
}
