// Package migration creates the database objects this product owns.
//
// The interesting part is not AutoMigrate, it is WHO the objects end up
// belonging to. The migration connects as one role and creates objects as
// another, so that the account the migration happens to use can be replaced
// without orphaning a single table.
package migration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"github.com/truvity/policy/examples/url-shortener/internal/models"
)

// appSchemas are the schemas this product owns. They are named here and
// nowhere else: the models are schema-qualified (see models.URL.TableName),
// so a schema added to a model without being added here fails at migration
// time rather than at first write.
var appSchemas = []string{"urls", "stats"}

// pgIdent quotes a PostgreSQL identifier: wrapped in double quotes with
// embedded double quotes doubled. Go's %q is STRING escaping (backslash
// rules) and produces invalid or unsafe SQL for identifiers.
func pgIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Run creates this product's schemas and tables, and grants the runtime
// role what it needs on them.
//
// ownerRole owns everything the migration creates. The migration does its
// DDL under SET ROLE, so the objects belong to that role rather than to
// whichever account the migration connected as. That is the whole point: a
// migration is run by a person or a job with strong rights, and the objects
// it leaves behind must outlive that account.
//
// appRole is the role the services run as. It is deliberately NOT the owner
// and cannot issue DDL; it receives the table rights it needs and nothing
// else, including on tables a later migration adds. Empty means the
// services connect as the owner, which is acceptable for a development
// cluster and is not how a deployment should be configured.
func Run(ctx context.Context, logger *slog.Logger, db *gorm.DB, ownerRole, appRole string) error {
	if ownerRole == "" {
		return fmt.Errorf("no owner role was named, so the objects would belong to the migration's own account")
	}

	logger.InfoContext(ctx, "migrating", slog.String("owner_role", ownerRole))

	// Everything after this point is the owner's doing, not ours.
	if err := db.Exec("SET ROLE " + pgIdent(ownerRole)).Error; err != nil {
		return fmt.Errorf("set role %s: %w", ownerRole, err)
	}

	defer func() {
		if err := db.Exec("RESET ROLE").Error; err != nil {
			logger.ErrorContext(ctx, "could not reset the role after migrating",
				slog.String("error", err.Error()))
		}
	}()

	// The schemas come first, because the models are schema-qualified and
	// AutoMigrate will not create the schema a table names.
	for _, schema := range appSchemas {
		if err := db.Exec("CREATE SCHEMA IF NOT EXISTS " + pgIdent(schema)).Error; err != nil {
			return fmt.Errorf("create schema %s: %w", schema, err)
		}
	}

	if err := db.AutoMigrate(&models.URL{}, &models.Stat{}); err != nil {
		return fmt.Errorf("create the tables: %w", err)
	}

	// Still the owner, which is what makes the grants stick: the grantor
	// recorded is the owner, so ALTER DEFAULT PRIVILEGES also covers the
	// tables a FUTURE migration creates.
	if appRole != "" {
		if err := grantAppRole(ctx, logger, db, appRole); err != nil {
			return fmt.Errorf("grant to %s: %w", appRole, err)
		}
	}

	logger.InfoContext(ctx, "migrated")

	return nil
}

// grantAppRole gives the runtime role the rights it needs on this product's
// schemas, for the tables that exist now and for the ones a later migration
// will add. Every statement is idempotent, because a migration runs on
// every deployment and not only on the first one.
func grantAppRole(ctx context.Context, logger *slog.Logger, db *gorm.DB, appRole string) error {
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ?)", appRole).Scan(&exists).Error; err != nil {
		return fmt.Errorf("look for the role: %w", err)
	}

	if !exists {
		return fmt.Errorf("no such role in this database: the services would have no rights on what this migration just created")
	}

	for _, schema := range appSchemas {
		s, r := pgIdent(schema), pgIdent(appRole)
		stmts := []string{
			fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", s, r),
			fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %s TO %s", s, r),
			fmt.Sprintf("GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %s TO %s", s, r),
			fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s", s, r),
			fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT USAGE, SELECT ON SEQUENCES TO %s", s, r),
		}
		for _, stmt := range stmts {
			if err := db.Exec(stmt).Error; err != nil {
				return fmt.Errorf("%s: %w", stmt, err)
			}
		}
	}

	logger.InfoContext(ctx, "granted the runtime role its rights",
		slog.String("app_role", appRole))

	return nil
}
