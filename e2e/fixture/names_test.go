package fixture

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFixtureProvidesWhatTheInfraChartWouldCreate is the drift guard.
//
// It renders url-shortener-infra (at tier: test, the box's tier — see that
// chart's values.yaml) a SECOND time, independently of Resolve, and reads
// the role and stream names straight out of that render. It then asks
// Resolve for the Names this fixture would create and reads them back
// through Provides — the same accessor apply.sh uses to know what to
// create. The two must agree on every name that lives on the infra chart's
// side of the interface.
//
// Rendering twice, rather than asserting against Resolve's own output, is
// what keeps this from being a test that can never fail: Resolve and this
// test call `helm template` independently, so a change to what the CHART
// renders is caught by BOTH, and a change to how Resolve WIRES a rendered
// value into Names — a typo, a field left at its zero value, a copy-paste
// that reads the wrong path — is caught by this one, because Provides()
// only ever reports what Resolve actually decided.
//
// The consumer durable names have no infra-chart side to compare against —
// nothing here renders a Consumer, because the client libraries create
// their own durable pull consumer on connect (see this package's doc
// comment) — so they are read from the application chart's OWN render
// instead, on the same terms.
func TestFixtureProvidesWhatTheInfraChartWouldCreate(t *testing.T) {
	o := DefaultOptions()

	root := t.TempDir()
	if err := writeCharts(root); err != nil {
		t.Fatal(err)
	}

	// Rendered under o.AppRelease, on purpose — the same release name
	// Resolve itself templates the infra chart under. See Options' doc
	// comment in names.go for why there is no separate infra release.
	appSecret := o.AppRelease + "-pg-runtime"
	infra, err := helmTemplate(o.AppRelease, filepath.Join(root, "url-shortener-infra"), o.Namespace,
		"--set", "postgres.runtimePasswordSecret="+appSecret,
	)
	if err != nil {
		t.Fatalf("rendering url-shortener-infra: %v", err)
	}

	cluster, err := docOfKind(infra, "Cluster")
	if err != nil {
		t.Fatal(err)
	}
	wantOwnerRole, err := stringPath(cluster, "spec", "bootstrap", "initdb", "owner")
	if err != nil {
		t.Fatal(err)
	}
	wantAppRole, err := stringPath(cluster, "spec", "managed", "roles", 0, "name")
	if err != nil {
		t.Fatal(err)
	}

	stream, err := docOfKind(infra, "Stream")
	if err != nil {
		t.Fatal(err)
	}
	wantStream, err := stringPath(stream, "spec", "name")
	if err != nil {
		t.Fatal(err)
	}
	wantSubjects, err := stringSlicePath(stream, "spec", "subjects")
	if err != nil {
		t.Fatal(err)
	}
	wantRedirect, wantRequest, err := splitSubjects(wantSubjects)
	if err != nil {
		t.Fatal(err)
	}

	app, err := helmTemplate(o.AppRelease, filepath.Join(root, "url-shortener"), o.Namespace,
		"--set", "database.host=placeholder",
		"--set", "database.owner.passwordSecret=placeholder",
		"--set", "database.app.passwordSecret=placeholder",
		"--set", "events.url=nats://placeholder:4222",
		"--set", "archive.bucket.name=placeholder",
	)
	if err != nil {
		t.Fatalf("rendering url-shortener: %v", err)
	}
	wantStat, err := configMapField(app, "stat.yaml", "events", "consumer", "durable")
	if err != nil {
		t.Fatal(err)
	}
	wantLog, err := configMapField(app, "log.yaml", "events", "consumer", "durable")
	if err != nil {
		t.Fatal(err)
	}

	names, err := Resolve(o)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got := names.Provides()

	want := map[string]string{
		"role:owner":       wantOwnerRole,
		"role:app":         wantAppRole,
		"stream":           wantStream,
		"subject:redirect": wantRedirect,
		"subject:request":  wantRequest,
		"consumer:stat":    wantStat,
		"consumer:log":     wantLog,
	}
	for key, w := range want {
		if g := got[key]; g != w {
			t.Errorf("%s: the fixture provides %q, the chart renders %q", key, g, w)
		}
	}

	// The Secrets and the bucket have no chart-rendered side to compare —
	// the infra chart takes the runtime Secret's name as a required VALUE
	// rather than deriving it, and the owner Secret and the bucket are
	// never rendered at all under `tier: test`. What is still worth
	// asserting is that Resolve did not leave any of them empty, which is
	// the shape a wiring bug (a struct field never set) takes here.
	for _, key := range []string{"secret:owner", "secret:app", "bucket"} {
		if got[key] == "" {
			t.Errorf("%s: the fixture provides an empty name", key)
		}
	}
}

// TestWriteChartsRoundTrips is a narrow sanity check on the embed helper
// duplicated from the chart tests: it must produce a directory `helm` can
// read at all, independent of anything Resolve does with it.
func TestWriteChartsRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if err := writeCharts(dir); err != nil {
		t.Fatal(err)
	}
	for _, chart := range []string{"url-shortener", "url-shortener-infra"} {
		if _, err := os.Stat(filepath.Join(dir, chart, "Chart.yaml")); err != nil {
			t.Errorf("%s/Chart.yaml: %v", chart, err)
		}
	}
}
