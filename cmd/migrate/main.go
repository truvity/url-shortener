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

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	policyconfig "github.com/truvity/policy/config"

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

	log.InfoContext(ctx, "starting", slog.String("component", "migrate"),
		slog.String("version", version), slog.String("commit", commit))

	dsn, err := dsn(cfg.Database)
	if err != nil {
		return err
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("connect to the database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("reach the connection pool: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := migration.RunMigrations(ctx, log, db, cfg.OwnerRole, cfg.AppRole); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	log.InfoContext(ctx, "migrated")
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
