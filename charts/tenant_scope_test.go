package charts_test

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/truvity/policy/conformance"

	yaml "go.yaml.in/yaml/v3"
)

// unmarshalYAML is a thin wrapper so the fixtures below read as what they
// are — a rendered configuration file, decoded into the shape this test
// cares about — rather than an inline error check at every call site.
func unmarshalYAML(t *testing.T, doc []byte, v any) {
	t.Helper()
	if err := yaml.Unmarshal(doc, v); err != nil {
		t.Fatalf("not valid YAML: %v\n%s", err, doc)
	}
}

// renderNamed is render/renderInfra with the release name and namespace as
// parameters, instead of fixed to "example" and whatever `helm template`
// defaults to. A tenant is exactly this pair — see
// templates/_helpers.tpl's "eventsScope" in both charts — so a test of
// tenant isolation has to be able to vary both independently.
func renderNamed(t *testing.T, chart, release, namespace string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("helm", append([]string{
		"template", release, chartDir(t, chart), "--namespace", namespace,
	}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tenantNames is what one tenant's pair of releases produces: the
// infrastructure chart's stream and two subjects, its database and two
// role names, and the application chart's own idea of its stream, its two
// subjects, and its two durable consumer names.
//
// Both charts are rendered as the SAME release name in the same namespace,
// which is the pairing platform.md rule 11 describes as the common case
// and the one neither chart's installName default needs help with.
type tenantNames struct {
	stream    string
	subjects  []string // redirect, request
	consumers []string // stat, log
	database  string
	ownerRole string
	appRole   string
}

func namesFor(t *testing.T, namespace, install string) tenantNames {
	t.Helper()

	// tenantScopedNames=true: the database and role names are OFF by
	// default (values.yaml), for every consumer that only ever relied on
	// this chart's fixed names — this helper is specifically about the
	// tenant-scoping rule, so it turns the flag on rather than asserting
	// against the fixed names every other tenant would also get.
	infra, err := renderNamed(t, "url-shortener-infra", install, namespace,
		append(infraDefaults(), "--set", "postgres.tenantScopedNames=true")...)
	if err != nil {
		t.Fatalf("infra chart does not render for %s/%s: %v\n%s", namespace, install, err, infra)
	}
	stream := docOfKind(t, infra, "Stream")
	spec, _ := stream["spec"].(map[string]any)
	infraStreamName, _ := spec["name"].(string)
	var infraSubjects []string
	for _, s := range spec["subjects"].([]any) {
		infraSubjects = append(infraSubjects, s.(string))
	}

	cluster := docOfKind(t, infra, "Cluster")
	clusterSpec, _ := cluster["spec"].(map[string]any)
	initdb, _ := clusterSpec["bootstrap"].(map[string]any)["initdb"].(map[string]any)
	database, _ := initdb["database"].(string)
	ownerRole, _ := initdb["owner"].(string)
	managed, _ := clusterSpec["managed"].(map[string]any)
	roles, _ := managed["roles"].([]any)
	role, _ := roles[0].(map[string]any)
	appRole, _ := role["name"].(string)

	app, err := renderNamed(t, "url-shortener", install, namespace, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatalf("application chart does not render for %s/%s: %v\n%s", namespace, install, err, app)
	}

	type redirectFile struct {
		Events struct {
			RedirectSubject string `yaml:"redirectSubject"`
			RequestSubject  string `yaml:"requestSubject"`
		} `yaml:"events"`
	}
	var rf redirectFile
	unmarshalYAML(t, conformance.ConfigMapData(t, []byte(app), "redirect.yaml"), &rf)

	type consumerFile struct {
		Events struct {
			Consumer struct {
				Stream  string `yaml:"stream"`
				Durable string `yaml:"durable"`
			} `yaml:"consumer"`
		} `yaml:"events"`
	}
	var stat, log consumerFile
	unmarshalYAML(t, conformance.ConfigMapData(t, []byte(app), "stat.yaml"), &stat)
	unmarshalYAML(t, conformance.ConfigMapData(t, []byte(app), "log.yaml"), &log)

	// The two charts are two computations of ONE name; if they ever
	// disagreed, the application chart would be connecting to a stream
	// the infrastructure chart did not create. That is the "MUST MATCH"
	// comment in both helpers, made mechanical.
	if stat.Events.Consumer.Stream != infraStreamName || log.Events.Consumer.Stream != infraStreamName {
		t.Errorf("%s/%s: the application chart's stream (%q, %q) does not match the infrastructure chart's (%q)",
			namespace, install, stat.Events.Consumer.Stream, log.Events.Consumer.Stream, infraStreamName)
	}
	appSubjects := []string{rf.Events.RedirectSubject, rf.Events.RequestSubject}
	if !sameSet(appSubjects, infraSubjects) {
		t.Errorf("%s/%s: the application chart's subjects %v do not match the infrastructure chart's %v",
			namespace, install, appSubjects, infraSubjects)
	}

	return tenantNames{
		stream:    infraStreamName,
		subjects:  infraSubjects,
		consumers: []string{stat.Events.Consumer.Durable, log.Events.Consumer.Durable},
		database:  database,
		ownerRole: ownerRole,
		appRole:   appRole,
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, x := range b {
		if !seen[x] {
			return false
		}
	}
	return true
}

// The rule under test: every cluster-global name — the stream, its
// subjects, the durable consumer names, and (with `postgres.tenantScopedNames`
// on) the database and its two role names — derives from namespace AND
// install name together, so that a tenant sharing either alone with another
// tenant still gets names of its own. The database and roles are
// cluster-global on the SAME terms as the stream: the local box's one
// Postgres server has never heard of a Kubernetes namespace either, and a
// `test`-tier install standing in for a `primary` install's own dedicated
// CNPG Cluster is the one place that matters — which is exactly why that
// derivation is opt-in rather than the chart's default; see
// templates/_helpers.tpl's "url-shortener-infra.postgresBase" and
// TestPostgresNamesStayGlobalByDefault / TestExplicitPostgresNamesWinOverTenantScoping
// below for the other two sides of that rule.
//
// Three tenants, covering both collision shapes that a single input
// misses:
//   - alpha and beta share a namespace and differ only by install name —
//     two CI runs in one namespace, or a CI run beside an engineer's own
//     install.
//   - alpha and gamma share an install name and differ only by
//     namespace — two engineers who each call their copy by the app's
//     own name.
//
// A regression here does not fail loudly. Both installs render, both
// install, and one consumer quietly reads the other's events — this is
// the test that stands in for noticing that in a cluster.
func TestClusterGlobalNamesAreTenantScoped(t *testing.T) {
	alpha := namesFor(t, "tenant-ns-a", "alpha")
	beta := namesFor(t, "tenant-ns-a", "beta")   // same namespace, different install
	gamma := namesFor(t, "tenant-ns-b", "alpha") // different namespace, same install

	tenants := map[string]tenantNames{"alpha": alpha, "beta": beta, "gamma": gamma}

	var allStreams []string
	var allSubjects []string
	var allConsumers []string
	for name, tn := range tenants {
		allStreams = append(allStreams, fmt.Sprintf("%s:%s", name, tn.stream))
		for _, s := range tn.subjects {
			allSubjects = append(allSubjects, fmt.Sprintf("%s:%s", name, s))
		}
		for _, c := range tn.consumers {
			allConsumers = append(allConsumers, fmt.Sprintf("%s:%s", name, c))
		}
	}

	assertDisjoint(t, "stream", map[string]string{"alpha": alpha.stream, "beta": beta.stream, "gamma": gamma.stream})
	assertDisjoint(t, "database", map[string]string{"alpha": alpha.database, "beta": beta.database, "gamma": gamma.database})
	assertDisjoint(t, "owner role", map[string]string{"alpha": alpha.ownerRole, "beta": beta.ownerRole, "gamma": gamma.ownerRole})
	assertDisjoint(t, "app role", map[string]string{"alpha": alpha.appRole, "beta": beta.appRole, "gamma": gamma.appRole})

	for _, pair := range [][2]string{{"alpha", "beta"}, {"alpha", "gamma"}, {"beta", "gamma"}} {
		a, b := tenants[pair[0]], tenants[pair[1]]
		if overlap := intersect(a.subjects, b.subjects); len(overlap) > 0 {
			t.Errorf("%s and %s share a subject: %v", pair[0], pair[1], overlap)
		}
		if overlap := intersect(a.consumers, b.consumers); len(overlap) > 0 {
			t.Errorf("%s and %s share a durable consumer name: %v", pair[0], pair[1], overlap)
		}
	}

	t.Logf("streams: %v", allStreams)
	t.Logf("subjects: %v", allSubjects)
	t.Logf("consumers: %v", allConsumers)
}

func assertDisjoint(t *testing.T, label string, values map[string]string) {
	t.Helper()
	seen := map[string]string{}
	for tenant, v := range values {
		if owner, ok := seen[v]; ok {
			t.Errorf("%s %q is shared by %s and %s", label, v, owner, tenant)
			continue
		}
		seen[v] = tenant
	}
}

func intersect(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range a {
		in[x] = true
	}
	var out []string
	for _, x := range b {
		if in[x] {
			out = append(out, x)
		}
	}
	return out
}

// An explicit installName is what pairs the two releases when they are NOT
// installed under one release name — the override platform.md rule 11's
// standalone default does not cover. Both charts must use it, not their
// own release name, once it is set.
func TestInstallNameOverridesTheReleaseName(t *testing.T) {
	infra, err := renderNamed(t, "url-shortener-infra", "infra-release", "shared-ns",
		append(infraDefaults(), "--set", "installName=shared-name")...)
	if err != nil {
		t.Fatalf("infra chart does not render: %v\n%s", infra, err)
	}
	stream := docOfKind(t, infra, "Stream")
	name, _ := stream["spec"].(map[string]any)["name"].(string)
	if name != "shared-ns-shared-name-events" {
		t.Errorf("stream name = %q, want a name built from installName, not the release name %q", name, "infra-release")
	}

	app, err := renderNamed(t, "url-shortener", "app-release", "shared-ns",
		append(defaults("--set", "images.web.tag=dev"), "--set", "installName=shared-name")...)
	if err != nil {
		t.Fatalf("application chart does not render: %v\n%s", app, err)
	}
	var rf struct {
		Events struct {
			RedirectSubject string `yaml:"redirectSubject"`
		} `yaml:"events"`
	}
	unmarshalYAML(t, conformance.ConfigMapData(t, []byte(app), "redirect.yaml"), &rf)
	if rf.Events.RedirectSubject != "shared-ns-shared-name.redirect" {
		t.Errorf("redirectSubject = %q, want a name built from installName, not the release name %q", rf.Events.RedirectSubject, "app-release")
	}

	// The two releases, installed under DIFFERENT names, still computed
	// the SAME stream — which is the whole reason the override exists.
	if name != "shared-ns-shared-name-events" {
		t.Fatalf("sanity: stream name changed underfoot: %q", name)
	}
}

// installName is a plain string a caller supplies, unlike `.Release.Name`
// and `.Release.Namespace` which Kubernetes and Helm already keep to a
// safe shape — so this chart has to check it itself, and REFUSE rather
// than silently clean it up: a lower-cased or truncated name is a name
// that quietly stopped being the one somebody chose.
func TestInstallNameIsRefusedWhenItIsNotASafeShape(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"uppercase", "Shortener"},            // case: NATS is case-sensitive, K8s names never are
		{"dot", "short.ener"},                 // a literal dot would split a subject into an extra token
		{"too long", strings.Repeat("a", 41)}, // one over the 40-character bound
	}
	for _, tc := range cases {
		t.Run("infra/"+tc.name, func(t *testing.T) {
			out, err := renderInfra(t, append(infraDefaults(), "--set", "installName="+tc.value)...)
			if err == nil {
				t.Fatalf("rendered with installName=%q, and should not have:\n%s", tc.value, out)
			}
			if !containsInstallName(out) {
				t.Errorf("the refusal does not name installName: %s", out)
			}
		})
		t.Run("app/"+tc.name, func(t *testing.T) {
			out, err := render(t, append(defaults(), "--set", "installName="+tc.value)...)
			if err == nil {
				t.Fatalf("rendered with installName=%q, and should not have:\n%s", tc.value, out)
			}
			if !containsInstallName(out) {
				t.Errorf("the refusal does not name installName: %s", out)
			}
		})
	}
}

func containsInstallName(s string) bool {
	return strings.Contains(s, "installName")
}

// postgres.tenantScopedNames defaults to false (values.yaml), so a platform
// that has never heard of it — every existing consumer today, which sets at
// most `postgres.runtimePasswordSecret` — keeps the three fixed names this
// chart has always rendered, whatever namespace or release it installs
// under. A regression here is a silent rename on the next release: an
// existing CNPG Cluster and the application already pointed at it would be
// handed a database and owner that do not match what was actually
// bootstrapped.
func TestPostgresNamesStayGlobalByDefault(t *testing.T) {
	one, err := renderNamed(t, "url-shortener-infra", "one", "ns-a", infraDefaults()...)
	if err != nil {
		t.Fatalf("infra chart does not render: %v\n%s", err, one)
	}
	two, err := renderNamed(t, "url-shortener-infra", "two", "ns-b", infraDefaults()...)
	if err != nil {
		t.Fatalf("infra chart does not render: %v\n%s", err, two)
	}

	for _, out := range []string{one, two} {
		cluster := docOfKind(t, out, "Cluster")
		spec, _ := cluster["spec"].(map[string]any)
		initdb, _ := spec["bootstrap"].(map[string]any)["initdb"].(map[string]any)
		if got := initdb["database"]; got != "url_shortener" {
			t.Errorf("database = %v, want the fixed default url_shortener with tenantScopedNames left off", got)
		}
		if got := initdb["owner"]; got != "url_shortener_owner" {
			t.Errorf("owner = %v, want the fixed default url_shortener_owner with tenantScopedNames left off", got)
		}
		managed, _ := spec["managed"].(map[string]any)
		roles, _ := managed["roles"].([]any)
		role, _ := roles[0].(map[string]any)
		if got := role["name"]; got != "url_shortener_app" {
			t.Errorf("runtime role = %v, want the fixed default url_shortener_app with tenantScopedNames left off", got)
		}
	}
}

// An explicit name wins over tenant-scoping unconditionally: a platform that
// turns `tenantScopedNames` on — for the stream and subjects it wants
// scoped — but still names its own database explicitly (a name a DBA
// already chose, a migration path) must get exactly that name, not a
// derived one. See templates/_helpers.tpl's "url-shortener-infra.resolvedDatabase"
// and its siblings for how "explicit" is told apart from "left at the
// chart's own default" once Helm has already merged the two.
func TestExplicitPostgresNamesWinOverTenantScoping(t *testing.T) {
	out, err := renderInfra(t, append(infraDefaults(),
		"--set", "postgres.tenantScopedNames=true",
		"--set", "postgres.database=chosen_db",
		"--set", "postgres.ownerRole=chosen_owner",
		"--set", "postgres.runtimeRole=chosen_app",
	)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	cluster := docOfKind(t, out, "Cluster")
	spec, _ := cluster["spec"].(map[string]any)
	initdb, _ := spec["bootstrap"].(map[string]any)["initdb"].(map[string]any)
	if got := initdb["database"]; got != "chosen_db" {
		t.Errorf("database = %v, want the explicit chosen_db even with tenantScopedNames on", got)
	}
	if got := initdb["owner"]; got != "chosen_owner" {
		t.Errorf("owner = %v, want the explicit chosen_owner even with tenantScopedNames on", got)
	}
	managed, _ := spec["managed"].(map[string]any)
	roles, _ := managed["roles"].([]any)
	role, _ := roles[0].(map[string]any)
	if got := role["name"]; got != "chosen_app" {
		t.Errorf("runtime role = %v, want the explicit chosen_app even with tenantScopedNames on", got)
	}
}
