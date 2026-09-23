// Package config is this service's configuration: one type per binary, each
// with a schema beside it, loaded and validated before anything is built.
//
// The types are hand-written and the schemas are authored. A test asserts
// they describe the same fields, which is the whole of the drift protection —
// see docs/decisions/0003-schemas-not-generators.md for why that is a test
// rather than a generator.
package config

import (
	"github.com/truvity/policy/config"

	urlshortener "github.com/truvity/policy/examples/url-shortener"
)

// Read returns one of this service's schemas by file name.
func Read(name string) []byte {
	b, err := urlshortener.Schemas.ReadFile("schemas/" + name)
	if err != nil {
		// Unreachable: the files are embedded at build time, so a missing
		// one fails to compile rather than at run time.
		panic(err)
	}
	return b
}

type (
	// Listen is a TCP listener.
	Listen struct {
		Address string `json:"address"`
	}

	// Log is how much this service says.
	Log struct {
		Level string `json:"level"`
	}

	// Postgres is a connection, with the password named rather than carried.
	Postgres struct {
		URL            string `json:"url"`
		PasswordEnv    string `json:"passwordEnv"`
		MaxConnections int    `json:"maxConnections"`
	}

	// NATS is a connection, and nothing about what is done with it.
	NATS struct {
		URL       string `json:"url"`
		TokenFile string `json:"tokenFile"`
	}

	// Consumer is what a durable consumer binds to.
	Consumer struct {
		Stream  string `json:"stream"`
		Durable string `json:"durable"`
		Subject string `json:"subject"`
	}

	// Drain is how long the service may take to finish in-flight work.
	// It is the service's end of ONE number shared with whatever deploys
	// it: the grace period the orchestrator grants is derived from this,
	// and a process that used its own default would be killed at whatever
	// moment that default happened to disagree.
	Drain struct {
		Seconds int `json:"seconds"`
	}

	// Migrate is the migration job: no listener, no probes. A job that runs
	// once and exits is not a service.
	Migrate struct {
		Log       Log      `json:"log"`
		Database  Postgres `json:"database"`
		OwnerRole string   `json:"ownerRole"`
		AppRole   string   `json:"appRole"`
	}

	// Redirect resolves short keys and says what happened.
	Redirect struct {
		Listen   Listen   `json:"listen"`
		Probes   Listen   `json:"probes"`
		Log      Log      `json:"log"`
		Drain    Drain    `json:"drain"`
		Database Postgres `json:"database"`
		Events   struct {
			NATS            NATS   `json:"nats"`
			RedirectSubject string `json:"redirectSubject"`
			RequestSubject  string `json:"requestSubject"`
		} `json:"events"`
	}

	// Stat counts what redirect emitted.
	Stat struct {
		Listen   Listen   `json:"listen"`
		Probes   Listen   `json:"probes"`
		Log      Log      `json:"log"`
		Drain    Drain    `json:"drain"`
		Database Postgres `json:"database"`
		Events   struct {
			NATS     NATS     `json:"nats"`
			Consumer Consumer `json:"consumer"`
		} `json:"events"`
	}
)

// LoadMigrate reads the migration job's configuration file, validates it
// against the job's schema, and returns it. A failure names the key.
func LoadMigrate(path string) (Migrate, error) {
	var c Migrate
	return c, config.Load(path, Read("migrate.json"), &c)
}

// LoadRedirect reads the redirect service's configuration file and
// validates it against the service's schema.
func LoadRedirect(path string) (Redirect, error) {
	var c Redirect
	return c, config.Load(path, Read("redirect.json"), &c)
}

// LoadStat reads the stat service's configuration file and validates it
// against the service's schema.
func LoadStat(path string) (Stat, error) {
	var c Stat
	return c, config.Load(path, Read("stat.json"), &c)
}
