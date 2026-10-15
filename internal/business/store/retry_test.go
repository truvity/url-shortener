package store_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/truvity/cnpg/v2/clients/go/pgclient"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/truvity/policy/examples/url-shortener/internal/business/store"
)

// script stands in for the server. It sees every statement and answers with
// the rows to return (for a query), the rows affected (for an exec) or an
// error, which is how a switchover reaches the client.
type script func(n int, query string) (rows [][]driver.Value, affected int64, err error)

var queryColumns = []string{"id", "url_key", "long_url", "created_at", "updated_at", "deleted_at", "expires_at"}

type fakeDriver struct {
	mu      sync.Mutex
	script  script
	queries []string
}

func (d *fakeDriver) Open(string) (driver.Conn, error) { return &fakeConn{d: d}, nil }

func (d *fakeDriver) answer(query string) ([][]driver.Value, int64, error) {
	d.mu.Lock()
	query = strings.TrimSpace(query)
	d.queries = append(d.queries, query)
	n := len(d.queries)
	d.mu.Unlock()
	return d.script(n, query)
}

func (d *fakeDriver) count(prefix string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := 0
	for _, q := range d.queries {
		if strings.HasPrefix(q, prefix) {
			c++
		}
	}
	return c
}

type fakeConn struct{ d *fakeDriver }

func (*fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (*fakeConn) Close() error                        { return nil }
func (*fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }

func (c *fakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	_, affected, err := c.d.answer(query)
	if err != nil {
		return nil, err
	}
	return driver.RowsAffected(affected), nil
}

func (c *fakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	rows, _, err := c.d.answer(query)
	if err != nil {
		return nil, err
	}
	return &fakeRows{rows: rows}, nil
}

type fakeRows struct {
	rows [][]driver.Value
	i    int
}

func (*fakeRows) Columns() []string { return queryColumns }
func (*fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.i])
	r.i++
	return nil
}

func row(deleted bool) [][]driver.Value {
	var deletedAt driver.Value
	if deleted {
		deletedAt = time.Now()
	}
	return [][]driver.Value{{"id", "abc", "https://example.test", time.Now(), nil, deletedAt, nil}}
}

// A server going away under a statement, as the client sees it.
func switchover() error { return &pgconn.PgError{Code: "57P01", Message: "terminating connection"} }

func newClient(t *testing.T, s script) (*store.Client, *fakeDriver) {
	t.Helper()
	d := &fakeDriver{script: s}
	sqlDB := sql.OpenDB(connector{d})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	require.NoError(t, err)
	policy := pgclient.RetryPolicy{Attempts: 4, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, Budget: 5 * time.Second}
	c, err := store.NewClient(t.Context(), slog.New(slog.DiscardHandler), db, func(ctx context.Context, fn func(context.Context) error) error {
		return pgclient.Retry(ctx, policy, fn)
	})
	require.NoError(t, err)
	return c, d
}

type connector struct{ d *fakeDriver }

func (c connector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{d: c.d}, nil }
func (c connector) Driver() driver.Driver                        { return c.d }

func isInsert(q string) bool { return strings.HasPrefix(q, "INSERT") }
func isUpdate(q string) bool { return strings.HasPrefix(q, "UPDATE") }

// The insert is interrupted by a switchover. The look-then-insert runs again
// and the URL is stored once.
func TestPutURLSurvivesASwitchover(t *testing.T) {
	failed := false
	c, d := newClient(t, func(_ int, q string) ([][]driver.Value, int64, error) {
		if isInsert(q) {
			if !failed {
				failed = true
				return nil, 0, switchover()
			}
			return nil, 1, nil
		}
		return nil, 0, nil // the look: nothing there
	})
	require.NoError(t, c.PutURL(t.Context(), "abc", "https://example.test", nil))
	require.Equal(t, 2, d.count("INSERT"))
}

// The insert committed and only the answer was lost. The second run finds the
// row and writes nothing: that is what makes the retry safe.
func TestPutURLRetryAfterALostCommitWritesNothing(t *testing.T) {
	committed := false
	c, d := newClient(t, func(_ int, q string) ([][]driver.Value, int64, error) {
		if isInsert(q) {
			committed = true
			return nil, 0, io.ErrUnexpectedEOF
		}
		if committed {
			return row(false), 0, nil
		}
		return nil, 0, nil
	})
	require.NoError(t, c.PutURL(t.Context(), "abc", "https://example.test", nil))
	require.Equal(t, 1, d.count("INSERT"))
}

// A constraint violation is the statement's fault, not the connection's: no
// second attempt.
func TestPutURLDoesNotRetryAConstraintViolation(t *testing.T) {
	c, d := newClient(t, func(_ int, q string) ([][]driver.Value, int64, error) {
		if isInsert(q) {
			return nil, 0, &pgconn.PgError{Code: "23505"}
		}
		return nil, 0, nil
	})
	require.Error(t, c.PutURL(t.Context(), "abc", "https://example.test", nil))
	require.Equal(t, 1, d.count("INSERT"))
}

// A database that stays away ends in an error after the attempts, not a hang.
func TestPutURLGivesUpAfterTheAttempts(t *testing.T) {
	c, d := newClient(t, func(_ int, q string) ([][]driver.Value, int64, error) {
		if isInsert(q) {
			return nil, 0, switchover()
		}
		return nil, 0, nil
	})
	require.Error(t, c.PutURL(t.Context(), "abc", "https://example.test", nil))
	require.Equal(t, 4, d.count("INSERT"))
}

// The counter adds one: running it again after a lost commit would count the
// click twice, so it is never retried.
func TestIncrementClickCountIsNotRetried(t *testing.T) {
	c, d := newClient(t, func(int, string) ([][]driver.Value, int64, error) { return nil, 0, switchover() })
	require.Error(t, c.IncrementClickCount(t.Context(), "https://example.test"))
	require.Equal(t, 1, d.count("INSERT"))
}

// The delete committed, the answer was lost; the retry matches no row, sees
// the URL deleted and reports the success the first run achieved.
func TestDeleteURLRetryAfterALostCommitSucceeds(t *testing.T) {
	committed := false
	c, d := newClient(t, func(_ int, q string) ([][]driver.Value, int64, error) {
		if isUpdate(q) {
			if !committed {
				committed = true
				return nil, 0, switchover()
			}
			return nil, 0, nil
		}
		return row(true), 0, nil
	})
	require.NoError(t, c.DeleteURL(t.Context(), "abc"))
	require.Equal(t, 2, d.count("UPDATE"))
}

// Without an interruption, deleting what is not there is still an error: the
// leniency belongs to a retry only.
func TestDeleteURLOfNothingIsStillAnError(t *testing.T) {
	c, d := newClient(t, func(int, string) ([][]driver.Value, int64, error) { return nil, 0, nil })
	require.ErrorContains(t, c.DeleteURL(t.Context(), "abc"), "not found")
	require.Equal(t, 1, d.count("UPDATE"))
}

// A retry that matches no row, where the URL is live, is a real failure.
func TestDeleteURLRetryOfALiveURLIsAnError(t *testing.T) {
	first := true
	c, _ := newClient(t, func(_ int, q string) ([][]driver.Value, int64, error) {
		if isUpdate(q) {
			if first {
				first = false
				return nil, 0, switchover()
			}
			return nil, 0, nil
		}
		return row(false), 0, nil
	})
	require.ErrorContains(t, c.DeleteURL(t.Context(), "abc"), "not found")
}

func TestRestoreURLRetryAfterALostCommitSucceeds(t *testing.T) {
	committed := false
	c, d := newClient(t, func(_ int, q string) ([][]driver.Value, int64, error) {
		if isUpdate(q) {
			if !committed {
				committed = true
				return nil, 0, switchover()
			}
			return nil, 0, nil
		}
		return row(false), 0, nil
	})
	require.NoError(t, c.RestoreURL(t.Context(), "abc"))
	require.Equal(t, 2, d.count("UPDATE"))
}

func TestUpdateURLSurvivesASwitchover(t *testing.T) {
	failed := false
	c, d := newClient(t, func(_ int, q string) ([][]driver.Value, int64, error) {
		if isUpdate(q) {
			if !failed {
				failed = true
				return nil, 0, switchover()
			}
			return nil, 1, nil
		}
		return row(false), 0, nil
	})
	info, err := c.UpdateURL(t.Context(), "abc", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, 2, d.count("UPDATE"))
}
