package charts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/truvity/policy/conformance"

	"github.com/truvity/policy/examples/url-shortener/charts"
	"github.com/truvity/policy/examples/url-shortener/internal/config"

	yaml "go.yaml.in/yaml/v3"
)

// chartDir writes the embedded charts out so `helm` can read them, and
// returns the path to the one named.
//
// The embed is what makes this test honest: Go's cache keys on the files the
// TEST package reads, not on what helm reads, so a template edit would
// otherwise leave a cached PASS behind and the contract would go unchecked.
func chartDir(t *testing.T, chart string) string {
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
	return filepath.Join(root, chart)
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
		"--set", "archive.bucket.name=url-shortener-archive",
	}, extra...)
}

func render(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("helm", append([]string{"template", "example", chartDir(t, "url-shortener")}, args...)...)
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
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	for _, tc := range rendered() {
		t.Run(tc.file, func(t *testing.T) {
			doc := conformance.ConfigMapData(t, []byte(out), tc.file)
			conformance.ValidDocument(t, doc, tc.schema())
		})
	}
}

// Every configuration file this chart renders, with the schema the binary
// that reads it validates against.
//
// `log.yaml` belongs to a component written in Python, and it is in this
// list on exactly the same terms as the others. That is the claim worth
// testing: the configuration contract is a property of the file, not of the
// language that reads it.
func rendered() []struct {
	file   string
	schema func() []byte
} {
	return []struct {
		file   string
		schema func() []byte
	}{
		{"migrate.yaml", func() []byte { return config.Read("migrate.json") }},
		{"redirect.yaml", func() []byte { return config.Read("redirect.json") }},
		{"stat.yaml", func() []byte { return config.Read("stat.json") }},
		{"urls.yaml", func() []byte { return config.Read("urls.json") }},
		{"web.yaml", func() []byte { return config.Read("web.json") }},
		{"log.yaml", func() []byte {
			return config.ReadPython("log/src/url_shortener_log/log.schema.json")
		}},
	}
}

// The same for a digest-pinned install, which is what a release produces.
// Different values, so a different chance to be wrong.
func TestADigestPinnedRenderAlsoProducesWhatTheBinariesAccept(t *testing.T) {
	out, err := render(t, defaults(
		"--set", "images.migrate.digest=sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"--set", "images.redirect.digest=sha256:2222222222222222222222222222222222222222222222222222222222222222",
		"--set", "images.urls.digest=sha256:3333333333333333333333333333333333333333333333333333333333333333",
		"--set", "images.stat.digest=sha256:4444444444444444444444444444444444444444444444444444444444444444",
		"--set", "images.log.digest=sha256:5555555555555555555555555555555555555555555555555555555555555555",
		"--set", "images.web.digest=sha256:6666666666666666666666666666666666666666666666666666666666666666",
	)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	for _, tc := range rendered() {
		t.Run(tc.file, func(t *testing.T) {
			conformance.ValidDocument(t, conformance.ConfigMapData(t, []byte(out), tc.file), tc.schema())
		})
	}
}

// What the chart renders, byte for byte, for two sets of values.
//
// The goldens are the check that nothing moved that nobody meant to move. A
// test that asserts a particular key is a test that says nothing about the
// other four hundred lines; a golden says something about all of them, and
// says it in a review rather than in a cluster.
//
// TWO cases, because they fail on different things. `minimal` is what the
// defaults render to, so a changed default shows up here. `everything` sets
// every value to something other than its default, so a template that
// stopped reading one shows up as a diff — which nothing else in this
// package would catch.
//
// Regenerate with `just golden` after reading the diff, never before.
func TestWhatTheChartRenders(t *testing.T) {
	for _, name := range []string{"minimal", "everything"} {
		t.Run(name, func(t *testing.T) {
			out, err := render(t, "-f", filepath.Join("testdata", name+".yaml"))
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

// firstDifference reports the first line that differs, with its number.
//
// The whole render is thousands of lines, and a test that printed all of it
// is a test whose output people stop reading — which is the same as not
// having one.
func firstDifference(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		w, g := "", ""
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return fmt.Sprintf("  line %d\n  want: %q\n  got:  %q", i+1, w, g)
		}
	}
	return "  (the files differ only in length)"
}

// A PUBLISHED chart installs with no image values at all.
//
// This is the test that was missing, and its absence was not visible from
// inside the repository: every other test here supplies a tag or a set of
// digests, so every one of them passed while the chart as published could
// not render a single Deployment. It was found by installing it — the
// release publishes the charts and the images in the same run, a digest
// only exists once the image is built, and the chart's values therefore
// reach the registry empty.
//
// What a published chart always knows is the version its release stamped.
// That is what it falls back to, and this asserts the fallback resolves to
// a reference for every component rather than to a refusal.
func TestAPublishedChartRendersWithNoImageValues(t *testing.T) {
	out, err := render(t, defaults()...)
	if err != nil {
		t.Fatalf("a chart with no image values must still render; this is what a consumer gets:\n%v\n%s", err, out)
	}

	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "image: ") {
			continue
		}
		ref := strings.TrimPrefix(trimmed, "image: ")
		if !strings.Contains(ref, ":") && !strings.Contains(ref, "@") {
			t.Errorf("%q names no version at all", ref)
		}
		seen[ref] = true
	}
	if len(seen) != 6 {
		t.Errorf("expected six image references, found %d: %v", len(seen), seen)
	}
}

// A route attaches to the parent it was GIVEN, kind included.
//
// A Gateway is not the only thing a route can attach to, and the default
// is silently wrong on a cluster that serves routes from something else.
// The failure has no good signal: the route is ACCEPTED, its status says
// so and goes on saying so, and the service answers 404. The only other
// tell is the listener reporting zero attached routes, which nobody
// watches.
//
// Found in a cluster, by a 404 on a service whose pods were all healthy.
func TestTheRouteAttachesToTheParentItWasGiven(t *testing.T) {
	out, err := render(t, defaults(
		"--set", "route.enabled=true",
		"--set", "route.hostname=example.test",
		"--set", "route.parentRef.name=business",
		"--set", "route.parentRef.kind=ListenerSet",
		"--set", "route.parentRef.group=gateway.networking.x-k8s.io",
		"--set", "route.parentRef.namespace=gateways",
	)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	route := docOfKind(t, out, "HTTPRoute")
	parents, ok := route["spec"].(map[string]any)["parentRefs"].([]any)
	if !ok || len(parents) != 1 {
		t.Fatalf("expected exactly one parent, got %#v", route["spec"])
	}

	parent, _ := parents[0].(map[string]any)
	for key, want := range map[string]string{
		"name":      "business",
		"kind":      "ListenerSet",
		"group":     "gateway.networking.x-k8s.io",
		"namespace": "gateways",
	} {
		if got := parent[key]; got != want {
			t.Errorf("parentRef.%s = %v, want %v", key, got, want)
		}
	}
}

// No endpoint means EXPORT NOTHING, in every component.
//
// Not "export to localhost and retry forever", which is what an SDK left
// to its own defaults does. That matters most where nobody is watching:
// a laptop, a test, a cluster with no collector. Decision 0006 records
// the failure this replaces -- a service that exported to a console in
// production because nothing set the environment name its code tested.
//
// Done in the chart rather than in six programs, so no component carries
// an enable flag and none of them can disagree.
func TestWithNoEndpointNothingIsExported(t *testing.T) {
	out, err := render(t, defaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	for _, doc := range documents(t, out) {
		kind, _ := doc["kind"].(string)
		if kind != "Deployment" && kind != "Job" {
			continue
		}
		for name, value := range telemetryEnvOf(t, doc) {
			switch name {
			case "OTEL_TRACES_EXPORTER", "OTEL_METRICS_EXPORTER", "OTEL_LOGS_EXPORTER":
				if value != "none" {
					t.Errorf("%s = %q with no endpoint configured, want none", name, value)
				}
			case "OTEL_EXPORTER_OTLP_ENDPOINT":
				t.Errorf("an endpoint was rendered where none was configured: %q", value)
			}
		}
	}
}

// Every component says who it is, and logs stay on stdout.
//
// `service.name` is a log STREAM field, so it must be stable for the life
// of the pod and carry no request, tenant or version. And OTLP logs are
// off deliberately: a node agent already collects stdout into the same
// store under the same namespace, so an exporter buys a second copy of
// what is there — and logs that exist only over OTLP vanish exactly when
// the exporter is what broke.
func TestEveryComponentNamesItselfAndLeavesLogsOnStdout(t *testing.T) {
	out, err := render(t, defaults("--set", "otel.endpoint=http://gateway:4318")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	seen := map[string]bool{}
	for _, doc := range documents(t, out) {
		kind, _ := doc["kind"].(string)
		if kind != "Deployment" && kind != "Job" {
			continue
		}
		env := telemetryEnvOf(t, doc)

		name := env["OTEL_SERVICE_NAME"]
		if name == "" {
			t.Errorf("a %s exports with no service name", kind)
		}
		if seen[name] {
			t.Errorf("two components share the service name %q; it is a log stream field", name)
		}
		seen[name] = true

		if env["OTEL_LOGS_EXPORTER"] != "none" {
			t.Errorf("%s exports OTLP logs (%q); stdout is already collected", name, env["OTEL_LOGS_EXPORTER"])
		}
		if env["OTEL_TRACES_EXPORTER"] != "otlp" || env["OTEL_METRICS_EXPORTER"] != "otlp" {
			t.Errorf("%s does not export traces and metrics with an endpoint set", name)
		}
	}

	if len(seen) != 6 {
		t.Errorf("expected all six components to carry telemetry, found %d: %v", len(seen), seen)
	}
}

// telemetryEnvOf reads the first container's environment as a map.
func telemetryEnvOf(t *testing.T, doc map[string]any) map[string]string {
	t.Helper()

	spec, _ := doc["spec"].(map[string]any)
	template, _ := spec["template"].(map[string]any)
	podSpec, _ := template["spec"].(map[string]any)
	containers, _ := podSpec["containers"].([]any)
	if len(containers) == 0 {
		return nil
	}

	container, _ := containers[0].(map[string]any)
	env, _ := container["env"].([]any)

	out := map[string]string{}
	for _, entry := range env {
		item, _ := entry.(map[string]any)
		name, _ := item["name"].(string)
		value, _ := item["value"].(string)
		out[name] = value
	}

	return out
}

// Six components are six DIFFERENT images.
//
// The chart used to take one `image.digest` and apply it to all of them,
// which renders perfectly and deploys the same container six times, each
// under a name suggesting otherwise. Nothing caught it: a render repeats a
// digest happily, and the configuration tests only read the ConfigMap.
//
// It took a real cluster to make the question come up at all, which is the
// argument for having one.
func TestEveryComponentGetsItsOwnImage(t *testing.T) {
	out, err := render(t, defaults(
		"--set", "images.migrate.digest=sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"--set", "images.redirect.digest=sha256:2222222222222222222222222222222222222222222222222222222222222222",
		"--set", "images.urls.digest=sha256:3333333333333333333333333333333333333333333333333333333333333333",
		"--set", "images.stat.digest=sha256:4444444444444444444444444444444444444444444444444444444444444444",
		"--set", "images.log.digest=sha256:5555555555555555555555555555555555555555555555555555555555555555",
		"--set", "images.web.digest=sha256:6666666666666666666666666666666666666666666666666666666666666666",
	)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	seen := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "image: ") {
			continue
		}
		ref := strings.TrimPrefix(trimmed, "image: ")
		digest := ref[strings.Index(ref, "@")+1:]
		if previous, ok := seen[digest]; ok && previous != ref {
			t.Errorf("two components share the digest %s:\n  %s\n  %s", digest, previous, ref)
		}
		seen[digest] = ref
	}
	if len(seen) < 6 {
		t.Errorf("expected six distinct images, found %d: %v", len(seen), seen)
	}
}

// No secret is ever rendered. A configuration file is mounted from a
// ConfigMap, printed when somebody debugs a deployment, and committed as a
// fixture; it must survive all three being true.
func TestNoSecretIsRendered(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
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
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
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
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
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

// A rollout must not have a gap, and the three numbers that make that true
// must agree. This is the only place it can be checked: it is a property of
// what is rendered, not of what runs.
func TestARolloutHasNoGap(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	for _, doc := range strings.Split(out, "\n---\n") {
		if !strings.Contains(doc, "kind: Deployment") {
			continue
		}

		name := docName(doc)

		// One instance cannot be replaced without a gap, whatever the
		// strategy says.
		if !strings.Contains(doc, "replicas: 2") {
			t.Errorf("%s: fewer than two instances, so a rollout has a gap", name)
		}

		// The default is 25%, which on two replicas removes one first.
		if !strings.Contains(doc, "maxUnavailable: 0") {
			t.Errorf("%s: the incumbent is removed before the replacement is ready", name)
		}

		if !strings.Contains(doc, "preStop") {
			t.Errorf("%s: no pre-stop delay, so traffic arrives after the process stops accepting", name)
		}

		if !strings.Contains(doc, "topologySpreadConstraints") {
			t.Errorf("%s: replicas may land on one machine, which satisfies the budget and not the intent", name)
		}
	}
}

// The grace period must exceed what the service is given to finish, or the
// orchestrator kills a draining process at the moment it would have
// succeeded. The failure looks like a network fault and is attributed to
// anything but the deploy.
func TestTheGracePeriodOutlastsTheDrain(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "drain.seconds=30", "--set", "drain.preStopSeconds=7")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	var seen int

	for _, doc := range strings.Split(out, "\n---\n") {
		for _, line := range strings.Split(doc, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "terminationGracePeriodSeconds:") {
				continue
			}

			seen++

			grace, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "terminationGracePeriodSeconds:")))
			if err != nil {
				t.Fatalf("%s: %v", docName(doc), err)
			}

			// 30 to finish, 7 before it starts: anything at or under 37
			// cuts the drain short.
			if grace <= 37 {
				t.Errorf("%s: grace period %d does not outlast a 30s drain behind a 7s delay", docName(doc), grace)
			}
		}
	}

	if seen == 0 {
		t.Fatal("no workload declares a grace period, so every drain is cut at the default")
	}
}

// A policy attaches to a route's rule BY NAME. A rule with no name cannot be
// targeted, and a policy that targets a name which does not exist is not
// refused — it is simply not attached, and the route keeps serving without
// it. An unauthenticated route that renders correctly is the failure this
// test exists to make impossible.
func TestEveryRouteRuleIsNamed(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "route.enabled=true", "--set", "route.parentRef.name=gw")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	var routes int

	for _, doc := range strings.Split(out, "\n---\n") {
		if !strings.Contains(doc, "kind: HTTPRoute") {
			continue
		}

		routes++

		lines := strings.Split(doc, "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != "rules:" {
				continue
			}

			// The first entry of the list that follows must carry a name.
			for _, l := range lines[i+1:] {
				if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "{{") {
					continue
				}

				if !strings.HasPrefix(strings.TrimSpace(l), "- name:") {
					t.Errorf("%s: the first rule is %q, which carries no name", docName(doc), strings.TrimSpace(l))
				}

				break
			}
		}
	}

	if routes == 0 {
		t.Fatal("no route rendered, so the rule name was not checked")
	}
}

// The chart grants nothing; it names an account. Every workload runs as it,
// so that a platform binding rights to that account binds them once.
func TestEveryWorkloadNamesTheAccount(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	var workloads int

	for _, doc := range strings.Split(out, "\n---\n") {
		if !strings.Contains(doc, "kind: Deployment") && !strings.Contains(doc, "kind: Job") {
			continue
		}

		workloads++

		if !strings.Contains(doc, "serviceAccountName:") {
			t.Errorf("%s: runs as the namespace's default account, which nothing can grant to", docName(doc))
		}
	}

	if workloads < 3 {
		t.Fatalf("expected the migration and both services, found %d", workloads)
	}
}

// docName pulls a manifest's metadata name out for an error message.
func docName(doc string) string {
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "  name: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "  name: "))
		}
	}

	return "an unnamed document"
}

// The migration and the services must not share an account, for the same
// reason they do not share a database credential: the migration's rights
// create and grant, and nothing on the request path should have them.
func TestTheMigrationAndTheServicesUseDifferentAccounts(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	var migrate, services []string

	for _, doc := range strings.Split(out, "\n---\n") {
		if !strings.Contains(doc, "kind: Deployment") && !strings.Contains(doc, "kind: Job") {
			continue
		}

		name := docName(doc)

		for _, line := range strings.Split(doc, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "serviceAccountName:") {
				continue
			}

			account := strings.TrimSpace(strings.TrimPrefix(line, "serviceAccountName:"))
			if strings.Contains(name, "migrate") {
				migrate = append(migrate, account)
			} else {
				services = append(services, account)
			}
		}
	}

	if len(migrate) == 0 || len(services) == 0 {
		t.Fatalf("expected the migration and the services to name an account each, got %v and %v", migrate, services)
	}

	for _, m := range migrate {
		for _, s := range services {
			if m == s {
				t.Errorf("the migration and a service both run as %q: the rights that create tables are on the request path", m)
			}
		}
	}
}

// Anything a pre-install hook REFERENCES must itself be a hook, because Helm
// applies every hook before the rest of the release. This has bitten three
// times here — a database, a configuration map, and an account — and each
// time the symptom was a hook that hung rather than an error naming a cause.
func TestWhatTheMigrationHookNeedsIsAlsoAHook(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	// A manifest is identified by KIND and name: the migration's Job and
	// the account it runs as are both called "<release>-migrate", and a
	// map keyed on the name alone silently loses one of them — which is
	// how the first version of this test passed while the install hung.
	type ref struct{ kind, name string }

	hooks := map[ref]bool{}
	needs := map[ref]bool{}

	for _, doc := range strings.Split(out, "\n---\n") {
		kind, name := docKind(doc), docName(doc)
		if kind == "" || name == "" {
			continue
		}

		if strings.Contains(doc, `"helm.sh/hook":`) {
			hooks[ref{kind, name}] = true
		}

		if kind != "Job" || !strings.Contains(name, "migrate") {
			continue
		}

		lines := strings.Split(doc, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)

			if account, ok := strings.CutPrefix(trimmed, "serviceAccountName:"); ok {
				needs[ref{"ServiceAccount", strings.TrimSpace(account)}] = true
			}

			// A volume's `configMap:` is followed by its name.
			if trimmed == "configMap:" && i+1 < len(lines) {
				if cm, ok := strings.CutPrefix(strings.TrimSpace(lines[i+1]), "name:"); ok {
					needs[ref{"ConfigMap", strings.TrimSpace(cm)}] = true
				}
			}
		}
	}

	if len(needs) < 2 {
		t.Fatalf("expected the migration to need an account and a configuration, found %v", needs)
	}

	for n := range needs {
		if !hooks[n] {
			t.Errorf("the migration hook needs %s/%s, which is an ordinary resource: "+
				"Helm applies hooks first, so the pod is never created and the install hangs",
				n.kind, n.name)
		}
	}
}

// docKind pulls a manifest's kind out.
func docKind(doc string) string {
	for _, line := range strings.Split(doc, "\n") {
		if kind, ok := strings.CutPrefix(line, "kind: "); ok {
			return strings.TrimSpace(kind)
		}
	}

	return ""
}

// THE test for an optional capability: with it off, the render carries no
// trace of it.
//
// This is what lets the default stay off forever. A chart is installed by
// someone whose platform provides none of this, and if turning the feature
// off still left a volume, a mount or a key behind, their pod would wait for
// something nobody serves. A golden would catch it eventually; this says so
// directly, and names what leaked.
func TestTransportOffLeavesNoTrace(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	for _, trace := range []string{
		"csi.cert-manager.io", // the driver
		"certificaterequests", // the permission to ask
		"trustDomain",         // the configuration block
		"identity",            // the volume and its mount
		"tls:",                // the block itself
	} {
		if strings.Contains(out, trace) {
			t.Errorf("the default render mentions %q; with the transport off it must carry no trace of it", trace)
		}
	}
}

// Turned on, each of those appears — otherwise the test above passes because
// the feature does not work at all.
func TestTransportOnRendersWhatThePlatformNeeds(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "tls.mode=strict",
		"--set", "tls.trustDomain=example.internal",
		"--set", "tls.peers.redirect[0].namespace=shop",
		"--set", "tls.peers.redirect[0].serviceAccount=web")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	for _, want := range []string{
		"spiffe.csi.cert-manager.io",
		"certificaterequests",
		"trustDomain: example.internal",
		"serviceAccount: web",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the render is missing %q, so the platform has nothing to act on", want)
		}
	}
}

// A trust domain is required the moment the transport is on. Without it a
// peer from ANY trust domain is admitted, which is a service that looks
// authenticated and is not.
// Transport on with NOBODY on any allow-list renders configuration the
// binaries accept.
//
// That combination is the DEFAULT — an empty list is what a service nobody
// has been granted looks like — and it was broken: `peers:` followed by an
// empty range renders a key with nothing under it, which is YAML null, not
// an empty list. Every binary refused it at start-up with "tls.peers: got
// null, want array" and crash-looped, and the chart rendered, installed and
// reported progress the whole time.
//
// A render test would not have caught it; this validates what each binary
// would actually read.
func TestTransportOnWithNoPeersIsStillValid(t *testing.T) {
	for _, mode := range []string{"permissive", "strict"} {
		t.Run(mode, func(t *testing.T) {
			out, err := render(t, defaults(
				"--set", "images.web.tag=dev",
				"--set", "tls.mode="+mode,
				"--set", "tls.trustDomain=example.test",
			)...)
			if err != nil {
				t.Fatalf("the chart does not render: %v\n%s", err, out)
			}
			for _, tc := range rendered() {
				t.Run(tc.file, func(t *testing.T) {
					// The assertion is the binary's own: validate what
					// the chart rendered with the schema the process
					// reads at start-up. A string check on the rendered
					// YAML would be checking indentation, which is not
					// what broke.
					doc := conformance.ConfigMapData(t, []byte(out), tc.file)
					conformance.ValidDocument(t, doc, tc.schema())
				})
			}
		})
	}
}

func TestTransportOnWithoutATrustDomainIsRefused(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev", "--set", "tls.mode=strict")...)
	if err == nil {
		t.Fatalf("a render with no trust domain was accepted:\n%s", out)
	}

	if !strings.Contains(out, "tls.trustDomain is required") {
		t.Errorf("the refusal does not say what is missing: %s", out)
	}
}

// Permissive means two ports, on the pod and on the Service alike. One
// listener cannot be both, and a Service that exposes only one of them makes
// the migration state unusable.
func TestPermissiveServesBothPorts(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "tls.mode=permissive",
		"--set", "tls.trustDomain=example.internal")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	var service string

	for _, doc := range strings.Split(out, "\n---\n") {
		if docKind(doc) == "Service" && strings.Contains(docName(doc), "redirect") {
			service = doc
		}
	}

	if service == "" {
		t.Fatal("no redirect Service rendered")
	}

	for _, port := range []string{"name: http", "name: https"} {
		if !strings.Contains(service, port) {
			t.Errorf("the Service does not carry %q, so one side of the migration is unreachable", port)
		}
	}
}

// The site and the resolver are DIFFERENT rules, and the named one is the
// site.
//
// A policy — for sign-in, for CSRF, for rate limiting — targets a rule by
// name. Attach it to the resolver and every short link demands a sign-in
// before it resolves, which is not a short link; leave the site unnamed and
// the policy protects nothing while reporting itself accepted. Both
// failures are silent and they are the same mistake pointing opposite ways.
//
// Found on a cluster: a policy whose target named a route this chart does
// not render, sitting "Accepted" beside a front end that answered 404
// because nothing routed to it at all.
func TestTheSiteAndTheResolverAreSeparateRules(t *testing.T) {
	out, err := render(t, defaults(
		"--set", "route.enabled=true",
		"--set", "route.hostname=example.test",
		"--set", "route.parentRef.name=business",
	)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	var route map[string]any
	for _, doc := range strings.Split(out, "\n---") {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil || m == nil {
			continue
		}
		if m["kind"] == "HTTPRoute" {
			route = m
			break
		}
	}
	if route == nil {
		t.Fatal("no HTTPRoute in the render")
	}

	// The route is named for the release. A policy targets a route by
	// name, so this is interface, not an internal detail.
	meta, _ := route["metadata"].(map[string]any)
	if got := meta["name"]; got != "example" {
		t.Errorf("the route is named %q; a platform's policy targets this name", got)
	}

	rules, _ := route["spec"].(map[string]any)["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("expected two rules — the site and the resolver — got %d", len(rules))
	}

	backendOf := map[string]string{}
	pathOf := map[string]string{}
	for _, r := range rules {
		rule, _ := r.(map[string]any)
		name, _ := rule["name"].(string)
		refs, _ := rule["backendRefs"].([]any)
		first, _ := refs[0].(map[string]any)
		backendOf[name], _ = first["name"].(string)
		matches, _ := rule["matches"].([]any)
		m0, _ := matches[0].(map[string]any)
		path, _ := m0["path"].(map[string]any)
		pathOf[name], _ = path["value"].(string)
	}

	// `app` is the DEFAULT of route.ruleName, and it must be the site:
	// that is the rule a sign-in policy is pointed at.
	if backendOf["app"] != "example-web" {
		t.Errorf("the rule a policy attaches to serves %q, not the site", backendOf["app"])
	}
	if pathOf["app"] != "/" {
		t.Errorf("the site rule matches %q, not the site root", pathOf["app"])
	}

	// The resolver is a separate rule, so protecting the site does not
	// lock it.
	if backendOf["redirect"] != "example-redirect" {
		t.Errorf("the resolver rule serves %q", backendOf["redirect"])
	}
	if pathOf["redirect"] != "/r/" {
		t.Errorf("the resolver rule matches %q", pathOf["redirect"])
	}
}
