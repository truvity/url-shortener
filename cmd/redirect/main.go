// Command redirect resolves a short key to a URL and says what happened.
//
// The whole graph is constructed here, in order, with plain constructors. Read
// top to bottom and you know what this process is made of and what it talks
// to; there is no container to ask.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	policyconfig "github.com/truvity/policy/config"

	"github.com/truvity/policy/examples/url-shortener/internal/api"
	"github.com/truvity/policy/examples/url-shortener/internal/business/redirect"
	"github.com/truvity/policy/examples/url-shortener/internal/business/store"
	"github.com/truvity/policy/examples/url-shortener/internal/config"
	"github.com/truvity/policy/examples/url-shortener/internal/events"
	"github.com/truvity/policy/examples/url-shortener/internal/middleware"
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

	cfg, err := config.LoadRedirect(*path)
	if err != nil {
		return err
	}

	log := runtime.Logger(cfg.Log.Level)
	version, commit := runtime.Version()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.InfoContext(ctx, "starting", slog.String("component", "redirect"),
		slog.String("version", version), slog.String("commit", commit))

	// --- what this process talks to ---

	db, closeDB, err := openDatabase(log, cfg.Database)
	if err != nil {
		return err
	}
	defer closeDB()

	nc, err := connect(cfg.Events.NATS)
	if err != nil {
		return err
	}
	// Drain rather than Close: it finishes what is in flight, which for a
	// publisher is the events of requests already answered.
	defer func() { _ = nc.Drain() }()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("open jetstream: %w", err)
	}
	publisher := events.NewJetStream(js)

	// --- what this process is ---

	storeClient, err := store.NewClient(ctx, log, db)
	if err != nil {
		return fmt.Errorf("build the store client: %w", err)
	}

	manager := redirect.NewManager(log, storeClient, subjectPublisher{
		publisher: publisher,
		subject:   cfg.Events.RedirectSubject,
	})
	requestLog := middleware.NewURLRequestMiddleware(log, subjectPublisher{
		publisher: publisher,
		subject:   cfg.Events.RequestSubject,
	})

	app := fiber.New()
	humaAPI := humafiber.New(app, huma.DefaultConfig("URL Shortener", version))
	redirect.RegisterHumaRoutes(ctx, log, humaAPI, manager, requestLog)
	app.Get(api.PathVersion, api.NewVersionHandler("redirect", &api.Version{Version: version, Commit: commit}))

	// --- serving ---

	probes := runtime.Probes(cfg.Probes.Address, func(ctx context.Context) error {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			return fmt.Errorf("database: %w", err)
		}
		if !nc.IsConnected() {
			return errors.New("not connected to the event stream")
		}
		return nil
	})

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return runtime.Serve(groupCtx, log, "probes", probes, 5*time.Second) })
	group.Go(func() error {
		<-groupCtx.Done()
		// The same drain the probe server gets, for the same reason: an
		// orchestrator keeps sending traffic for a moment after SIGTERM, and
		// a server that stops accepting immediately drops requests that were
		// already in flight.
		log.InfoContext(groupCtx, "draining", slog.String("server", "api"))
		return app.ShutdownWithTimeout(20 * time.Second)
	})
	group.Go(func() error {
		log.InfoContext(ctx, "listening", slog.String("address", cfg.Listen.Address))
		if err := app.Listen(cfg.Listen.Address, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	})
	return group.Wait()
}

// subjectPublisher binds a publisher to one subject, because the business
// code should not have to know which subject its events belong on — that is a
// deployment's decision and it arrives as configuration.
type subjectPublisher struct {
	publisher events.Publisher
	subject   string
}

func (s subjectPublisher) PublishEvents(ctx context.Context, batch []events.Event) error {
	out := make([]events.Event, 0, len(batch))
	for _, e := range batch {
		e.Subject = s.subject
		out = append(out, e)
	}
	return s.publisher.PublishEvents(ctx, out)
}

func openDatabase(log *slog.Logger, pg config.Postgres) (*gorm.DB, func(), error) {
	dsn, err := dsn(pg)
	if err != nil {
		return nil, nil, err
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// The library logs through the service's logger, not its own.
		// See runtime.GormLogger.
		Logger: runtime.GormLogger(log, time.Second),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("connect to the database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("reach the connection pool: %w", err)
	}
	if pg.MaxConnections > 0 {
		// Sized against the server's limit divided by the number of
		// instances, not guessed: every replica opens this many.
		sqlDB.SetMaxOpenConns(pg.MaxConnections)
	}
	return db, func() { _ = sqlDB.Close() }, nil
}

func connect(cfg config.NATS) (*nats.Conn, error) {
	opts := []nats.Option{nats.Name("url-shortener-redirect")}
	if cfg.CredentialsFile != "" {
		opts = append(opts, nats.UserCredentials(cfg.CredentialsFile))
	}
	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to the event stream: %w", err)
	}
	return nc, nil
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
