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
)

// TestRedirectTraceCrossesEveryComponent proves the ONE thing none of the
// other tests can: that a single request is a single TRACE across every
// hop it touches — redirect, the broker, stat and log's own consumers, and
// urls, which stat calls back into (docs/decisions on tracing; see
// internal/api/tracing.go's doc comment on why the incoming context is
// extracted before every span starts).
//
// The kind box carries no trace store (docs/decisions/0005-kind-is-the-gate.md's
// amendment: cloud and platform integrations are switched off there), so
// this is a t.Skip everywhere E2E_TRACES_URL is unset — which is every run
// on kind today. It is meant to run unchanged wherever a trace store IS
// reachable: a private repository's shared cluster, or after a promotion.
func TestRedirectTraceCrossesEveryComponent(t *testing.T) {
	if shared.tracesURL == "" {
		t.Skipf("%s is not set — no trace store on this tier", envTracesURL)
	}

	// Long enough to outlast the 30s polling bound below with margin for
	// setup — see log_test.go's identical reasoning.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	traceID, err := randomTraceID()
	if err != nil {
		t.Fatalf("draw a trace id: %v", err)
	}

	base := serviceURL(ctx, t, componentRedirect, httpPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/version", http.NoBody)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	// A hand-built W3C traceparent, so this test chooses the trace id rather
	// than discovering one after the fact — internal/api/tracing.go
	// extracts an incoming trace context on every request, which is the
	// property this asserts by using it.
	req.Header.Set("traceparent", fmt.Sprintf("00-%s-%s-01", traceID, randomSpanID(t)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s", errString(componentRedirect, "GET /version (to seed the trace)", err))
	}
	_ = resp.Body.Close()

	eventually(t, 30*time.Second, func() error {
		services, err := traceServices(ctx, shared.tracesURL, traceID)
		if err != nil {
			return err
		}
		var missing []string
		for _, want := range []string{componentRedirect, componentURLs} {
			if !anyContains(services, want) {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("trace %s carries services %v, missing %v", traceID, services, missing)
		}
		return nil
	})
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

// jaegerTraceResponse is the small subset of Jaeger's own JSON query API
// (GET {tracesURL}/api/traces/{traceID}) this test reads: which services
// contributed a span to the trace.
type jaegerTraceResponse struct {
	Data []struct {
		Spans []struct {
			ProcessID string `json:"processID"`
		} `json:"spans"`
		Processes map[string]struct {
			ServiceName string `json:"serviceName"`
		} `json:"processes"`
	} `json:"data"`
}

// traceServices fetches a trace by id from a Jaeger-API query endpoint and
// returns the distinct service names that contributed a span to it.
func traceServices(ctx context.Context, tracesURL, traceID string) ([]string, error) {
	url := strings.TrimRight(tracesURL, "/") + "/api/traces/" + traceID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query %s: answered %d", url, resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed jaegerTraceResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse the trace response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("no trace %s yet", traceID)
	}

	seen := map[string]bool{}
	var services []string
	for _, trace := range parsed.Data {
		names := make(map[string]string, len(trace.Processes))
		for id, p := range trace.Processes {
			names[id] = p.ServiceName
		}
		for _, span := range trace.Spans {
			name := names[span.ProcessID]
			if name != "" && !seen[name] {
				seen[name] = true
				services = append(services, name)
			}
		}
	}
	return services, nil
}
