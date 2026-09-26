package suite

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"
)

// shared is resolved once, in TestMain, and read by every test — never
// mutated after that point, so tests may run in any order or in parallel.
var shared env

// TestMain is the ONE gate: unless E2E_NAMESPACE is set, this package runs
// no tests and touches no network, so `go test ./...` — and therefore
// `just test` — stays hermetic (docs/guides/testing.md, "Traps"). Set it,
// and this binary is the same one meant to run against the kind box today,
// a private repository's shared cluster, or a cluster after a promotion —
// only the environment differs.
func TestMain(m *testing.M) {
	namespace, run := namespaceFromEnv()
	if !run {
		fmt.Fprintf(os.Stderr, "suite: %s is not set — skipping the cluster suite\n", envNamespace)
		os.Exit(0)
	}

	resolved, err := resolveEnvWithTimeout(namespace, 30*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "suite: %v\n", err)
		os.Exit(1)
	}
	shared = resolved

	code := m.Run()

	shared.cluster.CloseForwards()

	os.Exit(code)
}

// testDataPrefix marks every long URL and key this suite invents, so a
// person reading the archive bucket or the urls table by hand can tell this
// suite's rows from a real deployment's at a glance.
const testDataPrefix = "e2e-suite"

// randomSuffix draws a short, readable suffix for one test's data — long
// enough that two runs against the SAME standing tenant (a private
// repository's shared cluster, where nothing tears the install down between
// runs) never collide on the URL identity Create dedupes by.
func randomSuffix(t *testing.T) string {
	t.Helper()

	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, 10)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			t.Fatalf("draw a random suffix: %v", err)
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out)
}

// testLongURL is a long URL unique to this test, carrying testDataPrefix so
// it is recognisable wherever it ends up — the urls table, the archive
// bucket, a trace.
func testLongURL(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("https://example.com/%s/%s", testDataPrefix, randomSuffix(t))
}

// eventually polls check, bounded by patience, and fails the test with the
// last error once patience runs out — the shape every asynchronous
// assertion in this suite shares: the redirect answers and is done, so what
// it triggers (the counter, the archive) is proved by waiting, not by
// asserting once.
func eventually(t *testing.T, patience time.Duration, check func() error) {
	t.Helper()

	deadline := time.Now().Add(patience)
	var last error
	for {
		if err := check(); err == nil {
			return
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not become true within %s: %v", patience, last)
		}
		time.Sleep(time.Second)
	}
}
