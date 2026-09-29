package runtime

import (
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/truvity/policy/transport"
)

// NATSToken authenticates with a token the platform mounts as a file.
//
// The file is read on every CONNECT, not once at start-up, which is the
// whole point of the option: the token is short-lived and the platform
// replaces the file in place, so a client that read it once authenticates
// fine until its first reconnect and then fails at three in the morning,
// somewhere far from this line.
//
// What the broker does with the token is not this client's business. It
// hands it to an authorisation service, which answers from the account the
// workload actually runs as rather than from anything the client claims to
// be. That is why a token file and not a credential: the credential would
// be a secret to distribute and rotate, and this is an identity the runtime
// already attests.
func NATSToken(path string) nats.Option {
	return nats.TokenHandler(func() string {
		raw, err := os.ReadFile(path)
		if err != nil {
			// The handler cannot return an error, so an unreadable token
			// becomes an empty one and the broker refuses the connection.
			// That is the correct outcome and it is loud at the broker.
			return ""
		}

		return strings.TrimSpace(string(raw))
	})
}

// NATSOptions is the whole of what a service needs to dial the broker.
func NATSOptions(name, tokenFile string) ([]nats.Option, error) {
	opts := []nats.Option{
		nats.Name(name),
		// Never give up: a broker restart is an ordinary event and a
		// service that exits on one turns a blip into a rollout.
		nats.MaxReconnects(-1),
	}

	if tokenFile == "" {
		return opts, nil
	}

	if _, err := os.Stat(tokenFile); err != nil {
		return nil, fmt.Errorf("the token file this service was configured with is not readable: %w", err)
	}

	return append(opts, NATSToken(tokenFile)), nil
}

// NATSIdentity authenticates to the broker with the workload identity the
// platform mounted, over TLS, INSTEAD of a token: the certificate is the
// credential, and the broker maps the identity in it to a user with its own
// permissions.
//
// The certificate is the same one the service's own `tls` block serves, read
// through the same loader. It is re-read on every connect, not once at
// start-up, for the reason NATSToken re-reads its file: the platform rotates
// it within the hour, and a client that cached the first one connects fine
// until its first reconnect after the expiry and then fails at three in the
// morning. A connection already made is not affected by a rotation — the
// broker checks the certificate at the handshake only — so a rotation drops
// nothing; it is the NEXT connect that must present a fresh certificate.
//
// caFile is the trust bundle for the BROKER's certificate, and serverName is
// the name that certificate was issued for. Both are the broker's, not the
// workload identity's: a broker has a name and no workload identity.
//
// It refuses to build without an identity rather than dialling TLS with no
// client certificate. A broker that verifies clients refuses that at the
// handshake with an error about a missing certificate that says nothing about
// which setting was left off; this says it here.
func NATSIdentity(id *transport.Identity, caFile, serverName string) (nats.Option, error) {
	if id == nil {
		return nil, errors.New("events.nats.tls asks to present a workload identity, but this service has none: " +
			"set tls.mode to permissive or strict so an identity is mounted and loaded")
	}

	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read the broker's trust bundle: %w", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("the broker's trust bundle at %s holds no certificate", caFile)
	}

	return nats.Secure(id.ClientTo(roots, serverName)), nil
}
