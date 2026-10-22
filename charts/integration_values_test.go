package charts_test

import (
	"strings"
	"testing"

	"github.com/truvity/url-shortener/e2e/devel"
)

// The integration suite installs the pair with the values e2e/devel gives it.
// They are the one thing a chart's schema can contradict without any other
// test noticing, because nothing else renders them: a value the schema
// refuses fails the first run on a cluster, an hour after the change that
// broke it. So they are rendered here, on every `go test`.
func develConfig() devel.Config {
	return devel.Config{
		Namespace: "ci-example", InstallsDomain: "installs.example.test", Issuer: "https://issuer.example.test",
		ArchiveBucket: "test-bucket", ArchiveRegion: "eu-central-1", Release: "us-r42-a1",
	}
}

func setArgs(set []string) []string {
	var args []string
	for _, s := range set {
		args = append(args, "--set", s)
	}

	return args
}

func TestTheIntegrationSuitesValuesRenderTheInfrastructureChart(t *testing.T) {
	c := develConfig()

	out, err := renderInfra(t, append(setArgs(c.InfraSet()), "--namespace", c.Namespace)...)
	if err != nil {
		t.Fatalf("the infrastructure chart refuses the integration suite's values: %v\n%s", err, out)
	}

	for _, want := range []string{
		"kind: Cluster",
		"account: \"" + c.Namespace + "\"",
		"name: " + c.Release + "-pg-runtime",
		"- \"" + c.Namespace + "-" + c.Release + ".redirect\"",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the infrastructure render lacks %q:\n%s", want, out)
		}
	}
}

func TestTheIntegrationSuitesValuesRenderTheApplicationChart(t *testing.T) {
	c := develConfig()

	out, err := render(t, append(setArgs(c.AppSet()), "--namespace", c.Namespace)...)
	if err != nil {
		t.Fatalf("the application chart refuses the integration suite's values: %v\n%s", err, out)
	}

	// The release is "example" in this helper, so the names the suite derives
	// are checked where they appear in values rather than in the object
	// names.
	for _, want := range []string{
		"hostnames: [\"" + c.Host() + "\"]",
		"name: " + devel.ParentName,
		"kind: ListenerSet",
		"serviceAccountName: " + devel.StandingS3Account,
		c.DatabaseHost(),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the application render lacks %q", want)
		}
	}

	// The accounts are the install's, not the chart's: none is rendered, so
	// none can collide with an account the install already made.
	if strings.Contains(out, "kind: ServiceAccount") {
		t.Errorf("the application chart renders a ServiceAccount although serviceAccount.create is false")
	}

	// The archiver's prefix reaches its configuration.
	if !strings.Contains(out, c.ArchivePrefix()) {
		t.Errorf("the archive prefix %q is not in the render", c.ArchivePrefix())
	}
}
