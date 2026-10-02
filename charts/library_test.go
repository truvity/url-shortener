package charts_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/truvity/policy/conformance"
	"github.com/truvity/policy/examples/url-shortener/charts"
)

// The library chart, and the convention it carries (decision 0009, rules C15
// to C17 of docs/contracts/component.md): a service chart's values are
// `platform` and `config`, the library renders everything from them, and the
// configuration file is `config` verbatim.
//
// Two charts are held to it here. `testdata/service-example` is the minimal
// one that follows the convention exactly, so the rules are tested where they
// can be seen. url-shortener is a product chart that uses the library for
// everything but the mapping of its release-wide values onto each component,
// so what it is held to is what survives that mapping: its ports are its
// files', its checksum is its file's, its environment is declared.

const exampleChart = "testdata/service-example"

// exampleRender renders the example chart as release "r", with each values
// file (a path under testdata/) given in order.
func exampleRender(t *testing.T, values ...string) (string, error) {
	t.Helper()

	args := []string{"template", "r", chartDir(t, exampleChart), "--namespace", "ns"}
	for _, v := range values {
		args = append(args, "-f", filepath.Join("testdata", v))
	}
	out, err := helm(args...)

	return out, err
}

// exampleRenderYAML is exampleRender with the values given inline.
func exampleRenderYAML(t *testing.T, values string) (string, error) {
	t.Helper()

	f := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(f, []byte(values), 0o644); err != nil {
		t.Fatal(err)
	}

	return helm("template", "r", chartDir(t, exampleChart), "--namespace", "ns", "-f", f)
}

// effectiveConfig is what Helm hands the chart as `.Values.config`: the
// chart's own values.yaml with each values file merged over it, maps merging
// key by key and everything else replaced.
func effectiveConfig(t *testing.T, values ...string) map[string]any {
	t.Helper()

	read := func(p string) map[string]any {
		raw, err := charts.Files.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		unmarshalYAML(t, raw, &doc)
		cfg, _ := doc["config"].(map[string]any)

		return cfg
	}

	var merge func(dst, src map[string]any) map[string]any
	merge = func(dst, src map[string]any) map[string]any {
		for k, v := range src {
			sm, sok := v.(map[string]any)
			dm, dok := dst[k].(map[string]any)
			if sok && dok {
				dst[k] = merge(dm, sm)
			} else {
				dst[k] = v
			}
		}

		return dst
	}

	cfg := read(exampleChart + "/values.yaml")
	for _, v := range values {
		raw, err := os.ReadFile(filepath.Join("testdata", v))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		unmarshalYAML(t, raw, &doc)
		if over, ok := doc["config"].(map[string]any); ok {
			cfg = merge(cfg, over)
		}
	}

	return cfg
}

// The library renders nothing of its own and says so: a `type: library` chart
// is not installable, and the one the application chart packages is the one
// that is released.
func TestTheLibraryIsALibrary(t *testing.T) {
	raw, err := charts.Files.ReadFile("service-lib/Chart.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Type    string `yaml:"type"`
		Version string `yaml:"version"`
	}
	unmarshalYAML(t, raw, &meta)
	if meta.Type != "library" {
		t.Errorf("service-lib is type %q, want library", meta.Type)
	}
	if meta.Version != "0.0.0" {
		t.Errorf("service-lib's committed version is %q, want the placeholder 0.0.0 a release stamps (C1)", meta.Version)
	}

	if out, err := helm("template", "x", chartDir(t, "service-lib")); err == nil {
		t.Errorf("a library chart rendered something:\n%s", out)
	}
}

// vendoredFiles reads every file under dir in the embedded charts, by its path below dir.
func vendoredFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	got := map[string]string{}
	err := fs.WalkDir(charts.Files, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := charts.Files.ReadFile(p)
		got[strings.TrimPrefix(p, dir+"/")] = string(b)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// The application chart carries a COPY of the library under charts/, because
// `helmctl package` does not run `helm dependency update`: a library that only
// arrived at package time would be published without it. A copy is a second
// version of the truth, so a test holds it to the first.
//
// `just vendor-charts` refreshes the copy.
func TestTheVendoredLibraryIsTheLibrary(t *testing.T) {
	source := vendoredFiles(t, "service-lib")
	vendored := vendoredFiles(t, "url-shortener/charts/service-lib")

	if len(source) == 0 {
		t.Fatal("service-lib is empty")
	}
	for name, want := range source {
		got, ok := vendored[name]
		switch {
		case !ok:
			t.Errorf("url-shortener/charts/service-lib lacks %s: run `just vendor-charts`", name)
		case got != want:
			t.Errorf("url-shortener/charts/service-lib/%s differs from service-lib/%s: run `just vendor-charts`", name, name)
		}
	}
	for name := range vendored {
		if _, ok := source[name]; !ok {
			t.Errorf("url-shortener/charts/service-lib has %s, which service-lib does not: run `just vendor-charts`", name)
		}
	}
}

// C16, C15: a chart that follows the convention renders its configuration
// verbatim, derives every port from it, restarts on a change to it, and
// carries only the environment it declares — in the default render and in one
// that sets every platform field.
func TestAFollowingChartRendersItsConfigurationVerbatim(t *testing.T) {
	for name, values := range map[string][]string{
		"defaults":   nil,
		"everything": {"service-example-everything.yaml"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := exampleRender(t, values...)
			if err != nil {
				t.Fatalf("the chart does not render: %v\n%s", err, out)
			}
			config := effectiveConfig(t, values...)

			// The file the binary is given is the one it is validated
			// against, with the service's own schema.
			doc := conformance.ConfigMapData(t, []byte(out), "echo.yaml")
			schema, err := charts.Files.ReadFile(exampleChart + "/echo.schema.json")
			if err != nil {
				t.Fatal(err)
			}
			conformance.ValidDocument(t, doc, schema)

			conformance.ConfigMapEqualsConfig(t, []byte(out), "r-echo-config", "echo.yaml", config)
			conformance.PortsEqualConfig(t, []byte(out), "r-echo", config)
			conformance.ChecksumFollowsConfig(t, []byte(out), "r-echo", "r-echo-config", "echo.yaml")

			var secrets, allowed []string
			if name == "everything" {
				secrets = []string{"STORE_TOKEN"}
				allowed = []string{"CONFIG_FILE", "EXAMPLE_CLIENT_HOST"}
			}
			conformance.EnvIsDeclared(t, []byte(out), "r-echo", secrets, allowed...)
		})
	}
}

// What the library derives from the platform half, one rule at a time. Each
// value in service-example-everything.yaml has to arrive somewhere, or the
// library has quietly stopped reading it.
func TestEveryPlatformFieldIsReadSomewhere(t *testing.T) {
	out, err := exampleRender(t, "service-example-everything.yaml")
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	deploy := find(t, out, "Deployment", "r-echo")
	pod := podOf(deploy)
	container := pod["containers"].([]any)[0].(map[string]any)

	for _, tc := range []struct {
		what      string
		got, want any
	}{
		{"replicas", dig(deploy, "spec", "replicas"), 3},
		{"maxUnavailable", dig(deploy, "spec", "strategy", "rollingUpdate", "maxUnavailable"), 1},
		{"maxSurge", dig(deploy, "spec", "strategy", "rollingUpdate", "maxSurge"), 2},
		{"image", container["image"], "example.io/example/echo-custom@sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		{"imagePullPolicy", container["imagePullPolicy"], "Always"},
		{"serviceAccountName", pod["serviceAccountName"], "echo-account"},
		{"service account annotation", dig(find(t, out, "ServiceAccount", "echo-account"), "metadata", "annotations", "example.invalid/role"), "echo"},
		{"runAsUser", dig(pod, "securityContext", "runAsUser"), 1234},
		{"fsGroup", dig(pod, "securityContext", "fsGroup"), 9012},
		{"memory limit", dig(container, "resources", "limits", "memory"), "222Mi"},
		{"liveness path", dig(container, "livenessProbe", "httpGet", "path"), "/live"},
		{"liveness period", dig(container, "livenessProbe", "periodSeconds"), 7},
		{"liveness failures", dig(container, "livenessProbe", "failureThreshold"), 4},
		{"readiness path", dig(container, "readinessProbe", "httpGet", "path"), "/ready"},
		{"readiness timeout", dig(container, "readinessProbe", "timeoutSeconds"), 2},
		{"startup path", dig(container, "startupProbe", "httpGet", "path"), "/ready"},
		{"startup failures", dig(container, "startupProbe", "failureThreshold"), 30},
		{"preStop delay", dig(container, "lifecycle", "preStop", "sleep", "seconds"), 7},
		// The drain is ONE number: the file's own 33, the pre-stop delay of 7
		// and a margin of 5.
		{"termination grace", pod["terminationGracePeriodSeconds"], 45},
		{"identity driver", dig(volumeNamed(pod, "identity"), "csi", "driver"), "example.csi.invalid"},
		{"extra volume", dig(volumeNamed(pod, "extra"), "configMap", "name"), "example-extra"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s is %v, want %v", tc.what, tc.got, tc.want)
		}
	}

	// The path reaches the process in the variable the platform named, and
	// there is no argument beside it (decision 0002: ONE of the two).
	if args := container["args"]; args != nil {
		t.Errorf("platform.config.pathEnv is set and the container still has args %v", args)
	}
	env := telemetryEnvOf(t, deploy)
	if env["CONFIG_FILE"] != "/etc/echo/echo.yaml" {
		t.Errorf("CONFIG_FILE is %q, want the mounted file", env["CONFIG_FILE"])
	}
	// The telemetry variables, all from platform.telemetry.
	for k, want := range map[string]string{
		"OTEL_SERVICE_NAME":           "echo-service",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector.example.invalid:4318",
		"OTEL_TRACES_SAMPLER_ARG":     "0.25",
		"OTEL_RESOURCE_ATTRIBUTES":    "deployment.example.tier=primary",
		"OTEL_LOGS_EXPORTER":          "none",
	} {
		if env[k] != want {
			t.Errorf("%s is %q, want %q", k, env[k], want)
		}
	}
	// The identity is mounted where the configuration's own files are.
	for _, p := range mountPathsOf(deploy) {
		if p == "/var/run/example-identity" {
			return
		}
	}
	t.Errorf("the identity is not mounted at platform.tls.mountPath: %v", mountPathsOf(deploy))
}

func volumeNamed(pod map[string]any, name string) map[string]any {
	volumes, _ := pod["volumes"].([]any)
	for _, v := range volumes {
		if m, _ := v.(map[string]any); m["name"] == name {
			return m
		}
	}

	return nil
}

// Strict has ONE listener, the service's own, speaking TLS: no second port,
// and the identity mounted. Off mounts nothing and a platform can still ask
// for the mount.
func TestTheTransportModeDecidesThePortsAndTheMounts(t *testing.T) {
	cases := map[string]struct {
		values                      string
		wantIdentity, wantHTTPSPort bool
	}{
		"off": {"", false, false},
		"strict": {`config:
  tls:
    mode: strict
    certFile: /var/run/identity/tls.crt
    keyFile: /var/run/identity/tls.key
    caFile: /var/run/identity/ca.crt
    trustDomain: example.invalid
platform:
  tls: {csiDriver: example.csi.invalid}
`, true, false},
		"permissive": {`config:
  tls:
    mode: permissive
    address: ":9443"
    certFile: /var/run/identity/tls.crt
    keyFile: /var/run/identity/tls.key
    caFile: /var/run/identity/ca.crt
    trustDomain: example.invalid
platform:
  tls: {csiDriver: example.csi.invalid}
`, true, true},
		"mounted by the platform alone": {`platform:
  tls: {csiDriver: example.csi.invalid, mount: true}
`, true, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := exampleRenderYAML(t, tc.values)
			if err != nil {
				t.Fatalf("does not render: %v\n%s", err, out)
			}
			var overlay struct{ Config map[string]any }
			unmarshalYAML(t, []byte(tc.values), &overlay)
			config := effectiveConfigFrom(t, overlay.Config)
			conformance.PortsEqualConfig(t, []byte(out), "r-echo", config)
			conformance.ConfigMapEqualsConfig(t, []byte(out), "r-echo-config", "echo.yaml", config)

			deploy := find(t, out, "Deployment", "r-echo")
			if got := volumeNamed(podOf(deploy), "identity") != nil; got != tc.wantIdentity {
				t.Errorf("the identity is mounted: %v, want %v", got, tc.wantIdentity)
			}
			_, https := ports(deploy)["https"]
			if https != tc.wantHTTPSPort {
				t.Errorf("an https port exists: %v, want %v", https, tc.wantHTTPSPort)
			}
		})
	}
}

func effectiveConfigFrom(t *testing.T, over map[string]any) map[string]any {
	t.Helper()

	cfg := effectiveConfig(t)
	for k, v := range over {
		cfg[k] = v
	}

	return cfg
}

func find(t *testing.T, out, kind, name string) map[string]any {
	t.Helper()

	for _, d := range documents(t, out) {
		if got, _ := dig(d, "metadata", "name").(string); d["kind"] == kind && got == name {
			return d
		}
	}
	t.Fatalf("no %s %s in the render", kind, name)

	return nil
}

func ports(deploy map[string]any) map[string]int {
	got := map[string]int{}
	containers, _ := podOf(deploy)["containers"].([]any)
	list, _ := containers[0].(map[string]any)["ports"].([]any)
	for _, p := range list {
		m, _ := p.(map[string]any)
		got[m["name"].(string)], _ = m["containerPort"].(int)
	}

	return got
}

// The refusals. Each is a way to disagree with the file that the library turns
// into a render error, or the schema into a values error, instead of a pod that
// starts and does something nobody chose.
func TestTheConventionRefusesWhatWouldDisagreeWithTheFile(t *testing.T) {
	for name, tc := range map[string]struct{ values, want string }{
		"an identity file outside the mount": {`config:
  tls:
    mode: strict
    certFile: /elsewhere/tls.crt
    keyFile: /var/run/identity/tls.key
    caFile: /var/run/identity/ca.crt
    trustDomain: example.invalid
platform:
  tls: {csiDriver: example.csi.invalid}
`, "not under platform.tls.mountPath"},
		"an identity with no driver": {`config:
  tls:
    mode: strict
    certFile: /var/run/identity/tls.crt
    keyFile: /var/run/identity/tls.key
    caFile: /var/run/identity/ca.crt
    trustDomain: example.invalid
`, "platform.tls.csiDriver is required"},
		"permissive with no second address": {`config:
  tls:
    mode: permissive
    certFile: /var/run/identity/tls.crt
    keyFile: /var/run/identity/tls.key
    caFile: /var/run/identity/ca.crt
    trustDomain: example.invalid
platform:
  tls: {csiDriver: example.csi.invalid}
`, "config.tls.address"},
		"the default account":             {"platform: {serviceAccount: {name: default}}\n", `"default" is refused`},
		"a secret with no key":            {"platform: {secrets: {TOKEN: {secretName: s}}}\n", "key"},
		"a key the service does not know": {"config: {greting: hello}\n", "greting"},
		"a platform key nobody reads":     {"platform: {replcas: 2}\n", "replcas"},
		"a negative replica count":        {"platform: {replicas: -1}\n", "replicas"},
		"a configuration with no probes":  {"config: {probes: null}\n", "probes"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := exampleRenderYAML(t, tc.values)
			if err == nil {
				t.Fatalf("the render was accepted:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not say what is wrong (want %q): %s", tc.want, out)
			}
		})
	}
}

// What the example renders, byte for byte, so that a change to the library
// shows up in a review as a diff in what a chart that uses it renders. Update
// with `just golden` after reading the diff.
func TestWhatTheServiceExampleChartRenders(t *testing.T) {
	for name, values := range map[string][]string{
		"service-example":            nil,
		"service-example-everything": {"service-example-everything.yaml"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := exampleRender(t, values...)
			if err != nil {
				t.Fatalf("the chart does not render: %v\n%s", err, out)
			}

			golden := filepath.Join("testdata", "golden", name+".yaml")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Skip("golden updated")
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v — run `just golden` to create it", err)
			}
			if string(want) != out {
				t.Errorf("the render moved. Read the diff, then `just golden`:\n%s", firstDifference(string(want), out))
			}
		})
	}
}

// C17: every chart that commits a values.schema.src.json commits the schema it
// composes to — the platform schema, and `config` as the service's own — and
// not one edited by hand.
func TestEveryCommittedChartSchemaIsTheComposedOne(t *testing.T) {
	var found int
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "values.schema.src.json" {
			return err
		}
		found++
		dir := filepath.Dir(p)
		conformance.ChartSchemaIsComposed(t, os.DirFS(dir), "values.schema.src.json", "values.schema.json")

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("no chart has a values.schema.src.json, so the rule was not checked")
	}
}

// helm runs helm and returns what it printed, errors included.
func helm(args ...string) (string, error) {
	out, err := exec.Command("helm", args...).CombinedOutput()

	return string(out), err
}

// A Service for a process that listens on nothing routes to nothing, and the
// library refuses to render one. The example chart's schema already requires a
// listener, so this is asked of a chart with no schema at all: the library's
// own guard, not the schema's.
func TestAServiceIsRefusedForAProcessThatListensOnNothing(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Chart.yaml", "apiVersion: v2\nname: bare\nversion: 0.0.0\nappVersion: 0.0.0\n")
	write("values.yaml", `images: {job: {repository: example/job}}
platform: {service: {enabled: true}}
config: {probes: {address: ":7070"}}
`)
	write("templates/x.yaml", `{{ include "service-lib.service" (dict "context" $ "name" "job" "platform" .Values.platform "config" .Values.config) }}`)
	if err := os.CopyFS(filepath.Join(dir, "charts", "service-lib"), os.DirFS(chartDir(t, "service-lib"))); err != nil {
		t.Fatal(err)
	}

	out, err := helm("template", "r", dir)
	if err == nil || !strings.Contains(out, "listens on nothing") {
		t.Errorf("a Service for a process with no listener was not refused: %v\n%s", err, out)
	}
}

// Zero is a value. `default` would turn a delay of zero into the default delay,
// a surge of zero into one and no replicas into one: each of which the old
// chart rendered as asked.
func TestZeroIsNotMistakenForUnset(t *testing.T) {
	out, err := exampleRenderYAML(t, `platform:
  replicas: 0
  drain: {preStopSeconds: 0}
  strategy: {maxUnavailable: 1, maxSurge: 0}
`)
	if err != nil {
		t.Fatalf("does not render: %v\n%s", err, out)
	}
	deploy := find(t, out, "Deployment", "r-echo")
	container := podOf(deploy)["containers"].([]any)[0]
	for what, tc := range map[string]struct{ got, want any }{
		"replicas":         {dig(deploy, "spec", "replicas"), 0},
		"preStop delay":    {dig(container, "lifecycle", "preStop", "sleep", "seconds"), 0},
		"maxSurge":         {dig(deploy, "spec", "strategy", "rollingUpdate", "maxSurge"), 0},
		"maxUnavailable":   {dig(deploy, "spec", "strategy", "rollingUpdate", "maxUnavailable"), 1},
		"termination wait": {dig(podOf(deploy), "terminationGracePeriodSeconds"), 25},
	} {
		if tc.got != tc.want {
			t.Errorf("%s is %v, want %v", what, tc.got, tc.want)
		}
	}
}

// url-shortener is a product chart: it maps release-wide values onto each of
// its components, so its configuration is built rather than passed through
// (docs/guides/charts.md). What it is still held to is what survives that
// mapping, and what the library derives from the file it was handed: every
// component's ports are its own file's, its pods restart when its file
// changes, and it carries no environment it did not declare. Rendered with
// every optional thing off, on, and in between.
func TestEveryUrlShortenerComponentFollowsItsOwnFile(t *testing.T) {
	// The environment the platform's PostgreSQL client reads; nothing else is
	// allowed beside telemetry and the declared secrets.
	client := []string{
		"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGSSLMODE", "PGSSLROOTCERT",
		"CNPG_CLIENT_PASSWORD_FILE", "PGAPPNAME", "CNPG_CLIENT_POOL_MAX",
	}
	for _, values := range []string{"minimal", "everything", "per-component"} {
		t.Run(values, func(t *testing.T) {
			out, err := render(t, "-f", filepath.Join("testdata", values+".yaml"))
			if err != nil {
				t.Fatalf("the chart does not render: %v\n%s", err, out)
			}
			for _, component := range []string{"redirect", "urls", "web", "stat", "log"} {
				var config map[string]any
				unmarshalYAML(t, conformance.ConfigMapData(t, []byte(out), component+".yaml"), &config)

				workload, configMap := "example-"+component, "example-"+component+"-config"
				conformance.PortsEqualConfig(t, []byte(out), workload, config)
				conformance.ChecksumFollowsConfig(t, []byte(out), workload, configMap, component+".yaml")

				var secrets, allowed []string
				if component == "log" && values == "everything" {
					secrets = []string{"S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"}
				}
				if component == "redirect" || component == "urls" {
					allowed = client
				}
				conformance.EnvIsDeclared(t, []byte(out), workload, secrets, allowed...)
			}
		})
	}
}
