package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GormLogger adapts GORM's logger to the log contract.
//
// It exists because of a rule that is easy to state and easy to forget: a
// service emits ONE stream of JSON on stdout, at one level, and nothing it
// depends on gets to decide otherwise. GORM's default logger writes its own
// format, with ANSI colour, through the standard library's log package —
// readable in a terminal, unparseable in a log pipeline, and invisible to a
// level that was set in the configuration file.
//
// So every library that logs is wired to the service's logger at the
// composition root, the same way every other dependency is. Deciding this
// per service is how half of them end up with a second log format.
func GormLogger(log *slog.Logger, slow time.Duration) gormlogger.Interface {
	return &gormLog{log: log, slow: slow}
}

type gormLog struct {
	log  *slog.Logger
	slow time.Duration
}

// LogMode is GORM's per-call level switch. The level is the service's, set
// once from the configuration, so there is nothing here to change.
func (g *gormLog) LogMode(gormlogger.LogLevel) gormlogger.Interface { return g }

// The library hands us a format string and its arguments. They become a
// FIELD, under a constant message, rather than the message itself: a message
// that varies per call cannot be grouped, counted or alerted on, and
// "database said something" that occurs 4,000 times is a signal, while 4,000
// distinct messages are a wall.

func (g *gormLog) Info(ctx context.Context, msg string, args ...any) {
	g.log.InfoContext(ctx, "database", slog.String("detail", fmt.Sprintf(msg, args...)))
}

func (g *gormLog) Warn(ctx context.Context, msg string, args ...any) {
	g.log.WarnContext(ctx, "database", slog.String("detail", fmt.Sprintf(msg, args...)))
}

func (g *gormLog) Error(ctx context.Context, msg string, args ...any) {
	g.log.ErrorContext(ctx, "database", slog.String("detail", fmt.Sprintf(msg, args...)))
}

// Trace is called once per statement. The SQL goes at DEBUG, so a service
// running at INFO does not log every query, and a service that needs to see
// them says so in its configuration rather than being rebuilt.
func (g *gormLog) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()

	attrs := []any{
		slog.String("sql", sql),
		slog.Int64("rows", rows),
		slog.Duration("elapsed", elapsed),
	}

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		g.log.ErrorContext(ctx, "query failed", append(attrs, slog.String("error", err.Error()))...)
	case g.slow > 0 && elapsed > g.slow:
		g.log.WarnContext(ctx, "slow query", attrs...)
	default:
		g.log.DebugContext(ctx, "query", attrs...)
	}
}
