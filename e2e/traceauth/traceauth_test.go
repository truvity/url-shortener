package traceauth

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTokenFile stands in for the projected ServiceAccount token file the
// kubelet mounts — see Exchange's own doc comment on why this is read
// fresh from a file rather than passed as a string: a real caller's token
// rotates under it.
func writeTokenFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write the token fixture: %v", err)
	}
	return path
}

// TestExchangeSendsTheRFC8693Shape proves the request Exchange sends is
// the exact shape a token-exchange issuer expects — the form fields, the
// content type, and Basic auth carrying the client as the username with an
// EMPTY password (see Config.Client's own doc comment for why nothing
// else authenticates this request).
func TestExchangeSendsTheRFC8693Shape(t *testing.T) {
	tokenFile := writeTokenFile(t, "the-subject-token")

	var gotUser, gotPass string
	var gotOK bool
	var gotForm map[string][]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, gotOK = r.BasicAuth()
		if err := r.ParseForm(); err != nil {
			t.Fatalf("server: parse form: %v", err)
		}
		gotForm = map[string][]string(r.PostForm)

		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "the-bearer-token"})
	}))
	defer server.Close()

	got, err := Exchange(context.Background(), Config{
		TokenURL:  server.URL,
		Client:    "trace-reader",
		TokenFile: tokenFile,
	})
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got != "the-bearer-token" {
		t.Errorf("access token = %q, want %q", got, "the-bearer-token")
	}

	if !gotOK {
		t.Fatal("the request carried no Basic auth")
	}
	if gotUser != "trace-reader" {
		t.Errorf("Basic auth username = %q, want the client id", gotUser)
	}
	if gotPass != "" {
		t.Errorf("Basic auth password = %q, want empty", gotPass)
	}

	want := map[string]string{
		"grant_type":         "urn:ietf:params:oauth:grant-type:token-exchange",
		"subject_token":      "the-subject-token",
		"subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
		"audience":           "trace-reader",
	}
	for field, wantV := range want {
		gotV := ""
		if vs := gotForm[field]; len(vs) == 1 {
			gotV = vs[0]
		}
		if gotV != wantV {
			t.Errorf("form field %q = %q, want %q", field, gotV, wantV)
		}
	}
}

// TestExchangeRereadsTheTokenFileEveryCall proves Exchange never caches
// the subject token — a second call after the file changed sends the NEW
// contents, matching a real projected token the kubelet rotates under a
// long-lived pod.
func TestExchangeRereadsTheTokenFileEveryCall(t *testing.T) {
	tokenFile := writeTokenFile(t, "first-token")

	var lastSubjectToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		lastSubjectToken = r.PostForm.Get("subject_token")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok"})
	}))
	defer server.Close()

	cfg := Config{TokenURL: server.URL, Client: "trace-reader", TokenFile: tokenFile}

	if _, err := Exchange(context.Background(), cfg); err != nil {
		t.Fatalf("first Exchange: %v", err)
	}
	if lastSubjectToken != "first-token" {
		t.Fatalf("first call sent %q, want %q", lastSubjectToken, "first-token")
	}

	if err := os.WriteFile(tokenFile, []byte("rotated-token"), 0o600); err != nil {
		t.Fatalf("rotate the token fixture: %v", err)
	}

	if _, err := Exchange(context.Background(), cfg); err != nil {
		t.Fatalf("second Exchange: %v", err)
	}
	if lastSubjectToken != "rotated-token" {
		t.Errorf("second call sent %q, want the rotated token", lastSubjectToken)
	}
}

// TestExchangeFailsOnNoAccessToken proves a 200 with no access_token is an
// error, not a silent empty bearer token some later request would send
// unauthenticated and misread as "the store admits anonymous readers".
func TestExchangeFailsOnNoAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer server.Close()

	_, err := Exchange(context.Background(), Config{
		TokenURL:  server.URL,
		Client:    "trace-reader",
		TokenFile: writeTokenFile(t, "tok"),
	})
	if err == nil {
		t.Fatal("Exchange succeeded with no access_token in the response")
	}
}

// TestExchangeFailsOnNonOKStatus proves a non-200 (a client the issuer
// refuses, say) is reported with the response body, not swallowed.
func TestExchangeFailsOnNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("invalid_client"))
	}))
	defer server.Close()

	_, err := Exchange(context.Background(), Config{
		TokenURL:  server.URL,
		Client:    "trace-reader",
		TokenFile: writeTokenFile(t, "tok"),
	})
	if err == nil {
		t.Fatal("Exchange succeeded on a 403")
	}
	if !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("error %v does not carry the response body", err)
	}
}

// TestExchangeRequiresEveryField proves each of Config's three fields is
// actually required, the same claim the chart's own schema makes for the
// values that fill them — a caller with one missing gets a clear error,
// never a request sent with an empty piece silently accepted by the far
// end as "no audience" or "no subject".
func TestExchangeRequiresEveryField(t *testing.T) {
	tokenFile := writeTokenFile(t, "tok")

	cases := []struct {
		name string
		cfg  Config
	}{
		{"no TokenURL", Config{Client: "trace-reader", TokenFile: tokenFile}},
		{"no Client", Config{TokenURL: "http://example.invalid", TokenFile: tokenFile}},
		{"no TokenFile", Config{TokenURL: "http://example.invalid", Client: "trace-reader"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Exchange(context.Background(), c.cfg); err == nil {
				t.Fatal("Exchange succeeded with a required field missing")
			}
		})
	}
}

// TestHTTPClientWithNoCAFileIsTheDefaultClient proves the empty case:
// nothing here should build a custom transport when there is no bundle to
// add.
func TestHTTPClientWithNoCAFileIsTheDefaultClient(t *testing.T) {
	client, err := HTTPClient("")
	if err != nil {
		t.Fatalf("HTTPClient(\"\"): %v", err)
	}
	if client != http.DefaultClient {
		t.Error("HTTPClient(\"\") did not return http.DefaultClient")
	}
}

// TestHTTPClientTrustsAServerTheSystemRootsDoNot proves the whole point of
// the CA file argument: an httptest.NewTLSServer's certificate is signed
// by neither the system roots nor anything else by default, so a plain
// http.Client refuses it, and a client built from ITS OWN certificate
// accepts it.
func TestHTTPClientTrustsAServerTheSystemRootsDoNot(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	// PEM-encoded, the shape a real trust-manager bundle ships —
	// AppendCertsFromPEM (HTTPClient's own call) reads nothing else.
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caFile, pemBytes, 0o600); err != nil {
		t.Fatalf("write the CA fixture: %v", err)
	}

	if _, err := http.DefaultClient.Get(server.URL); err == nil {
		t.Fatal("the system trust store already accepted the test server's certificate; the fixture proves nothing")
	}

	client, err := HTTPClient(caFile)
	if err != nil {
		t.Fatalf("HTTPClient(%q): %v", caFile, err)
	}

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("client.Get with the CA bundle applied: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestHTTPClientFailsOnAMissingFile proves a caFile that does not exist is
// an error at construction, not a client that silently falls back to no
// verification at all.
func TestHTTPClientFailsOnAMissingFile(t *testing.T) {
	if _, err := HTTPClient(filepath.Join(t.TempDir(), "does-not-exist.crt")); err == nil {
		t.Fatal("HTTPClient succeeded with a nonexistent CA file")
	}
}
