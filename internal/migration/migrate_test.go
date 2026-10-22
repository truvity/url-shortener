package migration_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/truvity/url-shortener/internal/migration"
	"github.com/truvity/url-shortener/internal/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

const (
	ownerRole = "url_shortener_owner"
	appRole   = "url_shortener_app"
)

// migrated creates the two roles the platform provides (the owner, which the
// migration SETs ROLE to, and the runtime role), runs the migration as the
// superuser, and returns the database.
func migrated(t *testing.T) *pgtest.DB {
	t.Helper()

	db := pgtest.Current().NewDatabase(t)
	admin := db.Open(t, "")

	for _, role := range []string{ownerRole, appRole} {
		// Roles are cluster-wide: tolerate one a sibling test made.
		if _, err := admin.ExecContext(context.Background(),
			`DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '`+role+`') THEN CREATE ROLE `+role+` LOGIN; END IF; END $$`); err != nil {
			t.Fatalf("create role %s: %v", role, err)
		}
	}

	// The owner must be able to create what it owns in this database.
	if _, err := admin.ExecContext(context.Background(), `GRANT CREATE ON DATABASE `+db.Name+` TO `+ownerRole); err != nil {
		t.Fatalf("grant create to the owner: %v", err)
	}

	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: admin}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	if err := migration.Run(context.Background(), slog.Default(), gdb, ownerRole, appRole); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db
}

// TestMigrationCreatesWhatTheOwnerOwnsAndTheRuntimeRoleReads proves, against a
// real server, the two things only a database can answer and the cluster
// suite used to ask through a port-forward: the migration created the
// tables (the runtime role can read through them), and the tables belong to
// the owner role rather than to the account the migration connected as.
func TestMigrationCreatesWhatTheOwnerOwnsAndTheRuntimeRoleReads(t *testing.T) {
	db := migrated(t)

	app := db.Open(t, appRole)

	var regclass sql.NullString
	if err := app.QueryRowContext(context.Background(), `SELECT to_regclass('urls.urls')`).Scan(&regclass); err != nil {
		t.Fatalf("query urls.urls as the runtime role: %v", err)
	}

	if !regclass.Valid {
		t.Fatal("urls.urls does not exist: the migration did not create it")
	}

	if _, err := app.ExecContext(context.Background(), `SELECT count(*) FROM urls.urls`); err != nil {
		t.Fatalf("the runtime role cannot read urls.urls: %v", err)
	}

	var owner string
	if err := app.QueryRowContext(context.Background(),
		`SELECT tableowner FROM pg_tables WHERE schemaname = 'urls' AND tablename = 'urls'`).Scan(&owner); err != nil {
		t.Fatalf("read the table's owner: %v", err)
	}

	if owner != ownerRole {
		t.Fatalf("urls.urls belongs to %q, want the owner role %q: a table that belongs to the migration's own account dies with it", owner, ownerRole)
	}
}

// TestTheRuntimeRoleCannotIssueDDL is the role separation: a credential that
// can drop the table it serves reads from is a request-path right nobody
// meant to grant, and the migration exists to keep it off the runtime role.
func TestTheRuntimeRoleCannotIssueDDL(t *testing.T) {
	db := migrated(t)
	app := db.Open(t, appRole)

	for name, stmt := range map[string]string{
		"create a table in the product's schema": `CREATE TABLE urls.should_be_refused (id int)`,
		"drop the product's table":               `DROP TABLE urls.urls`,
		"alter the product's table":              `ALTER TABLE urls.urls ADD COLUMN should_be_refused int`,
	} {
		if _, err := app.ExecContext(context.Background(), stmt); !pgtest.IsPermissionDenied(err) {
			t.Errorf("the runtime role tried to %s: got %v, want a permission-denied error", name, err)
		}
	}

	// And it can write rows, which is the whole of what it is for.
	if _, err := app.ExecContext(context.Background(),
		`INSERT INTO urls.urls (id, url_key, long_url, created_at) VALUES ('h1', 'k1k1k1k1', 'https://example.com/a', now())`); err != nil {
		t.Fatalf("the runtime role cannot insert a row: %v", err)
	}
}

// TestMigratingTwiceChangesNothing: a migration runs on every deployment, not
// only the first, and the grants are idempotent.
func TestMigratingTwiceChangesNothing(t *testing.T) {
	db := migrated(t)
	admin := db.Open(t, "")

	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: admin}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	if err := migration.Run(context.Background(), slog.Default(), gdb, ownerRole, appRole); err != nil {
		t.Fatalf("the second run failed: %v", err)
	}
}

// TestAMissingRuntimeRoleIsRefused: granting rights to a role that does not
// exist would leave the services with no rights on what was just created.
func TestAMissingRuntimeRoleIsRefused(t *testing.T) {
	db := pgtest.Current().NewDatabase(t)
	admin := db.Open(t, "")

	if _, err := admin.ExecContext(context.Background(),
		`DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '`+ownerRole+`') THEN CREATE ROLE `+ownerRole+` LOGIN; END IF; END $$`); err != nil {
		t.Fatal(err)
	}

	if _, err := admin.ExecContext(context.Background(), `GRANT CREATE ON DATABASE `+db.Name+` TO `+ownerRole); err != nil {
		t.Fatal(err)
	}

	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: admin}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	if err := migration.Run(context.Background(), slog.Default(), gdb, ownerRole, "no_such_role_anywhere"); err == nil {
		t.Fatal("a missing runtime role was accepted")
	}
}
