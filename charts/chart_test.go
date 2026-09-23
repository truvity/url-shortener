package charts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// A rollout must not have a gap, and the three numbers that make that true
// must agree. This is the only place it can be checked: it is a property of
// what is rendered, not of what runs.
func TestARolloutHasNoGap(t *testing.T) {
	out, err := render(t, defaults("--set", "image.tag=dev")...)
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
	out, err := render(t, defaults("--set", "image.tag=dev",
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
	out, err := render(t, defaults("--set", "image.tag=dev",
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
	out, err := render(t, defaults("--set", "image.tag=dev")...)
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
	out, err := render(t, defaults("--set", "image.tag=dev")...)
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
	out, err := render(t, defaults("--set", "image.tag=dev")...)
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
	out, err := render(t, defaults("--set", "image.tag=dev")...)
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
	out, err := render(t, defaults("--set", "image.tag=dev",
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
func TestTransportOnWithoutATrustDomainIsRefused(t *testing.T) {
	out, err := render(t, defaults("--set", "image.tag=dev", "--set", "tls.mode=strict")...)
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
	out, err := render(t, defaults("--set", "image.tag=dev",
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
