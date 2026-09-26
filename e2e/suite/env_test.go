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
	//
	// Set to the EMPTY STRING (present in the environment, not merely
	// unset — see kubecontextFromEnv) to run with no `--context` at all:
	// that is what an in-cluster verification Job wants, so `kubectl` and
	// the harness fall back to the Pod's own ServiceAccount rather than a
	// kubeconfig context that does not exist there.
	envKubecontext = "E2E_KCTX"

	// envTracesURL is a Jaeger-API query endpoint. Unset (the kind box
	// carries no trace store, and a verification Job may have no route to
	// one either) skips the one trace-shaped test — see trace_test.go.
	envTracesURL = "E2E_TRACES_URL"

	// envS3Endpoint and envS3Region override where log_test.go's archive
	// check reaches the object store. Unset, it falls back to the kind
	// box's own S3 stand-in (object-store/s3) — which does not exist
	// outside kind, so TestLogArchivesTheRecord skips cleanly there unless
	// this is set. Set it to a real S3(-compatible) endpoint to run that
	// check elsewhere; credentials then come from the process's own
	// default AWS credential chain (the Pod's IAM identity), never the
	// kind box's static test credentials — see log_test.go.
	envS3Endpoint = "E2E_S3_ENDPOINT"
	envS3Region   = "E2E_S3_REGION"

	defaultKubecontext = "kind-policy"
	defaultS3Region    = "us-east-1"
)

// env is everything the suite resolved once, in TestMain, and every test
// reads from — the tenant's names (from the fixture's own resolver) and the
// cluster this run reaches Services through. The database test's role
// passwords are NOT resolved here: see db_test.go's appPasswordOrSkip for
// why that has to be lazy.
type env struct {
	cluster *harness.Cluster
	names   fixture.Names

	tracesURL string
}

// getenv reads name, or def when unset or blank.
func getenv(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// kubecontextFromEnv reports the kubeconfig context to run with.
//
// This is NOT getenv: a verification Job needs to set envKubecontext to the
// empty string ON PURPOSE, to get no `--context` flag at all rather than
// this package's kind-box default — and an env var present with an empty
// value is exactly that, distinguishable from the var being absent only by
// os.LookupEnv (os.Getenv, and therefore getenv, collapse both to "").
func kubecontextFromEnv() string {
	if v, ok := os.LookupEnv(envKubecontext); ok {
		return strings.TrimSpace(v)
	}
	return defaultKubecontext
}

// namespaceFromEnv reports the namespace to run against, and whether the
// suite should run at all — envNamespace is the switch.
func namespaceFromEnv() (string, bool) {
	ns := strings.TrimSpace(os.Getenv(envNamespace))
	return ns, ns != ""
}

// verificationHookMode reports whether this run IS the chart's own
// post-install/post-upgrade verification hook (templates/verification.yaml),
// running in-cluster rather than from a laptop or CI runner against the kind
// box or a caller's own kubeconfig context.
//
// The hook Job sets envKubecontext to the empty string ON PURPOSE — see
// kubecontextFromEnv — which is otherwise a shape nothing else produces: a
// human or a CI runner either leaves it unset (getting this package's kind
// default) or points it at a real context, never at "set, but empty". That
// same signal is also this suite's only way to know it must not shell out to
// `helm`: the hook Job runs the e2e image, which carries no helm binary (see
// e2e/Dockerfile) and no copy of the charts' embedded source the way this
// checkout does, so fixture.Resolve — which renders url-shortener-infra to
// read back a real install's database, role and secret names — cannot run
// there. Those names are also the PLATFORM's own values in the first place
// (postgres.database, postgres.ownerRole, postgres.runtimeRole,
// postgres.runtimePasswordSecret in charts/url-shortener-infra/values.yaml,
// each a `--set` on the install this Job was never told), so there would be
// nothing trustworthy to resolve even with helm on PATH.
//
// resolveEnv skips fixture.Resolve entirely in this mode; db_test.go and
// log_test.go's archive check notice the resulting zero-value names and skip
// cleanly instead of asserting a name nobody gave them.
func verificationHookMode() bool {
	v, ok := os.LookupEnv(envKubecontext)
	return ok && strings.TrimSpace(v) == ""
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

// resolveEnv resolves everything a test needs UNCONDITIONALLY: the
// fixture's names (read off the charts, exactly as apply.sh and install.sh
// read them — see fixture.Resolve's doc comment for why that, and not a
// copy of the convention, is what this suite depends on) and the cluster to
// reach Services through.
//
// It does NOT read the role passwords apply.sh generated into Secrets —
// that used to happen here, unconditionally, which meant a caller with no
// permission to read Secrets (a verification Job scoped to exactly what the
// suite's other tests need) failed EVERY test in this package before any of
// them ran. db_test.go's appPasswordOrSkip resolves that password lazily,
// inside the one test that needs it, so a missing permission skips that
// test alone.
//
// In verificationHookMode it skips fixture.Resolve altogether, for the same
// reason and the same way: that call needs `helm` (absent from the e2e
// image) and the platform's own install-time values (never handed to this
// Job) to mean anything, so running it here traded one whole-binary failure
// (a Secret this account cannot read) for another (a binary this image does
// not carry) — see verificationHookMode's doc comment. The returned Names
// carries only what this Job WAS given — its namespace and release, and the
// archive bucket's default or E2E_BUCKET override, neither of which the
// infra chart's render decides — leaving every chart-derived field at its
// zero value for the tests that need one to skip on.
func resolveEnv(_ context.Context, namespace string) (env, error) {
	// ctx is unused today: fixture.Resolve takes none, and nothing else
	// here shells out any more (see the doc comment above). Kept in the
	// signature — and in resolveEnvWithTimeout's bound around it — as a
	// guard against a future helm invocation hanging with nothing to
	// cancel it.
	d := fixture.DefaultOptions()
	appRelease := getenv(envAppRelease, d.AppRelease)
	bucket := getenv(envBucket, d.Bucket)

	var names fixture.Names
	if verificationHookMode() {
		names = fixture.Names{
			Options: fixture.Options{
				Namespace:  namespace,
				AppRelease: appRelease,
				Bucket:     bucket,
			},
		}
	} else {
		var err error
		names, err = fixture.Resolve(fixture.Options{
			Namespace:  namespace,
			AppRelease: appRelease,
			Bucket:     bucket,
		})
		if err != nil {
			return env{}, fmt.Errorf("resolve the fixture's names: %w", err)
		}
	}

	cluster := &harness.Cluster{Kubecontext: kubecontextFromEnv()}

	return env{
		cluster:   cluster,
		names:     names,
		tracesURL: strings.TrimSpace(os.Getenv(envTracesURL)),
	}, nil
}

// secretPassword reads a basic-auth Secret's password field. This is
// configuration, read the same way examples/url-shortener/e2e/fixture/apply.sh
// itself reads it back — not a probe of the product, which is why it is
// kubectl rather than a Service: nothing in this example serves its own
// credentials over a Service, on purpose.
//
// kubecontext == "" omits `--context` entirely, matching the harness's own
// helm/kubectl invocations (see helm.go) — a fixed "" argument would ask
// kubectl for a context literally named "", which fails everywhere,
// in-cluster included.
func secretPassword(ctx context.Context, kubecontext, namespace, secret string) (string, error) {
	argv := []string{"-n", namespace, "get", "secret", secret, "-o", "jsonpath={.data.password}"}
	if kubecontext != "" {
		argv = append([]string{"--context", kubecontext}, argv...)
	}

	out, err := exec.CommandContext(ctx, "kubectl", argv...).Output() //nolint:gosec // fixed argv, no shell
	if err != nil {
		return "", fmt.Errorf("kubectl get secret %s/%s: %w", namespace, secret, err)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return "", fmt.Errorf("secret %s/%s: password is not valid base64: %w", namespace, secret, err)
	}

	return string(decoded), nil
}
