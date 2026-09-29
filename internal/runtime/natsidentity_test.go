package runtime_test

import (
	"testing"

	"github.com/truvity/policy/examples/url-shortener/internal/runtime"
)

// Asking for the identity when the service has none is refused here, in words
// that name the setting, rather than dialling TLS with no client certificate
// and failing at a broker with an error that names nothing.
func TestNATSIdentityWithNoIdentityIsRefused(t *testing.T) {
	if _, err := runtime.NATSIdentity(nil, "/nonexistent", ""); err == nil {
		t.Fatal("no identity was accepted")
	}
}
