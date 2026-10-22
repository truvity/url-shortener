// Package journey is the url-shortener example's main journeys — create a
// short link, resolve it, read its click count back — written ONCE and
// shared by two callers that ask different questions of the same requests:
//
//   - e2e/suite runs each journey a handful of times
//     against a real install and asserts it behaved correctly (this IS a
//     short link, this redirect IS a 302 to the right place, this counter
//     DID move by exactly two);
//   - e2e/cmd/prober runs the same three, forever,
//     against a release nobody is otherwise calling, so there is signal to
//     read before, during and after a rollout even when no real traffic
//     exists yet.
//
// Both are the ONE library's callers. Before this package existed the suite
// held its own copy of "how to ask urls to create a link" and the prober
// would have needed a second one — which is exactly the drift a shared
// journey exists to rule out: a protocol change that both would need to
// follow, followed by only one of them.
//
// Nothing here knows which install it is talking to, whether it is running
// inside the cluster or reaching it through a port-forward, or what to do
// with a failure — those are the caller's decisions (t.Fatalf and an
// errString for the suite; a metric and a structured log line for the
// prober), and are deliberately not made here.
package journey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"connectrpc.com/connect"

	v1 "github.com/truvity/url-shortener/internal/gen/urlshortener/v1"
	"github.com/truvity/url-shortener/internal/gen/urlshortener/v1/urlshortenerv1connect"
)

// The three journeys' own names — shared between the e2e suite's own
// vocabulary and the prober's metric label, so the two name the same
// exercise the same way rather than by two literals that can drift apart.
const (
	Urls     = "urls"
	Redirect = "redirect"
	Stat     = "stat"
)

// CreateURL shortens longURL through UrlsService/Create and returns the URL
// it created — the "urls" journey. Passing "" for longURL asks the service
// to invent one of its own; every caller here always supplies one, so each
// link it creates is theirs to recognise later.
func CreateURL(ctx context.Context, client urlshortenerv1connect.UrlsServiceClient, longURL string) (*v1.Url, error) {
	resp, err := client.Create(ctx, connect.NewRequest(&v1.CreateRequest{LongUrl: longURL}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetUrl(), nil
}

// ClickCount reads key's click count back through UrlsService/Get — the
// counter's own effect, since stat carries no Service of its own for a
// caller to ask directly. The "stat" journey.
func ClickCount(ctx context.Context, client urlshortenerv1connect.UrlsServiceClient, key string) (int64, error) {
	resp, err := client.Get(ctx, connect.NewRequest(&v1.GetRequest{Key: key}))
	if err != nil {
		return 0, err
	}
	return resp.Msg.GetUrl().GetClickCount(), nil
}

// Resolve asks the redirect service for key and reports what it answered —
// the status code and the Location header — never following the redirect
// itself. The "redirect" journey.
//
// httpClient must be built with CheckRedirect returning
// http.ErrUseLastResponse: following the redirect would answer "what is at
// the long URL", a different question from "did the redirect service answer
// 302 with the right Location" — see
// e2e/suite/redirect_test.go's noRedirectClient for
// the same rule stated where the suite first needed it.
//
// A non-302 status is not an error return: a caller that wants a journey to
// FAIL when the answer is not a 302 checks status itself, the same way the
// suite already did before this call existed — this function only reports
// what happened, on a boundary a caller might reasonably want to treat as a
// close look, not a total refusal.
func Resolve(ctx context.Context, httpClient *http.Client, redirectBase, key string) (location string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, redirectBase+"/r/"+key, http.NoBody)
	if err != nil {
		return "", 0, fmt.Errorf("build the redirect request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.Header.Get("Location"), resp.StatusCode, nil
}

// Web is the front end's own journey: its name beside the others.
const Web = "web"

// ListViaWeb asks the FRONT END for the listing the page shows —
// GET /api/urls — and returns the keys it answered with. The front end makes
// that call to the URL service itself, over whatever transport the release
// runs (an authenticated one when the identity is on), so this is the
// journey that fails when the front end cannot verify, or be verified by, its
// backend: the other journeys call the URL service directly and never go
// through the front end's own client. A non-200 answer is an error here, the
// one place a 502 means the journey is broken rather than a status to report.
func ListViaWeb(ctx context.Context, httpClient *http.Client, webBase string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webBase+"/api/urls?pageSize=100", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build the listing request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the front end answered %d to a listing, wanted %d", resp.StatusCode, http.StatusOK)
	}

	var body struct {
		URLs []struct {
			Key string `json:"key"`
		} `json:"urls"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode the listing: %w", err)
	}

	keys := make([]string, 0, len(body.URLs))
	for _, u := range body.URLs {
		keys = append(keys, u.Key)
	}

	return keys, nil
}

// CreateViaWeb shortens longURL through the FRONT END's POST /api/urls, the
// call the page makes, and returns the key. It is the create journey for a
// caller that reaches a release only through its public host: the URL
// service is not routed, the front end is, and it makes the same
// UrlsService/Create call itself.
func CreateViaWeb(ctx context.Context, httpClient *http.Client, webBase, longURL string) (string, error) {
	body, err := json.Marshal(map[string]string{"longUrl": longURL})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webBase+"/api/urls", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build the create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return "", fmt.Errorf("the front end answered %d to a create, wanted %d: %s", resp.StatusCode, http.StatusCreated, detail)
	}

	var created struct {
		Key     string `json:"key"`
		LongURL string `json:"longUrl"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("decode the created link: %w", err)
	}

	if created.Key == "" {
		return "", fmt.Errorf("the front end created a link with no key")
	}

	return created.Key, nil
}

// ClickCountViaWeb reads key's click count through the front end's
// GET /api/url: the counter's effect, seen from outside (stat has no Service
// to ask).
func ClickCountViaWeb(ctx context.Context, httpClient *http.Client, webBase, key string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webBase+"/api/url?key="+url.QueryEscape(key), http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("build the lookup request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("the front end answered %d to a lookup, wanted %d", resp.StatusCode, http.StatusOK)
	}

	var got struct {
		Clicks int64 `json:"clicks"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return 0, fmt.Errorf("decode the lookup: %w", err)
	}

	return got.Clicks, nil
}
