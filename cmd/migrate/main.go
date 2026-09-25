// Command migrate brings the database schema up to date and exits.
//
// It runs as a job before the components that read the schema, which is why
// it has no listener and no probes: a job that runs once and exits is not a
// service, and a probe endpoint it never serves would be configuration a
// deployment can set and watch do nothing.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	policyconfig "github.com/truvity/policy/config"
	policytelemetry "github.com/truvity/policy/telemetry"

	"github.com/truvity/policy/examples/url-shortener/internal/config"
	"github.com/truvity/policy/examples/url-shortener/internal/migration"
	"github.com/truvity/policy/examples/url-shortener/internal/runtime"
)

func main() {
	if err := run(); err != nil {
		// Before the logger exists there is no structured log to write to,
		// and a configuration error is the most likely failure here.
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("config", os.Getenv("CONFIG_FILE"), "path to the configuration file")
	flag.Parse()
	if *path == "" {
		return fmt.Errorf("no configuration file: pass -config or set CONFIG_FILE")
	}

	cfg, err := config.LoadMigrate(*path)
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

	log.InfoContext(ctx, "starting", slog.String("component", "migrate"),
		slog.String("version", version), slog.String("commit", commit))

	dsn, err := dsn(cfg.Database)
	if err != nil {
		return err
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// The library logs through the service's logger, not its own.
		// See runtime.GormLogger.
		Logger: runtime.GormLogger(log, time.Second),
	})
	if err != nil {
		return fmt.Errorf("connect to the database: %w", err)
	}
	if err := runtime.TraceDatabase(db); err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("reach the connection pool: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := withSpan(ctx, "migrate.run", func(ctx context.Context) error {
		return migration.Run(ctx, log, db, cfg.OwnerRole, cfg.AppRole)
	}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	log.InfoContext(ctx, "migrated")
	return nil
}

// withSpan runs a step inside a span, and marks the span an error when the
// step fails.
//
// This Job started telemetry, connected to the gateway, flushed on the way
// out -- and created no span, so it exported nothing and looked exactly like
// a Job that ran and was never observed. A provider is where spans GO; it is
// not what makes them. The same was found in three other services, each
// found the same way: absent from the trace store's list of services while
// every dashboard said the pipeline was healthy.
//
// One span for the run, because that is the unit somebody asks about: did
// the migration happen, how long did it take, and did it fail. The failure
// is recorded on the span itself, or a trace of a Job that failed reads as
// one that succeeded.
func withSpan(ctx context.Context, name string, step func(context.Context) error) error {
	ctx, span := otel.Tracer("github.com/truvity/policy/examples/url-shortener").Start(ctx, name)
	defer span.End()

	if err := step(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return err
	}

	return nil
}

// dsn puts the password back into the connection string. The configuration
// carries the NAME of the variable holding it, never the value: a
// configuration file is rendered into a config map, printed when somebody
// debugs a deployment, and committed as a test fixture.
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
