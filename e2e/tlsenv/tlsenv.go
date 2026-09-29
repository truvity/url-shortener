// Package tlsenv is the ONE place examples/url-shortener/e2e/suite learns
// whether it must present a workload identity to the application release it
// tests, and where each target's authenticated listener is.
//
// It is a package of its own, rather than living inside suite, for the same
// reason traceauth is: suite's TestMain skips the WHOLE package unless
// E2E_NAMESPACE is set, and this logic needs no cluster — a mode read
// correctly and a port chosen correctly are hermetic claims, proved on
// every `just check` by tlsenv_test.go.
//
// It mirrors, for the suite Job, what charts/url-shortener-e2e's prober
// already does for the prober: a client either presents an identity or it
// does not, so there is no authenticated PORT to add on this side, only the
// switch, the certificate, and which of the TARGET's two ports to dial. That
// port depends on the TARGET's own mode, and the two targets differ: `urls`
// may be strict while `redirect`, which is fronted by a gateway, never is.
package tlsenv

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/truvity/policy/transport"
)

// The environment the url-shortener-e2e chart's Job sets when its identity
// is on. Every one of them is absent when it is off, which is what makes the
// off case byte-identical.
const (
	// EnvUrlsMode and EnvRedirectMode are the TARGET's mode — the mode the
	// application release runs that component in, told to this Job because
	// it is a separate Helm release and cannot read it. off when unset.
	EnvUrlsMode     = "E2E_URLS_TLS"
	EnvRedirectMode = "E2E_REDIRECT_TLS"

	// EnvPort is the authenticated port a target serves BESIDE its cleartext
	// one under permissive.
	EnvPort = "E2E_TLS_PORT"

	// EnvDir is where the identity is mounted (tls.crt, tls.key, ca.crt).
	EnvDir = "E2E_TLS_DIR"

	// EnvTrustDomain and EnvPeers say whom to accept an ANSWER from:
	// "namespace/serviceAccount", comma separated. Empty admits no answer.
	EnvTrustDomain = "E2E_TLS_TRUST_DOMAIN"
	EnvPeers       = "E2E_TLS_PEERS"

	// OrdinaryPort is the port every RPC-serving component answers on.
	OrdinaryPort = 8080

	defaultPort = 8443
)

// Config is what the environment said.
type Config struct {
	Urls        transport.Mode
	Redirect    transport.Mode
	Port        int
	Dir         string
	TrustDomain string
	Peers       []transport.Peer
}

// FromEnv reads Config from lookup (os.LookupEnv in the suite).
//
// Nothing set is the default and means cleartext everywhere. A mode that is
// not off, with the rest of what an identity needs missing, is an error here
// rather than a connection refused three layers later.
func FromEnv(lookup func(string) (string, bool)) (Config, error) {
	get := func(name string) string {
		v, _ := lookup(name)
		return strings.TrimSpace(v)
	}

	var c Config
	var err error

	if c.Urls, err = mode(EnvUrlsMode, get(EnvUrlsMode)); err != nil {
		return Config{}, err
	}
	if c.Redirect, err = mode(EnvRedirectMode, get(EnvRedirectMode)); err != nil {
		return Config{}, err
	}
	if c.Redirect == transport.Strict {
		return Config{}, fmt.Errorf("%s is strict: the redirect service is fronted by a gateway and is never strict", EnvRedirectMode)
	}

	c.Port = defaultPort
	if raw := get(EnvPort); raw != "" {
		if c.Port, err = strconv.Atoi(raw); err != nil || c.Port < 1 || c.Port > 65535 {
			return Config{}, fmt.Errorf("%s=%q is not a port", EnvPort, raw)
		}
	}

	if !c.On() {
		return c, nil
	}

	if c.Dir = get(EnvDir); c.Dir == "" {
		return Config{}, fmt.Errorf("%s is required once a target is not off: nowhere to read the identity from", EnvDir)
	}
	if c.TrustDomain = get(EnvTrustDomain); c.TrustDomain == "" {
		return Config{}, fmt.Errorf("%s is required once a target is not off: without it an answer from any trust domain is accepted", EnvTrustDomain)
	}

	// A non-nil empty list, never nil: an empty allow-list admits nobody,
	// which is a different statement from having none.
	c.Peers = []transport.Peer{}
	for _, raw := range strings.Split(get(EnvPeers), ",") {
		if raw = strings.TrimSpace(raw); raw == "" {
			continue
		}
		ns, sa, ok := strings.Cut(raw, "/")
		if !ok || ns == "" || sa == "" {
			return Config{}, fmt.Errorf("%s entry %q is not namespace/serviceAccount", EnvPeers, raw)
		}
		c.Peers = append(c.Peers, transport.Peer{Namespace: ns, ServiceAccount: sa})
	}

	return c, nil
}

func mode(name, raw string) (transport.Mode, error) {
	switch m := transport.Mode(raw); m {
	case "", transport.Off:
		return transport.Off, nil
	case transport.Permissive, transport.Strict:
		return m, nil
	}
	return "", fmt.Errorf("%s=%q is not off, permissive or strict", name, raw)
}

// On reports whether any target needs this process to present an identity.
func (c Config) On() bool {
	return c.Urls != transport.Off || c.Redirect != transport.Off
}

// ModeOf is the target's mode; anything but the two named targets is off.
func (c Config) ModeOf(component string) transport.Mode {
	switch component {
	case "urls":
		return c.Urls
	case "redirect":
		return c.Redirect
	}
	return transport.Off
}

// Endpoint says which port to dial and whether it speaks TLS: the ordinary
// port in cleartext when off; that SAME port over TLS when strict, since
// under strict the protocol changes and the address does not; the second
// port over TLS when permissive, since the ordinary one is still cleartext.
func (c Config) Endpoint(component string) (port int, secure bool) {
	switch c.ModeOf(component) {
	case transport.Strict:
		return OrdinaryPort, true
	case transport.Permissive:
		return c.Port, true
	}
	return OrdinaryPort, false
}

// Rewrite turns the base URL a harness resolved for the Endpoint's port into
// one with the right scheme.
func (c Config) Rewrite(component, base string) (string, error) {
	if _, secure := c.Endpoint(component); !secure {
		return base, nil
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse %q: %w", base, err)
	}
	u.Scheme = "https"
	return u.String(), nil
}

// Transport loads the identity. It returns nil when nothing is on, which
// callers must treat as "leave the client's own default transport alone"; a
// typed-nil *http.Transport stored in an interface field would not be nil.
func (c Config) Transport() (*http.Transport, error) {
	if !c.On() {
		return nil, nil
	}

	id, err := transport.Load(transport.Config{
		Mode:        transport.Strict, // a client presents or it does not; see the package comment
		CertFile:    c.Dir + "/tls.crt",
		KeyFile:     c.Dir + "/tls.key",
		CAFile:      c.Dir + "/ca.crt",
		TrustDomain: c.TrustDomain,
		Peers:       c.Peers,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("transport identity: %w", err)
	}

	return &http.Transport{TLSClientConfig: id.Client()}, nil
}
