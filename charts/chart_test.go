package charts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	policycharts "github.com/truvity/policy/charts"
	"github.com/truvity/policy/conformance"

	"github.com/truvity/policy/examples/url-shortener/charts"
	"github.com/truvity/policy/examples/url-shortener/internal/config"

	yaml "go.yaml.in/yaml/v3"
)

// chartDir writes the embedded charts out so `helm` can read them, and
// returns the path to the one named.
//
// They are laid out at the repository's OWN relative paths
// (`examples/url-shortener/charts/<chart>`, with the library at
// `charts/service-lib`), because a chart depends on the library through a
// `file://` path relative to itself; then `helm dependency build` resolves it
// from the committed Chart.lock, exactly as `helmctl package` does for a
// release. What renders is therefore the library as it stands in the tree.
//
// The embeds are what make this test honest: Go's cache keys on the files the
// TEST package reads, not on what helm reads, so a template edit would
// otherwise leave a cached PASS behind and the contract would go unchecked.
func chartDir(t *testing.T, chart string) string {
	t.Helper()
	top := t.TempDir()
	if err := os.CopyFS(top, os.DirFS(resolvedTree)); err != nil {
		t.Fatal(err)
	}
	if chart == "service-lib" {
		return filepath.Join(top, "charts", "service-lib")
	}

	return filepath.Join(top, "examples", "url-shortener", "charts", chart)
}

// resolvedTree is the charts laid out and their dependencies resolved, once
// per test run: `helm dependency build` costs far more than a render, and
// every one of the hundreds of renders here would pay it.
var resolvedTree string

func TestMain(m *testing.M) {
	top, err := os.MkdirTemp("", "url-shortener-charts-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	resolvedTree = top
	code := 1
	defer func() { _ = os.RemoveAll(top); os.Exit(code) }()

	root := filepath.Join(top, "examples", "url-shortener", "charts")
	if err := os.CopyFS(root, charts.Files); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	if err := os.CopyFS(filepath.Join(top, "charts"), policycharts.Library); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	for _, chart := range []string{"url-shortener", "testdata/service-example"} {
		dir := filepath.Join(root, chart)
		// An archive resolved on this machine is ignored by git and so absent
		// in CI; whatever the embed picked up is replaced by what the lock says.
		if err := os.RemoveAll(filepath.Join(dir, "charts")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		if out, err := exec.Command("helm", "dependency", "build", "--skip-refresh", dir).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "helm dependency build %s: %v\n%s", chart, err, out)
			return
		}
	}
	code = m.Run()
}

// defaults supplies the addresses every render needs. They are REQUIRED —
// the database and the stream belong to the infra release — so every test
// that is not about them says so once, here.
func defaults(extra ...string) []string {
	return append([]string{
		"--set", "database.host=example-pg-rw",
		"--set", "database.tls.rootCA.configMapName=example-root-ca",
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
	for _, name := range []string{"minimal", "everything", "per-component"} {
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

// Every component says who it is, and logs stay on stderr.
//
// `service.name` is a log STREAM field, so it must be stable for the life
// of the pod and carry no request, tenant or version. And OTLP logs are
// off deliberately: a node agent already collects stderr into the same
// store under the same namespace, so an exporter buys a second copy of
// what is there — and logs that exist only over OTLP vanish exactly when
// the exporter is what broke.
func TestEveryComponentNamesItselfAndLeavesLogsOnStderr(t *testing.T) {
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
			t.Errorf("%s exports OTLP logs (%q); stderr is already collected", name, env["OTEL_LOGS_EXPORTER"])
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
	// And the workloads read it from a Secret, mounted as a file, rather than
	// from a literal.
	if !strings.Contains(out, "secretName: example-pg-runtime") {
		t.Error("nothing mounts the password from a Secret, so something else must be supplying it")
	}
	if strings.Contains(out, "PGPASSWORD") {
		t.Error("the password is rendered as a variable; it is a mounted file the client re-reads")
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
		name := docName(doc)
		if kind := docKind(doc); kind != "Deployment" && kind != "Job" {
			continue
		}

		// The password is a Secret volume: `secret:` is followed by its name.
		for _, line := range strings.Split(doc, "\n") {
			secret, ok := strings.CutPrefix(strings.TrimSpace(line), "secretName: ")
			if !ok {
				continue
			}

			if strings.Contains(name, "migrate") {
				migrate = append(migrate, secret)
			} else {
				services = append(services, secret)
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

	// The root the database server is verified against is the PLATFORM's
	// ConfigMap: it exists in the namespace before any release, so it is not an
	// ordinary resource of this one that a hook could not wait for.
	delete(needs, ref{"ConfigMap", "example-root-ca"})

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
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
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
	// `strict` is the URL service's alone — see TestRedirectIsNeverStrict —
	// so it is reached through the per-component override.
	for name, mode := range map[string][]string{
		"permissive": {"--set", "tls.mode=permissive"},
		"strict":     {"--set", "tls.mode=permissive", "--set", "tls.components.urls.mode=strict"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := render(t, defaults(append([]string{
				"--set", "images.web.tag=dev",
				"--set", "tls.trustDomain=example.test",
			}, mode...)...)...)
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
	out, err := render(t, defaults("--set", "images.web.tag=dev", "--set", "tls.mode=permissive")...)
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

// tlsOf reads the `tls` block out of one rendered configuration file, as the
// binary that owns it would: mode, the authenticated address (permissive
// only) and the allow-list. A file with no block returns a nil map.
func tlsOf(t *testing.T, out, file string) map[string]any {
	t.Helper()

	var doc map[string]any
	if err := yaml.Unmarshal(conformance.ConfigMapData(t, []byte(out), file), &doc); err != nil {
		t.Fatalf("%s is not YAML: %v", file, err)
	}
	block, _ := doc["tls"].(map[string]any)
	return block
}

// urlsAddressOf is the address a caller (`web`, `stat`) dials the URL service
// at, which follows the URL SERVICE's effective mode and nothing else.
func urlsAddressOf(t *testing.T, out, file string) string {
	t.Helper()

	var doc struct {
		Urls struct{ Address string } `yaml:"urls"`
	}
	if err := yaml.Unmarshal(conformance.ConfigMapData(t, []byte(out), file), &doc); err != nil {
		t.Fatalf("%s is not YAML: %v", file, err)
	}
	return doc.Urls.Address
}

// THE point of the per-component override: `urls` strict while `redirect`
// stays permissive, every file still valid against its binary's schema.
//
// The three things that have to move together are asserted separately, since
// each is a different way for the pair to disagree: the URL service's own
// mode, the port its callers dial (the ordinary one, over TLS), and the
// redirect service, which must NOT have followed it.
func TestUrlsCanBeStrictWhileRedirectStaysPermissive(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.internal",
		"--set", "tls.peers.urls[0].namespace=other",
		"--set", "tls.peers.urls[0].serviceAccount=reader")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	for _, tc := range rendered() {
		t.Run(tc.file, func(t *testing.T) {
			conformance.ValidDocument(t, conformance.ConfigMapData(t, []byte(out), tc.file), tc.schema())
		})
	}

	if got := tlsOf(t, out, "urls.yaml")["mode"]; got != "strict" {
		t.Errorf("urls.yaml tls.mode = %v, want strict", got)
	}
	if _, ok := tlsOf(t, out, "urls.yaml")["address"]; ok {
		t.Error("urls.yaml names a second listener under strict; there is only the ordinary one")
	}
	redirect := tlsOf(t, out, "redirect.yaml")
	if redirect["mode"] != "permissive" || redirect["address"] != ":8443" {
		t.Errorf("redirect.yaml tls = %v, want permissive on :8443", redirect)
	}

	// Callers dial the URL service's ordinary port, over TLS.
	for _, file := range []string{"web.yaml", "stat.yaml"} {
		if got := urlsAddressOf(t, out, file); got != "https://example-urls:8080" {
			t.Errorf("%s dials the URL service at %q, want https://example-urls:8080", file, got)
		}
		if got := tlsOf(t, out, file)["mode"]; got != "strict" {
			t.Errorf("%s tls.mode = %v, want strict", file, got)
		}
	}

	// The Services follow the pods: only redirect grows a second port.
	for _, doc := range strings.Split(out, "\n---\n") {
		if docKind(doc) != "Service" {
			continue
		}
		hasHTTPS := strings.Contains(doc, "name: https")
		switch {
		case strings.Contains(docName(doc), "redirect") && !hasHTTPS:
			t.Error("the redirect Service lost its authenticated port")
		case strings.Contains(docName(doc), "urls") && hasHTTPS:
			t.Error("the urls Service carries an authenticated second port under strict")
		}
	}
}

// The override defaults to tls.mode, and adding the key changes nothing a
// release that leaves it unset can see: permissive everywhere renders the
// same as permissive with `urls` named permissive explicitly.
func TestUrlsOverrideDefaultsToTheReleaseWideMode(t *testing.T) {
	base := defaults("--set", "images.web.tag=dev", "--set", "tls.mode=permissive", "--set", "tls.trustDomain=example.internal")

	implicit, err := render(t, base...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, implicit)
	}
	explicit, err := render(t, append(base, "--set", "tls.components.urls.mode=permissive")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, explicit)
	}
	if implicit != explicit {
		t.Error("naming the release-wide mode explicitly for urls changed the render")
	}
}

// `urls` may be off or permissive beneath a permissive release, and the
// override may also turn a component ON under an off release.
func TestUrlsOverrideCanTurnTheTransportOnForOneComponent(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.internal")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	if tlsOf(t, out, "redirect.yaml") != nil {
		t.Error("redirect.yaml carries a tls block although its mode is off")
	}
	if got := tlsOf(t, out, "urls.yaml")["mode"]; got != "strict" {
		t.Errorf("urls.yaml tls.mode = %v, want strict", got)
	}
	// The callers still need an identity to call a strict URL service.
	if !strings.Contains(out, "spiffe.csi.cert-manager.io") {
		t.Error("no identity volume is rendered, so nothing can present a certificate")
	}
}

// Both refusals of a strict redirect, and the two ways of spelling them.
//
// Written down, the SCHEMA refuses it. Inherited (a release-wide strict with
// nothing said for redirect) only the render can, and it says why and what
// to do instead — a bare enum error would name a value and leave the
// operator to work out that the gateway is the reason.
func TestRedirectIsNeverStrict(t *testing.T) {
	cases := map[string]struct {
		set  []string
		want string
	}{
		"explicit": {
			set:  []string{"--set", "tls.mode=permissive", "--set", "tls.components.redirect.mode=strict"},
			want: "tls/components/redirect/mode",
		},
		"inherited": {
			set:  []string{"--set", "tls.mode=strict"},
			want: "redirect cannot be strict",
		},
		"inherited past an override for urls": {
			set:  []string{"--set", "tls.mode=strict", "--set", "tls.components.urls.mode=strict"},
			want: "redirect cannot be strict",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := render(t, defaults(append([]string{
				"--set", "images.web.tag=dev",
				"--set", "tls.trustDomain=example.internal",
			}, tc.set...)...)...)
			if err == nil {
				t.Fatalf("a strict redirect was accepted:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not say %q: %s", tc.want, out)
			}
		})
	}

	// A release-wide strict IS fine once redirect is named, since nothing
	// is then strict that must not be.
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "tls.mode=strict",
		"--set", "tls.components.redirect.mode=permissive",
		"--set", "tls.trustDomain=example.internal")...)
	if err != nil {
		t.Fatalf("a release-wide strict with redirect named permissive was refused: %v\n%s", err, out)
	}
	if got := tlsOf(t, out, "redirect.yaml")["mode"]; got != "permissive" {
		t.Errorf("redirect.yaml tls.mode = %v, want permissive", got)
	}
}

// A per-component key is only for a component that serves. `web` and `stat`
// call out and have no mode of their own, so a key for either is a typo the
// render must refuse rather than ignore.
func TestOnlyServingComponentsHaveATLSMode(t *testing.T) {
	for _, component := range []string{"web", "stat", "log", "migrate"} {
		t.Run(component, func(t *testing.T) {
			out, err := render(t, defaults("--set", "images.web.tag=dev",
				"--set", "tls.mode=permissive",
				"--set", "tls.trustDomain=example.internal",
				"--set", "tls.components."+component+".mode=strict")...)
			if err == nil {
				t.Fatalf("tls.components.%s was accepted:\n%s", component, out)
			}
		})
	}
}

// The empty allow-list under the override renders `[]`, never null — the
// same trap TestTransportOnWithNoPeersIsStillValid guards, reached through
// the new path. `urls` always carries its own release's counter, so the list
// is never empty there; `redirect` is the one that can be, and it must still
// be an array.
func TestPerComponentModeStillRendersEmptyPeersAsAnArray(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.internal")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	peers, ok := tlsOf(t, out, "redirect.yaml")["peers"].([]any)
	if !ok || len(peers) != 0 {
		t.Errorf("redirect tls.peers = %#v, want an empty array", tlsOf(t, out, "redirect.yaml")["peers"])
	}
}

// serviceAccountsOf maps every Deployment and Job in a render to the account
// it runs as, keyed by component (the name after the release's own prefix).
func serviceAccountsOf(t *testing.T, out string) map[string]string {
	t.Helper()

	got := map[string]string{}

	for _, doc := range strings.Split(out, "\n---\n") {
		if !strings.Contains(doc, "kind: Deployment") && !strings.Contains(doc, "kind: Job") {
			continue
		}

		var m struct {
			Metadata struct{ Name string } `yaml:"metadata"`
			Spec     struct {
				Template struct {
					Spec struct {
						ServiceAccountName string `yaml:"serviceAccountName"`
					} `yaml:"spec"`
				} `yaml:"template"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatalf("not YAML: %v", err)
		}

		got[strings.TrimPrefix(m.Metadata.Name, "example-")] = m.Spec.Template.Spec.ServiceAccountName
	}

	return got
}

// peerAccounts is the ServiceAccount names in one file's tls.peers.
func peerAccounts(t *testing.T, out, file string) []string {
	t.Helper()

	peers, _ := tlsOf(t, out, file)["peers"].([]any)
	names := []string{}

	for _, p := range peers {
		names = append(names, p.(map[string]any)["serviceAccount"].(string))
	}

	return names
}

func componentArgs(extra ...string) []string {
	return defaults(append([]string{
		"--set", "images.web.tag=dev",
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.internal",
	}, extra...)...)
}

// THE point of the switch: with it on, every component has its own account,
// and the URL service's allow-list is exactly its real internal callers.
func TestEveryComponentRunsAsItsOwnAccount(t *testing.T) {
	out, err := render(t, componentArgs()...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	got := serviceAccountsOf(t, out)
	want := map[string]string{
		"redirect": "example-redirect",
		"urls":     "example-urls",
		"web":      "example-web",
		"stat":     "example-stat",
		"log":      "example-log",
		"migrate":  "example-migrate",
	}

	for c, sa := range want {
		if got[c] != sa {
			t.Errorf("%s runs as %q, want %q (all: %v)", c, got[c], sa, got)
		}
	}

	if got, want := peerAccounts(t, out, "urls.yaml"), []string{"example-web", "example-stat"}; !reflect.DeepEqual(got, want) {
		t.Errorf("urls admits %v, want exactly %v", got, want)
	}

	if got := peerAccounts(t, out, "redirect.yaml"); len(got) != 0 {
		t.Errorf("redirect admits %v internally, want nobody", got)
	}

	// The callers accept an answer only from the URL service's OWN account.
	for _, f := range []string{"web.yaml", "stat.yaml"} {
		if got, want := peerAccounts(t, out, f), []string{"example-urls"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s accepts an answer from %v, want %v", f, got, want)
		}
	}

	for _, c := range rendered() {
		conformance.ValidDocument(t, conformance.ConfigMapData(t, []byte(out), c.file), c.schema())
	}

	for _, sa := range []string{"example-redirect", "example-urls", "example-web", "example-stat", "example-log"} {
		if !strings.Contains(out, "kind: ServiceAccount\nmetadata:\n  name: "+sa+"\n") {
			t.Errorf("no ServiceAccount %s is created", sa)
		}
	}

	if strings.Contains(out, "example-app") {
		t.Errorf("a shared application account is still rendered")
	}
}

// External grants are the operator's and do not move.
func TestExternalGrantsAndRenamedAccounts(t *testing.T) {
	out, err := render(t, componentArgs(
		"--set", "serviceAccount.components.stat.name=counter",
		"--set", "tls.peers.urls[0].namespace=other",
		"--set", "tls.peers.urls[0].serviceAccount=reader")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	if got, want := peerAccounts(t, out, "urls.yaml"), []string{"example-web", "counter", "reader"}; !reflect.DeepEqual(got, want) {
		t.Errorf("urls admits %v, want %v", got, want)
	}

	if got := serviceAccountsOf(t, out)["stat"]; got != "counter" {
		t.Errorf("stat runs as %q, want counter", got)
	}
}

// `serviceAccount.app.name` is the account `log` runs as, and nothing else:
// it is what a platform bound the archive bucket's cloud role to, and log is
// the only component that needs it.
func TestTheAppAccountNameIsLogsAndOnlyLogs(t *testing.T) {
	out, err := render(t, componentArgs(
		"--set", "serviceAccount.app.name=archiver",
		"--set", "serviceAccount.app.annotations.example\\.invalid/role=archive")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	for c, sa := range serviceAccountsOf(t, out) {
		if (sa == "archiver") != (c == "log") {
			t.Errorf("%s runs as %q: only log may run as the app account", c, sa)
		}
	}

	// The cloud-binding annotation lands on log's account alone.
	n := 0
	for _, doc := range strings.Split(out, "\n---\n") {
		var sa struct {
			Kind     string
			Metadata struct {
				Name        string
				Annotations map[string]string
			}
		}
		unmarshalYAML(t, []byte(doc), &sa)
		if sa.Kind == "ServiceAccount" && sa.Metadata.Annotations["example.invalid/role"] == "archive" {
			n++
			if sa.Metadata.Name != "archiver" {
				t.Errorf("the app annotations are on %q, not log's account", sa.Metadata.Name)
			}
		}
	}
	if n != 1 {
		t.Errorf("the app annotations are on %d accounts, want 1 (log's)", n)
	}
}

// THE contract rule (component.md C14): no two workloads of the chart share a
// ServiceAccount, in the default render and in a fully-set one. A workload
// identity is namespace plus account, so sharing one makes two components
// indistinguishable to an allow-list. The migration hook is NOT exempt: its
// rights create tables and must stay off the request path.
func TestNoTwoWorkloadsShareAServiceAccount(t *testing.T) {
	for name, args := range map[string][]string{
		"defaults":   defaults("--set", "images.web.tag=dev"),
		"everything": {"-f", filepath.Join("testdata", "everything.yaml")},
		"identity":   {"-f", filepath.Join("testdata", "per-component.yaml")},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := render(t, args...)
			if err != nil {
				t.Fatalf("render: %v\n%s", err, out)
			}

			byAccount := map[string]string{}

			for c, sa := range serviceAccountsOf(t, out) {
				if sa == "" {
					t.Errorf("%s names no account", c)
				}

				if other, dup := byAccount[sa]; dup {
					t.Errorf("%s and %s both run as %q", other, c, sa)
				}

				byAccount[sa] = c
			}

			if len(byAccount) < 6 {
				t.Errorf("expected five components and the migration, found %v", byAccount)
			}
		})
	}
}

// Redirect authenticates to the broker with its workload identity when asked,
// and ONLY redirect: `stat` and `log` keep their tokens, because the change
// is a pilot on one publisher and a schema that admits the key in a file
// nothing reads would be a field somebody eventually sets.
func TestEventsIdentityReplacesTheTokenForRedirectAlone(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "events.auth.audience=nats",
		"--set", "tls.mode=permissive",
		"--set", "tls.trustDomain=example.internal",
		"--set", "events.tls.enabled=true",
		"--set", "events.tls.caConfigMap=broker-ca",
		"--set", "events.tls.serverName=broker.example.internal")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	redirect := conformance.ConfigMapData(t, []byte(out), "redirect.yaml")
	conformance.ValidDocument(t, redirect, config.Read("redirect.json"))

	for _, want := range []string{
		"caFile: /var/run/events-ca/ca-certificates.crt",
		"serverName: broker.example.internal",
	} {
		if !strings.Contains(string(redirect), want) {
			t.Errorf("redirect.yaml is missing %q:\n%s", want, redirect)
		}
	}

	if strings.Contains(string(redirect), "tokenFile") {
		t.Errorf("redirect.yaml still names a token file: the certificate must be the only credential, or a certificate the broker cannot map falls through to the token path and succeeds as somebody else:\n%s", redirect)
	}

	for _, other := range []string{"stat.yaml", "log.yaml"} {
		doc := conformance.ConfigMapData(t, []byte(out), other)
		if !strings.Contains(string(doc), "tokenFile: /var/run/events/token") {
			t.Errorf("%s lost its token: the identity is redirect's alone", other)
		}

		if strings.Contains(string(doc), "events-ca") {
			t.Errorf("%s was given the broker's trust bundle: the identity is redirect's alone", other)
		}
	}
}

// Off by default, and then the render carries no trace of it: the broker
// trust bundle volume, the config key. A golden would catch it; this names it.
func TestEventsIdentityOffLeavesNoTrace(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev", "--set", "events.auth.audience=nats")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	for _, trace := range []string{"events-ca", "events-ca/", "serverName"} {
		if strings.Contains(out, trace) {
			t.Errorf("the default render mentions %q; with events.tls off it must carry no trace of it", trace)
		}
	}
}

// The refusals: an identity with nothing mounted, and no trust bundle for the
// broker, each name the setting rather than failing later at a handshake.
func TestEventsIdentityIsRefusedWhereItCannotWork(t *testing.T) {
	for name, tc := range map[string]struct {
		set  []string
		want string
	}{
		"redirect has no identity": {
			set:  []string{"--set", "events.tls.enabled=true", "--set", "events.tls.caConfigMap=broker-ca"},
			want: "redirect has none",
		},
		"no trust bundle for the broker": {
			set: []string{"--set", "tls.mode=permissive", "--set", "tls.trustDomain=example.internal",
				"--set", "events.tls.enabled=true"},
			want: "events.tls.caConfigMap is required",
		},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := render(t, defaults(append([]string{"--set", "images.web.tag=dev"}, tc.set...)...)...)
			if err == nil {
				t.Fatalf("the render was accepted:\n%s", out)
			}

			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not say what is wrong (want %q): %s", tc.want, out)
			}
		})
	}
}

// What a workload that dials the database is handed: the libpq environment the
// platform's PostgreSQL client reads, as parts. No connection URL appears
// anywhere in the render, because a URL is a string a parameter can be dropped
// from on its way to the driver.
func TestTheDatabaseIsParts(t *testing.T) {
	out, err := render(t, defaults("--namespace", "shop", "--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatalf("does not render: %v\n%s", err, out)
	}
	for _, unwanted := range []string{"postgres://", "postgresql://", "sslmode=require", "DATABASE_PASSWORD", "passwordEnv"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("%q rendered: the connection is parts, and the password is a file", unwanted)
		}
	}
	dialing := 0
	for name, doc := range workloadsOf(t, out) {
		if !dialsTheDatabase(name) {
			continue
		}
		dialing++
		env := telemetryEnvOf(t, doc)
		for key, want := range map[string]string{
			"PGHOST":                    "example-pg-rw.shop.svc." + "cluster." + "local",
			"PGDATABASE":                "url_shortener",
			"PGSSLMODE":                 "verify-full",
			"PGSSLROOTCERT":             "/etc/url-shortener-pg-ca/ca-certificates.crt",
			"CNPG_CLIENT_PASSWORD_FILE": "/etc/url-shortener-pg-password/password",
		} {
			if env[key] != want {
				t.Errorf("%s: %s is %q, want %q", name, key, env[key], want)
			}
		}
	}
	if dialing != 3 {
		t.Errorf("found %d workloads that dial the database, want 3 (urls, redirect, migrate)", dialing)
	}
}

// dialsTheDatabase says whether a workload (by name, in a release called
// "example") is one of the three that hold a database connection.
func dialsTheDatabase(name string) bool {
	return name == "example-urls" || name == "example-redirect" || name == "example-migrate"
}

// workloadsOf parses a render and returns every Deployment and Job by name.
// The tests that follow ask what a workload is HANDED, not how the YAML that
// says so happens to be spelled: a quoting or indentation change in a
// template is not a change to any of the properties below.
func workloadsOf(t *testing.T, out string) map[string]map[string]any {
	t.Helper()

	got := map[string]map[string]any{}
	for _, doc := range documents(t, out) {
		if kind, _ := doc["kind"].(string); kind != "Deployment" && kind != "Job" {
			continue
		}
		meta, _ := doc["metadata"].(map[string]any)
		name, _ := meta["name"].(string)
		got[name] = doc
	}

	return got
}

// podOf is a workload's pod spec.
func podOf(doc map[string]any) map[string]any {
	spec, _ := doc["spec"].(map[string]any)
	template, _ := spec["template"].(map[string]any)
	pod, _ := template["spec"].(map[string]any)

	return pod
}

// mountPathsOf is every path any container of a workload mounts something at.
func mountPathsOf(doc map[string]any) []string {
	var paths []string
	containers, _ := podOf(doc)["containers"].([]any)
	for _, c := range containers {
		mounts, _ := c.(map[string]any)["volumeMounts"].([]any)
		for _, m := range mounts {
			path, _ := m.(map[string]any)["mountPath"].(string)
			paths = append(paths, path)
		}
	}

	return paths
}

// secretNamesOf is every Secret a workload's volumes read.
func secretNamesOf(doc map[string]any) []string {
	var names []string
	volumes, _ := podOf(doc)["volumes"].([]any)
	for _, v := range volumes {
		secret, _ := v.(map[string]any)["secret"].(map[string]any)
		if name, ok := secret["secretName"].(string); ok {
			names = append(names, name)
		}
	}

	return names
}

// Each workload logs in as ITS role, with that role's Secret, and no other:
// the services as the runtime role, the migration as the owner.
func TestEachWorkloadGetsItsOwnRolesCredential(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatal(err)
	}
	workloads := workloadsOf(t, out)
	for _, tc := range []struct {
		workload, role, secret string
	}{
		{"example-urls", "url_shortener_app", "example-pg-runtime"},
		{"example-redirect", "url_shortener_app", "example-pg-runtime"},
		{"example-migrate", "url_shortener_owner", "example-pg-app"},
	} {
		doc, ok := workloads[tc.workload]
		if !ok {
			t.Errorf("no workload named %q", tc.workload)
			continue
		}
		if got := telemetryEnvOf(t, doc)["PGUSER"]; got != tc.role {
			t.Errorf("%s: PGUSER is %q, not %s", tc.workload, got, tc.role)
		}
		if got := secretNamesOf(doc); len(got) != 1 || got[0] != tc.secret {
			t.Errorf("%s: the password Secrets are %v, not exactly %s", tc.workload, got, tc.secret)
		}
		if !strings.Contains(fmt.Sprint(podOf(doc)["volumes"]), "path:password") {
			t.Errorf("%s: the password is not projected to a file", tc.workload)
		}
	}
}

// verify-full is the only setting. `require` is refused with the reason
// rather than rendered into a Deployment the client would refuse to start,
// and a missing root is refused too: there would be nothing to verify against.
func TestDatabaseTLSIsVerifyFullOrNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"require": {
			args: []string{"--set", "database.tls.mode=require"},
			want: "verify-full",
		},
		"no root": {
			args: []string{"--set", "database.tls.rootCA.configMapName="},
			want: "database.tls.rootCA.configMapName is required",
		},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := render(t, defaults(append([]string{"--set", "images.web.tag=dev"}, tc.args...)...)...)
			if err == nil {
				t.Fatalf("the render was accepted:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not say what is wrong (want %q): %s", tc.want, out)
			}
		})
	}

	// A platform that already sets the one accepted value keeps rendering.
	if out, err := render(t, defaults("--set", "database.tls.mode=verify-full", "--set", "images.web.tag=dev")...); err != nil {
		t.Errorf("mode=verify-full is refused: %v\n%s", err, out)
	}
}

// The host is the fully-qualified name (the only form a server certificate
// carries) in the release's namespace, with the cluster's DNS suffix, and the
// root is mounted as a directory into exactly the three workloads that dial
// the database.
func TestDatabaseHostAndRoot(t *testing.T) {
	out, err := render(t, defaults(
		"--namespace", "shop",
		"--set", "database.clusterDomain=cluster.example",
		"--set", "images.web.tag=dev",
	)...)
	if err != nil {
		t.Fatalf("does not render: %v\n%s", err, out)
	}
	hosts, roots, passwords := 0, 0, 0
	for name, doc := range workloadsOf(t, out) {
		if telemetryEnvOf(t, doc)["PGHOST"] == "example-pg-rw.shop.svc.cluster.example" {
			hosts++
		}
		for _, path := range mountPathsOf(doc) {
			switch path {
			case "/etc/url-shortener-pg-ca":
				roots++
			case "/etc/url-shortener-pg-password":
				passwords++
			}
		}
		if !dialsTheDatabase(name) && (telemetryEnvOf(t, doc)["PGHOST"] != "" || len(secretNamesOf(doc)) != 0) {
			t.Errorf("%s holds a database connection or a password, and does not dial the database", name)
		}
	}
	if hosts != 3 {
		t.Errorf("the qualified host is set %d times, want 3 (urls, redirect, migrate)", hosts)
	}
	// urls, redirect, migrate: each mounts it; nothing else does.
	if roots != 3 {
		t.Errorf("the root is mounted %d times, want 3 (urls, redirect, migrate)", roots)
	}
	if passwords != 3 {
		t.Errorf("a password is mounted %d times, want 3 (urls, redirect, migrate)", passwords)
	}
	if strings.Contains(out, "subPath") {
		t.Error("a subPath mount does not follow a rotation of the ConfigMap or the Secret")
	}

	// A host that is already qualified is left alone.
	out, err = render(t, defaults("--set", "database.host=pg-rw.db.svc.example", "--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range workloadsOf(t, out) {
		if dialsTheDatabase(name) && telemetryEnvOf(t, doc)["PGHOST"] != "pg-rw.db.svc.example" {
			t.Errorf("%s: a qualified host was rewritten to %q", name, telemetryEnvOf(t, doc)["PGHOST"])
		}
	}
}

// stat has no database, so it must not be handed a credential for one.
func TestStatHasNoDatabasePassword(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range strings.Split(out, "\n---\n") {
		if strings.Contains(doc, "app.kubernetes.io/component: stat") &&
			strings.Contains(doc, "kind: Deployment") &&
			(strings.Contains(doc, "database-password") || strings.Contains(doc, "PGHOST")) {
			t.Error("stat carries a database credential or connection")
		}
	}
}

// The cluster's DNS suffix defaults to the one every cluster has unless told
// otherwise.
func TestDatabaseClusterDomainDefault(t *testing.T) {
	out, err := render(t, defaults("--namespace", "shop")...)
	if err != nil {
		t.Fatal(err)
	}
	if want := "example-pg-rw.shop.svc." + "cluster." + "local"; !strings.Contains(out, want) {
		t.Errorf("the default suffix is not applied; want %q", want)
	}
}

// The front end's CSP is configured from the chart and rendered into the
// file the binary validates. A value that stopped reaching it would leave a
// page on the default policy with nothing to say so.
func TestTheContentSecurityPolicyReachesTheFrontEndConfiguration(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "web.csp.mode=enforce",
		"--set", "web.csp.connectSrc[0]=https://collector.example")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	var web struct {
		Csp struct {
			Mode       string
			ConnectSrc []string `yaml:"connectSrc"`
		}
	}
	unmarshalYAML(t, conformance.ConfigMapData(t, []byte(out), "web.yaml"), &web)
	if web.Csp.Mode != "enforce" || len(web.Csp.ConnectSrc) != 1 || web.Csp.ConnectSrc[0] != "https://collector.example" {
		t.Errorf("web.yaml's csp is %+v, want mode enforce and connectSrc [https://collector.example]", web.Csp)
	}
}

func TestAnOriginThatIsNotAnOriginIsRefusedForTheContentSecurityPolicy(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev",
		"--set", "web.csp.connectSrc[0]=https://collector.example; script-src *")...)
	if err == nil {
		t.Fatalf("a directive smuggled through connectSrc rendered:\n%s", out)
	}
}

// Browser telemetry is off unless asked for: a default render has no `faro`
// block at all, so nothing changes for anyone who does not set it.
func TestBrowserTelemetryIsOffByDefaultAndRendersWhenEnabled(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	if strings.Contains(out, "faro:") {
		t.Errorf("a default render carries a faro block")
	}

	out, err = render(t, defaults("--set", "images.web.tag=dev",
		"--set", "web.faro.enabled=true",
		"--set", "web.faro.collectorUrl=https://collector.example/collect",
		"--set-json", "web.faro.sampleRate=0.5")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	var web struct {
		Faro struct {
			Enabled      bool
			CollectorURL string  `yaml:"collectorUrl"`
			SampleRate   float64 `yaml:"sampleRate"`
		}
	}
	unmarshalYAML(t, conformance.ConfigMapData(t, []byte(out), "web.yaml"), &web)
	if !web.Faro.Enabled || web.Faro.CollectorURL != "https://collector.example/collect" || web.Faro.SampleRate != 0.5 {
		t.Errorf("web.yaml's faro is %+v, want enabled with the collector given and a sample rate of 0.5", web.Faro)
	}
}

func TestBrowserTelemetryDefaultsToTheSameOriginPathAndRefusesPlainHTTP(t *testing.T) {
	out, err := render(t, defaults("--set", "images.web.tag=dev", "--set", "web.faro.enabled=true")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	var web struct {
		Faro struct {
			CollectorURL string `yaml:"collectorUrl"`
		}
	}
	unmarshalYAML(t, conformance.ConfigMapData(t, []byte(out), "web.yaml"), &web)
	if web.Faro.CollectorURL != "/faro/collect" {
		t.Errorf("the collector is %q, not the same-origin default", web.Faro.CollectorURL)
	}
	if out, err := render(t, defaults("--set", "images.web.tag=dev", "--set", "web.faro.enabled=true",
		"--set", "web.faro.collectorUrl=http://plain.example")...); err == nil {
		t.Fatalf("a plain-HTTP collector was accepted:\n%s", out)
	}
}

// The telemetry rule is optional and PUBLIC: its own rule, exact path, POST
// only, so the site's sign-in policy (which attaches to a rule by name) never
// covers it and nothing else of the collector is exposed through it.
func TestTheTelemetryRouteRuleIsOptionalAndNarrow(t *testing.T) {
	routed := []string{"--set", "images.web.tag=dev", "--set", "route.enabled=true", "--set", "route.parentRef.name=gw"}
	out, err := render(t, defaults(routed...)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	if strings.Contains(out, "/faro/collect") {
		t.Errorf("a default route carries the telemetry rule")
	}

	out, err = render(t, defaults(append(routed,
		"--set", "route.faro.enabled=true",
		"--set", "route.faro.backend.name=collector",
		"--set", "route.faro.backend.port=12347",
		"--set", "route.faro.rewritePath=/collect")...)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	for _, want := range []string{"name: faro", "type: Exact", "value: /faro/collect", "method: POST",
		"replaceFullPath: /collect", "name: collector"} {
		if !strings.Contains(out, want) {
			t.Errorf("the render lacks %q", want)
		}
	}

	if out, err := render(t, defaults(append(routed, "--set", "route.faro.enabled=true")...)...); err == nil {
		t.Fatalf("a telemetry rule with no backend rendered:\n%s", out)
	}
}

// A component that CALLS the URL service over the authenticated transport
// holds everything it needs to trust the answer: the identity volume is
// mounted, and the trust bundle (`caFile`) it verifies the answer against, its
// own certificate and its key are all named UNDER that mount. The front end is
// a Node process that verifies the answer against exactly this bundle and
// nothing else (its built-in roots never include the platform's), so a release
// where this drifts — the file moved, the mount gone — answers 502 on every
// listing while every probe stays green. This holds the wiring release 1.36.0
// rendered: a regression here is a front end that cannot verify its backend.
func TestTheCallersOfTheURLServiceTrustItsAnswers(t *testing.T) {
	out, err := render(t, componentArgs()...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	const mount = "/var/run/identity"
	workloads := workloadsOf(t, out)

	for _, c := range []string{"web", "stat"} {
		block := tlsOf(t, out, c+".yaml")
		if block == nil {
			t.Fatalf("%s has no tls block while the URL service is strict", c)
		}
		if block["mode"] != "strict" {
			t.Errorf("%s tls.mode = %v, want strict", c, block["mode"])
		}
		for key, file := range map[string]string{"caFile": "ca.crt", "certFile": "tls.crt", "keyFile": "tls.key"} {
			if got, want := block[key], mount+"/"+file; got != want {
				t.Errorf("%s tls.%s = %v, want %s", c, key, got, want)
			}
		}

		deploy := workloads["example-"+c]
		if deploy == nil {
			t.Fatalf("no workload example-%s in %v", c, workloads)
		}
		if volumeNamed(podOf(deploy), "identity") == nil {
			t.Errorf("%s does not carry the identity volume, so tls.caFile names a file nothing mounts", c)
		}
		if !slices.Contains(mountPathsOf(deploy), mount) {
			t.Errorf("%s does not mount the identity at %s: %v", c, mount, mountPathsOf(deploy))
		}
	}
}
