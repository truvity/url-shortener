// Package traceauth is the ONE place examples/url-shortener/e2e/suite's
// trace-shaped test (trace_test.go) trades a projected ServiceAccount
// token for a bearer token it can query a trace store with — RFC 8693
// token exchange, over plain net/http.
//
// It is a package of its own, rather than living inside suite alongside
// trace_test.go, because suite's TestMain (main_test.go) skips the WHOLE
// package unless E2E_NAMESPACE is set — the right behaviour for tests that
// need a cluster, and the wrong one for THIS logic, which needs neither: a
// form encoded correctly and a response parsed correctly are hermetic
// claims, provable with httptest and nothing else (traceauth_test.go), and
// a claim this package's own tests should prove on every `just check`,
// cluster or no cluster.
//
// TWO TRUST STORES, NEVER MERGED: Exchange verifies tokenURL against the
// process's own default trust store (Go's system roots) — appropriate for
// a public identity issuer, whose certificate the system roots already
// sign. HTTPClient, separately, verifies a trace store against a CA
// bundle a caller hands it — appropriate for a private endpoint whose
// leaf only that bundle signs. Handing Exchange a private CA bundle would
// not make it "extra safe"; it would be a bundle the public issuer's
// certificate was never issued from, and the exchange would fail with an
// unknown-authority error for a store that was never in danger. See each
// function's own doc comment.
package traceauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Config is everything Exchange needs to trade one projected ServiceAccount
// token for a bearer token scoped to a roster client.
type Config struct {
	// TokenURL is the issuer's token endpoint. Verified against the
	// process's own default trust store — see the package doc comment.
	TokenURL string

	// Client is both the RFC 8693 `audience` this exchange asks for and
	// the HTTP Basic auth username it authenticates the request with —
	// the same convention a Kargo gate's own token exchange uses: the
	// client a caller asks to become IS the audience it names, and there
	// is no separate secret to send alongside it (Basic auth with an
	// empty password), because what proves the request is the subject
	// token below, not a client secret this Job would otherwise have to
	// hold.
	Client string

	// TokenFile is the path to a projected ServiceAccount token — read
	// fresh on every call, never cached, because the kubelet rotates the
	// file under the pod and a token read once at start-up would age out
	// mid-run.
	TokenFile string
}

// tokenResponse is the one field of RFC 8693's response this package
// reads.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

// Exchange reads cfg.TokenFile and trades it at cfg.TokenURL for a bearer
// token scoped to cfg.Client, via RFC 8693 token exchange
// (grant_type=urn:ietf:params:oauth:grant-type:token-exchange,
// subject_token_type=urn:ietf:params:oauth:token-type:jwt).
func Exchange(ctx context.Context, cfg Config) (string, error) {
	if cfg.TokenURL == "" {
		return "", fmt.Errorf("traceauth: TokenURL is required")
	}
	if cfg.Client == "" {
		return "", fmt.Errorf("traceauth: Client is required")
	}
	if cfg.TokenFile == "" {
		return "", fmt.Errorf("traceauth: TokenFile is required")
	}

	subjectToken, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return "", fmt.Errorf("traceauth: read %s: %w", cfg.TokenFile, err)
	}

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("subject_token", strings.TrimSpace(string(subjectToken)))
	form.Set("subject_token_type", "urn:ietf:params:oauth:token-type:jwt")
	form.Set("audience", cfg.Client)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("traceauth: build the exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(cfg.Client, "")

	// The process's own default transport: the public issuer's
	// certificate is signed by roots the system trust store already
	// carries — see the package doc comment for why this is never the
	// private CA bundle HTTPClient below is for.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("traceauth: exchange at %s: %w", cfg.TokenURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("traceauth: read the exchange response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("traceauth: exchange at %s answered %d: %s", cfg.TokenURL, resp.StatusCode, body)
	}

	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("traceauth: parse the exchange response: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("traceauth: exchange at %s returned no access_token: %s", cfg.TokenURL, body)
	}

	return parsed.AccessToken, nil
}

// HTTPClient builds a client for querying a trace store — the system's
// default trust store when caFile is empty (a trace store reachable the
// same way the public issuer is), or one that ALSO trusts caFile's PEM
// bundle, appended to (never replacing) the system roots, when it is set
// — a trace store on a private trust domain the system roots do not sign.
//
// Appending rather than replacing matters here for the same reason it
// would not for Exchange above: this client is never used against the
// public issuer, only against traces.url, so there is no risk of the
// private bundle shadowing a public certificate it was never meant to
// verify — but a caller that later points traces.url at a public store
// still gets a client that can reach it.
func HTTPClient(caFile string) (*http.Client, error) {
	if caFile == "" {
		return http.DefaultClient, nil
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("traceauth: read %s: %w", caFile, err)
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("traceauth: %s carries no usable PEM certificate", caFile)
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}, nil
}
