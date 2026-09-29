package tlsenv_test

import (
	"strings"
	"testing"

	"github.com/truvity/policy/examples/url-shortener/e2e/tlsenv"
	"github.com/truvity/policy/transport"
)

func env(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
}

// Nothing set is the default, and it must be cleartext everywhere with no
// identity to load: a Job installed with its identity off runs as before.
func TestNothingSetIsCleartextEverywhere(t *testing.T) {
	c, err := tlsenv.FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.On() {
		t.Error("On with nothing set")
	}
	for _, component := range []string{"urls", "redirect"} {
		port, secure := c.Endpoint(component)
		if port != 8080 || secure {
			t.Errorf("%s: %d secure=%v, want 8080 cleartext", component, port, secure)
		}
	}
	tr, err := c.Transport()
	if err != nil || tr != nil {
		t.Errorf("Transport() = %v, %v; want nil, nil", tr, err)
	}
}

// urls strict with redirect permissive: the two targets differ, and each
// gets ITS port.
func TestEachTargetGetsItsOwnPort(t *testing.T) {
	c, err := tlsenv.FromEnv(env(map[string]string{
		tlsenv.EnvUrlsMode:     "strict",
		tlsenv.EnvRedirectMode: "permissive",
		tlsenv.EnvPort:         "9443",
		tlsenv.EnvDir:          "/var/run/identity",
		tlsenv.EnvTrustDomain:  "example.invalid",
		tlsenv.EnvPeers:        "shop/app, other/reader",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if port, secure := c.Endpoint("urls"); port != 8080 || !secure {
		t.Errorf("urls: %d secure=%v, want 8080 over TLS", port, secure)
	}
	if port, secure := c.Endpoint("redirect"); port != 9443 || !secure {
		t.Errorf("redirect: %d secure=%v, want 9443 over TLS", port, secure)
	}
	if len(c.Peers) != 2 || c.Peers[1] != (transport.Peer{Namespace: "other", ServiceAccount: "reader"}) {
		t.Errorf("peers = %v", c.Peers)
	}

	got, err := c.Rewrite("urls", "http://10.0.0.1:8080")
	if err != nil || got != "https://10.0.0.1:8080" {
		t.Errorf("Rewrite = %q, %v", got, err)
	}
}

func TestOnlyTheMountedIdentityIsRequiredToBeNamed(t *testing.T) {
	c, err := tlsenv.FromEnv(env(map[string]string{
		tlsenv.EnvUrlsMode:    "strict",
		tlsenv.EnvDir:         "/id",
		tlsenv.EnvTrustDomain: "example.invalid",
	}))
	if err != nil {
		t.Fatal(err)
	}
	// Empty peers is a NON-NIL empty list: nobody admitted, not "no list".
	if c.Peers == nil || len(c.Peers) != 0 {
		t.Errorf("peers = %#v, want a non-nil empty list", c.Peers)
	}
	if c.Port != 8443 {
		t.Errorf("port = %d, want the 8443 default", c.Port)
	}
}

func TestRefusals(t *testing.T) {
	on := map[string]string{tlsenv.EnvUrlsMode: "strict", tlsenv.EnvDir: "/id", tlsenv.EnvTrustDomain: "example.invalid"}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for key, val := range on {
			m[key] = val
		}
		if v == "-" {
			delete(m, k)
		} else {
			m[k] = v
		}
		return m
	}

	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"redirect strict":   {with(tlsenv.EnvRedirectMode, "strict"), "never strict"},
		"unknown mode":      {with(tlsenv.EnvUrlsMode, "mutual"), "not off, permissive or strict"},
		"bad port":          {with(tlsenv.EnvPort, "70000"), "not a port"},
		"no dir":            {with(tlsenv.EnvDir, "-"), tlsenv.EnvDir},
		"no trust domain":   {with(tlsenv.EnvTrustDomain, "-"), tlsenv.EnvTrustDomain},
		"malformed peer":    {with(tlsenv.EnvPeers, "justonename"), "namespace/serviceAccount"},
		"peer no namespace": {with(tlsenv.EnvPeers, "/sa"), "namespace/serviceAccount"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tlsenv.FromEnv(env(tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
