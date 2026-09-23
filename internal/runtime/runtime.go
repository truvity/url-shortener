// Package runtime is what this service writes instead of importing a
// framework: a logger, a probe listener, and a shutdown.
//
// It is deliberately here rather than in a shared library. The whole of it is
// under a hundred lines, every line is one a reader can follow, and a shared
// version would grow options for the differences between services — which is
// how the framework this example replaced began.
package runtime

import (
	"github.com/truvity/policy/transport"

	"github.com/gofiber/fiber/v3"

	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
	"time"
)

// Logger returns the one logger this service uses: JSON, on stdout, at one
// level. Per-package levels are not a thing here — they sound useful twice a
// year and cost a configuration surface every service, chart and operator has
// to know about.
func Logger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

// Version reads what this binary was built from. Nothing passes it in: a
// version that can disagree with the binary is worse than no version at all,
// because it is trusted.
func Version() (version, commit string) {
	version, commit = "dev", "unknown"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			commit = s.Value
		}
	}
	return version, commit
}

// ReadyFunc reports whether this instance can serve traffic now.
type ReadyFunc func(context.Context) error

// Probes serves /health/live and /health/ready on its own listener.
//
// Its own listener, because readiness must be answerable when the service's
// own listener is saturated — which is exactly when somebody is asking.
//
// Liveness checks NOTHING. A liveness probe that touches a database turns a
// slow dependency into a restart loop, and a restart loop into an outage that
// looks like the service's fault.
func Probes(addr string, ready ReadyFunc) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			if err := ready(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(err.Error()))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}

// Serve runs an HTTP server until ctx is done, then drains it.
//
// The drain is the part that no test notices and every rolling upgrade needs:
// an orchestrator sends SIGTERM and keeps sending traffic for a moment, so a
// server that stops accepting immediately drops requests that were already
// in flight and the deploy gets blamed for something else.
func Serve(ctx context.Context, log *slog.Logger, name string, srv *http.Server, grace time.Duration) error {
	var once sync.Once
	var serveErr error

	go func() {
		<-ctx.Done()
		stopping, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
		defer cancel()
		log.InfoContext(ctx, "draining", slog.String("server", name))
		if err := srv.Shutdown(stopping); err != nil {
			log.ErrorContext(ctx, "drain did not finish", slog.String("server", name), slog.Any("error", err))
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		once.Do(func() { serveErr = err })
	}
	return serveErr
}

// Drain turns the configured number of seconds into a duration, falling back
// to a default when nothing was configured.
//
// The fallback is a courtesy for a local run, not a deployment setting. In a
// deployment the number is supplied, because the grace period the
// orchestrator grants is derived from the same one: a service that quietly
// used its own default would be killed at whatever moment that default
// happened to disagree with the budget, and the symptom is a truncated
// response rather than anything that names a timeout.
func Drain(seconds int) time.Duration {
	if seconds <= 0 {
		return 20 * time.Second
	}

	return time.Duration(seconds) * time.Second
}

// ListenTLS is the Fiber listener configuration for a service's own port.
//
// Fiber serves a supplied tls.Config as given, which is what makes the
// mounted identity usable: the certificate comes from a callback, so a
// rotation is picked up without restarting, and the peer check runs inside
// the handshake rather than as middleware nobody can be sure ran.
//
// A nil identity means cleartext, and that is the ordinary case: the chart's
// default is off, because a service must be installable on a platform that
// provides no identity at all.
func ListenTLS(id *transport.Identity) fiber.ListenConfig {
	cfg := fiber.ListenConfig{DisableStartupMessage: true}
	if id == nil {
		return cfg
	}

	cfg.TLSConfig = id.Server()

	return cfg
}
