package config_test

import (
	"bytes"
	"encoding/json"
	"reflect"
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
		"urls":     {config.Urls{}, "urls.json"},
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
		if cfg.Urls.Address == "" {
			t.Error("the URL service's address did not decode")
		}
	})

	t.Run("urls", func(t *testing.T) {
		cfg, err := config.LoadUrls("testdata/urls.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.Address == "" {
			t.Error("listen.address did not decode")
		}
		if cfg.Database.URL == "" {
			t.Error("the database URL did not decode")
		}
	})
}

// The counter carries no database credential at all, and that is asserted
// rather than left to a reading of the type.
//
// It is the ownership rule showing up as an absence, which is the shape it
// usually takes: the table belongs to the URL service, the counter asks it,
// and the credential this component would otherwise hold — with whatever
// rights that credential carried — simply does not exist here.
func TestTheCounterCannotReachTheDatabase(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(config.Read("stat.json"), &schema); err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Properties["database"]; ok {
		t.Error("stat.json describes a database: the counter does not own the table")
	}
	// Structural rather than a search for the word, because every `$ref` in
	// this file is a URL and would match one.
	if bytes.Contains(config.Read("stat.json"), []byte("fragments/postgres.json")) {
		t.Error("stat.json references the postgres fragment: the counter asks the URL service instead")
	}
	if _, ok := reflect.TypeOf(config.Stat{}).FieldByName("Database"); ok {
		t.Error("config.Stat has a Database field: the counter asks the URL service instead")
	}
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
