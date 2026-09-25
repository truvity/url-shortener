// Package fixture stands in for the url-shortener-infra chart on the local
// cluster.
//
// On a public repository's kind box the infra chart is not installed at
// all: it binds an application to one platform's choices (a database
// operator, a broker's controller, cloud objects), and the box is servers
// only — see docs/decisions/0005-kind-is-the-gate.md and
// hack/kind/README.md. Something still has to create the database, the two
// roles, the stream and the bucket the application chart's values point at,
// under the EXACT names that chart expects. This package is that something.
//
// It takes those names FROM the charts rather than repeating them: Resolve
// renders url-shortener-infra the same way a real install would, and reads
// back what it would have created. A chart that changes how it names a
// role, a stream or a subject changes what this package creates the next
// time it runs, with nothing here to edit — see names_test.go, which proves
// this by breaking it on purpose.
package fixture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/truvity/policy/examples/url-shortener/charts"
)

// boxPostgresAddress is the one Postgres server the local cluster carries
// (hack/kind/postgres.yaml), reached cross-namespace the same short way the
// example's own scripts already reach NATS and S3 — "<service>.<namespace>.svc"
// — rather than through a same-named Service in the application's own
// namespace. A Service of that shape was tried first, as an ExternalName
// pointed at this address, to mimic the name CNPG would have given its
// read-write endpoint; Kubernetes' DNS chains an ExternalName's CNAME
// through the resolver's search path, and that chain failed here with
// "server misbehaving" rather than resolving. Reaching the real address
// directly has no such indirection to fail.
const boxPostgresAddress = "postgres.postgres.svc"

// Options are the installer's choices: which namespace this install uses,
// which release name the application chart installs under, and the names
// this fixture itself invents for the handful of things no chart derives —
// the two password Secrets' names (the infra chart takes the runtime one as
// a required value; the owner one is CNPG's own naming convention, not this
// chart's) and the archive bucket (a cloud object the infra chart only
// names under a `primary` install, which the local box never is).
//
// There is no separate InfraRelease. The infra chart is never actually
// installed on this box (see this package's doc comment) — Resolve only
// TEMPLATES it, and templates it under AppRelease, the same name the real
// application release uses. That is deliberate, not a simplification: both
// charts compute their cluster-global event names (the stream, its
// subjects, the durable consumers) from their OWN release name by default,
// and will keep doing so once truvity/policy#71 lands an explicit
// `installName` value with exactly that default. Rendering both charts
// under one name is what keeps the two computations equal on either side of
// that change, with nothing here to update when it merges.
type Options struct {
	Namespace  string
	AppRelease string
	Bucket     string
}

// DefaultOptions are what the example's own scripts use: hack/install.sh,
// hack/smoke.sh and this package's own apply.sh must all agree on these, and
// do — by calling this rather than repeating them.
func DefaultOptions() Options {
	return Options{
		Namespace:  "shortener",
		AppRelease: "example",
		Bucket:     "url-shortener-archive",
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.Namespace == "" {
		o.Namespace = d.Namespace
	}
	if o.AppRelease == "" {
		o.AppRelease = d.AppRelease
	}
	if o.Bucket == "" {
		o.Bucket = d.Bucket
	}
	return o
}

// Names is everything the fixture creates, and everything the application
// chart must be given to find it.
type Names struct {
	Options

	Database     string // the database name, from the infra chart's bootstrap.initdb.database
	OwnerRole    string // migrates: creates tables and grants the runtime role its rights
	AppRole      string // the runtime role every service connects as
	DatabaseHost string // the box's one Postgres server, reached the same cross-namespace way NATS and S3 already are: <service>.<namespace>.svc
	OwnerSecret  string // the owner role's password Secret, under CNPG's own naming: <cluster>-app
	AppSecret    string // the runtime role's password Secret; this fixture names it and tells the infra chart

	Stream          string   // the infra chart's Stream.spec.name
	Subjects        []string // the infra chart's Stream.spec.subjects
	RedirectSubject string   // whichever of Subjects the redirect service publishes to
	RequestSubject  string   // whichever of Subjects the archiver reads
	StatConsumer    string   // the durable name the counter binds to (the app chart's own value, not the infra chart's)
	LogConsumer     string   // the durable name the archiver binds to
}

// Resolve renders both charts and reads back the names they use. It needs
// `helm` on PATH and nothing else — no cluster, no network beyond what helm
// itself needs to run locally, which is none: both charts have no
// dependencies to fetch.
func Resolve(o Options) (Names, error) {
	o = o.withDefaults()

	root, err := os.MkdirTemp("", "url-shortener-fixture-charts-")
	if err != nil {
		return Names{}, err
	}
	defer func() { _ = os.RemoveAll(root) }()
	if err := writeCharts(root); err != nil {
		return Names{}, err
	}

	// Both charts are templated under AppRelease — see Options' doc comment
	// for why that, and not two different release names, is what keeps the
	// infra chart's idea of the stream equal to the app chart's.
	clusterName := o.AppRelease + "-pg"
	appSecret := o.AppRelease + "-pg-runtime"

	infra, err := helmTemplate(o.AppRelease, filepath.Join(root, "url-shortener-infra"), o.Namespace,
		"--set", "postgres.runtimePasswordSecret="+appSecret,
	)
	if err != nil {
		return Names{}, fmt.Errorf("rendering url-shortener-infra: %w", err)
	}

	cluster, err := docOfKind(infra, "Cluster")
	if err != nil {
		return Names{}, err
	}
	renderedClusterName, err := stringPath(cluster, "metadata", "name")
	if err != nil {
		return Names{}, fmt.Errorf("cluster metadata.name: %w", err)
	}
	if renderedClusterName != clusterName {
		// The infra chart names the Cluster after ITS OWN release name; this
		// fixture must agree, because OwnerSecret below is built from
		// clusterName without rendering again.
		return Names{}, fmt.Errorf("the infra chart named the Cluster %q, expected %q — its naming changed and this package was not updated to match", renderedClusterName, clusterName)
	}
	database, err := stringPath(cluster, "spec", "bootstrap", "initdb", "database")
	if err != nil {
		return Names{}, fmt.Errorf("cluster spec.bootstrap.initdb.database: %w", err)
	}
	ownerRole, err := stringPath(cluster, "spec", "bootstrap", "initdb", "owner")
	if err != nil {
		return Names{}, fmt.Errorf("cluster spec.bootstrap.initdb.owner: %w", err)
	}
	appRole, err := stringPath(cluster, "spec", "managed", "roles", 0, "name")
	if err != nil {
		return Names{}, fmt.Errorf("cluster spec.managed.roles[0].name: %w", err)
	}

	stream, err := docOfKind(infra, "Stream")
	if err != nil {
		return Names{}, err
	}
	streamName, err := stringPath(stream, "spec", "name")
	if err != nil {
		return Names{}, fmt.Errorf("stream spec.name: %w", err)
	}
	subjects, err := stringSlicePath(stream, "spec", "subjects")
	if err != nil {
		return Names{}, fmt.Errorf("stream spec.subjects: %w", err)
	}
	if len(subjects) != 2 {
		return Names{}, fmt.Errorf("the Stream carries %d subjects, want exactly 2 (redirect and request)", len(subjects))
	}
	redirectSubject, requestSubject, err := splitSubjects(subjects)
	if err != nil {
		return Names{}, err
	}

	// The application chart's OWN defaults for the two durable consumer
	// names — the infra chart has no opinion about them at all, because
	// nothing here creates a Consumer resource; the client libraries bind
	// (and create, if missing) their own durable pull consumer on connect.
	app, err := helmTemplate(o.AppRelease, filepath.Join(root, "url-shortener"), o.Namespace,
		"--set", "database.host=placeholder",
		"--set", "database.owner.passwordSecret=placeholder",
		"--set", "database.app.passwordSecret=placeholder",
		"--set", "events.url=nats://placeholder:4222",
		"--set", "archive.bucket.name=placeholder",
	)
	if err != nil {
		return Names{}, fmt.Errorf("rendering url-shortener: %w", err)
	}
	statConsumer, err := configMapField(app, "stat.yaml", "events", "consumer", "durable")
	if err != nil {
		return Names{}, err
	}
	logConsumer, err := configMapField(app, "log.yaml", "events", "consumer", "durable")
	if err != nil {
		return Names{}, err
	}

	return Names{
		Options:         o,
		Database:        database,
		OwnerRole:       ownerRole,
		AppRole:         appRole,
		DatabaseHost:    boxPostgresAddress,
		OwnerSecret:     clusterName + "-app",
		AppSecret:       appSecret,
		Stream:          streamName,
		Subjects:        subjects,
		RedirectSubject: redirectSubject,
		RequestSubject:  requestSubject,
		StatConsumer:    statConsumer,
		LogConsumer:     logConsumer,
	}, nil
}

// splitSubjects tells the redirect subject from the request one by what
// each NAMES, not by position — the infra chart's own values list them in a
// fixed order today, but that is an accident of the current default, not an
// interface this package should depend on.
func splitSubjects(subjects []string) (redirect, request string, err error) {
	for _, s := range subjects {
		switch {
		case strings.Contains(s, "redirect"):
			redirect = s
		case strings.Contains(s, "log") || strings.Contains(s, "request"):
			request = s
		}
	}
	if redirect == "" || request == "" {
		return "", "", fmt.Errorf("could not tell the redirect subject from the request one in %v: neither name contains \"redirect\" or \"log\"/\"request\"", subjects)
	}
	return redirect, request, nil
}

// helmTemplate runs `helm template` and returns the raw render.
func helmTemplate(release, chartDir, namespace string, extra ...string) (string, error) {
	args := append([]string{"template", release, chartDir, "--namespace", namespace}, extra...)
	cmd := exec.Command("helm", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("helm %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// writeCharts extracts the embedded charts to dir, exactly as the chart
// tests do (charts_test.go's chartDir) — duplicated rather than shared,
// because that helper takes a *testing.T and this package has callers
// (apply.sh, by way of cmd/resolve) that are not tests.
func writeCharts(dir string) error {
	var write func(sub string) error
	write = func(sub string) error {
		entries, err := charts.Files.ReadDir(sub)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := filepath.Join(sub, e.Name())
			out := filepath.Join(dir, p)
			if e.IsDir() {
				if err := os.MkdirAll(out, 0o755); err != nil {
					return err
				}
				if err := write(p); err != nil {
					return err
				}
				continue
			}
			b, err := charts.Files.ReadFile(p)
			if err != nil {
				return err
			}
			if err := os.WriteFile(out, b, 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	top, err := charts.Files.ReadDir(".")
	if err != nil {
		return err
	}
	for _, e := range top {
		if !e.IsDir() {
			continue
		}
		if err := os.MkdirAll(filepath.Join(dir, e.Name()), 0o755); err != nil {
			return err
		}
		if err := write(e.Name()); err != nil {
			return err
		}
	}
	return nil
}

// docOfKind decodes a multi-document render and returns the one document of
// the given kind, as a generic map.
func docOfKind(rendered, kind string) (map[string]any, error) {
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	var found map[string]any
	var kinds []string
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding the render: %w", err)
		}
		if m == nil {
			continue
		}
		if k, _ := m["kind"].(string); k != "" {
			kinds = append(kinds, k)
			if k == kind {
				if found != nil {
					return nil, fmt.Errorf("more than one %s in the render", kind)
				}
				found = m
			}
		}
	}
	if found == nil {
		return nil, fmt.Errorf("no %s in the render; found %v", kind, kinds)
	}
	return found, nil
}

// configMapField renders reaches into a ConfigMap's named data key, parses
// IT as YAML too (every chart-rendered configuration file here is YAML),
// and reads a nested field out of that.
func configMapField(rendered, dataKey string, path ...string) (string, error) {
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var m struct {
			Kind string            `yaml:"kind"`
			Data map[string]string `yaml:"data"`
		}
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("decoding the render: %w", err)
		}
		if m.Kind != "ConfigMap" {
			continue
		}
		file, ok := m.Data[dataKey]
		if !ok {
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(file), &doc); err != nil {
			return "", fmt.Errorf("%s is not valid YAML: %w", dataKey, err)
		}
		anyPath := make([]any, len(path))
		for i, p := range path {
			anyPath[i] = p
		}
		return stringPath(doc, anyPath...)
	}
	return "", fmt.Errorf("no ConfigMap in the render carries a %q key", dataKey)
}

// stringPath walks a decoded YAML document by a path of string keys and int
// indices, and returns the string at the end of it.
func stringPath(doc any, path ...any) (string, error) {
	cur := doc
	for _, step := range path {
		switch k := step.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return "", fmt.Errorf("expected a mapping at %v, got %T", path, cur)
			}
			cur, ok = m[k]
			if !ok {
				return "", fmt.Errorf("no %q at %v", k, path)
			}
		case int:
			s, ok := cur.([]any)
			if !ok {
				return "", fmt.Errorf("expected a list at %v, got %T", path, cur)
			}
			if k >= len(s) {
				return "", fmt.Errorf("index %d out of range at %v", k, path)
			}
			cur = s[k]
		}
	}
	s, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("%v is a %T, not a string", path, cur)
	}
	return s, nil
}

// stringSlicePath is stringPath for a list of strings.
func stringSlicePath(doc any, path ...any) ([]string, error) {
	cur := doc
	for _, step := range path {
		switch k := step.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("expected a mapping at %v, got %T", path, cur)
			}
			cur, ok = m[k]
			if !ok {
				return nil, fmt.Errorf("no %q at %v", k, path)
			}
		case int:
			s, ok := cur.([]any)
			if !ok {
				return nil, fmt.Errorf("expected a list at %v, got %T", path, cur)
			}
			cur = s[k]
		}
	}
	list, ok := cur.([]any)
	if !ok {
		return nil, fmt.Errorf("%v is a %T, not a list", path, cur)
	}
	out := make([]string, len(list))
	for i, v := range list {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%v[%d] is a %T, not a string", path, i, v)
		}
		out[i] = s
	}
	return out, nil
}
