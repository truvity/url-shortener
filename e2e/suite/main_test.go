// Package suite is the end-to-end suite, in the shape of the two doors
// (package devel): it calls a deployed release only through its PUBLIC host,
// with a `devel:url-shortener` token, and the cluster only through the
// Kubernetes API (rollout status, pod logs).
//
// It is inert unless E2E_URL is set, so `go test ./...` -- and therefore
// `just test` -- never touches a network or a cluster. `e2ectl env` prints
// the variables for the release it installed; the integration workflow and
// `just e2e-test` set them.
//
// What it does not do, on purpose: reach the database, the broker or the
// bucket. A suite that can only do what a customer can do cannot pass
// because of something a customer cannot reach, and runs from anywhere that
// holds the two credentials -- a GitHub-hosted runner included. The database
// questions (the migration, the two roles) are asked of a local PostgreSQL in
// internal/migration; the archive is asked of the archiver's own log.
package suite

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/truvity/url-shortener/e2e/devel"
)

const (
	envURL       = "E2E_URL"
	envNamespace = "E2E_NAMESPACE"
	envRelease   = "E2E_RELEASE"
)

// shared is resolved once in TestMain and read by every test; never mutated
// after that, so tests may run in any order or in parallel.
var shared struct {
	base      string
	namespace string
	release   string
	tokens    *devel.TokenSource
}

func TestMain(m *testing.M) {
	base := strings.TrimSuffix(strings.TrimSpace(os.Getenv(envURL)), "/")
	if base == "" {
		fmt.Fprintf(os.Stderr, "suite: %s is not set -- skipping the cluster suite\n", envURL)
		os.Exit(0)
	}

	cfg, err := devel.FromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "suite: %v\n", err)
		os.Exit(1)
	}

	shared.base = base
	shared.namespace = cfg.Namespace
	shared.release = cfg.Release
	shared.tokens = devel.NewTokenSource(cfg.Issuer)

	// The pre-flight gate: helm --wait returns before the gateway serves the
	// route, so the first test must not be what finds out.
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	err = devel.WaitPublic(ctx, base, shared.tokens)

	cancel()

	if err != nil {
		fmt.Fprintf(os.Stderr, "suite: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// testDataPrefix marks every long URL this suite invents, so a person reading
// the table by hand can tell this suite's rows from a real use at a glance.
const testDataPrefix = "e2e-suite"

// testLongURL is a long URL unique to this test.
func testLongURL(t *testing.T) string {
	t.Helper()

	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	out := make([]byte, 10)

	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			t.Fatalf("draw a random suffix: %v", err)
		}

		out[i] = alphabet[n.Int64()]
	}

	return fmt.Sprintf("https://example.com/%s/%s", testDataPrefix, out)
}

// eventually polls check, bounded by patience, and fails with the last error
// once patience runs out: the redirect answers and is done, so what it
// triggers (the counter, the archive) is proved by waiting, not by asserting
// once.
func eventually(t *testing.T, patience time.Duration, check func() error) {
	t.Helper()

	deadline := time.Now().Add(patience)

	var last error

	for {
		if last = check(); last == nil {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("did not become true within %s: %v", patience, last)
		}

		time.Sleep(time.Second)
	}
}

// bearer adds a fresh token to every request, minting again when the held one
// is near its end (the suite outlives a 15-minute token).
type bearer struct {
	next http.RoundTripper
}

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := shared.tokens.Token(req.Context())
	if err != nil {
		return nil, err
	}

	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)

	return b.next.RoundTrip(req)
}

// httpClient is the one client every call to the release goes through.
func httpClient(follow bool) *http.Client {
	c := &http.Client{Timeout: 30 * time.Second, Transport: bearer{next: http.DefaultTransport}}
	if !follow {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}

	return c
}
