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
// literal at more than this one place. stat and log carry no Service (see
// charts/url-shortener/templates/{stat,log}.yaml) but are still
// Deployments, which is what readiness_test.go checks for all five.
const (
	componentURLs     = "urls"
	componentRedirect = "redirect"
	componentWeb      = "web"
	componentStat     = "stat"
	componentLog      = "log"
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
//
// The PORT and the SCHEME come from tlsenv, not from the caller: they depend
// on the TARGET's own mode, which differs by component (`urls` may be strict
// while `redirect`, fronted by a gateway, never is). With the identity off —
// the default — that is the ordinary port in cleartext, exactly as before.
func serviceURL(ctx context.Context, t *testing.T, component string) string {
	t.Helper()

	port, _ := shared.tls.Endpoint(component)
	url, err := shared.cluster.ServiceURL(ctx, shared.names.Namespace, service(component), port)
	if err != nil {
		t.Fatalf("resolve the %s Service: %v", component, err)
	}

	url, err = shared.tls.Rewrite(component, url)
	if err != nil {
		t.Fatalf("address the %s Service: %v", component, err)
	}
	return url
}

// httpClient is the client every call to the release under test goes
// through: the plain default when no identity is loaded, and one presenting
// the mounted certificate — verifying the ANSWERING peer's identity rather
// than its name (transport.Identity.Client) — when the Job's is on.
//
// Transport is set only when an identity exists, deliberately never to a
// typed-nil *http.Transport: an interface field holding one is not a nil
// interface, so net/http would call RoundTrip on it instead of falling back
// to its default.
func httpClient(follow bool) *http.Client {
	c := &http.Client{Timeout: 10 * time.Second}
	if !follow {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	if shared.identity != nil {
		c.Transport = shared.identity
	}
	return c
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

	baseURL := serviceURL(ctx, t, componentURLs)
	return urlshortenerv1connect.NewUrlsServiceClient(httpClient(true), baseURL)
}

// errString is a small helper so callers can name a failing RPC without
// repeating fmt.Errorf's shape at every call site.
func errString(component, what string, err error) error {
	return fmt.Errorf("%s: %s: %w", component, what, wrapThroughForward(component, err))
}
