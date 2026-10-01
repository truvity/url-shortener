package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// OpenDatabase opens the connection pool and waits for the server to answer,
// retrying per retry while it does not.
//
// The automatic ping gorm does at Open is switched off and replaced by one
// that takes the retry's context, because Open cannot be cancelled: a
// SIGTERM during the wait would otherwise have to outlast a hung dial.
//
// What comes back is a *pool*, and that is what makes the later case work
// without code of its own: database/sql discards a connection that broke and
// dials a fresh one on the next query, so a server restarted mid-run costs the
// queries issued while it was down and nothing after. Readiness (a ping per
// probe) is what keeps traffic away meanwhile.
func OpenDatabase(ctx context.Context, log *slog.Logger, dsn string, maxConnections int, retry Retry) (*gorm.DB, func(), error) {
	var db *gorm.DB
	err := retry.Do(ctx, log, "database", func(ctx context.Context) error {
		opened, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			// The library logs through the service's logger, not its own.
			// See GormLogger.
			Logger:               GormLogger(log, time.Second),
			DisableAutomaticPing: true,
		})
		if err != nil {
			return fmt.Errorf("open the database: %w", err)
		}
		sqlDB, err := opened.DB()
		if err != nil {
			return fmt.Errorf("reach the connection pool: %w", err)
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			_ = sqlDB.Close()
			return fmt.Errorf("connect to the database: %w", err)
		}
		db = opened
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	if err := TraceDatabase(db); err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("reach the connection pool: %w", err)
	}
	if maxConnections > 0 {
		sqlDB.SetMaxOpenConns(maxConnections)
	}
	return db, func() { _ = sqlDB.Close() }, nil
}
