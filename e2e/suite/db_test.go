// Database access, reached through the harness the same way every other
// Service in this suite is: (*harness.Cluster).ServiceURL opens a
// port-forward on the kind tier and returns "http://127.0.0.1:<port>" — the
// "http" is only the label the harness gives a local TCP endpoint it does
// not otherwise interpret; kubectl's port-forward tunnels the bytes of
// whatever protocol is spoken over it, and Postgres's wire protocol is what
// gets tunnelled here. Postgres is the box's own server, not something this
// chart renders (docs/decisions/0005-kind-is-the-gate.md), so it is not one
// of the components client.go's serviceURL knows how to name — this file
// reaches it directly, by hack/kind/postgres.yaml's own Service name.
package suite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

// postgresService and postgresNamespace name the box's one Postgres
// server's Service, addressed the same cross-namespace way
// examples/url-shortener/e2e/fixture/apply.sh already reaches it.
const (
	postgresService   = "postgres"
	postgresNamespace = "postgres"
	postgresPort      = 5432
)

// TestMigrationRanAndRolesAreSeparate proves two things through ONE
// connection each, both of which only a real database can answer:
//
//   - the migration Job completed: the owner role's own tables
//     (urls.urls) exist and the runtime role can read them;
//   - the runtime role really cannot do what the owner role can: DDL. A
//     credential that can drop the table it serves reads is a request-path
//     right nobody meant to grant, and internal/migration/migrate.go exists
//     specifically to keep it off that role.
func TestMigrationRanAndRolesAreSeparate(t *testing.T) {
	if shared.names.OwnerRole == shared.names.AppRole {
		t.Fatalf("the owner role and the app role resolved to the SAME name (%q) — there is no separation to test",
			shared.names.OwnerRole)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	appDB := openAsRole(ctx, t, shared.names.AppRole, shared.appPassword)
	defer func() { _ = appDB.Close() }()

	// The migration completed: the table the owner's migration created is
	// there, and the runtime role — which never ran a migration — can read
	// through it. `to_regclass` answers NULL for a table that does not
	// exist, rather than erroring, so this distinguishes "not migrated" from
	// a connection failure cleanly.
	var regclass sql.NullString
	if err := appDB.QueryRowContext(ctx, `SELECT to_regclass('urls.urls')`).Scan(&regclass); err != nil {
		t.Fatalf("query urls.urls as the app role: %v", err)
	}
	if !regclass.Valid {
		t.Fatalf("urls.urls does not exist — the migration Job did not complete")
	}

	// The runtime role cannot create a table. Postgres reports this as
	// SQLSTATE 42501 (insufficient_privilege); a role that COULD issue this
	// would also be able to drop urls.urls, which is the request-path right
	// migrate.go's SET ROLE dance exists to withhold.
	_, err := appDB.ExecContext(ctx, `CREATE TABLE urls.e2e_should_be_refused (id int)`)
	if err == nil {
		_, _ = appDB.ExecContext(ctx, `DROP TABLE urls.e2e_should_be_refused`)
		t.Fatalf("the app role (%s) was able to CREATE TABLE — it is not separated from the owner role", shared.names.AppRole)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("CREATE TABLE as the app role failed with %v, wanted a permission-denied error", err)
	}
}

// openAsRole opens a *sql.DB against the box's Postgres, through the
// harness, authenticated as role — and fails the test immediately (rather
// than on first query) if the connection itself does not work, so a broken
// forward reads as a clear setup failure instead of a confusing query error.
func openAsRole(ctx context.Context, t *testing.T, role, password string) *sql.DB {
	t.Helper()

	dsn := postgresDSN(ctx, t, role, password)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open the database as %s: %v", role, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("%s", wrapDBErr(fmt.Errorf("ping as %s: %w", role, err)))
	}
	return db
}

// postgresDSN resolves the box's Postgres address through the harness — the
// same ServiceURL every other Service in this suite goes through — and
// renders a DSN from it. Built with net/url rather than fmt.Sprintf: the
// generated password apply.sh draws (base64 of random bytes) can contain
// characters a DSN must percent-encode.
func postgresDSN(ctx context.Context, t *testing.T, role, password string) string {
	t.Helper()

	raw, err := shared.cluster.ServiceURL(ctx, postgresNamespace, postgresService, postgresPort)
	if err != nil {
		t.Fatalf("resolve the postgres Service: %v", err)
	}
	hostport := strings.TrimPrefix(raw, "http://")

	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(role, password),
		Host:   hostport,
		Path:   "/" + shared.names.Database,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()

	return u.String()
}

// wrapDBErr enriches a Postgres connection error with the forward it went
// through, the same way wrapThroughForward does for every HTTP call in this
// suite.
func wrapDBErr(err error) error {
	if fw, ok := shared.cluster.ForwardFor(postgresNamespace, postgresService); ok {
		return fw.Err(err)
	}
	return err
}
