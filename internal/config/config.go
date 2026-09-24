// Package config is this service's configuration: one type per binary, each
// with a schema beside it, loaded and validated before anything is built.
//
// The types are hand-written and the schemas are authored. A test asserts
// they describe the same fields, which is the whole of the drift protection —
// see docs/decisions/0003-schemas-not-generators.md for why that is a test
// rather than a generator.
package config

import (
	"github.com/truvity/policy/transport"

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

// ReadPython returns the schema of a component that is not written in Go.
//
// There is one, and it is here rather than beside the others because a
// Python wheel carries only what is inside the package. The chart renders
// its configuration file like any other, and the chart's tests validate it
// like any other — a platform that had to know which language a workload
// was written in would be a platform every new language has to be added to.
func ReadPython(path string) []byte {
	b, err := urlshortener.PythonSchemas.ReadFile(path)
	if err != nil {
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

	// Client is another service this one calls, by address.
	//
	// An address and nothing else: which protocol it speaks is the caller's
	// decision and is in the code, and whether the connection is
	// authenticated is the `tls` block's business. A field per transport
	// option here would be a second place to describe the same thing.
	Client struct {
		Address string `json:"address"`
	}

	// Drain is how long the service may take to finish in-flight work.
	// It is the service's end of ONE number shared with whatever deploys
	// it: the grace period the orchestrator grants is derived from this,
	// and a process that used its own default would be killed at whatever
	// moment that default happened to disagree.
	Drain struct {
		Seconds int `json:"seconds"`
	}

	// TLS is the mutually authenticated transport, when the platform
	// provides one. It is the policy package's own type, so that a service
	// declaring it also gets the loader and the peer check: a second
	// description of the same shape is the thing the config contract exists
	// to prevent.
	//
	// It is NOT part of the shared envelope, and that is deliberate. A
	// migration has no transport to secure, and the counter serves nothing
	// but its probes. A field every service carries and only some can use is
	// a field a deployment sets and watches do nothing — which is what the
	// telemetry block was before it was deleted.
	TLS = transport.Config

	// Migrate is the migration job: no listener, no probes. A job that runs
	// once and exits is not a service.
	Migrate struct {
		Log       Log      `json:"log"`
		Database  Postgres `json:"database"`
		OwnerRole string   `json:"ownerRole"`
		AppRole   string   `json:"appRole"`
	}

	// Urls owns the URL tables and serves the RPC boundary over them.
	//
	// It carries no `events` block, which is the shape of the rule rather
	// than an omission: a boundary of ownership is an RPC, and this service
	// is on the answering end of one. It publishes nothing and consumes
	// nothing.
	Urls struct {
		Listen   Listen   `json:"listen"`
		Probes   Listen   `json:"probes"`
		Log      Log      `json:"log"`
		Drain    Drain    `json:"drain"`
		TLS      TLS      `json:"tls"`
		Database Postgres `json:"database"`
	}

	// Redirect resolves short keys and says what happened.
	Redirect struct {
		Listen   Listen   `json:"listen"`
		Probes   Listen   `json:"probes"`
		Log      Log      `json:"log"`
		Drain    Drain    `json:"drain"`
		TLS      TLS      `json:"tls"`
		Database Postgres `json:"database"`
		Events   struct {
			NATS            NATS   `json:"nats"`
			RedirectSubject string `json:"redirectSubject"`
			RequestSubject  string `json:"requestSubject"`
		} `json:"events"`
	}

	// Stat counts what the redirect service published.
	//
	// It has NO database block. The table belongs to the URL service and the
	// counter asks it — so the credential this component would otherwise
	// hold, and the rights that credential would carry, simply do not exist
	// here. That is the ownership rule showing up as an absence, which is
	// the shape it usually takes.
	Stat struct {
		Probes Listen `json:"probes"`
		Log    Log    `json:"log"`
		Drain  Drain  `json:"drain"`
		// The counter SERVES nothing but its probes, and still carries a
		// transport block: it is the example's first in-cluster RPC client,
		// and a client presents an identity too. The same block does both
		// jobs, which is why `transport.Load` returns something that can
		// answer `Server()` and `Client()`.
		TLS    TLS    `json:"tls"`
		Urls   Client `json:"urls"`
		Events struct {
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

// LoadUrls reads the URL service's configuration file and validates it
// against the service's schema.
func LoadUrls(path string) (Urls, error) {
	var c Urls
	return c, config.Load(path, Read("urls.json"), &c)
}

// LoadStat reads the stat service's configuration file and validates it
// against the service's schema.
func LoadStat(path string) (Stat, error) {
	var c Stat
	return c, config.Load(path, Read("stat.json"), &c)
}
