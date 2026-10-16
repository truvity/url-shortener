package runtime

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/truvity/cnpg/v2/clients/go/pgclient"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The start-up patience this service has always had: 0.5 s doubling to 10 s,
// for up to 3 minutes. The client's own default (45 s) is the right
// ceiling for a call in flight and too short for a process that starts while
// the database is reloading, which is routine and must not become a restart
// loop with a back-off of its own.
//
// Applied only when the environment says nothing: CNPG_CLIENT_RETRY_ATTEMPTS
// and CNPG_CLIENT_RETRY_BUDGET still win, and so does _RETRY_MAX_DELAY.
var startupRetry = pgclient.RetryPolicy{
	Attempts:     1000,
	InitialDelay: 500 * time.Millisecond,
	MaxDelay:     10 * time.Second,
	Budget:       3 * time.Minute,
}

// DatabaseConfig reads the connection from the libpq environment the
// platform's client library defines (PGHOST, PGDATABASE, PGUSER,
// PGSSLROOTCERT, CNPG_CLIENT_PASSWORD_FILE and the CNPG_CLIENT_* tuning),
// with this service's start-up patience under any retry the environment did
// not set.
//
// There is no connection URL anywhere in this service. A URL is a string a
// parameter can be dropped from on its way to the driver, and a dropped
// `sslrootcert` turns verify-full into a connection that does not verify;
// the client takes parts, and refuses anything weaker than verify-full.
func DatabaseConfig(getenv func(string) string) (pgclient.Config, error) {
	cfg, err := pgclient.FromEnv(getenv)
	if err != nil {
		return pgclient.Config{}, err
	}
	if getenv("CNPG_CLIENT_RETRY_ATTEMPTS") == "" && getenv("CNPG_CLIENT_RETRY_BUDGET") == "" && getenv("CNPG_CLIENT_RETRY_MAX_DELAY") == "" {
		cfg.Retry = startupRetry
	}
	return cfg, cfg.Validate()
}

// LogRetry returns the pgclient retry hook that reports every failed attempt
// at warn level: the attempt number, the error and the delay before the next
// try. Without it a database that is away for minutes looks like a hung
// start-up.
func LogRetry(ctx context.Context, log *slog.Logger) func(attempt int, err error, delay time.Duration) {
	return func(attempt int, err error, delay time.Duration) {
		log.WarnContext(ctx, "database not ready, retrying",
			slog.Int("attempt", attempt),
			slog.Any("error", err),
			slog.Duration("delay", delay))
	}
}

// Database is the connection a service holds: the gorm handle over the
// client's pool, plus the two things the client adds that gorm does not
// know about, readiness and the retry of a unit of work.
type Database struct {
	// Gorm is the handle the store queries through.
	Gorm *gorm.DB

	pool    *pgclient.Pool
	request pgclient.RetryPolicy
}

// Health is the readiness check: SELECT 1 on a pooled connection, bounded to
// two seconds by the client, through the real path (Service, TLS, password).
func (d *Database) Health(ctx context.Context) error {
	if err := d.pool.Health(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	return nil
}

// Do runs one unit of work again when, and only when, the connection failed
// under it (a switchover, a crash, a demoted primary: see
// pgclient.IsRetryable). The unit MUST be safe to run twice: a statement that
// committed just before the connection broke is run again.
func (d *Database) Do(ctx context.Context, fn func(context.Context) error) error {
	return pgclient.Retry(ctx, d.request, fn)
}

// RequestRetry is the patience of a call in flight, which is not the
// patience of a process starting. The start-up policy waits minutes; a
// request that waits that long has long been abandoned by its caller. When
// the environment tuned the retry, that wins; otherwise the client's own
// default applies (pgclient.DefaultRetryPolicy: no try cap, 0.2 to 2 s
// back-off, a 45 s budget), which outlasts a CloudNativePG switchover (10 to
// 30 s). A caller's own deadline still ends the wait sooner.
func RequestRetry(ctx context.Context, cfg pgclient.Config, log *slog.Logger) pgclient.RetryPolicy {
	p := cfg.Retry
	if p.Attempts == startupRetry.Attempts && p.InitialDelay == startupRetry.InitialDelay &&
		p.MaxDelay == startupRetry.MaxDelay && p.Budget == startupRetry.Budget {
		p = pgclient.DefaultRetryPolicy()
	}
	p.OnRetry = func(attempt int, err error, delay time.Duration) {
		log.WarnContext(ctx, "database call interrupted, retrying",
			slog.Int("attempt", attempt),
			slog.Any("error", err),
			slog.Duration("delay", delay))
	}
	return p
}

// OpenDatabase connects, waiting for the server per cfg.Retry, and returns a
// gorm handle over the client's pool, with the function that closes both.
//
// gorm is handed the pool the client built (pgx's own pool, through
// database/sql), not a connection string of its own. That is what keeps the
// rules in ONE place: the TLS settings, the files re-read for every new
// connection, the statement timeout. A second pool opened by gorm from a
// string would have none of them.
//
// What comes back is a *pool*, which is what makes a server restarted
// mid-run cost the queries issued while it was down and nothing after:
// the pool discards a connection that broke and dials a fresh one. Readiness
// (a ping per probe) keeps traffic away meanwhile.
//
// Tracing stays this package's gorm plugin (TraceDatabase), so pgclient's
// own pgx tracer (otelpg) is deliberately NOT set: both would record every
// statement, and a trace with each query twice is worse than one without.
func OpenDatabase(ctx context.Context, log *slog.Logger, cfg pgclient.Config) (*Database, func(), error) {
	cfg.Retry.OnRetry = LogRetry(ctx, log)
	pool, err := pgclient.New(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to the database: %w", err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool.Pool)
	boundSQL(sqlDB, cfg)

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		// The library logs through the service's logger, not its own.
		// See GormLogger.
		Logger: GormLogger(log, time.Second),
		// pgclient.New already proved the connection.
		DisableAutomaticPing: true,
	})
	if err != nil {
		_ = sqlDB.Close()
		pool.Close()
		return nil, nil, fmt.Errorf("open the database: %w", err)
	}
	if err := TraceDatabase(db); err != nil {
		_ = sqlDB.Close()
		pool.Close()
		return nil, nil, err
	}
	log.InfoContext(ctx, "database connected",
		slog.String("host", cfg.Host), slog.String("database", cfg.Database), slog.String("user", cfg.User))
	return &Database{Gorm: db, pool: pool, request: RequestRetry(context.WithoutCancel(ctx), cfg, log)}, func() {
		_ = sqlDB.Close()
		pool.Close()
	}, nil
}

// boundSQL gives database/sql the pool's own ceiling, so a burst waits in one
// place. OpenDBFromPool already keeps no idle connections in database/sql: the
// pool owns idleness and lifetime, which is what lets a renewed certificate or
// password reach every connection within one lifetime.
func boundSQL(db *sql.DB, cfg pgclient.Config) {
	db.SetMaxOpenConns(int(cfg.MaxConns))
}
