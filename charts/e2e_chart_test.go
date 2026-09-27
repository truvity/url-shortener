package charts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
// charts/url-shortener/templates/verification.yaml's hook Job, this
// chart's identity is its OWN version (folded into the Job's name), not
// "the one verification Helm re-creates on every sync".
func TestTheE2EChartRendersNoHook(t *testing.T) {
	out, err := renderE2E(t, e2eDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	if strings.Contains(out, "helm.sh/hook") {
		t.Error("a helm.sh/hook annotation was rendered; this chart is a plain Job")
	}
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
