package config_test

import (
	"strings"
	"testing"

	"github.com/truvity/policy/conformance"

	"github.com/truvity/policy/examples/url-shortener/internal/config"
)

// Each binary's type and its schema describe the same fields.
//
// This is the drift check the configuration contract asks for, and the
// failure it exists for is mundane: a field renamed on one side only. The
// deployment then sets a key nothing reads, or the binary reads a key nothing
// sets, and in both cases the service starts on a default nobody chose.
func TestEveryConfigurationTypeMatchesItsSchema(t *testing.T) {
	for name, tc := range map[string]struct {
		value  any
		schema string
	}{
		"migrate":  {config.Migrate{}, "migrate.json"},
		"redirect": {config.Redirect{}, "redirect.json"},
		"stat":     {config.Stat{}, "stat.json"},
	} {
		t.Run(name, func(t *testing.T) {
			conformance.TypeMatchesSchema(t, tc.value, config.Read(tc.schema))
		})
	}
}

// The configuration each binary ships as an example actually loads.
//
// A documented configuration that would be refused at start-up is worse than
// none, because it is the one people copy.
func TestTheExampleConfigurationsLoad(t *testing.T) {
	t.Run("migrate", func(t *testing.T) {
		cfg, err := config.LoadMigrate("testdata/migrate.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.OwnerRole == "" {
			t.Error("ownerRole did not decode")
		}
	})

	t.Run("redirect", func(t *testing.T) {
		cfg, err := config.LoadRedirect("testdata/redirect.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Events.RedirectSubject == "" || cfg.Events.RequestSubject == "" {
			t.Error("the subjects did not decode")
		}
		if cfg.Probes.Address == "" {
			t.Error("probes.address did not decode")
		}
	})

	t.Run("stat", func(t *testing.T) {
		cfg, err := config.LoadStat("testdata/stat.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Events.Consumer.Durable == "" {
			t.Error("the durable consumer name did not decode")
		}
	})
}

// A password written into the file is refused, and the error does not repeat
// it. The schemas are strict everywhere, not only at the top level, which is
// what makes this true of a nested object.
func TestAPasswordInTheFileIsRefused(t *testing.T) {
	const withPassword = "testdata/redirect-with-password.yaml"
	writeFixture(t, withPassword, `listen:
  address: ":8080"
probes:
  address: ":7070"
database:
  url: postgres://redirect@db:5432/url_shortener
  password: hunter2-never-in-a-file
events:
  nats:
    url: nats://nats:4222
  redirectSubject: a
  requestSubject: b
`)

	_, err := config.LoadRedirect(withPassword)
	if err == nil {
		t.Fatal("a password in the configuration file was accepted")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("the error quoted the secret it refused:\n%v", err)
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("the error does not name the offending key:\n%v", err)
	}
}
