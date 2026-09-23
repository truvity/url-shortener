package charts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truvity/policy/conformance"

	"github.com/truvity/policy/examples/url-shortener/charts"
	"github.com/truvity/policy/examples/url-shortener/internal/config"
)

// chartDir is the embedded chart, written out so `helm` can read it.
//
// The embed is what makes this test honest: Go's cache keys on the files the
// TEST package reads, not on what helm reads, so a template edit would
// otherwise leave a cached PASS behind and the contract would go unchecked.
func chartDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	entries, err := charts.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var write func(dir string)
	write = func(dir string) {
		items, err := charts.Files.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			p := filepath.Join(dir, item.Name())
			out := filepath.Join(root, p)
			if item.IsDir() {
				if err := os.MkdirAll(out, 0o755); err != nil {
					t.Fatal(err)
				}
				write(p)
				continue
			}
			b, err := charts.Files.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(out, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, e := range entries {
		if e.IsDir() {
			if err := os.MkdirAll(filepath.Join(root, e.Name()), 0o755); err != nil {
				t.Fatal(err)
			}
			write(e.Name())
		}
	}
	return filepath.Join(root, "url-shortener")
}

// defaults supplies the addresses every render needs. They are REQUIRED —
// the database and the stream belong to the infra release — so every test
// that is not about them says so once, here.
func defaults(extra ...string) []string {
	return append([]string{
		"--set", "database.host=example-pg-rw",
		"--set", "database.owner.passwordSecret=example-pg-app",
		"--set", "database.app.passwordSecret=example-pg-runtime",
		"--set", "events.url=nats://nats.nats.svc:4222",
	}, extra...)
}

func render(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("helm", append([]string{"template", "example", chartDir(t)}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// THE test of the configuration contract: what the chart renders is
// validated with the schema the BINARY validates against at start-up.
//
// Without it the two drift in the only direction that matters — the chart
// keeps setting a key the binary stopped reading, and the service runs on a
// default nobody chose, with no signal but behaviour.
func TestWhatTheChartRendersIsWhatTheBinariesAccept(t *testing.T) {
	out, err := render(t, defaults("--set", "image.tag=dev")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	for _, tc := range []struct{ file, schema string }{
		{"migrate.yaml", "migrate.json"},
		{"redirect.yaml", "redirect.json"},
		{"stat.yaml", "stat.json"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			rendered := conformance.ConfigMapData(t, []byte(out), tc.file)
			conformance.ValidDocument(t, rendered, config.Read(tc.schema))
		})
	}
}

// The same for a digest-pinned install, which is what a release produces.
// Different values, so a different chance to be wrong.
func TestADigestPinnedRenderAlsoProducesWhatTheBinariesAccept(t *testing.T) {
	out, err := render(t, defaults(
		"--set", "image.digest=sha256:0000000000000000000000000000000000000000000000000000000000000000",
	)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	for _, tc := range []struct{ file, schema string }{
		{"migrate.yaml", "migrate.json"},
		{"redirect.yaml", "redirect.json"},
		{"stat.yaml", "stat.json"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			conformance.ValidDocument(t, conformance.ConfigMapData(t, []byte(out), tc.file), config.Read(tc.schema))
		})
	}
}

// No secret is ever rendered. A configuration file is mounted from a
// ConfigMap, printed when somebody debugs a deployment, and committed as a
// fixture; it must survive all three being true.
func TestNoSecretIsRendered(t *testing.T) {
	out, err := render(t, defaults("--set", "image.tag=dev")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ file string }{{"migrate.yaml"}, {"redirect.yaml"}, {"stat.yaml"}} {
		body := string(conformance.ConfigMapData(t, []byte(out), tc.file))
		if strings.Contains(body, "password:") {
			t.Errorf("%s carries a password field; the file names the variable, it does not hold the value:\n%s", tc.file, body)
		}
	}
	// And the deployments read it from a Secret rather than a literal.
	if !strings.Contains(out, "secretKeyRef") {
		t.Error("nothing reads the password from a Secret, so something else must be supplying it")
	}
}

// Every refusal has a fixture, and every fixture must fail. A rule with no
// fixture is a rule that will quietly stop working.
func TestEveryRefusalRefuses(t *testing.T) {
	entries, err := os.ReadDir("testdata/invalid")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no fixtures: a strict chart with nothing proving it is strict")
	}
	for _, e := range entries {
		t.Run(e.Name(), func(t *testing.T) {
			out, err := render(t, "-f", filepath.Join("testdata", "invalid", e.Name()))
			if err == nil {
				t.Fatalf("rendered, and should not have:\n%s", out)
			}
		})
	}
}

// The probe paths are the contract's, and the chart is where they are
// promised to the platform. A rename on one side is a pod that never becomes
// ready, or worse, one that is restarted while healthy.
func TestProbesUseTheContractPaths(t *testing.T) {
	out, err := render(t, defaults("--set", "image.tag=dev")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/health/live", "/health/ready"} {
		if strings.Count(out, want) < 2 {
			t.Errorf("%s is not on every component that serves probes", want)
		}
	}
}

// The migration and the services must not share a credential.
//
// They are separate roles in the chart's values, but "separate" is only
// worth anything if the two actually read different secrets: a render that
// handed both the owner's password would look correct in every other test
// here while giving the request path the right to drop a table.
func TestTheMigrationAndTheServicesUseDifferentCredentials(t *testing.T) {
	out, err := render(t, defaults("--set", "image.tag=dev")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	var migrate, services []string

	for _, doc := range strings.Split(out, "\n---\n") {
		name := ""
		for _, line := range strings.Split(doc, "\n") {
			if strings.HasPrefix(line, "  name: ") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "  name: "))

				break
			}
		}

		for i, line := range strings.Split(doc, "\n") {
			if !strings.Contains(line, "DATABASE_PASSWORD") {
				continue
			}

			lines := strings.Split(doc, "\n")
			for _, l := range lines[i:min(i+5, len(lines))] {
				l = strings.TrimSpace(l)
				if !strings.HasPrefix(l, "name: ") {
					continue
				}

				secret := strings.TrimPrefix(l, "name: ")
				if strings.Contains(name, "migrate") {
					migrate = append(migrate, secret)
				} else {
					services = append(services, secret)
				}

				break
			}
		}
	}

	if len(migrate) == 0 || len(services) == 0 {
		t.Fatalf("expected the migration and the services to read a password each, got %v and %v", migrate, services)
	}

	for _, m := range migrate {
		for _, s := range services {
			if m == s {
				t.Errorf("the migration and a service both read %q: the owner's rights are on the request path", m)
			}
		}
	}
}
