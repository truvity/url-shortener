// Command urls owns the URL tables and answers questions about them.
//
// It is the example's ownership boundary: one service holds the tables, and
// everything that writes them asks it. The counter calls RecordClick rather
// than issuing an UPDATE, which is the rule in docs/contracts/service.md §8
// made into a process.
//
// The redirect service is the deliberate exception and is worth reading as
// one: it reads the table directly, because a redirect is the hot path and a
// second network hop on it is not worth what it buys. RPC for a boundary of
// ownership, events for fan-out, a direct read only where latency justifies
// a second reader — all three are in this example on purpose.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	policyconfig "github.com/truvity/policy/config"
	policytelemetry "github.com/truvity/policy/telemetry"
	"github.com/truvity/policy/transport"

	"github.com/truvity/policy/examples/url-shortener/internal/business/store"
	"github.com/truvity/policy/examples/url-shortener/internal/business/urls"
	"github.com/truvity/policy/examples/url-shortener/internal/config"
	"github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1/urlshortenerv1connect"
	"github.com/truvity/policy/examples/url-shortener/internal/runtime"
)

const component = "urls"

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

	cfg, err := config.LoadUrls(*path)
	if err != nil {
		return err
	}

	log := runtime.Logger(cfg.Log.Level)
	version, commit := runtime.Version()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Telemetry, from OpenTelemetry's own environment (decision 0006).
	// With no endpoint configured this installs exporters that do nothing,
	// so a laptop and a cluster run the same code down the same path.
	shutdownTelemetry, err := policytelemetry.Start(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// A short grace of its own: the context above is already cancelled
		// by the time this runs, and a flush on a cancelled context sends
		// nothing -- which loses exactly the spans that describe the
		// shutdown somebody is looking into.
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(flush); err != nil {
			log.WarnContext(ctx, "telemetry did not flush", slog.String("error", err.Error()))
		}
	}()

	log.InfoContext(ctx, "starting", slog.String("component", component),
		slog.String("version", version), slog.String("commit", commit))

	db, closeDB, err := openDatabase(log, cfg.Database)
	if err != nil {
		return err
	}
	defer closeDB()

	storeClient, err := store.NewClient(ctx, log, db)
	if err != nil {
		return fmt.Errorf("build the store client: %w", err)
	}
	handler := urls.New(log, storeClient, component, runtime.Version)

	identity, err := transport.Load(cfg.TLS, log)
	if err != nil {
		return fmt.Errorf("transport identity: %w", err)
	}

	// ONE handler, serving Connect, gRPC and gRPC-Web. That is the whole
	// reason for choosing this shape over a bare gRPC server: a browser and
	// a Go client and a curl all reach the same methods, and a unary call is
	// a plain POST with a JSON body that a person can type.
	mux := http.NewServeMux()
	mux.Handle(urlshortenerv1connect.NewUrlsServiceHandler(handler, connectOptions(log)...))
	mux.Handle(urlshortenerv1connect.NewMetaServiceHandler(handler, connectOptions(log)...))

	// HTTP/2 WITHOUT TLS, alongside HTTP/1.1. A gRPC client in the cluster
	// needs HTTP/2, and with `tls.mode: off` there is no TLS to negotiate it
	// over — so it is negotiated in cleartext instead. Without this the
	// server answers HTTP/1.1 and every gRPC client fails at the handshake
	// while Connect clients keep working, which makes the fault look like
	// the client's.
	//
	// The standard library does this since Go 1.24; the x/net/h2c wrapper
	// that used to be the way is deprecated in favour of exactly this field.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	api := &http.Server{
		Addr:              cfg.Listen.Address,
		Handler:           mux,
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Under `strict` the address does not move and only the protocol does,
	// so nothing downstream has to be told a new port. Cleartext HTTP/2 goes
	// away with it: over TLS, HTTP/2 is negotiated by ALPN instead.
	if identity.Mode() == transport.Strict {
		api.TLSConfig = identity.Server()
		api.Protocols = nil
	}

	probes := runtime.Probes(cfg.Probes.Address, func(ctx context.Context) error {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			return fmt.Errorf("database: %w", err)
		}
		return nil
	})

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return runtime.Serve(groupCtx, log, "probes", probes, 5*time.Second) })
	group.Go(func() error {
		log.InfoContext(ctx, "listening",
			slog.String("address", cfg.Listen.Address),
			slog.String("transport", string(identity.Mode())))
		if identity.Mode() == transport.Strict {
			return runtime.ServeTLS(groupCtx, log, "api", api, runtime.Drain(cfg.Drain.Seconds))
		}
		return runtime.Serve(groupCtx, log, "api", api, runtime.Drain(cfg.Drain.Seconds))
	})

	// Under `permissive` the service answers on TWO listeners, so one caller
	// migrates at a time. Under `strict` the address does not move and only
	// the protocol changes.
	if identity.Mode() == transport.Permissive {
		authenticated := &http.Server{
			Addr:              cfg.TLS.Address,
			Handler:           mux,
			TLSConfig:         identity.Server(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		group.Go(func() error {
			log.InfoContext(ctx, "listening",
				slog.String("address", cfg.TLS.Address),
				slog.String("transport", "authenticated"))
			return runtime.ServeTLS(groupCtx, log, "api-tls", authenticated, runtime.Drain(cfg.Drain.Seconds))
		})
	}
	return group.Wait()
}

// connectOptions are the same for every handler this binary serves, which is
// the point of naming them once: a service whose two handlers disagree about
// interceptors has one of them unprotected, and nothing says which.
func connectOptions(log *slog.Logger) []connect.HandlerOption {
	options := []connect.HandlerOption{
		connect.WithInterceptors(procedureLogger(log)),
		connect.WithRecover(recoverToError(log)),
	}

	// Spans for every procedure, and the incoming trace context continued
	// rather than restarted.
	//
	// TRUSTED, because the caller is another service of this platform, and
	// the alternative is the default: an untrusted server span starts a
	// trace of its own and only LINKS to the caller's. That is the right
	// default for a service facing the internet, where any client could
	// otherwise choose the trace this service joins and whether it is
	// sampled. Here it is what left every request as two unconnected traces.
	// Every caller reaches this service through the platform's transport
	// rules, so a hostile caller is not what this trusts.
	//
	// Installing exporters is not instrumentation: a service with a tracer
	// provider and nothing creating spans exports nothing, and the only
	// symptom is a service missing from the trace store while every
	// dashboard reports the pipeline healthy. Found exactly that way.
	//
	// A failure here is not fatal. Telemetry that can refuse to start is
	// telemetry that can take the service with it, and a service that will
	// not serve because it cannot be observed has the priority backwards.
	if otelInterceptor, err := otelconnect.NewInterceptor(otelconnect.WithTrustRemote()); err != nil {
		// Background, because this is start-up: there is no request whose
		// context this belongs to.
		log.WarnContext(context.Background(), "serving without spans", slog.String("error", err.Error()))
	} else {
		options = append(options, connect.WithInterceptors(otelInterceptor))
	}

	return options
}

// recoverToError turns a panic in a handler into an ordinary error.
//
// Without it a panic is not an error at all to the caller: net/http resets
// the HTTP/2 stream, and what arrives is `Stream closed with error code
// NGHTTP2_INTERNAL_ERROR` -- a transport failure, naming nothing, for a
// request that may already have written its row. That is exactly what a
// user saw, and it says the opposite of what happened.
//
// This is the net, not the fix: the bug behind that panic was fixed where it
// was. The net exists because the next one will not have been anticipated.
// It logs the stack at error level with the procedure named, so it is loud
// where it is useful, and it tells the CALLER only that something went
// wrong -- a panic value is an implementation detail and a stack trace is
// not an answer.
func recoverToError(log *slog.Logger) func(context.Context, connect.Spec, http.Header, any) error {
	return func(ctx context.Context, spec connect.Spec, _ http.Header, recovered any) error {
		log.ErrorContext(ctx, "handler panicked",
			slog.String("procedure", spec.Procedure),
			slog.Any("panic", recovered),
			slog.String("stack", string(debug.Stack())),
		)

		return connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
}

// procedureLogger puts the procedure name on every log line a call produces.
//
// A JSON log with no procedure is a log you can count and not read: it says a
// service was slow without saying at what.
func procedureLogger(log *slog.Logger) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			started := time.Now()
			res, err := next(ctx, req)
			attrs := []any{
				slog.String("procedure", req.Spec().Procedure),
				slog.Int64("ms", time.Since(started).Milliseconds()),
			}
			if err != nil {
				attrs = append(attrs, slog.String("code", connect.CodeOf(err).String()))
				log.ErrorContext(ctx, "call refused", attrs...)
				return res, err
			}
			log.InfoContext(ctx, "call served", attrs...)
			return res, nil
		}
	}
}

func openDatabase(log *slog.Logger, pg config.Postgres) (*gorm.DB, func(), error) {
	dsn, err := dsn(pg)
	if err != nil {
		return nil, nil, err
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: runtime.GormLogger(log, time.Second),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("connect to the database: %w", err)
	}
	if err := runtime.TraceDatabase(db); err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("reach the connection pool: %w", err)
	}
	if pg.MaxConnections > 0 {
		sqlDB.SetMaxOpenConns(pg.MaxConnections)
	}
	return db, func() { _ = sqlDB.Close() }, nil
}

func dsn(pg config.Postgres) (string, error) {
	if pg.PasswordEnv == "" {
		return pg.URL, nil
	}
	password, err := policyconfig.Secret(pg.PasswordEnv)
	if err != nil {
		return "", err
	}
	return injectPassword(pg.URL, password)
}
