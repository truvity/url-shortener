// Command prober is always-on synthetic traffic for the url-shortener
// example: walk the same journeys a real caller does — create a short
// link, resolve it, watch its counter move — in a loop, against a release
// nobody is otherwise calling.
//
// It exists because a fresh install with no traffic always looks green. The
// Kargo gate a bake window is judged against needs steady signal before,
// during and after a rollout, and this is that signal — deliberately a
// SEPARATE workload from examples/url-shortener/e2e/suite's Job: the suite
// proves a release IS healthy, once, and exits; this proves it STAYS
// healthy, for as long as it runs. See docs/guides/testing.md.
//
// The journeys themselves are not reimplemented here. They are the exact
// same requests examples/url-shortener/e2e/suite makes, factored into
// examples/url-shortener/e2e/journey so the two callers cannot drift on
// what a journey even is.
//
// Like every binary here, the whole graph is constructed in main, in order,
// with plain constructors — there is no container to ask.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	policytelemetry "github.com/truvity/policy/telemetry"

	"github.com/truvity/policy/examples/url-shortener/e2e/journey"
	"github.com/truvity/policy/examples/url-shortener/internal/config"
	"github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1/urlshortenerv1connect"
	"github.com/truvity/policy/examples/url-shortener/internal/runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("config", os.Getenv("CONFIG_FILE"), "path to the configuration file")
	flag.Parse()
	if *path == "" {
		return errors.New("no configuration file: pass -config or set CONFIG_FILE")
	}

	cfg, err := config.LoadProber(*path)
	if err != nil {
		return err
	}

	interval, err := time.ParseDuration(cfg.Interval)
	if err != nil {
		return fmt.Errorf("interval %q: %w", cfg.Interval, err)
	}

	log := runtime.Logger(cfg.Log.Level)
	version, commit := runtime.Version()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Telemetry, from OpenTelemetry's own environment (decision 0006), on
	// exactly the same terms as every other Go component here. With no
	// endpoint configured this installs exporters that do nothing, so kind
	// — which carries no collector — and a real cluster run the same code
	// down the same path.
	shutdownTelemetry, err := policytelemetry.Start(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// A flush of its own: the context above is already cancelled by
		// the time this runs, and a flush on a cancelled context sends
		// nothing — which would lose exactly the last interval's spans.
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(flush); err != nil {
			log.WarnContext(ctx, "telemetry did not flush", slog.String("error", err.Error()))
		}
	}()

	meter := otel.Meter("github.com/truvity/policy/examples/url-shortener/e2e/cmd/prober")
	journeys, err := meter.Int64Counter("probe.journey",
		metric.WithDescription("Synthetic-traffic journeys this prober ran, by outcome. A Prometheus reader sees this as probe_journey_total."),
		metric.WithUnit("{journey}"))
	if err != nil {
		return fmt.Errorf("build the journey counter: %w", err)
	}
	duration, err := meter.Float64Histogram("probe.journey.duration",
		metric.WithDescription("How long one journey took, success or failure. A Prometheus reader sees this as probe_journey_duration_seconds."),
		metric.WithUnit("s"))
	if err != nil {
		return fmt.Errorf("build the journey duration histogram: %w", err)
	}

	log.InfoContext(ctx, "starting", slog.String("component", "prober"),
		slog.String("version", version), slog.String("commit", commit),
		slog.String("interval", interval.String()))

	// --- what this process talks to ---
	//
	// Plain HTTP, no identity: this prober calls the SAME two Services the
	// e2e suite calls, over the SAME kind of client — see
	// examples/url-shortener/e2e/suite/client_test.go and redirect_test.go.
	// A platform that turns on transport identity for the release turns it
	// on for this workload too, by the same chart-wide switch; there is no
	// second copy of that decision here to keep in step with it.
	urlsClient := urlshortenerv1connect.NewUrlsServiceClient(&http.Client{Timeout: 10 * time.Second}, cfg.Urls.Address)
	redirectClient := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	prober := &prober{
		log:            log,
		journeys:       journeys,
		duration:       duration,
		urls:           urlsClient,
		redirectHTTP:   redirectClient,
		redirectBase:   cfg.Redirect.Address,
		keyPrefix:      cfg.KeyPrefix,
		statPatience:   30 * time.Second,
		statPollPeriod: time.Second,
	}

	// --- serving ---
	//
	// Liveness only: this process has nothing it depends on to be "ready"
	// beyond having started, and a readiness check that failed because a
	// JOURNEY failed would be wrong — a prober whose target is genuinely
	// down should keep probing it and keep counting the failures, not stop
	// answering ready and get restarted, which would look like recovery
	// while proving nothing.
	probes := runtime.Probes(cfg.Probes.Address, nil)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return runtime.Serve(groupCtx, log, "probes", probes, 5*time.Second) })
	group.Go(func() error {
		prober.loop(groupCtx, interval)
		return nil
	})

	return group.Wait()
}

// prober runs the three journeys, forever, and reports each one.
type prober struct {
	log      *slog.Logger
	journeys metric.Int64Counter
	duration metric.Float64Histogram

	urls         urlshortenerv1connect.UrlsServiceClient
	redirectHTTP *http.Client
	redirectBase string

	keyPrefix string

	// How long, and how often, the "stat" journey polls for the click
	// count to move before it gives up — the same shape
	// examples/url-shortener/e2e/suite's own `eventually` polls with, for
	// the same reason: the counter moves through the broker, not the
	// request that triggered it, so a single read racing the consumer is
	// not a failure of the product.
	statPatience   time.Duration
	statPollPeriod time.Duration
}

// loop runs one pass immediately, then one every interval, until ctx is
// done.
func (p *prober) loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		p.once(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// once walks urls -> redirect -> stat for ONE fresh short link. Each step
// only runs if the one before it succeeded: a "redirect" that never got a
// key to resolve is not a failure of redirect, it is nothing to test at
// all, and counting it as one would blame the wrong component for an
// "urls" outage.
func (p *prober) once(ctx context.Context) {
	longURL := fmt.Sprintf("https://example.com/%s%s", p.keyPrefix, randomSuffix())

	key, ok := record(ctx, p, journey.Urls, func() (string, error) {
		url, err := journey.CreateURL(ctx, p.urls, longURL)
		if err != nil {
			return "", err
		}
		return url.GetKey(), nil
	})
	if !ok {
		return
	}

	_, ok = record(ctx, p, journey.Redirect, func() (string, error) {
		location, status, err := journey.Resolve(ctx, p.redirectHTTP, p.redirectBase, key)
		if err != nil {
			return "", err
		}
		if status != http.StatusFound {
			return "", fmt.Errorf("answered %d, want %d", status, http.StatusFound)
		}
		if location != longURL {
			return "", fmt.Errorf("redirected to %q, want %q", location, longURL)
		}
		return location, nil
	})
	if !ok {
		return
	}

	record(ctx, p, journey.Stat, func() (int64, error) {
		return p.waitForOneClick(ctx, key)
	})
}

// waitForOneClick polls ClickCount until it reads exactly 1 — this journey's
// own short link, resolved exactly once above — or gives up after
// statPatience. Exactly, not "at least": this key is unique to this one
// pass, so anything but 1 is either the consumer double-counting or the
// counter never having moved at all.
func (p *prober) waitForOneClick(ctx context.Context, key string) (int64, error) {
	deadline := time.Now().Add(p.statPatience)
	var last int64
	for {
		count, err := journey.ClickCount(ctx, p.urls, key)
		if err == nil {
			last = count
			if count == 1 {
				return count, nil
			}
			err = fmt.Errorf("click_count is %d, want exactly 1", count)
		}

		if time.Now().After(deadline) {
			return last, err
		}

		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(p.statPollPeriod):
		}
	}
}

// record times fn, emits the counter and the histogram, logs the outcome,
// and reports whether it succeeded so once can decide whether the next
// journey has anything to run against.
//
// Generic over fn's return type so this is written once for three journeys
// that hand back different things (a key, a Location, a click count) —
// value is discarded on failure by returning T's zero value, which every
// caller here ignores anyway.
func record[T any](ctx context.Context, p *prober, name string, fn func() (T, error)) (T, bool) {
	start := time.Now()
	value, err := fn()
	elapsed := time.Since(start)

	result := "success"
	if err != nil {
		result = "failure"
	}

	attrs := metric.WithAttributes(attribute.String("journey", name), attribute.String("result", result))
	p.journeys.Add(ctx, 1, attrs)
	p.duration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attribute.String("journey", name)))

	if err != nil {
		p.log.ErrorContext(ctx, "probe journey failed",
			slog.String("journey", name), slog.String("result", result),
			slog.Float64("duration_seconds", elapsed.Seconds()), slog.String("error", err.Error()))
		var zero T
		return zero, false
	}

	p.log.InfoContext(ctx, "probe journey succeeded",
		slog.String("journey", name), slog.String("result", result),
		slog.Float64("duration_seconds", elapsed.Seconds()))
	return value, true
}

// randomSuffix draws a short, readable suffix for one pass's long URL — the
// prober's own answer to examples/url-shortener/e2e/suite's randomSuffix,
// which needs a *testing.T this process does not have.
func randomSuffix() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, 10)
	for i := range out {
		out[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return string(out)
}
