package suite

import (
	"context"
	"encoding/json"
	"fmt"
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
//
// Every request to the release is a record, the readiness probes' included, so
// the test first waits for the archiver to go quiet (longer than its batch
// window), counts what it has archived, makes ONE redirect, and wants one more.
func TestLogArchivesTheRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	prefix := shared.namespace + "/" + shared.release + "/"
	logs := func() (string, error) {
		out, err := kubectl(ctx, "logs", "deployment/"+shared.release+"-log", "--tail=1000").Output()

		return string(out), err
	}

	last, stable := -1, time.Time{}

	eventually(t, 2*time.Minute, func() error {
		out, err := logs()
		if err != nil {
			return err
		}

		n := devel.CountArchived(out, prefix)
		if n != last {
			last, stable = n, time.Now()
		}

		if time.Since(stable) < 8*time.Second {
			return errNotQuiet
		}

		return nil
	})

	baseline := last

	if _, err := createAndRedirect(ctx, t); err != nil {
		t.Fatalf("create and resolve a link for the archiver to record: %v", err)
	}

	eventually(t, 2*time.Minute, func() error {
		out, err := logs()
		if err != nil {
			return err
		}

		if n := devel.CountArchived(out, prefix); n <= baseline {
			return fmt.Errorf("%d archived line(s), still the %d from before the redirect", n, baseline)
		}

		return nil
	})
}

const errNotQuiet = archiveError("the archiver is still archiving")

type archiveError string

func (e archiveError) Error() string { return string(e) }

func createAndRedirect(ctx context.Context, t *testing.T) (string, error) {
	t.Helper()

	key, err := journey.CreateViaWeb(ctx, httpClient(true), shared.base, testLongURL(t))
	if err != nil {
		return "", err
	}

	_ = followRedirect(ctx, t, key)

	return key, nil
}

// dumpLogs prints the tail of each component's log into the test output: read
// only, through the Kubernetes API, for a failure that needs explaining.
func dumpLogs(t *testing.T, components ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	for _, c := range components {
		out, err := kubectl(ctx, "logs", "deployment/"+shared.release+"-"+c, "--tail=40").CombinedOutput()
		t.Logf("---- %s ----\n%s(err: %v)", c, out, err)
	}
}
