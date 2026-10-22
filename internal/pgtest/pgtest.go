// Package pgtest starts a PostgreSQL server for the tests that need a real
// one, from TestMain, on the machine that runs them.
//
// No container and no cluster: the server is the devbox `postgresql_18`
// package (initdb and pg_ctl on PATH), a private data directory under the
// temp dir, 127.0.0.1 on a free port, `trust` authentication and no TLS. It
// is the same PostgreSQL major as the clusters this service runs on, and
// stops with the test binary.
//
// TEST_DATABASE_URL replaces the server: a URL of a superuser on a server
// that already runs (a laptop's own). The tests then create their databases
// there and drop them.
//
// If there is no server to start and none to use, Main refuses to run when
// CI is set -- a database test that silently passes by skipping is a
// database test that was never run -- and skips locally.
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

// Server is a running PostgreSQL the tests can create databases on.
type Server struct {
	// admin is the URL of a superuser on the maintenance database.
	admin *url.URL
	stop  func()
}

var (
	current *Server
	counter atomic.Int64
)

// Main is a TestMain body: it starts the server, runs the tests and stops
// the server. Use it as `func TestMain(m *testing.M) { pgtest.Main(m) }`.
func Main(m *testing.M) {
	code := run(m)
	os.Exit(code)
}

func run(m *testing.M) int {
	srv, err := Start()
	if err != nil {
		if os.Getenv("CI") != "" {
			fmt.Fprintf(os.Stderr, "pgtest: %v\n(CI is set, so a database test is not allowed to skip)\n", err)

			return 1
		}

		fmt.Fprintf(os.Stderr, "pgtest: %v -- skipping the database tests\n", err)

		return 0
	}

	current = srv
	defer srv.Stop()

	return m.Run()
}

// Start starts (or connects to) a server. Most callers want Main.
func Start() (*Server, error) {
	if raw := os.Getenv("TEST_DATABASE_URL"); raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("TEST_DATABASE_URL: %w", err)
		}

		return &Server{admin: u, stop: func() {}}, nil
	}

	initdb, err := exec.LookPath("initdb")
	if err != nil {
		return nil, fmt.Errorf("no initdb on PATH (the devbox postgresql_18 package) and no TEST_DATABASE_URL")
	}

	pgctl, err := exec.LookPath("pg_ctl")
	if err != nil {
		return nil, fmt.Errorf("no pg_ctl on PATH (the devbox postgresql_18 package) and no TEST_DATABASE_URL")
	}

	dir, err := os.MkdirTemp("", "url-shortener-pg-")
	if err != nil {
		return nil, err
	}

	data := filepath.Join(dir, "data")

	if out, err := exec.Command(initdb, "-D", data, "-U", "postgres", "--auth=trust", "--encoding=UTF8", "--locale=C").CombinedOutput(); err != nil { //nolint:gosec // fixed argv
		_ = os.RemoveAll(dir)

		return nil, fmt.Errorf("initdb: %w\n%s", err, out)
	}

	port, err := freePort()
	if err != nil {
		_ = os.RemoveAll(dir)

		return nil, err
	}

	// 127.0.0.1 on a TCP port rather than a Unix socket: a path in a long
	// temp directory can exceed the socket path limit, and a client library
	// that validates its endpoint as host:port refuses a socket path.
	opts := fmt.Sprintf("-c listen_addresses=127.0.0.1 -c port=%d -c unix_socket_directories=%s "+
		"-c fsync=off -c synchronous_commit=off -c full_page_writes=off -c shared_buffers=32MB", port, dir)

	if out, err := exec.Command(pgctl, "-D", data, "-o", opts, "-l", filepath.Join(dir, "log"), "-w", "-t", "60", "start").CombinedOutput(); err != nil { //nolint:gosec // fixed argv
		logs, _ := os.ReadFile(filepath.Join(dir, "log"))
		_ = os.RemoveAll(dir)

		return nil, fmt.Errorf("pg_ctl start: %w\n%s\n%s", err, out, logs)
	}

	u := &url.URL{Scheme: "postgres", User: url.User("postgres"), Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), Path: "/postgres"}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()

	return &Server{admin: u, stop: func() {
		_ = exec.Command(pgctl, "-D", data, "-m", "immediate", "stop").Run() //nolint:gosec // fixed argv
		_ = os.RemoveAll(dir)
	}}, nil
}

// Stop stops the server and removes its data.
func (s *Server) Stop() { s.stop() }

// Current is the server Main started; tests call it after TestMain ran.
func Current() *Server { return current }

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()

	return l.Addr().(*net.TCPAddr).Port, nil
}

// DB is a database created for one test, with the means to connect to it as
// any role.
type DB struct {
	Name  string
	admin *url.URL
}

// NewDatabase creates an empty database for the calling test and drops it
// when the test ends.
func (s *Server) NewDatabase(t *testing.T) *DB {
	t.Helper()

	name := fmt.Sprintf("t%d_%d", time.Now().UnixNano()%1_000_000_000, counter.Add(1))

	admin, err := sql.Open("pgx", s.admin.String())
	if err != nil {
		t.Fatalf("open the maintenance database: %v", err)
	}
	defer func() { _ = admin.Close() }()

	if _, err := admin.ExecContext(context.Background(), `CREATE DATABASE `+name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}

	t.Cleanup(func() {
		c, err := sql.Open("pgx", s.admin.String())
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()

		_, _ = c.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	})

	return &DB{Name: name, admin: s.admin}
}

// URL is a connection URL for the database as the named role ("" is the
// superuser). The server trusts every local connection, so there is no
// password: the roles under test are separated by what they may DO, which is
// what the tests ask.
func (d *DB) URL(role string) string {
	u := *d.admin
	u.Path = "/" + d.Name

	if role != "" {
		u.User = url.User(role)
	}

	return u.String()
}

// Open connects to the database as the named role ("" is the superuser).
func (d *DB) Open(t *testing.T, role string) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", d.URL(role))
	if err != nil {
		t.Fatalf("open %s as %q: %v", d.Name, role, err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("connect to %s as %q: %v", d.Name, role, err)
	}

	return db
}

// IsPermissionDenied reports whether err is PostgreSQL's insufficient
// privilege (SQLSTATE 42501), the way a refused DDL reads.
func IsPermissionDenied(err error) bool {
	return err != nil && strings.Contains(err.Error(), "42501")
}
