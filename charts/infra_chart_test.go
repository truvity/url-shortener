package charts_test

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

// infraDefaults supplies the one value the infrastructure chart refuses to
// invent: the name of the secret holding the runtime role's password.
func infraDefaults(extra ...string) []string {
	return append([]string{
		"--set", "postgres.runtimePasswordSecret=example-pg-runtime",
	}, extra...)
}

func renderInfra(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("helm", append([]string{"template", "example", chartDir(t, "url-shortener-infra")}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The infrastructure chart's goldens, on the same terms as the application
// chart's: `minimal` catches a changed default, `everything` catches a
// template that stopped reading a value.
//
// `everything` matters more here than it does next door. Most of this
// chart's values exist for a PLATFORM to set and for nobody else, so they
// are unset in every render an example ever does — which means the only
// thing standing between "the platform passes it" and "the platform passes
// it and it goes nowhere" is a fixture that sets all of them.
//
// Regenerate with `just golden` after reading the diff, never before.
func TestWhatTheInfraChartRenders(t *testing.T) {
	for _, name := range []string{"infra-minimal", "infra-everything"} {
		t.Run(name, func(t *testing.T) {
			out, err := renderInfra(t, "-f", filepath.Join("testdata", name+".yaml"))
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

// A chart renders what it was given, or it refuses.
//
// An archive with no server name joins whatever timeline is already under
// the default, and nobody finds out until the restore. Rendering something
// here would be worse than failing.
func TestTheInfraChartRefusesAnArchiveItCannotName(t *testing.T) {
	out, err := renderInfra(t, infraDefaults("--set", "postgres.backup.objectStoreName=example-archive")...)
	if err == nil {
		t.Fatalf("an archive was named with no server name and the chart rendered anyway:\n%s", out)
	}
	if !strings.Contains(out, "postgres.backup.serverName") {
		t.Errorf("the refusal does not name postgres.backup.serverName, so nobody reading it knows what to set:\n%s", out)
	}
}

// The platform cannot rename what the chart's own selectors look for.
//
// Labels are the one pass-through here that could collide with something
// load-bearing. A platform stamping `app.kubernetes.io/instance` would take
// the release apart one resource at a time, and every render would look
// correct.
func TestTheChartsLabelsWinOverThePlatforms(t *testing.T) {
	out, err := renderInfra(t, infraDefaults(
		"--set", `postgres.labels.app\.kubernetes\.io/instance=hijacked`,
		"--set", `postgres.labels.example\.io/pool=durable`,
	)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	cluster := docOfKind(t, out, "Cluster")
	labels, _ := cluster["metadata"].(map[string]any)["labels"].(map[string]any)
	if got := labels["app.kubernetes.io/instance"]; got != "example" {
		t.Errorf("a platform label overwrote the release name: instance = %v, want example", got)
	}
	if got := labels["example.io/pool"]; got != "durable" {
		t.Errorf("the platform's own label did not survive: example.io/pool = %v", got)
	}
}

// An account and a server list are two answers to one question.
//
// Without an account the stream says which broker to dial. With one, the
// account carries both the broker and the identity, and a server list
// beside it is the chart arguing with the broker about who this is — which
// NACK resolves by ignoring one of them, silently, and not always the same
// one.
func TestTheStreamNamesAnAccountOrAServerButNeverBoth(t *testing.T) {
	t.Run("no account", func(t *testing.T) {
		out, err := renderInfra(t, infraDefaults()...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		spec := docOfKind(t, out, "Stream")["spec"].(map[string]any)
		if _, ok := spec["servers"]; !ok {
			t.Error("no account and no servers: the stream is created on nothing")
		}
		if _, ok := spec["account"]; ok {
			t.Error("an account appeared without being asked for")
		}
	})

	t.Run("account", func(t *testing.T) {
		out, err := renderInfra(t, infraDefaults("--set", "events.account=url-shortener")...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		spec := docOfKind(t, out, "Stream")["spec"].(map[string]any)
		if got := spec["account"]; got != "url-shortener" {
			t.Errorf("account = %v, want url-shortener", got)
		}
		if _, ok := spec["servers"]; ok {
			t.Error("a server list was rendered beside the account")
		}
	})
}

// Rule 6 of the platform contract, mechanically: a service FINDS its store,
// it does not make it.
//
// `platform.md` says this is checked by review today and that it should not
// be. This is the check — the application chart renders nothing whose kind
// belongs to the infrastructure one, and the infrastructure chart renders
// nothing that runs.
//
// It is not theoretical. The two charts exist as two because the migration
// runs as a pre-install hook and Helm runs hooks before anything else in
// the same release, so a chart creating its own database could never
// migrate it. That ordering was discovered by building it the other way
// first, and nothing but this test would notice it being undone.
func TestNeitherChartRendersTheOthersKinds(t *testing.T) {
	infrastructure := map[string]bool{"Cluster": true, "Stream": true, "Consumer": true, "Bucket": true}
	running := map[string]bool{"Deployment": true, "StatefulSet": true, "DaemonSet": true, "CronJob": true}

	app, err := render(t, defaults("--set", "image.tag=dev")...)
	if err != nil {
		t.Fatalf("the application chart does not render: %v\n%s", err, app)
	}
	for _, kind := range kindsIn(t, app) {
		if infrastructure[kind] {
			t.Errorf("the application chart renders a %s — it is making what it should be finding", kind)
		}
	}

	infra, err := renderInfra(t, infraDefaults()...)
	if err != nil {
		t.Fatalf("the infrastructure chart does not render: %v\n%s", err, infra)
	}
	for _, kind := range kindsIn(t, infra) {
		if running[kind] {
			t.Errorf("the infrastructure chart renders a %s — the thing with a separate lifetime has grown a workload", kind)
		}
	}
}

// docOfKind returns the one rendered document of a kind, decoded.
func docOfKind(t *testing.T, out, kind string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, m := range documents(t, out) {
		if m["kind"] != kind {
			continue
		}
		if found != nil {
			t.Fatalf("more than one %s was rendered; this helper assumes one", kind)
		}
		found = m
	}
	if found == nil {
		t.Fatalf("no %s in the render:\n%s", kind, out)
	}
	return found
}

// kindsIn lists the kinds a render produced, in the order they appear.
func kindsIn(t *testing.T, out string) []string {
	t.Helper()
	var kinds []string
	for _, m := range documents(t, out) {
		if kind, ok := m["kind"].(string); ok {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		t.Fatalf("nothing was rendered:\n%s", out)
	}
	return kinds
}

// documents decodes a helm render into its documents.
//
// A decoder rather than a split on `---`: that separator appears inside
// block scalars too, and a test that quietly stopped seeing half the render
// would pass for the wrong reason, which is the only kind of green worth
// worrying about.
func documents(t *testing.T, out string) []map[string]any {
	t.Helper()
	var docs []map[string]any
	dec := yaml.NewDecoder(strings.NewReader(out))
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("the render is not YAML: %v\n%s", err, out)
		}
		if m != nil {
			docs = append(docs, m)
		}
	}
	return docs
}
