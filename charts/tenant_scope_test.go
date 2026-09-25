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
// infrastructure chart's stream and two subjects, and the application
// chart's own idea of its stream, its two subjects, and its two durable
// consumer names.
//
// Both charts are rendered as the SAME release name in the same namespace,
// which is the pairing platform.md rule 11 describes as the common case
// and the one neither chart's installName default needs help with.
type tenantNames struct {
	stream    string
	subjects  []string // redirect, request
	consumers []string // stat, log
}

func namesFor(t *testing.T, namespace, install string) tenantNames {
	t.Helper()

	infra, err := renderNamed(t, "url-shortener-infra", install, namespace, infraDefaults()...)
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
// subjects, the durable consumer names — derives from namespace AND
// install name together, so that a tenant sharing either alone with
// another tenant still gets names of its own.
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
