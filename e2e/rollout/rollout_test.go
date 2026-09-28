package rollout

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/truvity/gemaal/pkg/harness"
)

// stubKubectl is a minimal harness.Runner double: it answers "kubectl get
// deployments" (the release listing), "kubectl get deployment NAME" (a
// sequence of JSON snapshots, one per poll — an in-progress rollout
// observed, then a finished one, or a rollout stuck the whole time) and
// "kubectl rollout status deployment/NAME" (the fallback path with no
// version to prove).
type stubKubectl struct {
	listOut     string
	getOuts     []string
	getCalls    int
	rolloutErr  error
	rolloutName string
}

func (s *stubKubectl) Run(_ context.Context, argv ...string) error {
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "rollout status") {
		s.rolloutName = joined

		return s.rolloutErr
	}

	return nil
}

func (s *stubKubectl) Output(_ context.Context, argv ...string) (string, error) {
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "get deployments") {
		return s.listOut, nil
	}

	// "get deployment <name> -n <ns> -o json": walk the scripted
	// sequence, holding on the last entry once exhausted — the same
	// shape a rollout stuck at one state forever needs.
	i := s.getCalls
	if i >= len(s.getOuts) {
		i = len(s.getOuts) - 1
	}
	s.getCalls++

	return s.getOuts[i], nil
}

// deploymentJSON renders one Deployment's `kubectl get -o json` — the
// exact fields (*harness.Cluster).WaitForDeploymentsAtVersion polls,
// mirroring gemaal's own pkg/harness kube_test.go fixtures.
func deploymentJSON(generation int, version string, replicas, statusReplicas, updated, available int) string {
	return fmt.Sprintf(`{
		"metadata": {"generation": %d},
		"spec": {"replicas": %d, "template": {"metadata": {"labels": {"app.kubernetes.io/version": %q}}}},
		"status": {"observedGeneration": %d, "replicas": %d, "updatedReplicas": %d, "availableReplicas": %d}
	}`, generation, replicas, version, generation, statusReplicas, updated, available)
}

const oneDeploymentList = `{"items": [
	{"metadata": {"name": "example-stat", "annotations": {"meta.helm.sh/release-name": "example"}}}
]}`

func testCluster(runner harness.Runner) *harness.Cluster {
	return &harness.Cluster{
		Runner:              runner,
		RolloutTimeout:      50 * time.Millisecond,
		RolloutPollInterval: time.Millisecond,
	}
}

func TestWaitForPromoted(t *testing.T) {
	t.Run("version known: in-progress then complete", func(t *testing.T) {
		s := &stubKubectl{
			listOut: oneDeploymentList,
			getOuts: []string{
				// The pod template already names v2 (the controller
				// pushed it), but the rollout has not finished: one old
				// pod is still around, one new one is not yet available.
				deploymentJSON(2, "v2", 2, 3, 1, 1),
				// Now fully rolled out.
				deploymentJSON(2, "v2", 2, 2, 2, 2),
			},
		}

		err := WaitForPromoted(context.Background(), testCluster(s), "emp-jdoe", "example", "v2")
		if err != nil {
			t.Fatalf("WaitForPromoted: %v", err)
		}
	})

	t.Run("version known: stuck on the old version times out naming the release", func(t *testing.T) {
		s := &stubKubectl{
			listOut: oneDeploymentList,
			// The Deployment's own spec never moves to v2 — e.g. a
			// GitOps controller has not yet applied the promoted app
			// release when this polls. This is the exact race found on a
			// real cluster: the test-chart Job started before the
			// application release's Deployments were updated at all, so
			// a plain rollout-complete wait would have seen the OLD
			// generation already fully rolled out and returned
			// immediately.
			getOuts: []string{deploymentJSON(1, "v1", 2, 2, 2, 2)},
		}

		err := WaitForPromoted(context.Background(), testCluster(s), "emp-jdoe", "example", "v2")
		if err == nil {
			t.Fatal("expected an error; the Deployment never reached v2")
		}
		for _, want := range []string{"example", "emp-jdoe", `"v2"`} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err.Error(), want)
			}
		}
	})

	t.Run("version unknown: falls back to the plain rollout-complete wait", func(t *testing.T) {
		s := &stubKubectl{listOut: oneDeploymentList}

		err := WaitForPromoted(context.Background(), testCluster(s), "emp-jdoe", "example", "")
		if err != nil {
			t.Fatalf("WaitForPromoted: %v", err)
		}
		if !strings.Contains(s.rolloutName, "rollout status deployment/example-stat") {
			t.Errorf("expected a plain `kubectl rollout status` call, got %q", s.rolloutName)
		}
	})

	t.Run("version unknown: a stuck rollout still times out naming the deployment", func(t *testing.T) {
		s := &stubKubectl{
			listOut:    oneDeploymentList,
			rolloutErr: fmt.Errorf("timed out waiting for the condition"),
		}

		err := WaitForPromoted(context.Background(), testCluster(s), "emp-jdoe", "example", "")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "example-stat") {
			t.Errorf("error %q does not name the deployment", err.Error())
		}
	})

	t.Run("cluster.RolloutTimeout unset falls back to harness.DefaultRolloutTimeout", func(t *testing.T) {
		s := &stubKubectl{
			listOut: oneDeploymentList,
			getOuts: []string{deploymentJSON(1, "v1", 1, 1, 1, 1)},
		}
		c := &harness.Cluster{Runner: s} // RolloutTimeout and RolloutPollInterval left at zero

		err := WaitForPromoted(context.Background(), c, "emp-jdoe", "example", "v1")
		if err != nil {
			t.Fatalf("WaitForPromoted: %v", err)
		}
	})
}
