package suite

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/truvity/policy/examples/url-shortener/e2e/rollout"
)

// TestDeploymentsAreReady asserts every Deployment this release owns —
// urls, redirect, web, stat and log — is Available with every replica
// ready.
//
// Not a Service probe: a Pod is only marked Ready after the kubelet's own
// readinessProbe has already passed (the chart wires that up, and the
// platform enforces it — see charts/url-shortener/templates/*.yaml's
// livenessProbe/readinessProbe blocks), so asking the SAME question again
// through a Service would prove nothing new. It would also need widening
// who can reach the probe listener — see the git history of
// charts/url-shortener/templates/{urls,redirect,web}.yaml, where that port
// was added to and then removed from each Service for exactly this reason.
// This is also the ONLY test in this suite that covers stat and log at
// all: neither carries a Service (see charts/url-shortener/templates/
// {stat,log}.yaml), so nothing else here has a way to name them.
//
// Reached through the harness's own kubectl runner
// ((*Cluster).ReleaseDeployments and WaitForDeployments), never a Service:
// a Deployment's readiness is not something a Service call can answer.
func TestDeploymentsAreReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	names, err := shared.cluster.ReleaseDeployments(ctx, shared.names.Namespace, shared.names.AppRelease)
	if err != nil {
		t.Fatalf("list the release's Deployments: %v", err)
	}

	for _, component := range []string{componentURLs, componentRedirect, componentWeb, componentStat, componentLog} {
		want := service(component)
		if !slices.Contains(names, want) {
			t.Errorf("no Deployment %q for component %q — the release %q in %q is missing it",
				want, component, shared.names.AppRelease, shared.names.Namespace)
		}
	}
	if t.Failed() {
		return
	}

	// WaitForDeployments is `kubectl rollout status` per Deployment: it
	// only returns nil once every one of them has every replica updated,
	// available and ready — belt-and-suspenders after helm --wait, and the
	// whole story for a suite that reused a standing install rather than
	// deploying it itself. TestMain's own waitForPromotedRollout already
	// proved this (and, when the promoted version is known, proved it
	// against THAT version specifically) before this test — or any other
	// in this package — ran at all, so this is a second, cheap
	// confirmation rather than the suite's only line of defense against a
	// rollout still in progress.
	if err := shared.cluster.WaitForDeployments(ctx, shared.names.Namespace, shared.names.AppRelease); err != nil {
		t.Fatalf("not every Deployment of release %q in %q is ready: %v", shared.names.AppRelease, shared.names.Namespace, err)
	}
}

// waitForPromotedRollout is the suite's PRE-FLIGHT gate: called once from
// TestMain, before m.Run(), so that no test — whichever one Go happens to
// run first, which this package does not control — can observe a release
// still catching up to what was promoted.
//
// The decision itself (which of gemaal's two waits to call, how to default
// a timeout, how to wrap an error) lives in e2e/rollout, a package of its
// own: this package's TestMain (main_test.go) skips every test here unless
// E2E_NAMESPACE names a real cluster, which is the wrong gate for logic
// that needs neither — see rollout's own package doc comment, and
// e2e/traceattrs / e2e/traceauth for the same reasoning applied to this
// suite's other cluster-independent pieces. rollout_test.go, in that
// package, is where this is actually unit-tested.
func waitForPromotedRollout(ctx context.Context, e env) error {
	return rollout.WaitForPromoted(ctx, e.cluster, e.names.Namespace, e.names.AppRelease, e.appVersion)
}
