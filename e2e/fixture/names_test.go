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

	top := t.TempDir()
	root := filepath.Join(top, "examples", "url-shortener", "charts")
	if err := writeCharts(top, root); err != nil {
		t.Fatal(err)
	}

	// Rendered under o.AppRelease, on purpose — the same release name
	// Resolve itself templates the infra chart under. See Options' doc
	// comment in names.go for why there is no separate infra release.
	appSecret := o.AppRelease + "-pg-runtime"
	// tenantScopedNames=true, on the same terms as Resolve's own call in
	// names.go — an independent render that left it off would not agree
	// with Resolve on the database or role names, for a reason that has
	// nothing to do with either side drifting.
	infra, err := helmTemplate(o.AppRelease, filepath.Join(root, "url-shortener-infra"), o.Namespace,
		"--set", "postgres.runtimePasswordSecret="+appSecret,
		"--set", "postgres.tenantScopedNames=true",
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
		"--set", "database.tls.rootCA.configMapName=placeholder",
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

// TestPostgresNamesAreTenantScoped is the fixture-level twin of the
// infrastructure chart's own TestClusterGlobalNamesAreTenantScoped
// (charts/tenant_scope_test.go): the local cluster's one Postgres server is
// shared by every install the same way its NATS broker is, so a database or
// role name that repeats between two tenants finds the other's — which is
// exactly the bug this test guards. Two collision shapes, on the same terms
// as the chart's own test:
//   - two installs sharing a namespace but not a release (two agents, or an
//     engineer beside CI, in one namespace);
//   - two installs sharing a release name but not a namespace (two
//     engineers who each call their copy "example").
//
// A regression here does not fail loudly: both fixtures apply, both
// installs come up, and the second one's run silently resets the
// credentials the first is already running on — see names.go's package
// doc comment.
func TestPostgresNamesAreTenantScoped(t *testing.T) {
	type tenant struct{ namespace, release string }
	cases := []tenant{
		{"fx-shared", "a"},
		{"fx-shared", "b"}, // same namespace, different release
		{"fx-a", "shared"},
		{"fx-b", "shared"}, // same release, different namespace
	}

	type identifiers struct{ database, owner, app string }
	got := make(map[tenant]identifiers, len(cases))
	for _, tc := range cases {
		names, err := Resolve(Options{Namespace: tc.namespace, AppRelease: tc.release})
		if err != nil {
			t.Fatalf("Resolve(%s/%s): %v", tc.namespace, tc.release, err)
		}
		got[tc] = identifiers{names.Database, names.OwnerRole, names.AppRole}
	}

	for i, a := range cases {
		for _, b := range cases[i+1:] {
			ga, gb := got[a], got[b]
			if ga.database == gb.database {
				t.Errorf("%+v and %+v share a database name %q", a, b, ga.database)
			}
			if ga.owner == gb.owner {
				t.Errorf("%+v and %+v share an owner role %q", a, b, ga.owner)
			}
			if ga.app == gb.app {
				t.Errorf("%+v and %+v share an app role %q", a, b, ga.app)
			}
		}
	}
}

// TestPostgresNamesStayWithinThePostgresLimitWhenTheScopeIsLong is the
// truncate-and-hash rule, proved from the fixture's own side: Postgres
// refuses an identifier over 63 bytes (NAMEDATALEN - 1), and two tenants
// long enough to be truncated to the same 48-character prefix must still
// not land on the same name — see
// charts/url-shortener-infra/templates/_helpers.tpl's
// "url-shortener-infra.postgresBase" for the rule itself.
func TestPostgresNamesStayWithinThePostgresLimitWhenTheScopeIsLong(t *testing.T) {
	// 63 characters — the maximum a Kubernetes namespace allows — so the
	// combined "<namespace>-<release>" scope is well past Postgres' own
	// 63-byte identifier limit before this package does anything about it.
	long := "a-very-long-namespace-name-that-runs-right-up-against-the-limit"

	one, err := Resolve(Options{Namespace: long, AppRelease: "one"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	two, err := Resolve(Options{Namespace: long, AppRelease: "two"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for _, n := range []Names{one, two} {
		for label, id := range map[string]string{"database": n.Database, "owner role": n.OwnerRole, "app role": n.AppRole} {
			if len(id) > 63 {
				t.Errorf("%s %q is %d bytes, over Postgres' 63-byte identifier limit", label, id, len(id))
			}
		}
	}
	if one.Database == two.Database {
		t.Errorf("two different tenants truncated to the same database name %q", one.Database)
	}
	if one.OwnerRole == two.OwnerRole {
		t.Errorf("two different tenants truncated to the same owner role %q", one.OwnerRole)
	}
	if one.AppRole == two.AppRole {
		t.Errorf("two different tenants truncated to the same app role %q", one.AppRole)
	}
}

// TestWriteChartsRoundTrips is a narrow sanity check on the embed helper
// duplicated from the chart tests: it must produce a directory `helm` can
// read at all, independent of anything Resolve does with it.
func TestWriteChartsRoundTrips(t *testing.T) {
	top := t.TempDir()
	dir := filepath.Join(top, "examples", "url-shortener", "charts")
	if err := writeCharts(top, dir); err != nil {
		t.Fatal(err)
	}
	for _, chart := range []string{"url-shortener", "url-shortener-infra"} {
		if _, err := os.Stat(filepath.Join(dir, chart, "Chart.yaml")); err != nil {
			t.Errorf("%s/Chart.yaml: %v", chart, err)
		}
	}
}
