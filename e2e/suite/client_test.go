package suite

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1/urlshortenerv1connect"
)

// component names the chart's own, matching the release names
// examples/url-shortener/e2e/fixture reads off it — never repeated as a
// literal at more than this one place.
const (
	componentURLs     = "urls"
	componentRedirect = "redirect"
	componentWeb      = "web"
)

// httpPort and probesPort are the two ports EVERY Service in this chart
// carries — see charts/url-shortener/templates/{urls,redirect,web}.yaml.
const (
	httpPort   = 8080
	probesPort = 7070
)

// service renders the Service name for one of this chart's components —
// {{ include "url-shortener.name" . }}-{{ component }}, which is
// {AppRelease}-{component} for this chart's own naming helper.
func service(component string) string {
	return shared.names.AppRelease + "-" + component
}

// serviceURL resolves a Service's URL through the harness — a
// port-forward to the exact Pod on the kind tier, the ClusterIP directly
// everywhere else (harness.Cluster.ServiceURL) — and fails the test with a
// clear name if it cannot.
func serviceURL(ctx context.Context, t *testing.T, component string, port int) string {
	t.Helper()

	url, err := shared.cluster.ServiceURL(ctx, shared.names.Namespace, service(component), port)
	if err != nil {
		t.Fatalf("resolve the %s Service: %v", component, err)
	}
	return url
}

// wrapThroughForward enriches err with the Pod a request travelled through,
// when this Cluster opened a forward for (namespace, component) — the kind
// tier's answer to "which pod": a pod that restarted mid-test otherwise
// reads as a bare Service failure. A no-op on the shared tier, where
// ServiceURL opened no forward at all.
func wrapThroughForward(component string, err error) error {
	if err == nil {
		return nil
	}
	if fw, ok := shared.cluster.ForwardFor(shared.names.Namespace, service(component)); ok {
		return fw.Err(err)
	}
	return err
}

// urlsClient builds a Connect client to the UrlsService, over a plain
// http.Client — a Connect unary call is an ordinary POST with a JSON body,
// which is the same claim hack/smoke.sh proved with curl; this suite proves
// it by using the generated client instead of hand-rolling the request.
func urlsClient(ctx context.Context, t *testing.T) urlshortenerv1connect.UrlsServiceClient {
	t.Helper()

	baseURL := serviceURL(ctx, t, componentURLs, httpPort)
	return urlshortenerv1connect.NewUrlsServiceClient(&http.Client{Timeout: 10 * time.Second}, baseURL)
}

// probeHealth asks a component's probe port the same two questions the
// kubelet already asks the Pod directly (livenessProbe/readinessProbe in
// the chart) — but through the Service, which is the one thing a rendered
// chart cannot prove: that the probe port the chart wires up is actually
// the one the Service reaches.
func probeHealth(ctx context.Context, t *testing.T, component string) {
	t.Helper()

	base := serviceURL(ctx, t, component, probesPort)
	client := &http.Client{Timeout: 5 * time.Second}

	for _, path := range []string{"/health/live", "/health/ready"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, http.NoBody)
		if err != nil {
			t.Fatalf("%s %s: build request: %v", component, path, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", component, path, wrapThroughForward(component, err))
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s %s answered %d, wanted 200", component, path, resp.StatusCode)
		}
	}
}

// errString is a small helper so callers can name a failing RPC without
// repeating fmt.Errorf's shape at every call site.
func errString(component, what string, err error) error {
	return fmt.Errorf("%s: %s: %w", component, what, wrapThroughForward(component, err))
}
