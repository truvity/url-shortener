// Package rollout is the e2e suite's PRE-FLIGHT gate: it waits until an
// application release has actually finished rolling out — to a SPECIFIC
// promoted version, when one is known — before anything is allowed to
// test it.
//
// It is a package of its own, not code living inside
// examples/url-shortener/e2e/suite alongside readiness_test.go, for the
// same reason e2e/traceattrs and e2e/traceauth are: suite's TestMain
// (main_test.go) skips the WHOLE package unless E2E_NAMESPACE is set —
// the right behaviour for a test that needs a cluster, and the wrong one
// for what this package does, which needs neither: WaitForPromoted's own
// decision (which gemaal call to make, how to default a timeout, how to
// wrap an error) is a hermetic claim, provable with a scripted kubectl
// double on every `just check`, cluster or not (rollout_test.go).
//
// The race this closes was seen on a real cluster: the
// url-shortener-e2e chart's own Job is a separate Application a GitOps
// controller can sync while the application release's own Deployments
// are still rolling out. Once the Job's suite starts,
// (*harness.Cluster).WaitForDeployments (`kubectl rollout status`) alone
// is not enough — called before the controller has pushed the promoted
// spec to a Deployment AT ALL, it sees the OLD generation already fully
// rolled out and returns immediately, and the suite goes on to exercise
// (and, in trace_test.go's case, inspect a trace produced by) the
// PREVIOUS version's pods.
package rollout

import (
	"context"
	"fmt"
	"time"

	"github.com/truvity/gemaal/pkg/harness"
)

// VersionLabel is the standard label
// charts/url-shortener/templates/_helpers.tpl's "url-shortener.labels"
// stamps on every pod template with the release's OWN chart version —
// never a Deployment's SELECTOR (see that helper's own doc comment for
// why). WaitForPromoted reads it back, through
// (*harness.Cluster).WaitForDeploymentsAtVersion, to prove a test run is
// judging the version that was actually promoted.
const VersionLabel = "app.kubernetes.io/version"

// safetyMargin is added on top of the harness's own rollout timeout for
// the context this wraps the wait in. The harness's internal deadline
// only fires BETWEEN polls (see (*harness.Cluster).WaitForDeploymentsAtVersion),
// so a single hung kubectl call — no network route, a wedged API server —
// would otherwise block past it; this bounds the whole wait regardless.
const safetyMargin = 30 * time.Second

// WaitForPromoted waits until every Deployment of release, in namespace,
// has finished rolling out — bounded by cluster.RolloutTimeout
// (harness.DefaultRolloutTimeout when unset).
//
// When appVersion is known — the url-shortener-e2e chart's Job always
// sets E2E_APP_VERSION to its own chart's version, which is the
// application chart's too (both release under one version together, see
// .github/workflows/release.yaml) — this proves every live pod of the
// release carries THAT version's VersionLabel AND has finished rolling
// out, via (*harness.Cluster).WaitForDeploymentsAtVersion.
//
// Empty (the outside-in / kind loop, which runs the suite directly
// rather than through the chart's Job, so there is no separately
// promoted version to know) it falls back to the plain rollout-complete
// wait, (*harness.Cluster).WaitForDeployments — still worth doing once,
// up front, rather than leaving every test to assume it.
func WaitForPromoted(ctx context.Context, cluster *harness.Cluster, namespace, release, appVersion string) error {
	timeout := cluster.RolloutTimeout
	if timeout <= 0 {
		timeout = harness.DefaultRolloutTimeout
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout+safetyMargin)
	defer cancel()

	if appVersion == "" {
		return cluster.WaitForDeployments(waitCtx, namespace, release)
	}

	if err := cluster.WaitForDeploymentsAtVersion(waitCtx, namespace, VersionLabel, appVersion, release); err != nil {
		return fmt.Errorf("release %q in %q is not yet rolled out to version %q: %w", release, namespace, appVersion, err)
	}

	return nil
}
