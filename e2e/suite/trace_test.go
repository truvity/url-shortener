package suite

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/truvity/policy/examples/url-shortener/e2e/journey"
	"github.com/truvity/policy/examples/url-shortener/e2e/traceattrs"
	"github.com/truvity/policy/examples/url-shortener/e2e/traceauth"
)

// TestRedirectTraceCrossesEveryComponent proves the ONE thing none of the
// other tests can: that a single request is a single TRACE across every
// hop it touches — and, on the same trace, that every span attribute
// exported from that hop is one its own service was configured to allow
// (see traceattrs.ServiceExtensions and traceattrs.CheckAttributes),
// which is telemetry/attributes.go's own claim ("an attribute nobody
// allow-listed is exported") proved from OUTSIDE the process rather than
// by that package's own unit tests alone.
//
// The real call graph of one GET /r/{key}, read off the code rather than
// assumed:
//
//   - redirect answers the request itself — internal/business/redirect/
//     manager.go's Manager.RedirectWithInfo reads the long URL from ITS OWN
//     store (URLResolver.GetURLString); there is no synchronous RPC to urls
//     on this path.
//   - the SAME handler publishes a URLRedirect event to the broker before it
//     answers (manager.go's emitRedirectEvent), and — because every Huma
//     operation is wrapped in NewURLRequestMiddleware
//     (internal/middleware's doc comment, wired in
//     internal/business/redirect/routes.go's RegisterHumaRoutes) — a
//     URLRequest event too, once the handler returns.
//   - stat's consumer (stat/src/.../Stat.kt's `consumed`) continues that
//     trace as a CONSUMER span per URLRedirect message, and — still inside
//     it — calls UrlsService/RecordClick on urls (Stat.kt's recordClick,
//     dispatched through an executor wrapped in Context.taskWrapping so the
//     CLIENT span keeps the same parent). That RPC is the one and only way
//     urls enters a redirect's trace.
//   - log's consumer (log/src/.../tracing.py's `receive`) does the same for
//     the URLRequest message: one child span per message, continuing the
//     SAME trace. Its own write (archive.flush) is only LINKED to that span,
//     not a child of it — log holds a batch of messages from many requests
//     and cannot make the write a child of any single one of them
//     (tracing.py's own doc comment) — so the flush lands in a trace of its
//     own, not this one; only the per-message "receive" span is asserted
//     below.
//
// So redirect, stat and urls are asserted here: all three finish within the
// request/consume/RPC chain above, no batching involved. log's "receive"
// span belongs to the same trace too, but it is exported only once its
// batch flushes — bounded by archive.batch.maxSeconds, a value this test
// does not control and which varies by tier (e.g. hack/install.sh sets it
// to 5s for kind-shaped installs) — so asserting it here would tie this
// test's timeout to a chart value it has no way to read. That is a gap in
// coverage, not evidence log's context is broken; TestLogArchivesTheRecord
// already proves log processes the right record, just not on this trace's
// clock.
//
// The kind box carries no trace store (docs/decisions/0005-kind-is-the-gate.md's
// amendment: cloud and platform integrations are switched off there), so
// this is a t.Skip everywhere E2E_TRACES_URL is unset — which is every run
// on kind today. It is meant to run unchanged wherever a trace store IS
// reachable: a private repository's shared cluster, or after a promotion.
//
// A trace store that authenticates its readers is handled the same way —
// nothing here decides on its own; see env_test.go's envTracesTokenURL,
// envTracesClient and envTracesTokenFile doc comment and
// examples/url-shortener/e2e/traceauth for the RFC 8693 exchange this
// triggers when they are set. All three unset (the kind tier's own case,
// and any tier whose trace store admits anonymous readers) sends the same
// unauthenticated request this test always sent.
func TestRedirectTraceCrossesEveryComponent(t *testing.T) {
	if shared.tracesURL == "" {
		t.Skipf("%s is not set — no trace store on this tier", envTracesURL)
	}

	// Long enough to outlast the 30s polling bound below with margin for
	// setup, PLUS a token exchange — see log_test.go's identical
	// reasoning for the base budget.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	bearer, httpClient, err := tracesAuth(ctx)
	if err != nil {
		t.Fatalf("set up the trace store's credentials: %v", err)
	}

	// A real short link. GET /r/{key} is what runs the graph above — GET
	// /version does not: it is registered directly on the fiber app (see
	// cmd/redirect/main.go), never through humaAPI, so
	// NewURLRequestMiddleware never wraps it and it publishes no event at
	// all. A trace seeded there can carry a "redirect" span and nothing
	// downstream of it, structurally, no matter how long this test waits.
	client := urlsClient(ctx, t)
	longURL := testLongURL(t)
	created, err := journey.CreateURL(ctx, client, longURL)
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create the URL under test", err))
	}
	key := created.GetKey()

	traceID, err := randomTraceID()
	if err != nil {
		t.Fatalf("draw a trace id: %v", err)
	}

	base := serviceURL(ctx, t, componentRedirect, httpPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/r/"+key, http.NoBody)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	// A hand-built W3C traceparent, so this test chooses the trace id rather
	// than discovering one after the fact — internal/api/tracing.go
	// extracts an incoming trace context on every request, which is the
	// property this asserts by using it.
	req.Header.Set("traceparent", fmt.Sprintf("00-%s-%s-01", traceID, randomSpanID(t)))

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("%s", errString(componentRedirect, "GET /r/"+key+" (to seed the trace)", err))
	}
	_ = resp.Body.Close()

	// Captured by the closure below and read again once eventually
	// returns, so the attribute check after it runs on the SAME fetch that
	// proved every service present — never a second query for the same
	// trace.
	var trace traceattrs.Response

	eventually(t, 30*time.Second, func() error {
		fetched, err := fetchTrace(ctx, httpClient, shared.tracesURL, bearer, traceID)
		if err != nil {
			return err
		}
		trace = fetched

		services := trace.Services()
		var missing []string
		for _, want := range []string{componentRedirect, componentStat, componentURLs} {
			if !anyContains(services, want) {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("trace %s carries services %v, missing %v", traceID, services, missing)
		}
		return nil
	})

	// The other half of this test's claim: not just that redirect, stat
	// and urls each contributed a span, but that none of those spans (or
	// any other service's, on the same trace) carries an attribute its own
	// telemetry setup was never configured to let through — see
	// traceattrs.ServiceExtensions for where each service's own allow-list
	// comes from and traceattrs' storeAddedTags for the handful of tags
	// this exempts as the trace store's own, not any service's.
	if offenses := traceattrs.CheckAttributes(trace, traceattrs.ServiceExtensions); len(offenses) > 0 {
		lines := make([]string, len(offenses))
		for i, offense := range offenses {
			lines[i] = offense.String()
		}
		t.Fatalf("span attributes outside the allow-list:\n%s", strings.Join(lines, "\n"))
	}
}

// tracesAuth builds what fetchTrace needs to reach shared.tracesURL: a
// bearer token (empty when the store needs none) and the http.Client to
// send the request with — carrying shared.tracesCAFile's bundle when one
// was given, on top of the process's own default trust store (see
// traceauth.HTTPClient's own doc comment).
//
// The exchange itself (traceauth.Exchange) is skipped entirely when
// shared.tracesTokenURL is unset — that is what "this store needs no
// auth" means here, and it is also every run on the kind tier today.
func tracesAuth(ctx context.Context) (bearer string, httpClient *http.Client, err error) {
	httpClient, err = traceauth.HTTPClient(shared.tracesCAFile)
	if err != nil {
		return "", nil, fmt.Errorf("build the traces http.Client: %w", err)
	}

	if shared.tracesTokenURL == "" {
		return "", httpClient, nil
	}

	bearer, err = traceauth.Exchange(ctx, traceauth.Config{
		TokenURL:  shared.tracesTokenURL,
		Client:    shared.tracesClient,
		TokenFile: shared.tracesTokenFile,
	})
	if err != nil {
		return "", nil, fmt.Errorf("exchange for a traces bearer token: %w", err)
	}
	return bearer, httpClient, nil
}

func randomTraceID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomSpanID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("draw a span id: %v", err)
	}
	return hex.EncodeToString(b)
}

func anyContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// fetchTrace fetches a trace by id from a Jaeger-API query endpoint,
// parsed into the shape traceattrs.CheckAttributes and Response.Services
// both read. bearer is sent as an Authorization header only when it is
// non-empty — see tracesAuth's own doc comment for when that is.
//
// Called exactly once per eventually poll and never again afterwards —
// TestRedirectTraceCrossesEveryComponent keeps the last parsed Response
// and runs its attribute check on THAT, rather than fetching the same
// trace a second time once the service-presence check above has already
// succeeded.
func fetchTrace(ctx context.Context, httpClient *http.Client, tracesURL, bearer, traceID string) (traceattrs.Response, error) {
	url := strings.TrimRight(tracesURL, "/") + "/api/traces/" + traceID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return traceattrs.Response{}, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return traceattrs.Response{}, fmt.Errorf("query %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return traceattrs.Response{}, fmt.Errorf("query %s: answered %d", url, resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return traceattrs.Response{}, err
	}

	var parsed traceattrs.Response
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return traceattrs.Response{}, fmt.Errorf("parse the trace response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return traceattrs.Response{}, fmt.Errorf("no trace %s yet", traceID)
	}

	return parsed, nil
}
