package charts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truvity/policy/conformance"

	"github.com/truvity/policy/examples/url-shortener/internal/config"

	yaml "go.yaml.in/yaml/v3"
)

// e2eDefaults supplies the values the url-shortener-e2e chart refuses to
// invent — the same shapes examples/url-shortener/e2e/fixture/names.go
// resolves off a real install, stood in here so a test that is not about
// them says so once.
func e2eDefaults(extra ...string) []string {
	return append([]string{
		"--set", "appRelease=example",
		"--set", "database.host=example-pg-rw",
		"--set", "database.name=url_shortener",
		"--set", "database.owner.role=url_shortener_owner",
		"--set", "database.app.role=url_shortener_app",
		"--set", "database.app.passwordSecret=example-pg-runtime",
		"--set", "events.stream=shortener-example-events",
		"--set", "events.redirectSubject=shortener-example.redirect",
		"--set", "events.requestSubject=shortener-example.log",
		"--set", "events.statConsumer=shortener-example-stat",
		"--set", "events.logConsumer=shortener-example-log",
		"--set", "archive.bucket=url-shortener-archive",
		"--set", "images.e2e.digest=sha256:1111111111111111111111111111111111111111111111111111111111111111",
	}, extra...)
}

func renderE2E(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("helm", append([]string{"template", "example-e2e", chartDir(t, "url-shortener-e2e")}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The goldens, on the same terms as the other two charts': `e2e-minimal` is
// what the defaults render to, `e2e-everything` sets every value to
// something else, so a template that stopped reading one shows up as a
// diff here rather than as a case the Job silently ran with no name.
//
// Regenerate with `just golden` after reading the diff, never before.
func TestWhatTheE2EChartRenders(t *testing.T) {
	for _, name := range []string{"e2e-minimal", "e2e-everything"} {
		t.Run(name, func(t *testing.T) {
			out, err := renderE2E(t, "-f", filepath.Join("testdata", name+".yaml"))
			if err != nil {
				t.Fatalf("the chart does not render: %v\n%s", err, out)
			}

			path := filepath.Join("testdata", "golden", name+".yaml")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Skip("golden updated")
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v — run `just golden` to create it", err)
			}
			if string(want) != out {
				t.Errorf("the render moved. Read the diff, then `just golden`:\n%s",
					firstDifference(string(want), out))
			}
		})
	}
}

// Strict everywhere this chart defines something: a typo must fail the
// render rather than be silently ignored — the same claim
// TestEveryRefusalRefuses proves for the application chart, proved here
// for this one instead of sharing that test's fixture directory (which is
// read against the application chart by name).
func TestTheE2EChartRejectsUnknownKeys(t *testing.T) {
	out, err := renderE2E(t, e2eDefaults("--set", "notAKey=oops")...)
	if err == nil {
		t.Fatalf("rendered with an unknown key, and should not have:\n%s", out)
	}
	if !strings.Contains(out, "additional properties") {
		t.Errorf("the refusal does not say why: %s", out)
	}
}

// Every value this chart refuses to invent is actually required — a Job
// installed with one missing would otherwise run with an empty name and
// pass every check trivially.
func TestTheE2EChartRequiresEveryName(t *testing.T) {
	out, err := renderE2E(t)
	if err == nil {
		t.Fatalf("rendered with nothing set at all:\n%s", out)
	}
	for _, want := range []string{"appRelease", "database", "events", "archive"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not mention %q: %s", want, out)
		}
	}
}

// The Job's own name folds in this chart's VERSION, not only the release —
// see templates/_helpers.tpl's "url-shortener-e2e.jobName" for why: a
// Job's spec is immutable, so re-applying an upgraded chart under the same
// Job name would be refused rather than converge, on both install paths
// this repository's charts must render the same under (see
// docs/guides/testing.md#two-install-paths-and-why-both-must-render-the-same).
// A checkout's own chart carries version 0.0.0 (Chart.yaml's placeholder,
// stamped by a release) — sanitised to dashes, since a dot is not a
// character this test wants to depend on Kubernetes continuing to accept
// in a label value.
func TestTheE2EJobNameIncludesTheChartVersion(t *testing.T) {
	out, err := renderE2E(t, e2eDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	job := docOfKind(t, out, "Job")
	meta, _ := job["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	if name != "example-e2e-0-0-0" {
		t.Errorf("the Job is named %q, want a name carrying the chart's own version", name)
	}
}

// No hook annotation anywhere — this chart is a plain Job, applied the
// same way `helm upgrade --install` and a GitOps controller's `helm
// template` + apply already handle any ordinary resource. See
// templates/job.yaml's own doc comment for why: unlike
// charts/url-shortener/templates/migrate.yaml's hook Job, this chart's
// identity is its OWN version (folded into the Job's name), not "the one
// Helm re-creates on every sync".
func TestTheE2EChartRendersNoHook(t *testing.T) {
	out, err := renderE2E(t, e2eDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	if strings.Contains(out, "helm.sh/hook") {
		t.Error("a helm.sh/hook annotation was rendered; this chart is a plain Job")
	}
}

// job.ttlSecondsAfterFinished is OPTIONAL and UNSET by default — see
// values.yaml's own comment on why: a GitOps controller with self-heal on
// would otherwise recreate the Job the moment it deletes itself. Set, it
// still has to clear the schema's own 120s floor.
func TestTheE2EJobTTLIsUnsetByDefault(t *testing.T) {
	t.Run("unset by default", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults()...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		if strings.Contains(out, "ttlSecondsAfterFinished") {
			t.Error("ttlSecondsAfterFinished was rendered with nothing set — it must be left out by default")
		}
	})

	t.Run("rendered when set", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults("--set", "job.ttlSecondsAfterFinished=900")...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		if !strings.Contains(out, "ttlSecondsAfterFinished: 900") {
			t.Error("job.ttlSecondsAfterFinished=900 was set but not rendered")
		}
	})

	t.Run("below the schema's floor is refused", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults("--set", "job.ttlSecondsAfterFinished=10")...)
		if err == nil {
			t.Fatalf("rendered with job.ttlSecondsAfterFinished below 120, and should not have:\n%s", out)
		}
		if !strings.Contains(out, "ttlSecondsAfterFinished") {
			t.Errorf("the refusal does not mention ttlSecondsAfterFinished: %s", out)
		}
	})
}

// job.annotations is OPTIONAL, EMPTY by default, and rendered on the Job's
// own metadata ONLY — never the pod template's — so a GitOps controller can
// key a force/replace sync off it without the chart taking a position on
// what the annotation is called or what it does. See values.yaml's own
// comment on `job.annotations` for the immutable-Job problem this solves.
func TestTheE2EJobAnnotationsAreOptional(t *testing.T) {
	t.Run("unset by default", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults()...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}

		job := docOfKind(t, out, "Job")
		meta, _ := job["metadata"].(map[string]any)
		if _, set := meta["annotations"]; set {
			t.Errorf("the Job carries annotations with job.annotations left at its default: %v", meta["annotations"])
		}
	})

	t.Run("set", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults(
			"--set", "job.annotations.example\\.test/replace-strategy=force",
		)...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}

		job := docOfKind(t, out, "Job")
		meta, _ := job["metadata"].(map[string]any)
		annotations, _ := meta["annotations"].(map[string]any)
		if got, _ := annotations["example.test/replace-strategy"].(string); got != "force" {
			t.Errorf("job.annotations was set but not rendered on the Job's metadata (got %v)", annotations)
		}

		spec, _ := job["spec"].(map[string]any)
		template, _ := spec["template"].(map[string]any)
		podMeta, _ := template["metadata"].(map[string]any)
		if _, set := podMeta["annotations"]; set {
			t.Errorf("job.annotations leaked onto the pod template's metadata: %v", podMeta["annotations"])
		}
	})
}

// mode: full runs every case; mode: tenant skips exactly the one DDL-issuing
// case — see values.yaml's own comment on `mode` for why.
func TestTheE2EModeControlsWhichCasesRun(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults()...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		if strings.Contains(out, "-test.skip") {
			t.Error("mode: full skips a case; every case should run")
		}
	})

	t.Run("tenant", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults("--set", "mode=tenant")...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		if !strings.Contains(out, "-test.skip=TestMigrationRanAndRolesAreSeparate") {
			t.Error("mode: tenant does not skip the role-separation check")
		}
	})
}

// No RBAC on Secrets, and no secretKeyRef but the one for the runtime
// role's own password — matching the design this suite's own db_test.go
// depends on: the password arrives as an environment variable the kubelet
// resolves, never a permission this account holds.
func TestTheE2ERoleGrantsNothingOnSecrets(t *testing.T) {
	out, err := renderE2E(t, e2eDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	role := ""
	for _, doc := range strings.Split(out, "\n---\n") {
		if docKind(doc) == "Role" {
			role = doc
			break
		}
	}
	if role == "" {
		t.Fatal("no Role rendered")
	}
	if strings.Contains(role, "secrets") {
		t.Error("the Role grants something on secrets; it must grant none")
	}
	if strings.Contains(role, "portforward") {
		t.Error("the Role grants pods/portforward, which E2E_KCTX=\"\" never needs — see rbac.yaml's own comment")
	}

	if !strings.Contains(out, "secretKeyRef") {
		t.Error("nothing reads the runtime role's password from a Secret, so something else must be supplying it")
	}
	if strings.Count(out, "secretKeyRef") != 1 {
		t.Errorf("expected exactly one secretKeyRef (the runtime role's password), found %d", strings.Count(out, "secretKeyRef"))
	}
}

// Static S3 credentials are OPTIONAL, and read the same way the database
// password is: a Secret name, never a value, via `secretKeyRef` — so an
// environment with no ambient identity to reach the archive under (the
// kind box's own S3 stand-in, at a real endpoint nothing an IMDS-shaped
// credential chain can authenticate to) can still run the archive check.
func TestTheE2EChartWiresStaticS3CredentialsWhenGiven(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults()...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		if strings.Contains(out, "E2E_S3_ACCESS_KEY_ID") {
			t.Error("E2E_S3_ACCESS_KEY_ID was rendered with no archive.credentialsSecret set")
		}
	})

	t.Run("set", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults(
			"--set", "archive.endpoint=http://s3.object-store.svc:4566",
			"--set", "archive.credentialsSecret=example-archive",
		)...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		for _, want := range []string{
			"E2E_S3_ACCESS_KEY_ID", "E2E_S3_SECRET_ACCESS_KEY", "name: example-archive",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("archive.credentialsSecret is set but %q is missing", want)
			}
		}
	})
}

// Optional trace-store auth is on exactly the same terms as
// TestTheE2EChartWiresStaticS3CredentialsWhenGiven above: a shape that
// renders nothing extra unless traces.tokenExchange.tokenURL and
// traces.caConfigMap are set, so a store that admits anonymous readers
// never gets a projected token or a volume it has no use for.
func TestTheE2EChartWiresTracesAuthWhenGiven(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults("--set", "traces.url=https://jaeger.example.test")...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		for _, unwanted := range []string{
			"E2E_TRACES_TOKEN_URL", "E2E_TRACES_CLIENT", "E2E_TRACES_TOKEN_FILE",
			"E2E_TRACES_CA_FILE", "traces-token", "traces-ca", "volumeMounts", "volumes:",
		} {
			if strings.Contains(out, unwanted) {
				t.Errorf("%q was rendered with no traces.tokenExchange.tokenURL or traces.caConfigMap set", unwanted)
			}
		}
	})

	t.Run("token exchange set", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults(
			"--set", "traces.url=https://jaeger.example.test",
			"--set", "traces.tokenExchange.tokenURL=https://issuer.example.test/token",
			"--set", "traces.tokenExchange.client=trace-reader",
		)...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		for _, want := range []string{
			"E2E_TRACES_TOKEN_URL", "https://issuer.example.test/token",
			"E2E_TRACES_CLIENT", "trace-reader",
			"E2E_TRACES_TOKEN_FILE", "/var/run/traces/token",
			"name: traces-token", "audience: \"access-issuer\"",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("traces.tokenExchange.tokenURL is set but %q is missing", want)
			}
		}
		if strings.Contains(out, "E2E_TRACES_CA_FILE") {
			t.Error("E2E_TRACES_CA_FILE was rendered with no traces.caConfigMap set")
		}
	})

	t.Run("CA bundle set", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults(
			"--set", "traces.url=https://jaeger.example.test",
			"--set", "traces.caConfigMap=custom-traces-ca",
		)...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		for _, want := range []string{
			"E2E_TRACES_CA_FILE", "/var/run/traces-ca/ca-certificates.crt",
			"name: traces-ca", "name: custom-traces-ca",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("traces.caConfigMap is set but %q is missing", want)
			}
		}
		if strings.Contains(out, "E2E_TRACES_TOKEN_URL") {
			t.Error("E2E_TRACES_TOKEN_URL was rendered with no traces.tokenExchange.tokenURL set")
		}
	})

	t.Run("caConfigMapKey overrides the mounted file name", func(t *testing.T) {
		out, err := renderE2E(t, e2eDefaults(
			"--set", "traces.url=https://jaeger.example.test",
			"--set", "traces.caConfigMap=custom-traces-ca",
			"--set", "traces.caConfigMapKey=bundle.pem",
		)...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		if !strings.Contains(out, "/var/run/traces-ca/bundle.pem") {
			t.Error("traces.caConfigMapKey did not change the mounted CA file path")
		}
	})
}

// Every workload and Service carries the instance label — the render-side
// half of docs/guides/conformance.md's render-and-apply rule, proved here
// on the same terms as TestEveryWorkloadAndServiceCarriesTheInstanceLabel
// proves it for the other two charts.
func TestTheE2EJobCarriesTheInstanceLabel(t *testing.T) {
	out, err := renderE2E(t, e2eDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	job := docOfKind(t, out, "Job")
	meta, _ := job["metadata"].(map[string]any)
	labels, _ := meta["labels"].(map[string]any)
	if got, _ := labels["app.kubernetes.io/instance"].(string); got != "example-e2e" {
		t.Errorf("the Job carries no app.kubernetes.io/instance label (got %q)", got)
	}
}

// The prober is off by default: a chart installable by someone with no bake
// window to feed it must not run one uninvited (values.yaml's own comment
// on `prober.enabled`).
func TestTheProberIsOffByDefault(t *testing.T) {
	out, err := renderE2E(t, e2eDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	if strings.Contains(out, "component: prober") {
		t.Error("the prober rendered although prober.enabled was left at its default")
	}
}

func proberDefaults(extra ...string) []string {
	return e2eDefaults(append([]string{
		"--set", "prober.enabled=true",
		"--set", "images.prober.digest=sha256:2222222222222222222222222222222222222222222222222222222222222222",
	}, extra...)...)
}

// The prober's Deployment recreates rather than rolling — see
// templates/prober.yaml's own comment on why a single-replica synthetic
// traffic generator must not leave the old pod running (and probing)
// while a broken new one fails to come up.
func TestTheProberDeploymentRecreates(t *testing.T) {
	out, err := renderE2E(t, proberDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	deploy := docOfKind(t, out, "Deployment")
	spec, _ := deploy["spec"].(map[string]any)
	strategy, _ := spec["strategy"].(map[string]any)
	if got, _ := strategy["type"].(string); got != "Recreate" {
		t.Errorf("the prober's Deployment strategy is %q, want Recreate", got)
	}
}

// TestWhatTheE2EChartRendersIsWhatTheProberBinaryAccepts is
// TestWhatTheChartRendersIsWhatTheBinariesAccept's own claim
// (chart_test.go), proved here for the prober's own configuration file:
// what this chart renders is validated with the SAME schema
// examples/url-shortener/e2e/cmd/prober validates against at start-up.
// Without this, the chart could keep setting a key the binary stopped
// reading and the prober would run on a default nobody chose, with no
// signal but behaviour.
func TestWhatTheE2EChartRendersIsWhatTheProberBinaryAccepts(t *testing.T) {
	out, err := renderE2E(t, proberDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	doc := conformance.ConfigMapData(t, []byte(out), "prober.yaml")
	conformance.ValidDocument(t, doc, config.Read("prober.json"))
}

// The prober's Deployment carries the instance label, on the same terms as
// every other workload this pair of charts renders — the render-side half
// of the render-and-apply rule (conformance_test.go's
// TestEveryWorkloadAndServiceCarriesTheInstanceLabel proves it generically
// across all three charts; this asserts it specifically for the prober so
// a reader does not have to go looking for where that coverage comes from).
func TestTheProberCarriesTheInstanceLabel(t *testing.T) {
	out, err := renderE2E(t, proberDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	deploy := docOfKind(t, out, "Deployment")
	meta, _ := deploy["metadata"].(map[string]any)
	labels, _ := meta["labels"].(map[string]any)
	if got, _ := labels["app.kubernetes.io/instance"].(string); got != "example-e2e" {
		t.Errorf("the prober's Deployment carries no app.kubernetes.io/instance label (got %q)", got)
	}
}

// THE test for an optional capability, proved for this chart's OWN tls
// block on the same terms as chart_test.go's TestTransportOffLeavesNoTrace
// proves it for the application chart's: with it off (the default), the
// render carries no trace of it at all — not the CSI volume, not the
// certificate-request permission, not even a ServiceAccount naming the
// prober specifically. A chart installed by someone whose platform
// provides none of this must render byte-identical to one that had never
// heard of it, prober enabled or not.
func TestProberTransportOffLeavesNoTrace(t *testing.T) {
	out, err := renderE2E(t, proberDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	for _, trace := range []string{
		"csi.cert-manager.io", // the driver
		"certificaterequests", // the permission to ask
		"trustDomain",         // the configuration block
		"identity",            // the volume and its mount
		"tls:",                // the block itself
	} {
		if strings.Contains(out, trace) {
			t.Errorf("the default render mentions %q; with tls.mode off it must carry no trace of it", trace)
		}
	}
}

// Turned on, the prober gets exactly what a platform with workload
// identity needs: its own account (distinct from the e2e Job's), the CSI
// volume and its mount at tls.mountPath, and — while tls.grantRequest is
// true — the Role letting that account ask for a certificate.
func TestProberTransportOnRendersIdentity(t *testing.T) {
	out, err := renderE2E(t, proberDefaults(
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.invalid",
		"--set", "tls.peers[0].namespace=example",
		"--set", "tls.peers[0].serviceAccount=example-app")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	for _, want := range []string{
		"spiffe.csi.cert-manager.io",
		"certificaterequests",
		"trustDomain: example.invalid",
		"serviceAccount: example-app",
		"mountPath: /var/run/identity",
		"serviceAccountName: example-e2e-prober",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the render is missing %q, so the platform has nothing to act on", want)
		}
	}

	// The prober's own account is a SEPARATE ServiceAccount from the e2e
	// Job's — see templates/_helpers.tpl's own comment on
	// "url-shortener-e2e.proberServiceAccountName" for why the two carry
	// different grants.
	var accounts []string
	for _, doc := range strings.Split(out, "\n---\n") {
		if docKind(doc) == "ServiceAccount" {
			accounts = append(accounts, docName(doc))
		}
	}
	if len(accounts) != 2 {
		t.Fatalf("expected two ServiceAccounts (the Job's and the prober's), found %v", accounts)
	}
}

// The prober's own account needs `create certificaterequests` ONLY while
// tls.grantRequest asks for it — see charts/url-shortener/templates/
// identity-rbac.yaml's own comment for why a platform granting this
// centrally sets that value false, and the render must then carry no Role
// for a permission nothing here uses.
func TestProberTransportGrantRequestIsOptional(t *testing.T) {
	out, err := renderE2E(t, proberDefaults(
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.invalid",
		"--set", "tls.grantRequest=false")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	if strings.Contains(out, "certificaterequests") {
		t.Error("certificaterequests was rendered with tls.grantRequest=false")
	}
	// The account itself is unaffected: the CSI mount still needs one to
	// derive an identity from, only the PERMISSION to ask is a platform's
	// to grant centrally instead.
	if !strings.Contains(out, "serviceAccountName: example-e2e-prober") {
		t.Error("the prober's own account was not rendered although tls.mode is on")
	}
}

// Transport on with NOBODY on the prober's own allow-list renders
// configuration the prober binary accepts — the SAME claim
// chart_test.go's TestTransportOnWithNoPeersIsStillValid proves for the
// application chart, proved here for the prober's config file instead.
//
// `peers:` followed by an empty range is YAML null, not an empty array;
// the prober would refuse it at start-up with "tls.peers: got null, want
// array" and crash-loop while the Deployment reports Ready throughout. An
// empty allow-list is exactly what tls.peers left at its default (nobody
// granted yet) looks like, so this is the DEFAULT this test proves against.
func TestProberTLSPeersRenderAsAnEmptyArrayNotNull(t *testing.T) {
	out, err := renderE2E(t, proberDefaults(
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.invalid")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	doc := conformance.ConfigMapData(t, []byte(out), "prober.yaml")
	conformance.ValidDocument(t, doc, config.Read("prober.json"))

	if !strings.Contains(out, "peers:\n        []") {
		t.Errorf("tls.peers with nothing set did not render as an empty array:\n%s", out)
	}
}

// A trust domain is required the moment the prober's own transport is on,
// on exactly the same terms as the application chart's tls.trustDomain:
// without it a peer from ANY trust domain would be admitted, which is a
// caller that looks authenticated and is not.
func TestProberTransportOnWithoutATrustDomainIsRefused(t *testing.T) {
	out, err := renderE2E(t, proberDefaults("--set", "tls.mode=permissive")...)
	if err == nil {
		t.Fatalf("a render with no trust domain was accepted:\n%s", out)
	}
	if !strings.Contains(out, "tls.trustDomain is required") {
		t.Errorf("the refusal does not say what is missing: %s", out)
	}
}

// jobEnv returns the e2e Job container's environment as name -> value, and
// the whole Job document.
func jobEnv(t *testing.T, out string) (map[string]string, string) {
	t.Helper()

	for _, doc := range strings.Split(out, "\n---\n") {
		if docKind(doc) != "Job" {
			continue
		}
		var job struct {
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Env []struct{ Name, Value string } `yaml:"env"`
						} `yaml:"containers"`
					} `yaml:"spec"`
				} `yaml:"template"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &job); err != nil {
			t.Fatalf("the Job is not YAML: %v", err)
		}
		env := map[string]string{}
		for _, e := range job.Spec.Template.Spec.Containers[0].Env {
			env[e.Name] = e.Value
		}
		return env, doc
	}
	t.Fatal("no Job rendered")
	return nil, ""
}

// With the Job's identity off — the default, and also with the prober's
// transport on — the Job carries no trace of it: no variable, no volume, no
// mount, no Role to ask for a certificate on ITS account.
func TestJobTransportOffLeavesNoTrace(t *testing.T) {
	for name, extra := range map[string][]string{
		"defaults":                     nil,
		"prober transport on, job off": {"--set", "prober.enabled=true", "--set", "tls.mode=permissive", "--set", "tls.trustDomain=example.invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := renderE2E(t, proberDefaults(extra...)...)
			if err != nil {
				t.Fatalf("the chart does not render: %v\n%s", err, out)
			}

			env, job := jobEnv(t, out)
			for k := range env {
				if strings.Contains(k, "TLS") {
					t.Errorf("the Job sets %s with its identity off", k)
				}
			}
			for _, trace := range []string{"csi:", "identity", "certificaterequests-job", "request-identity"} {
				if strings.Contains(job, trace) {
					t.Errorf("the Job mentions %q with its identity off", trace)
				}
			}
			for _, doc := range strings.Split(out, "\n---\n") {
				if docKind(doc) == "Role" && strings.Contains(docName(doc), "-e2e-request-identity") {
					t.Errorf("a Role for the Job to ask for an identity was rendered with job.tls.enabled false")
				}
			}
		})
	}
}

// Turned on, the Job gets what the prober gets — the CSI volume and its
// mount, and a Role for ITS OWN account to ask — and its environment names
// each target's port per mode: urls strict on the ordinary port, redirect
// permissive on the second.
func TestJobTransportOnRendersIdentityAndPerTargetModes(t *testing.T) {
	out, err := renderE2E(t, e2eDefaults(
		"--set", "job.tls.enabled=true",
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.port=9443",
		"--set", "tls.trustDomain=example.invalid",
		"--set", "tls.peers[0].namespace=example",
		"--set", "tls.peers[0].serviceAccount=example-app",
		"--set", "tls.peers[1].namespace=other",
		"--set", "tls.peers[1].serviceAccount=reader")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	env, job := jobEnv(t, out)
	want := map[string]string{
		"E2E_URLS_TLS":         "strict",
		"E2E_REDIRECT_TLS":     "permissive",
		"E2E_TLS_PORT":         "9443",
		"E2E_TLS_DIR":          "/var/run/identity",
		"E2E_TLS_TRUST_DOMAIN": "example.invalid",
		"E2E_TLS_PEERS":        "example/example-app,other/reader",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}

	for _, want := range []string{"spiffe.csi.cert-manager.io", "mountPath: /var/run/identity", "serviceAccountName: example-e2e-e2e"} {
		if !strings.Contains(job, want) {
			t.Errorf("the Job is missing %q", want)
		}
	}

	// The Role is for the Job's OWN account — no second ServiceAccount is
	// created for it.
	var bound, accounts int
	for _, doc := range strings.Split(out, "\n---\n") {
		switch {
		case docKind(doc) == "RoleBinding" && strings.Contains(docName(doc), "-e2e-request-identity"):
			if !strings.Contains(doc, "name: example-e2e-e2e\n") {
				t.Errorf("the identity RoleBinding does not bind the Job's account:\n%s", doc)
			}
			bound++
		case docKind(doc) == "ServiceAccount":
			accounts++
		}
	}
	if bound != 1 {
		t.Errorf("expected one identity RoleBinding for the Job, found %d", bound)
	}
	if accounts != 1 {
		t.Errorf("expected only the Job's ServiceAccount (the prober is off), found %d", accounts)
	}
}

// An empty allow-list is an empty STRING for the suite, which it reads as a
// list that admits nobody — and the prober's config file, beside it, still
// renders `[]` and never null (see TestProberTLSPeersRenderAsAnEmptyArrayNotNull).
func TestJobTransportWithNoPeersRendersAnEmptyList(t *testing.T) {
	out, err := renderE2E(t, proberDefaults(
		"--set", "prober.enabled=true",
		"--set", "job.tls.enabled=true",
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.invalid")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	env, _ := jobEnv(t, out)
	if v, ok := env["E2E_TLS_PEERS"]; !ok || v != "" {
		t.Errorf("E2E_TLS_PEERS = %q (set: %v), want the empty string", v, ok)
	}
	if !strings.Contains(out, "peers:\n        []") {
		t.Errorf("the prober's tls.peers with nothing set did not render as an empty array:\n%s", out)
	}
}

// Both callers dial each target on ITS port: with urls strict and redirect
// permissive the prober must not assume they match — a strict redirect
// address would point at a listener the application never serves.
func TestProberDialsEachTargetOnItsOwnPort(t *testing.T) {
	out, err := renderE2E(t, proberDefaults(
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.invalid")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	cfg := string(conformance.ConfigMapData(t, []byte(out), "prober.yaml"))
	for _, want := range []string{"address: https://example-urls:8080", "address: https://example-redirect:8443"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("the prober's configuration is missing %q:\n%s", want, cfg)
		}
	}
	conformance.ValidDocument(t, []byte(cfg), config.Read("prober.json"))
}

// The refusals: the Job's identity needs the transport on; redirect is never
// a strict target, written down or inherited; and a component key only
// exists for the two targets.
func TestE2ETransportRefusals(t *testing.T) {
	cases := map[string]struct {
		set  []string
		want string
	}{
		"job identity with the transport off": {
			set:  []string{"--set", "job.tls.enabled=true"},
			want: "job.tls.enabled needs the transport on",
		},
		"redirect strict, written": {
			set:  []string{"--set", "tls.mode=permissive", "--set", "tls.components.redirect.mode=strict", "--set", "tls.trustDomain=example.invalid"},
			want: "tls/components/redirect/mode",
		},
		"redirect strict, inherited": {
			set:  []string{"--set", "tls.mode=strict", "--set", "tls.trustDomain=example.invalid"},
			want: "the redirect target cannot be strict",
		},
		"redirect strict, inherited, nothing else enabled": {
			set:  []string{"--set", "tls.mode=strict"},
			want: "the redirect target cannot be strict",
		},
		"a component that is not a target": {
			set:  []string{"--set", "tls.components.web.mode=strict"},
			want: "tls/components",
		},
		"job identity without a trust domain": {
			set:  []string{"--set", "job.tls.enabled=true", "--set", "tls.mode=permissive"},
			want: "tls.trustDomain is required",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defaults := proberDefaults
			if name == "redirect strict, inherited, nothing else enabled" {
				// No prober and no Job identity: nothing else would look
				// at the target, and it must still be refused.
				defaults = e2eDefaults
			}
			out, err := renderE2E(t, defaults(tc.set...)...)
			if err == nil {
				t.Fatalf("accepted:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not say %q: %s", tc.want, out)
			}
		})
	}
}
