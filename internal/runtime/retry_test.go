package runtime_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/truvity/policy/examples/url-shortener/internal/runtime"
)

var quiet = slog.New(slog.DiscardHandler)

func fast(timeout time.Duration) runtime.Retry {
	return runtime.Retry{Initial: time.Millisecond, Max: 4 * time.Millisecond, Timeout: timeout}
}

func TestRetryConnectsAfterTheDatabaseComesBack(t *testing.T) {
	calls := 0
	err := fast(5*time.Second).Do(context.Background(), quiet, "database", func(context.Context) error {
		calls++
		if calls <= 3 {
			return errors.New("connection refused")
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 4, calls)
}

func TestRetryGivesUpAfterTheWindowAndKeepsTheLastError(t *testing.T) {
	cause := errors.New("connection refused")
	calls := 0
	err := fast(50*time.Millisecond).Do(context.Background(), quiet, "database", func(context.Context) error {
		calls++
		return cause
	})
	require.ErrorIs(t, err, runtime.ErrGaveUp)
	require.ErrorIs(t, err, cause)
	require.Greater(t, calls, 1)
}

func TestRetryGivesUpOnAnAttemptThatHangs(t *testing.T) {
	start := time.Now()
	err := fast(50*time.Millisecond).Do(context.Background(), quiet, "database", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	require.ErrorIs(t, err, runtime.ErrGaveUp)
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestRetryStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := runtime.Retry{Initial: time.Hour, Max: time.Hour, Timeout: time.Hour}.Do(ctx, quiet, "database", func(context.Context) error {
		calls++
		cancel() // SIGTERM arrives while the first wait begins.
		return errors.New("connection refused")
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, runtime.ErrGaveUp)
	require.Equal(t, 1, calls)
}

func TestRetryBacksOffAndCapsTheWait(t *testing.T) {
	var at []time.Time
	r := runtime.Retry{Initial: 20 * time.Millisecond, Max: 40 * time.Millisecond, Timeout: 5 * time.Second}
	_ = r.Do(context.Background(), quiet, "database", func(context.Context) error {
		at = append(at, time.Now())
		if len(at) == 4 {
			return nil
		}
		return errors.New("refused")
	})
	require.Len(t, at, 4)
	// Waits are in [w/2, w] for w = 20, 40, 40 ms.
	require.GreaterOrEqual(t, at[1].Sub(at[0]), 10*time.Millisecond)
	require.GreaterOrEqual(t, at[2].Sub(at[1]), 20*time.Millisecond)
	require.GreaterOrEqual(t, at[3].Sub(at[2]), 20*time.Millisecond)
	require.Less(t, at[3].Sub(at[2]), 500*time.Millisecond)
}

func TestRetryFromEnv(t *testing.T) {
	none := func(string) (string, bool) { return "", false }
	got, err := runtime.RetryFromEnv(none)
	require.NoError(t, err)
	require.Equal(t, runtime.DefaultRetry(), got)

	env := map[string]string{
		runtime.EnvConnectInitial: "1s",
		runtime.EnvConnectMax:     "5s",
		runtime.EnvConnectTimeout: "30s",
	}
	got, err = runtime.RetryFromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.NoError(t, err)
	require.Equal(t, runtime.Retry{Initial: time.Second, Max: 5 * time.Second, Timeout: 30 * time.Second}, got)

	for _, bad := range []string{"soon", "0s", "-1s"} {
		_, err = runtime.RetryFromEnv(func(k string) (string, bool) {
			if k == runtime.EnvConnectTimeout {
				return bad, true
			}
			return "", false
		})
		require.ErrorContains(t, err, runtime.EnvConnectTimeout, bad)
	}
}
