package store_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/truvity/cnpg/v2/clients/go/pgclient"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/truvity/policy/examples/url-shortener/internal/business/store"
)

// These tests use the REAL path the service uses -- a pgx pool handed to
// database/sql (stdlib.OpenDBFromPool) and then to gorm -- against a server
// that fails the way a switchover does. The fake driver in retry_test.go
// cannot show that the error keeps its type through pgx, database/sql and
// gorm; this can.

// listen serves h on a loopback port and returns the port.
func listen(t *testing.T, h func(net.Conn)) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go h(c)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

// closedPort is a port nothing listens on: connection refused.
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

// fatalMessage is a PostgreSQL ErrorResponse with severity FATAL.
func fatalMessage(code, msg string) []byte {
	body := append([]byte("SFATAL\x00C"+code+"\x00M"+msg+"\x00"), 0)
	n := len(body) + 4
	return append([]byte{'E', byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, body...)
}

func realGorm(t *testing.T, port int) *gorm.DB {
	t.Helper()
	pc, err := pgxpool.ParseConfig(fmt.Sprintf("host=127.0.0.1 port=%d user=u dbname=d sslmode=disable connect_timeout=2", port))
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), pc)
	require.NoError(t, err)
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = sqlDB.Close(); pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	require.NoError(t, err)
	return db
}

// A connection that cannot be made, a server that hangs up, and the two
// server-side shutdown codes all reach the caller of gorm still recognised by
// the client's own IsRetryable. If this ever fails, the type is being lost
// between pgx and gorm and no retry wrapped around the store can help.
func TestConnectionFailuresKeepTheirTypeThroughGorm(t *testing.T) {
	serverError := func(code, msg string) func(net.Conn) {
		return func(c net.Conn) {
			b := make([]byte, 64)
			_, _ = c.Read(b)
			_, _ = c.Write(fatalMessage(code, msg))
			_ = c.Close()
		}
	}
	cases := []struct {
		name string
		port func(t *testing.T) int
	}{
		{"refused", closedPort},
		{"hangs up", func(t *testing.T) int { return listen(t, func(c net.Conn) { _ = c.Close() }) }},
		{"57P01 admin shutdown", func(t *testing.T) int {
			return listen(t, serverError("57P01", "terminating connection due to administrator command"))
		}},
		{"57P03 shutting down", func(t *testing.T) int {
			return listen(t, serverError("57P03", "the database system is shutting down"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := realGorm(t, tc.port(t))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var one int
			err := db.WithContext(ctx).Raw("select 1").Scan(&one).Error
			require.Error(t, err)
			require.True(t, pgclient.IsRetryable(err), "%T %v", err, err)
		})
	}
}

// The store, over the real path, runs a read again while the database is away
// and reports the failure only when the policy gives up.
func TestReadsOverARealPoolAreRetried(t *testing.T) {
	var tries atomic.Int32
	policy := pgclient.RetryPolicy{Attempts: 3, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, Budget: 5 * time.Second}
	c, err := store.NewClient(t.Context(), slog.New(slog.DiscardHandler), realGorm(t, closedPort(t)),
		func(ctx context.Context, fn func(context.Context) error) error {
			return pgclient.Retry(ctx, policy, func(ctx context.Context) error {
				tries.Add(1)
				return fn(ctx)
			})
		})
	require.NoError(t, err)

	_, err = c.GetURLByLongURL(t.Context(), "https://example.test")
	var connErr *pgconn.ConnectError
	require.ErrorAs(t, err, &connErr)
	require.EqualValues(t, 3, tries.Load(), "the lookup was not retried")
}
