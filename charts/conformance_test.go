package charts_test

import (
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/truvity/policy/examples/url-shortener/charts"
)

// forbiddenInTemplates are constructs that behave differently — or not at
// all — depending on which of the two install paths in
// docs/guides/testing.md#two-install-paths-and-why-both-must-render-the-same
// a chart is put through. `helm upgrade --install` holds a Helm release
// record; a GitOps controller's `helm template` + apply holds none. A chart
// that reaches for any of these has an opinion about which one it is
// running under, and docs/guides/conformance.md is where each row of this
// list is explained.
var forbiddenInTemplates = []struct {
	name string
	re   *regexp.Regexp
}{
	// `lookup` queries the live cluster through Helm's own client. Under
	// `helm template` there is no client, so it always returns empty —
	// silently, not as a refusal.
	{"lookup", regexp.MustCompile(`\blookup\s+"`)},
	// These three exist only because a release record exists. Rendered
	// with no record at all, `.IsInstall` and `.IsUpgrade` are both
	// false and `.Revision` is always zero, which a template branching on
	// them cannot tell apart from a real first install.
	{".Release.IsUpgrade", regexp.MustCompile(`\.Release\.IsUpgrade\b`)},
	{".Release.IsInstall", regexp.MustCompile(`\.Release\.IsInstall\b`)},
	{".Release.Revision", regexp.MustCompile(`\.Release\.Revision\b`)},
	// A value generated inside a template — a password, a key, a
	// certificate — is generated again on every render. `helm
	// upgrade --install` hides that by reusing the previous release's
	// computed values; a render-and-apply install has nothing to reuse
	// from, so the value changes on every sync and never converges.
	{"randAlphaNum", regexp.MustCompile(`\brandAlphaNum\b`)},
	{"randAlpha", regexp.MustCompile(`\brandAlpha\b`)},
	{"randNumeric", regexp.MustCompile(`\brandNumeric\b`)},
	{"randAscii", regexp.MustCompile(`\brandAscii\b`)},
	{"genPrivateKey", regexp.MustCompile(`\bgenPrivateKey\b`)},
	{"genCA", regexp.MustCompile(`\bgenCA\b`)},
	{"genSelfSignedCert", regexp.MustCompile(`\bgenSelfSignedCert\b`)},
	{"derivePassword", regexp.MustCompile(`\bderivePassword\b`)},
	// `helm install`/`upgrade` stamp this annotation; a render-and-apply
	// install never does. A template that selects an object by it finds
	// nothing there, every time, on the path most consumers actually use.
	{"meta.helm.sh/release-name", regexp.MustCompile(`meta\.helm\.sh/release-name`)},
}

// TestNoChartUsesAConstructRenderAndApplyCannotHonour walks every template
// this repository embeds and fails on anything in forbiddenInTemplates.
//
// A static scan, not a render: every construct above either renders EMPTY
// or renders FALSE under `helm template`, so a test that only inspected the
// rendered output would see nothing wrong — the very failure mode the rule
// exists to catch. Reading the template source is the only way to see the
// dependency at all.
func TestNoChartUsesAConstructRenderAndApplyCannotHonour(t *testing.T) {
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := charts.Files.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			p := path.Join(dir, e.Name())
			if e.IsDir() {
				walk(p)
				continue
			}
			// Only what Helm actually executes as a template: values
			// files and the schema are data, not templates, and
			// flagging a coincidental match inside them would make
			// this test something people learn to ignore.
			if !strings.Contains(p, "/templates/") {
				continue
			}

			b, err := charts.Files.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range forbiddenInTemplates {
				loc := forbidden.re.FindIndex(b)
				if loc == nil {
					continue
				}
				line := 1 + strings.Count(string(b[:loc[0]]), "\n")
				t.Errorf("%s:%d uses %s, which behaves differently — or not at all — "+
					"under a render-and-apply install with no Helm release record; "+
					"see docs/guides/conformance.md", p, line, forbidden.name)
			}
		}
	}
	walk(".")
}

// TestEveryWorkloadAndServiceCarriesTheInstanceLabel is the render-side half
// of the same rule: whatever a chart actually produces must carry the ONE
// label both install paths agree on, because the annotation `helm
// install`/`upgrade` stamps is the thing a render-and-apply install never
// has.
func TestEveryWorkloadAndServiceCarriesTheInstanceLabel(t *testing.T) {
	workloadKinds := map[string]bool{
		"Deployment": true, "StatefulSet": true, "DaemonSet": true,
		"CronJob": true, "Job": true, "Service": true,
	}

	app, err := render(t, defaults("--set", "images.web.tag=dev", "--set", "verification.enabled=true")...)
	if err != nil {
		t.Fatalf("the application chart does not render: %v\n%s", err, app)
	}
	infra, err := renderInfra(t, infraDefaults()...)
	if err != nil {
		t.Fatalf("the infrastructure chart does not render: %v\n%s", err, infra)
	}
	// The prober enabled too, not only the Job: it is the other workload
	// this chart can render, and the ONLY Deployment it carries at all —
	// TestTheE2EJobCarriesTheInstanceLabel already covers the Job on its
	// own, but nothing else exercises the prober's Deployment through this
	// check unless it is turned on here.
	e2e, err := renderE2E(t, e2eDefaults(
		"--set", "prober.enabled=true",
		"--set", "images.prober.digest=sha256:2222222222222222222222222222222222222222222222222222222222222222",
	)...)
	if err != nil {
		t.Fatalf("the e2e chart does not render: %v\n%s", err, e2e)
	}

	// Each render was installed under its own release name — renderE2E
	// names a SEPARATE release from render/renderInfra's "example", since
	// this chart tests somebody else's release rather than sharing its
	// name — so the label each one's objects must carry differs to match.
	renders := []struct {
		out, release string
	}{
		{app, "example"},
		{infra, "example"},
		{e2e, "example-e2e"},
	}

	var checked int
	for _, r := range renders {
		for _, doc := range documents(t, r.out) {
			kind, _ := doc["kind"].(string)
			if !workloadKinds[kind] {
				continue
			}
			checked++

			meta, _ := doc["metadata"].(map[string]any)
			labels, _ := meta["labels"].(map[string]any)
			if got, _ := labels["app.kubernetes.io/instance"].(string); got != r.release {
				t.Errorf("%s %v carries no app.kubernetes.io/instance label (got %q, want %q): "+
					"a render-and-apply install identifies this release's objects by that "+
					"label alone, never by the meta.helm.sh/release-name annotation "+
					"`helm install` stamps", kind, meta["name"], got, r.release)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no workload or Service kind was rendered, so the label was not checked")
	}
}

// TestNoPlainJobSetsTTLSecondsAfterFinished is the render-side half of
// docs/guides/conformance.md's row on Job cleanup: a Job with no
// `helm.sh/hook` annotation is applied the same way by both install paths
// — `helm upgrade --install` and a GitOps controller's `helm template` +
// apply — and a controller running with self-heal on treats a Job that
// deleted itself as MISSING from the live state and recreates it, which
// `ttlSecondsAfterFinished` guarantees a plain Job eventually does. A HOOK
// Job is exempt: Helm's own hook machinery is what deletes and recreates
// it, never self-heal — see templates/migrate.yaml and
// templates/verification.yaml's own `before-hook-creation` comments.
func TestNoPlainJobSetsTTLSecondsAfterFinished(t *testing.T) {
	app, err := render(t, defaults("--set", "images.web.tag=dev", "--set", "verification.enabled=true")...)
	if err != nil {
		t.Fatalf("the application chart does not render: %v\n%s", err, app)
	}
	infra, err := renderInfra(t, infraDefaults()...)
	if err != nil {
		t.Fatalf("the infrastructure chart does not render: %v\n%s", err, infra)
	}
	// The prober enabled too, on the same terms as
	// TestEveryWorkloadAndServiceCarriesTheInstanceLabel above: it renders
	// no extra Job, but exercising it here keeps this chart's render
	// covered by every default-values check the same way.
	e2e, err := renderE2E(t, e2eDefaults(
		"--set", "prober.enabled=true",
		"--set", "images.prober.digest=sha256:2222222222222222222222222222222222222222222222222222222222222222",
	)...)
	if err != nil {
		t.Fatalf("the e2e chart does not render: %v\n%s", err, e2e)
	}

	var checked int
	for _, out := range []string{app, infra, e2e} {
		for _, doc := range documents(t, out) {
			if kind, _ := doc["kind"].(string); kind != "Job" {
				continue
			}
			checked++

			meta, _ := doc["metadata"].(map[string]any)
			name, _ := meta["name"].(string)
			annotations, _ := meta["annotations"].(map[string]any)
			if _, isHook := annotations["helm.sh/hook"]; isHook {
				continue
			}

			spec, _ := doc["spec"].(map[string]any)
			if _, set := spec["ttlSecondsAfterFinished"]; set {
				t.Errorf("Job %q has no helm.sh/hook annotation but sets ttlSecondsAfterFinished: "+
					"a GitOps controller with self-heal on recreates a Job that deleted itself — "+
					"see docs/guides/conformance.md", name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no Job was rendered, so the rule was not checked")
	}
}
