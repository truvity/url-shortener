// Package suite is the ONE end-to-end suite for the url-shortener example.
//
// It replaces hack/smoke.sh with the same claim, proved the same way — every
// assertion goes through a Service endpoint, never a `kubectl exec` into a
// Pod — but as Go tests, so the same binary that runs here today runs
// unchanged wherever this example is next installed: a private repository's
// shared cluster, or a cluster after a promotion. Only the environment
// changes; see docs/guides/testing.md.
//
// It is inert unless E2E_NAMESPACE is set, so `go test ./...` — and
// therefore `just test` — never touches a network or a cluster. Names come
// from the environment and from examples/url-shortener/e2e/fixture's own
// resolver, never repeated here by hand: a chart that renames a role, a
// stream or a bucket changes what this suite asks for the next time it
// runs, with nothing here to edit.
package suite

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/truvity/gemaal/pkg/harness"

	"github.com/truvity/policy/examples/url-shortener/e2e/fixture"
)

const (
	// envNamespace is the ONE variable that turns the suite on. Unset, every
	// test in this package is skipped — see TestMain.
	envNamespace = "E2E_NAMESPACE"

	// envAppRelease and envBucket override the fixture's own defaults, for a
	// caller installing the example under a different release or bucket
	// name than examples/url-shortener/hack/install.sh does.
	envAppRelease = "E2E_APP_RELEASE"
	envBucket     = "E2E_BUCKET"

	// envKubecontext targets a kubeconfig context. Defaulted to the local
	// box's own convention rather than "whatever is current" — see
	// hack/smoke.sh's identical KCTX default and the comment beside it: a
	// second kind cluster in another terminal must never silently redirect
	// this suite.
	envKubecontext = "E2E_KCTX"

	// envTracesURL is a Jaeger-API query endpoint. Unset (the kind box
	// carries no trace store) skips the one trace-shaped test — see
	// trace_test.go.
	envTracesURL = "E2E_TRACES_URL"

	defaultKubecontext = "kind-policy"
)

// env is everything the suite resolved once, in TestMain, and every test
// reads from — the tenant's names (from the fixture's own resolver), the
// cluster this run reaches Services through, and the credentials the
// database test needs.
type env struct {
	cluster *harness.Cluster
	names   fixture.Names

	ownerPassword string
	appPassword   string

	tracesURL string
}

// getenv reads name, or def when unset or blank.
func getenv(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// namespaceFromEnv reports the namespace to run against, and whether the
// suite should run at all — envNamespace is the switch.
func namespaceFromEnv() (string, bool) {
	ns := strings.TrimSpace(os.Getenv(envNamespace))
	return ns, ns != ""
}

// resolveEnvWithTimeout is resolveEnv bounded by its own context, kept out
// of TestMain itself: `defer cancel()` beside an os.Exit on the error path
// never runs (gocritic's exitAfterDefer), so the context this needs lives
// and dies entirely inside this function instead.
func resolveEnvWithTimeout(namespace string, timeout time.Duration) (env, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return resolveEnv(ctx, namespace)
}

// resolveEnv resolves everything a test needs: the fixture's names (read
// off the charts, exactly as apply.sh and install.sh read them — see
// fixture.Resolve's doc comment for why that, and not a copy of the
// convention, is what this suite depends on) and the two role passwords
// apply.sh generated into Secrets, which this package has no other way to
// learn.
func resolveEnv(ctx context.Context, namespace string) (env, error) {
	d := fixture.DefaultOptions()

	names, err := fixture.Resolve(fixture.Options{
		Namespace:  namespace,
		AppRelease: getenv(envAppRelease, d.AppRelease),
		Bucket:     getenv(envBucket, d.Bucket),
	})
	if err != nil {
		return env{}, fmt.Errorf("resolve the fixture's names: %w", err)
	}

	kctx := getenv(envKubecontext, defaultKubecontext)
	cluster := &harness.Cluster{Kubecontext: kctx}

	ownerPassword, err := secretPassword(ctx, kctx, names.Namespace, names.OwnerSecret)
	if err != nil {
		return env{}, fmt.Errorf("the owner role's password (%s): %w", names.OwnerSecret, err)
	}
	appPassword, err := secretPassword(ctx, kctx, names.Namespace, names.AppSecret)
	if err != nil {
		return env{}, fmt.Errorf("the app role's password (%s): %w", names.AppSecret, err)
	}

	return env{
		cluster:       cluster,
		names:         names,
		ownerPassword: ownerPassword,
		appPassword:   appPassword,
		tracesURL:     strings.TrimSpace(os.Getenv(envTracesURL)),
	}, nil
}

// secretPassword reads a basic-auth Secret's password field. This is
// configuration, read the same way examples/url-shortener/e2e/fixture/apply.sh
// itself reads it back — not a probe of the product, which is why it is
// kubectl rather than a Service: nothing in this example serves its own
// credentials over a Service, on purpose.
func secretPassword(ctx context.Context, kubecontext, namespace, secret string) (string, error) {
	out, err := exec.CommandContext(ctx, "kubectl", //nolint:gosec // fixed argv, no shell
		"--context", kubecontext, "-n", namespace,
		"get", "secret", secret, "-o", "jsonpath={.data.password}",
	).Output()
	if err != nil {
		return "", fmt.Errorf("kubectl get secret %s/%s: %w", namespace, secret, err)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return "", fmt.Errorf("secret %s/%s: password is not valid base64: %w", namespace, secret, err)
	}

	return string(decoded), nil
}
