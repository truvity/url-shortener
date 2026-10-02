package runtime_test

import (
	"strings"
	"testing"
	"time"

	"github.com/truvity/policy/examples/url-shortener/internal/runtime"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func verifyFull() map[string]string {
	return map[string]string{
		"PGHOST":                    "pg-rw.shop.svc.cluster.example",
		"PGDATABASE":                "url_shortener",
		"PGUSER":                    "url_shortener_app",
		"PGSSLROOTCERT":             "/etc/url-shortener-pg-ca/ca-certificates.crt",
		"CNPG_CLIENT_PASSWORD_FILE": "/etc/url-shortener-pg-password/password",
	}
}

// The environment the chart renders is enough, and carries the TLS
// settings through to the client as parts: the root file, the host the
// certificate must name, and the password FILE (re-read for every new
// connection, so a rotated Secret reaches a running pod).
func TestDatabaseConfigFromTheChartsEnvironment(t *testing.T) {
	cfg, err := runtime.DatabaseConfig(env(verifyFull()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "pg-rw.shop.svc.cluster.example" || cfg.User != "url_shortener_app" || cfg.Database != "url_shortener" {
		t.Errorf("the connection parts did not arrive: %v", cfg)
	}
	if cfg.SSLRootCert != "/etc/url-shortener-pg-ca/ca-certificates.crt" {
		t.Errorf("the root file did not arrive: %q", cfg.SSLRootCert)
	}
	if cfg.PasswordFile != "/etc/url-shortener-pg-password/password" {
		t.Errorf("the password file did not arrive: %q", cfg.PasswordFile)
	}
}

// A deployment that lost its CA, or asks for anything weaker than
// verify-full, is refused before anything is dialled. The two mistakes that
// matter most are the ones that would otherwise connect.
func TestDatabaseConfigRefusesWhatIsNotVerifyFull(t *testing.T) {
	t.Run("no root file", func(t *testing.T) {
		e := verifyFull()
		delete(e, "PGSSLROOTCERT")
		if _, err := runtime.DatabaseConfig(env(e)); err == nil {
			t.Fatal("a connection with nothing to verify against was accepted")
		}
	})
	t.Run("sslmode require", func(t *testing.T) {
		e := verifyFull()
		e["PGSSLMODE"] = "require"
		_, err := runtime.DatabaseConfig(env(e))
		if err == nil {
			t.Fatal("sslmode=require was accepted")
		}
		if !strings.Contains(err.Error(), "verify-full") {
			t.Errorf("the refusal does not say what is accepted: %v", err)
		}
	})
	t.Run("no host", func(t *testing.T) {
		e := verifyFull()
		delete(e, "PGHOST")
		if _, err := runtime.DatabaseConfig(env(e)); err == nil {
			t.Fatal("a connection with no host was accepted")
		}
	})
}

// The password never reaches a log line through the configuration.
func TestDatabaseConfigDoesNotPrintThePassword(t *testing.T) {
	e := verifyFull()
	e["PGPASSWORD"] = "hunter2-never-printed"
	cfg, err := runtime.DatabaseConfig(env(e))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.String(), "hunter2") {
		t.Errorf("the configuration printed the password: %s", cfg)
	}
}

// Start-up keeps this service's old patience (a database that reloads for a
// minute must not become a restart loop), unless the environment tunes it.
func TestDatabaseConfigKeepsTheStartupPatience(t *testing.T) {
	cfg, err := runtime.DatabaseConfig(env(verifyFull()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retry.Budget != 3*time.Minute || cfg.Retry.MaxDelay != 10*time.Second {
		t.Errorf("the start-up retry is %+v, want 3m budget and 10s ceiling", cfg.Retry)
	}

	e := verifyFull()
	e["CNPG_CLIENT_RETRY_BUDGET"] = "45s"
	cfg, err = runtime.DatabaseConfig(env(e))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retry.Budget != 45*time.Second {
		t.Errorf("the environment's retry budget was overridden: %+v", cfg.Retry)
	}
}
