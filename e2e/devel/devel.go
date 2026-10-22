// Package devel is the integration suite's deploy half: it installs a
// release of the two charts into this repository's CI namespace on the devel
// cluster, and says where the release answers.
//
// THE TWO DOORS. The test process touches a release through exactly two
// doors and nothing else:
//
//  1. the Kubernetes API, with `sluisctl kube-token --audience k8s:devel`
//     (kubeconfig written by hack/kubeconfig.sh): helm install and
//     uninstall, rollout status, pod logs;
//  2. the release's PUBLIC host, https://<release>.<installs domain>, with
//     `sluisctl token --audience devel:url-shortener` (a second exchange).
//
// No ClusterIP, no database DSN, no bucket and no in-cluster DNS name in the
// test process. In a workflow both credentials are a GitHub OIDC exchange
// (`id-token: write` is the only requirement); on a laptop they are the
// `sluisctl login` session. Nothing is stored and nothing rotates.
//
// NO ESTATE PARTICULARS HERE. This repository is public and its leak canary
// refuses internal host names, so the cluster's API endpoint, the installs
// domain, the issuer and the archive bucket arrive through the environment
// (repository variables in the workflow); see FromEnv.
package devel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// App is the name the charts and the release names are built on.
	App = "url-shortener"

	// Kubecontext is the context hack/kubeconfig.sh writes.
	Kubecontext = "devel@oidc"

	// Audience is the pseudo-customer audience a release's public host
	// accepts (the gateway's JWT policy on the installs ListenerSet).
	Audience = "devel:" + App

	// ClusterAudience opens the Kubernetes API.
	ClusterAudience = "k8s:devel"

	// DefaultNamespace is this repository's CI namespace.
	DefaultNamespace = "ci-truvity-" + App

	// StandingS3Account is the namespace's standing ServiceAccount that has
	// the only Pod Identity association in a CI namespace: the archiver runs
	// as it, so it can write under the shared test bucket's prefix for this
	// namespace. A test install owns no AWS object (a tenant cannot apply
	// ACK resources).
	StandingS3Account = "tenant-test-s3"

	// ParentName names the ListenerSet a release's route attaches to: the
	// per-install wildcard group.
	ParentName = App + "-installs"

	// ParentNamespace is the namespace that ListenerSet lives in.
	ParentNamespace = "envoy-gateway-system"

	// NATSAudience is the audience of the ServiceAccount token the
	// consumers present to the broker.
	NATSAudience = "nats"

	releasePrefix = "us-r"
)

// Config is everything an install needs from outside the repository.
type Config struct {
	// Namespace is the CI namespace (E2E_NAMESPACE).
	Namespace string
	// InstallsDomain is the wildcard's base: a release answers at
	// <release>.<InstallsDomain> (E2E_INSTALLS_DOMAIN).
	InstallsDomain string
	// Issuer is the access issuer's URL for `sluisctl token` (SLUIS_ISSUER).
	Issuer string
	// ArchiveBucket and ArchiveRegion are the shared test bucket the
	// archiver writes under this namespace's prefix (E2E_ARCHIVE_BUCKET,
	// E2E_ARCHIVE_REGION).
	ArchiveBucket string
	ArchiveRegion string
	// ChartsDir holds the packaged charts (E2E_CHARTS_DIR, default dist/charts).
	ChartsDir string
	// Release names this install (E2E_RELEASE); CI derives it from the run.
	Release string
}

// FromEnv reads the configuration, naming every missing variable at once.
func FromEnv() (Config, error) {
	c := Config{
		Namespace:      getenv("E2E_NAMESPACE", DefaultNamespace),
		InstallsDomain: strings.TrimSpace(os.Getenv("E2E_INSTALLS_DOMAIN")),
		Issuer:         strings.TrimSpace(os.Getenv("SLUIS_ISSUER")),
		ArchiveBucket:  strings.TrimSpace(os.Getenv("E2E_ARCHIVE_BUCKET")),
		ArchiveRegion:  strings.TrimSpace(os.Getenv("E2E_ARCHIVE_REGION")),
		ChartsDir:      getenv("E2E_CHARTS_DIR", filepath.Join("dist", "charts")),
		Release:        strings.TrimSpace(os.Getenv("E2E_RELEASE")),
	}

	if c.Release == "" {
		c.Release = DefaultRelease(os.Getenv)
	}

	var missing []string

	for name, value := range map[string]string{
		"E2E_INSTALLS_DOMAIN": c.InstallsDomain,
		"SLUIS_ISSUER":        c.Issuer,
		"E2E_ARCHIVE_BUCKET":  c.ArchiveBucket,
		"E2E_ARCHIVE_REGION":  c.ArchiveRegion,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("not set: %s (repository variables in the workflow; see .github/workflows/integration.yaml)", strings.Join(missing, ", "))
	}

	return c, nil
}

// DefaultRelease is unique per run and attempt in CI, and per person
// elsewhere.
func DefaultRelease(env func(string) string) string {
	if run := strings.TrimSpace(env("GITHUB_RUN_ID")); run != "" {
		attempt := strings.TrimSpace(env("GITHUB_RUN_ATTEMPT"))
		if attempt == "" {
			attempt = "1"
		}

		return fmt.Sprintf("%s%s-a%s", releasePrefix, run, attempt)
	}

	user := strings.ToLower(strings.TrimSpace(env("USER")))
	user = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}

		return -1
	}, user)

	if user == "" {
		user = "local"
	}

	if len(user) > 12 {
		user = user[:12]
	}

	return "us-l-" + user
}

// IsRunRelease reports whether a release name is one DefaultRelease makes
// in CI: only those are ever swept.
func IsRunRelease(name string) bool { return strings.HasPrefix(name, releasePrefix) }

// InfraRelease is the infrastructure release of the pair.
func (c Config) InfraRelease() string { return c.Release + "-infra" }

// Cluster is the database Cluster the infra chart renders: <infra release>-pg.
func (c Config) Cluster() string { return c.InfraRelease() + "-pg" }

// DatabaseHost is the read-write Service of that Cluster (the chart appends
// the namespace and the cluster domain, so the host verifies against the
// certificate CNPG issues for it).
func (c Config) DatabaseHost() string { return c.Cluster() + "-rw" }

// RuntimeSecret is the runtime role's basic-auth Secret, which the install
// creates before the infra release (the chart never invents one).
func (c Config) RuntimeSecret() string { return c.Release + "-pg-runtime" }

// OwnerSecret is the owner role's Secret, which CNPG creates with the
// Cluster.
func (c Config) OwnerSecret() string { return c.Cluster() + "-app" }

// CAConfigMap is the ConfigMap the services verify the database against.
func (c Config) CAConfigMap() string { return c.Release + "-pg-ca" }

// Host is the release's public host: one label under the installs domain.
func (c Config) Host() string { return c.Release + "." + c.InstallsDomain }

// BaseURL is the release's public base URL.
func (c Config) BaseURL() string { return "https://" + c.Host() }

// ArchivePrefix is where the archiver writes: under this namespace and
// release, because the shared bucket's policy confines a workload to its
// namespace's keys.
func (c Config) ArchivePrefix() string {
	return c.Namespace + "/" + c.Release + "/" + App + "/requests"
}

// Accounts are the ServiceAccounts the install creates itself (the charts'
// own would collide with the standing account the archiver must use):
// one per component but `log`, and the migration's.
func (c Config) Accounts() []string {
	return []string{c.Release + "-redirect", c.Release + "-urls", c.Release + "-web", c.Release + "-stat", c.Release + "-migrate"}
}

// InfraSet is the infrastructure release's values.
func (c Config) InfraSet() []string {
	return []string{
		// The name the pair shares: the stream and the subjects derive from
		// it, and the application release below is told the same.
		"installName=" + c.Release,
		"tier=test",
		"postgres.runtimePasswordSecret=" + c.RuntimeSecret(),
		// The broker gives each namespace an account of its own; the stream
		// is created inside it, and one copy is a test's answer.
		"events.account=" + c.Namespace,
		"events.replicas=1",
		"events.storage=memory",
		"events.maxAge=1h",
	}
}

// AppSet is the application release's values. Everything in it is
// test-shaped; production values come from the delivery pipeline.
func (c Config) AppSet() []string {
	set := []string{
		"installName=" + c.Release,
		"tier=test",

		// The database: dialled by its Service, verified against the CA the
		// install published from the Cluster's own authority.
		"database.host=" + c.DatabaseHost(),
		"database.tls.rootCA.configMapName=" + c.CAConfigMap(),
		"database.migration.passwordSecret=" + c.OwnerSecret(),
		"database.app.passwordSecret=" + c.RuntimeSecret(),

		// The broker, authenticating with a ServiceAccount token.
		"events.url=nats://nats.nats.svc:4222",
		"events.auth.audience=" + NATSAudience,

		// The archive: the shared test bucket under this release's prefix,
		// closed every few seconds so a test sees its record.
		"archive.bucket.name=" + c.ArchiveBucket,
		"archive.bucket.region=" + c.ArchiveRegion,
		"archive.prefix=" + c.ArchivePrefix(),
		"archive.batch.maxRecords=5",
		"archive.batch.maxSeconds=5",

		// The accounts: made by the install (Accounts), except the
		// archiver's, which is the namespace's standing one.
		"serviceAccount.create=false",
		"serviceAccount.components.log.name=" + StandingS3Account,

		// The route: the public host, on the installs ListenerSet.
		"route.enabled=true",
		"route.hostname=" + c.Host(),
		"route.parentRef.name=" + ParentName,
		"route.parentRef.group=gateway.networking.k8s.io",
		"route.parentRef.kind=ListenerSet",
		"route.parentRef.namespace=" + ParentNamespace,
	}

	return set
}

func getenv(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}

	return def
}
