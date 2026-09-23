// Package migration provides database migration functionality with proper role switching.
// It uses SET ROLE to create tables as the owner role, enabling clean pulumi destroy.
package migration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"github.com/truvity/policy/examples/url-shortener/internal/models"
)

// appSchemas are the schemas the app models live in (urls.urls /
// stats.stats) — the grant surface for the runtime role.
var (
	appSchemas = []string{"urls", "stats"}
)

// pgIdent quotes a PostgreSQL identifier: wrapped in double quotes with
// embedded double quotes doubled. Go's %q is STRING escaping (backslash
// rules) and produces invalid or unsafe SQL for identifiers.
func pgIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// RunMigrations executes database migrations with proper role switching.
// It uses SET ROLE to create tables as the owner role, ensuring tables are
// owned by the owner (not the migration user), which enables clean pulumi destroy.
//
// The ownerRole parameter should be the name of the PostgreSQL owner role
// (e.g., "stack-owner"). This is typically loaded from the PG_DATABASE_*_OWNER_ROLE
// environment variable.
//
// appRole, when non-empty, is the DDL-incapable runtime role (the
// cnpg-cluster v2 cert role) that receives DML grants on the app schemas
// after the DDL lands; grants run as the owner, so ALTER DEFAULT
// PRIVILEGES covers future owner-created tables too. A missing role is
// skipped with a warning — pre-v2 ring2 installs have no runtime role.
//
// Example:
//
//	err := migration.RunMigrations(ctx, logger, db, "o-tsarev-sl-us-owner", "app")
func RunMigrations(ctx context.Context, logger *slog.Logger, db *gorm.DB, ownerRole, appRole string) error {
	if ownerRole == "" {
		return fmt.Errorf("owner role is required for migration")
	}

	logger.InfoContext(ctx, "starting database migration",
		slog.String("owner_role", ownerRole),
	)

	// SET ROLE to owner for DDL - tables will be created as owner
	if err := db.Exec("SET ROLE " + pgIdent(ownerRole)).Error; err != nil {
		return fmt.Errorf("failed to set role to %s: %w", ownerRole, err)
	}

	// Ensure we reset role even if migration fails
	defer func() {
		if err := db.Exec("RESET ROLE").Error; err != nil {
			logger.ErrorContext(ctx, "failed to reset role after migration",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Run AutoMigrate - tables created as owner
	logger.InfoContext(ctx, "running AutoMigrate for URL and Stat models")
	if err := db.AutoMigrate(&models.URL{}, &models.Stat{}); err != nil {
		return fmt.Errorf("failed to run AutoMigrate: %w", err)
	}

	// Still under SET ROLE owner: the owner owns the schemas and tables,
	// so it is the right grantor, and ALTER DEFAULT PRIVILEGES records
	// against the owner for future migrations' tables.
	if appRole != "" {
		if err := grantAppRole(ctx, logger, db, appRole); err != nil {
			return fmt.Errorf("grant app role: %w", err)
		}
	}

	logger.InfoContext(ctx, "database migration completed successfully")
	return nil
}

// grantAppRole gives the runtime role DML on the app schemas. Idempotent
// (GRANT re-runs are no-ops). Skips with a warning when the role does not
// exist — ring2 installs predating cnpg-cluster v2 declare no runtime
// role, and the migration must keep working there.
func grantAppRole(ctx context.Context, logger *slog.Logger, db *gorm.DB, appRole string) error {
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ?)", appRole).Scan(&exists).Error; err != nil {
		return fmt.Errorf("check role %s: %w", appRole, err)
	}

	if !exists {
		logger.WarnContext(ctx, "app role does not exist, skipping grants (pre-v2 ring2 install?)",
			slog.String("app_role", appRole),
		)

		return nil
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

	logger.InfoContext(ctx, "granted app role on app schemas",
		slog.String("app_role", appRole),
	)

	return nil
}
