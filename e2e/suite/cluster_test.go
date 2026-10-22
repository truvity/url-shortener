package suite

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/truvity/url-shortener/e2e/devel"
	"github.com/truvity/url-shortener/e2e/journey"
)

func expectGate() bool { return strings.TrimSpace(os.Getenv("E2E_EXPECT_GATE")) == "true" }

// kubectl is the Kubernetes API, the suite's first door: read only here
// (rollout status and pod logs), as `devel@oidc`.
func kubectl(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "kubectl", append([]string{"--context", devel.Kubecontext, "-n", shared.namespace}, args...)...) //nolint:gosec // fixed argv
}

// TestDeploymentsAreReady asserts every Deployment this release owns -- urls,
// redirect, web, stat and log -- is Available with every replica ready.
//
// Not a public-host probe: a Pod is marked Ready only after its own readiness
// probe has passed, and this is the ONLY test that covers stat and log at
// all: neither has a Service or a route.
func TestDeploymentsAreReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	out, err := kubectl(ctx, "get", "deployments", "-l", "app.kubernetes.io/instance="+shared.release, "-o", "json").Output()
	if err != nil {
		t.Fatalf("list the release's Deployments: %v", err)
	}

	var list struct {
		Items []struct {
			Metadata struct{ Name string }  `json:"metadata"`
			Spec     struct{ Replicas int } `json:"spec"`
			Status   struct {
				ReadyReplicas     int `json:"readyReplicas"`
				AvailableReplicas int `json:"availableReplicas"`
				UpdatedReplicas   int `json:"updatedReplicas"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal(out, &list); err != nil {
		t.Fatalf("parse the Deployments: %v", err)
	}

	have := map[string]bool{}

	for _, d := range list.Items {
		have[d.Metadata.Name] = true

		if d.Status.ReadyReplicas != d.Spec.Replicas || d.Status.AvailableReplicas != d.Spec.Replicas || d.Status.UpdatedReplicas != d.Spec.Replicas {
			t.Errorf("%s: %d/%d ready, %d available, %d updated", d.Metadata.Name, d.Status.ReadyReplicas, d.Spec.Replicas, d.Status.AvailableReplicas, d.Status.UpdatedReplicas)
		}
	}

	for _, component := range []string{"urls", "redirect", "web", "stat", "log"} {
		if name := shared.release + "-" + component; !have[name] {
			t.Errorf("no Deployment %q for component %q", name, component)
		}
	}
}

// TestLogArchivesTheRecord proves the fourth language does its part: the
// archiver read the SAME rendered configuration as everything else, bound a
// durable consumer on the request subject, and wrote an object to the bucket
// within its batch window. The bucket is not a door the suite has, so it asks
// the archiver itself: its log says `archived` with the object's key (under
// this release's prefix) and the number of records, which it writes only
// after the put succeeded.
func TestLogArchivesTheRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	started := time.Now()

	if _, err := createAndRedirect(ctx, t); err != nil {
		t.Fatalf("create and resolve a link for the archiver to record: %v", err)
	}

	eventually(t, 120*time.Second, func() error {
		out, err := kubectl(ctx, "logs", "deployment/"+shared.release+"-log", "--since-time="+started.UTC().Format(time.RFC3339), "--tail=500").Output()
		if err != nil {
			return err
		}

		return devel.ArchivedLine(string(out), shared.namespace+"/"+shared.release+"/")
	})
}

func createAndRedirect(ctx context.Context, t *testing.T) (string, error) {
	t.Helper()

	key, err := journey.CreateViaWeb(ctx, httpClient(true), shared.base, testLongURL(t))
	if err != nil {
		return "", err
	}

	_ = followRedirect(ctx, t, key)

	return key, nil
}
