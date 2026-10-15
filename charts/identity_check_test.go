package charts_test

import (
	"strconv"
	"strings"
	"testing"
)

// The identity check (decision 0013), seen from the application chart.
//
// A pod admitted before the cluster's admission webhook can see its Pod
// Identity association runs without credentials for its whole life. The
// check is a hook Job, as the component's own account, that fails until a
// pod gets them; each retry is a new pod. These tests hold the shape that
// makes it work.

// primaryApp is a primary install of the application chart: the identity
// account is the infrastructure chart's.
func primaryApp(extra ...string) []string {
	return defaults(append([]string{
		"--set", "tier=primary",
		"--set", "serviceAccount.app.name=archiver",
	}, extra...)...)
}

func identityCheckJobs(t *testing.T, out string) []map[string]any {
	t.Helper()
	var jobs []map[string]any
	for _, doc := range documents(t, out) {
		if doc["kind"] != "Job" {
			continue
		}
		meta, _ := doc["metadata"].(map[string]any)
		if name, _ := meta["name"].(string); strings.HasSuffix(name, "-identity-check") {
			jobs = append(jobs, doc)
		}
	}
	return jobs
}

func at(t *testing.T, v any, path ...string) any {
	t.Helper()
	for _, key := range path {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("no %q on the way to %v", key, path)
		}
		v = m[key]
	}
	return v
}

// The account that carries a cloud identity is accompanied by the check.
//
// On a primary install the account is the infrastructure chart's, which is
// installed and healthy first, so a hook can name it; the application chart
// renders the check, as that account, and never the account.
func TestACloudIdentityAccountIsAccompaniedByTheIdentityCheck(t *testing.T) {
	infra, err := renderInfra(t, infraDefaults(
		"--set", "tier=primary",
		"--set", "cloud.bucket=b", "--set", "cloud.iamName=i", "--set", "cloud.clusterName=c",
		"--set", "cloud.accountID=acct", "--set", "cloud.region=r", "--set", "cloud.serviceAccount=archiver",
	)...)
	if err != nil {
		t.Fatalf("the infrastructure chart does not render: %v\n%s", err, infra)
	}
	account := at(t, docOfKind(t, infra, "ServiceAccount"), "metadata", "name")

	app, err := render(t, primaryApp()...)
	if err != nil {
		t.Fatalf("the application chart does not render: %v\n%s", err, app)
	}

	for _, doc := range documents(t, app) {
		if doc["kind"] == "ServiceAccount" && at(t, doc, "metadata", "name") == account {
			t.Errorf("the application chart renders the account %v the association binds; the infrastructure chart's is the only one, so that no pod is admitted before it", account)
		}
	}

	jobs := identityCheckJobs(t, app)
	if len(jobs) != 1 {
		t.Fatalf("a primary install renders %d identity checks, want one for the one component with a cloud identity:\n%s", len(jobs), app)
	}
	job := jobs[0]

	if got := at(t, job, "spec", "template", "spec", "serviceAccountName"); got != account {
		t.Errorf("the check runs as %v, want the account the association binds (%v)", got, account)
	}

	// Ordered before the release by the mechanism all three install paths
	// share: Helm runs it, Argo CD maps it to PreSync.
	ann := at(t, job, "metadata", "annotations").(map[string]any)
	if ann["helm.sh/hook"] != "pre-install,pre-upgrade" {
		t.Errorf("helm.sh/hook is %v; the check must run before the release's workloads, on install and on upgrade", ann["helm.sh/hook"])
	}
	if ann["helm.sh/hook-delete-policy"] != "before-hook-creation" {
		t.Errorf("hook-delete-policy is %v, want before-hook-creation", ann["helm.sh/hook-delete-policy"])
	}
	if _, ok := ann["helm.sh/hook-weight"]; !ok {
		t.Error("the check has no hook weight, so it is unordered against the migration")
	}

	// Each retry must be a NEW pod: credentials are injected at admission, so
	// a container restarted in the same pod would fail the same way forever.
	if got := at(t, job, "spec", "template", "spec", "restartPolicy"); got != "Never" {
		t.Errorf("restartPolicy is %v; anything but Never retries inside the pod that was admitted without credentials", got)
	}
	spec := at(t, job, "spec").(map[string]any)
	if spec["backoffLimit"] != 6 && spec["backoffLimit"] != float64(6) {
		t.Errorf("backoffLimit is %v, want 6", spec["backoffLimit"])
	}
	if spec["activeDeadlineSeconds"] != 300 && spec["activeDeadlineSeconds"] != float64(300) {
		t.Errorf("activeDeadlineSeconds is %v, want 300", spec["activeDeadlineSeconds"])
	}
}

// Nothing waits for an association on a test install, so nothing is checked.
func TestATestInstallRendersNoIdentityCheck(t *testing.T) {
	out, err := render(t, defaults(
		"--set", "serviceAccount.app.name=archiver",
		`--set`, `serviceAccount.app.annotations.example\.io/role=archive`,
	)...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}
	if jobs := identityCheckJobs(t, out); len(jobs) != 0 {
		t.Errorf("a test install renders an identity check; the account would be created by the same release, after a hook that needs it:\n%s", out)
	}
}

// Only the component with a cloud identity is checked.
func TestOnlyTheCloudIdentityComponentIsChecked(t *testing.T) {
	out, err := render(t, primaryApp()...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}
	for _, job := range identityCheckJobs(t, out) {
		if got := at(t, job, "metadata", "labels", "app.kubernetes.io/component"); got != "log-identity-check" {
			t.Errorf("an identity check for component %v, which has no cloud identity", got)
		}
	}
}

// The infrastructure chart orders what the check depends on: the role, then
// the association, then the account the application chart only names.
func TestTheInfraChartOrdersRoleAssociationAccount(t *testing.T) {
	out, err := renderInfra(t, infraDefaults(
		"--set", "tier=primary",
		"--set", "cloud.bucket=b", "--set", "cloud.iamName=i", "--set", "cloud.clusterName=c",
		"--set", "cloud.accountID=acct", "--set", "cloud.region=r", "--set", "cloud.serviceAccount=a",
	)...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	// Helm sorts a render by kind, so the order that holds on every install
	// path is the sync wave, not the position in the output.
	wave := func(kind string) int {
		raw, _ := at(t, docOfKind(t, out, kind), "metadata", "annotations", "argocd.argoproj.io/sync-wave").(string)
		n, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("the %s has no numeric sync wave: %q", kind, raw)
		}
		return n
	}
	role, association, account := wave("Role"), wave("PodIdentityAssociation"), wave("ServiceAccount")
	if role >= association || association >= account {
		t.Errorf("the Role is wave %d, the association %d and the account %d; each must come strictly after the one it depends on", role, association, account)
	}
}

// No selector of the chart matches the check's pod.
//
// A Job pod with no readiness probe is Ready while it runs, so a Service that
// selected it would route real traffic to a pod that serves nothing, and a
// PodDisruptionBudget that matched it would skew the disruption arithmetic.
func TestNoSelectorMatchesTheIdentityCheckPod(t *testing.T) {
	out, err := render(t, primaryApp("--set", "availability=high")...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	jobs := identityCheckJobs(t, out)
	if len(jobs) != 1 {
		t.Fatalf("want one identity check, got %d", len(jobs))
	}
	labels, _ := at(t, jobs[0], "spec", "template", "metadata", "labels").(map[string]any)
	if len(labels) == 0 {
		t.Fatal("the check's pod has no labels")
	}

	matches := func(selector any) bool {
		sel, _ := selector.(map[string]any)
		if len(sel) == 0 {
			return false // an empty selector is a different bug, not this one
		}
		for k, v := range sel {
			if labels[k] != v {
				return false
			}
		}
		return true
	}

	var checked int
	for _, doc := range documents(t, out) {
		var selector any
		switch doc["kind"] {
		case "Service":
			selector = at(t, doc, "spec", "selector")
		case "PodDisruptionBudget", "Deployment":
			selector = at(t, doc, "spec", "selector", "matchLabels")
		default:
			continue
		}
		checked++
		if matches(selector) {
			t.Errorf("%v %v selects the identity check's pod (%v)", doc["kind"], at(t, doc, "metadata", "name"), labels)
		}
	}
	if checked == 0 {
		t.Fatal("no Service, budget or Deployment was rendered, so no selector was checked")
	}
}
