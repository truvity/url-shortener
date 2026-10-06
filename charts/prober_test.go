package charts_test

import (
	"strings"
	"testing"

	"github.com/truvity/policy/conformance"
	"github.com/truvity/policy/examples/url-shortener/internal/config"
)

// The always-on prober is a workload of the application chart, off by default
// (the end-to-end suite itself runs from the product's own CI, not from a
// chart). Its tests follow the application chart's own conventions: it follows
// the release's `tls` and `otel`, and its address formula is the release's own
// Service names.

func proberOn(extra ...string) []string {
	return defaults(append([]string{"--set", "prober.enabled=true"}, extra...)...)
}

func TestTheProberIsOffByDefault(t *testing.T) {
	out, err := render(t, defaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	if strings.Contains(out, "component: prober") || strings.Contains(out, "-prober") {
		t.Error("the prober rendered although prober.enabled was left at its default")
	}
}

// The prober's Deployment recreates rather than rolling: a single-replica
// synthetic traffic generator must not leave the old pod running (and probing)
// while a broken new one fails to come up (templates/prober.yaml).
func TestTheProberDeploymentRecreates(t *testing.T) {
	out, err := render(t, proberOn()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	var prober map[string]any
	for _, m := range documents(t, out) {
		if m["kind"] == "Deployment" && docNameOf(m) == "example-prober" {
			prober = m
		}
	}
	if prober == nil {
		t.Fatalf("no prober Deployment:\n%s", out)
	}
	spec, _ := prober["spec"].(map[string]any)
	strategy, _ := spec["strategy"].(map[string]any)
	if got, _ := strategy["type"].(string); got != "Recreate" {
		t.Errorf("the prober's Deployment strategy is %q, want Recreate", got)
	}
	if got := spec["replicas"]; got != 1 && got != float64(1) {
		t.Errorf("the prober runs %v replicas, want 1 whatever availability says", got)
	}
}

func docNameOf(m map[string]any) string {
	meta, _ := m["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	return name
}

// What this chart renders for the prober is validated with the SAME schema the
// prober binary validates against at start-up, so the chart cannot keep
// setting a key the binary stopped reading.
func TestWhatTheProberRendersIsWhatTheProberBinaryAccepts(t *testing.T) {
	for name, extra := range map[string][]string{
		"transport off": nil,
		"transport on": {
			"--set", "tls.mode=permissive", "--set", "tls.components.urls.mode=strict",
			"--set", "tls.trustDomain=example.invalid",
		},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := render(t, proberOn(extra...)...)
			if err != nil {
				t.Fatalf("the chart does not render: %v\n%s", err, out)
			}
			doc := conformance.ConfigMapData(t, []byte(out), "prober.yaml")
			conformance.ValidDocument(t, doc, config.Read("prober.json"))
		})
	}
}

// The prober carries the instance label, as every workload of the chart does.
func TestTheProberCarriesTheInstanceLabel(t *testing.T) {
	out, err := render(t, proberOn()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	for _, m := range documents(t, out) {
		if m["kind"] != "Deployment" || docNameOf(m) != "example-prober" {
			continue
		}
		meta, _ := m["metadata"].(map[string]any)
		labels, _ := meta["labels"].(map[string]any)
		if got, _ := labels["app.kubernetes.io/instance"].(string); got != "example" {
			t.Errorf("the prober's Deployment carries instance label %q, want the release name", got)
		}
		return
	}
	t.Fatal("no prober Deployment")
}

// With the transport off (the default) the prober carries no trace of it.
func TestProberTransportOffLeavesNoTrace(t *testing.T) {
	out, err := render(t, proberOn()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	for _, doc := range strings.Split(out, "\n---\n") {
		if !strings.Contains(doc, "example-prober") {
			continue
		}
		doc = withoutComments(doc)
		for _, trace := range []string{"csi.cert-manager.io", "certificaterequests", "trustDomain", "identity", "tls:", "serviceAccountName"} {
			if strings.Contains(doc, trace) {
				t.Errorf("a prober object mentions %q with tls.mode off:\n%s", trace, doc)
			}
		}
	}
}

// withoutComments drops the YAML comment lines a template carries into its
// render, so a word in an explanation is not mistaken for a rendered field.
func withoutComments(doc string) string {
	var kept []string
	for _, line := range strings.Split(doc, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// Turned on, the prober gets its own account (distinct from every
// component's), the CSI volume, permission to ask for a certificate, and an
// allow-list of exactly the two components it dials.
func TestProberTransportOnRendersIdentity(t *testing.T) {
	out, err := render(t, proberOn(
		"--set", "tls.mode=permissive",
		"--set", "tls.components.urls.mode=strict",
		"--set", "tls.trustDomain=example.invalid")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	if !strings.Contains(out, "serviceAccountName: example-prober") {
		t.Error("the prober does not run as its own account")
	}
	if !serviceAccountsContain(t, out, "example-prober") {
		t.Error("the prober's account is not rendered")
	}
	if !strings.Contains(out, "name: example-prober\n    namespace:") {
		t.Error("the prober's account may not ask for its identity: it is not a subject of the request-identity binding")
	}

	cfg := string(conformance.ConfigMapData(t, []byte(out), "prober.yaml"))
	for _, want := range []string{
		"trustDomain: example.invalid",
		"serviceAccount: example-urls",
		"serviceAccount: example-redirect",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("the prober's configuration is missing %q:\n%s", want, cfg)
		}
	}
}

func serviceAccountsContain(t *testing.T, out, name string) bool {
	t.Helper()
	for _, m := range documents(t, out) {
		if m["kind"] == "ServiceAccount" && docNameOf(m) == name {
			return true
		}
	}
	return false
}

// `tls.grantRequest: false` leaves the permission to the platform; the account
// itself is still rendered, because the CSI mount needs one to derive an
// identity from.
func TestProberTransportGrantRequestIsOptional(t *testing.T) {
	out, err := render(t, proberOn(
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
	if !serviceAccountsContain(t, out, "example-prober") {
		t.Error("the prober's account was not rendered although tls.mode is on")
	}
}

// Both targets are dialled on THEIR port: with urls strict and redirect
// permissive the prober must not assume they match.
func TestProberDialsEachTargetOnItsOwnPort(t *testing.T) {
	out, err := render(t, proberOn(
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
}

// The prober's account must not be one of the components'.
func TestTheProberNeedsAnAccountOfItsOwn(t *testing.T) {
	out, err := render(t, proberOn("--set", "prober.serviceAccount.name=example-urls")...)
	if err == nil {
		t.Fatalf("a prober sharing urls's account was accepted:\n%s", out)
	}
	if !strings.Contains(out, "the prober needs an account of its own") {
		t.Errorf("the refusal does not say why: %s", out)
	}
}
