package runtime

import (
	"fmt"
	"os"
	"strings"

	"github.com/nats-io/nats.go"
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
