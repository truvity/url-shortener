package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"
)

// The environment variables that tune the start-up retry. Durations are Go
// duration strings ("500ms", "10s", "3m"). Unset means the default.
const (
	EnvConnectInitial = "DATABASE_CONNECT_INITIAL_BACKOFF"
	EnvConnectMax     = "DATABASE_CONNECT_MAX_BACKOFF"
	EnvConnectTimeout = "DATABASE_CONNECT_TIMEOUT"
)

// Retry is how long, and how patiently, a process waits for a dependency
// that is not there yet.
//
// A database that restarts for a minute is routine: a planned failover, a
// configuration reload, a node drain. A service that exits on the first
// refused connection turns that minute into a restart loop with a growing
// back-off of its own, so the outage outlasts the cause.
type Retry struct {
	// Initial is the first wait; each later wait doubles, up to Max.
	Initial time.Duration
	// Max caps a single wait.
	Max time.Duration
	// Timeout is how long to keep trying in all. After it the last error is
	// returned and the process exits: a database that is gone for minutes is
	// a fault for the orchestrator to show, not one to hide forever.
	Timeout time.Duration
}

// DefaultRetry is 0.5 s doubling to 10 s, for up to 3 minutes.
func DefaultRetry() Retry {
	return Retry{Initial: 500 * time.Millisecond, Max: 10 * time.Second, Timeout: 3 * time.Minute}
}

// RetryFromEnv is DefaultRetry with whatever the environment overrides.
// A value that does not parse, or is not positive, is an error naming the
// variable: a typo must not quietly become "wait forever" or "do not wait".
func RetryFromEnv(lookup func(string) (string, bool)) (Retry, error) {
	r := DefaultRetry()
	for _, f := range []struct {
		name string
		into *time.Duration
	}{
		{EnvConnectInitial, &r.Initial},
		{EnvConnectMax, &r.Max},
		{EnvConnectTimeout, &r.Timeout},
	} {
		raw, ok := lookup(f.name)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		d, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return Retry{}, fmt.Errorf("%s: %w", f.name, err)
		}
		if d <= 0 {
			return Retry{}, fmt.Errorf("%s: %q must be positive", f.name, raw)
		}
		*f.into = d
	}
	if r.Max < r.Initial {
		r.Max = r.Initial
	}
	return r, nil
}

// ErrGaveUp wraps the last error when the retry window closed.
var ErrGaveUp = errors.New("gave up waiting")

// Do calls dial until it succeeds, the window closes, or ctx is cancelled.
//
// Every failed attempt is logged at WARN with the attempt number and the wait
// that follows. The wait is the back-off with full jitter in its upper half
// (between half and all of it), so replicas restarted together do not
// reconnect in step.
//
// dial must honour the context it is given: it is cancelled when the window
// closes, so an attempt that hangs cannot outlive it.
//
// When ctx is cancelled (SIGTERM during the wait) the result is ctx's error
// and the caller should exit without treating it as a failure. When the
// window closes the result wraps both ErrGaveUp and the last dial error.
func (r Retry) Do(ctx context.Context, log *slog.Logger, what string, dial func(context.Context) error) error {
	window, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	wait := r.Initial
	for attempt := 1; ; attempt++ {
		err := dial(window)
		if err == nil {
			if attempt > 1 {
				log.InfoContext(ctx, "connected", slog.String("what", what), slog.Int("attempt", attempt))
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if window.Err() != nil {
			return fmt.Errorf("%w for %s after %d attempts (%s): %w", ErrGaveUp, what, attempt, r.Timeout, err)
		}

		// Between half and all of the back-off.
		sleep := wait/2 + rand.N(wait/2+1)
		log.WarnContext(ctx, "not reachable, will retry",
			slog.String("what", what),
			slog.Int("attempt", attempt),
			slog.Duration("retry_in", sleep),
			slog.String("error", err.Error()))

		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-window.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%w for %s after %d attempts (%s): %w", ErrGaveUp, what, attempt, r.Timeout, err)
		case <-timer.C:
		}
		wait = min(wait*2, r.Max)
	}
}
