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
	// that is what an in-cluster Job wants (the url-shortener-e2e chart's
	// own Job always sets it this way), so `kubectl` and the harness fall
	// back to the Pod's own ServiceAccount rather than a kubeconfig
	// context that does not exist there.
	envKubecontext = "E2E_KCTX"

	// envTracesURL is a Jaeger-API query endpoint. Unset (the kind box
	// carries no trace store, and an in-cluster Job may have no route to
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

	// envS3AccessKeyID and envS3SecretAccessKey carry STATIC credentials
	// for envS3Endpoint, set beside it — see log_test.go's s3ClientOrSkip
	// doc comment for why: an endpoint with no ambient identity behind it
	// (the kind box's own S3 stand-in, reached at a real address but with
	// nothing an IMDS-shaped credential chain can discover) still needs
	// something to authenticate with. Both unset (the common case, and
	// every case before this pair existed) falls back to the default AWS
	// credential chain, unchanged.
	envS3AccessKeyID     = "E2E_S3_ACCESS_KEY_ID"
	envS3SecretAccessKey = "E2E_S3_SECRET_ACCESS_KEY"

	// envNamesFromEnv switches the suite to its SECOND source of names,
	// beside fixture.Resolve (kind, via helm): every name the suite
	// needs, read from its own E2E_* variable rather than rendered from a
	// chart. This is what the url-shortener-e2e chart's Job sets — see
	// its own templates/job.yaml doc comment for why it carries no copy
	// of charts/url-shortener-infra to render (no `helm` in the e2e
	// image) — and it runs EVERY case, never skipping one for a missing
	// name. Any value turns it on; unlike envKubecontext, an empty one is
	// not itself a distinct signal — see namesFromEnvMode.
	envNamesFromEnv = "E2E_NAMES_FROM_ENV"

	// The rest of fixture.Names, one variable per field the
	// url-shortener-e2e chart's Job can supply — see namesFromEnv and
	// fixture.Names' own doc comment for what each one is.
	envDatabase        = "E2E_DATABASE"
	envDatabaseHost    = "E2E_DATABASE_HOST"
	envOwnerRole       = "E2E_OWNER_ROLE"
	envAppRole         = "E2E_APP_ROLE"
	envAppSecret       = "E2E_APP_SECRET"
	envStream          = "E2E_STREAM"
	envRedirectSubject = "E2E_REDIRECT_SUBJECT"
	envRequestSubject  = "E2E_REQUEST_SUBJECT"
	envStatConsumer    = "E2E_STAT_CONSUMER"
	envLogConsumer     = "E2E_LOG_CONSUMER"

	// envAppPassword carries the runtime role's password ITSELF, not the
	// name of the Secret holding it — see db_test.go's appPasswordOrSkip.
	// The url-shortener-e2e chart's Job reads it from that Secret via
	// `secretKeyRef`, which needs no RBAC grant at all (the value is
	// resolved when the Pod is admitted, never read back by this Pod's
	// own ServiceAccount token) — unlike `kubectl get secret`, which is
	// what a caller with no Secret permission is exactly meant to skip on
	// instead — see db_test.go's appPasswordOrSkip.
	envAppPassword = "E2E_APP_PASSWORD"

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
// This is NOT getenv: an in-cluster Job (the url-shortener-e2e chart's own)
// needs to set envKubecontext to the empty string ON PURPOSE, to get no
// `--context` flag at all rather than this package's kind-box default — and
// an env var present with an empty value is exactly that, distinguishable
// from the var being absent only by os.LookupEnv (os.Getenv, and therefore
// getenv, collapse both to "").
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

// namesFromEnvMode reports whether this run should build fixture.Names
// entirely from environment variables — the url-shortener-e2e chart's Job
// — rather than rendering charts/url-shortener-infra with helm (the kind
// flow) — see envNamesFromEnv's doc comment.
func namesFromEnvMode() bool {
	return strings.TrimSpace(os.Getenv(envNamesFromEnv)) != ""
}

// namesFromEnv builds fixture.Names entirely from this Job's own
// environment: every name fixture.Resolve would otherwise have rendered
// from charts/url-shortener-infra arrives here as a value the
// url-shortener-e2e chart's Job was given instead, one environment
// variable per field.
//
// OwnerSecret is left at its zero value: nothing in this package ever
// reads it — only examples/url-shortener/e2e/fixture/apply.sh, which this
// Job never runs, connects as the owner role at all — so there is no
// variable for it to come from.
func namesFromEnv(namespace, appRelease, bucket string) fixture.Names {
	return fixture.Names{
		Options: fixture.Options{
			Namespace:  namespace,
			AppRelease: appRelease,
			Bucket:     bucket,
		},
		Database:        os.Getenv(envDatabase),
		OwnerRole:       os.Getenv(envOwnerRole),
		AppRole:         os.Getenv(envAppRole),
		DatabaseHost:    os.Getenv(envDatabaseHost),
		AppSecret:       os.Getenv(envAppSecret),
		Stream:          os.Getenv(envStream),
		RedirectSubject: os.Getenv(envRedirectSubject),
		RequestSubject:  os.Getenv(envRequestSubject),
		StatConsumer:    os.Getenv(envStatConsumer),
		LogConsumer:     os.Getenv(envLogConsumer),
	}
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
// permission to read Secrets failed EVERY test in this package before any
// of them ran. db_test.go's appPasswordOrSkip resolves that password
// lazily, inside the one test that needs it, so a missing permission skips
// that test alone.
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
	switch {
	case namesFromEnvMode():
		names = namesFromEnv(namespace, appRelease, bucket)
	default:
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
