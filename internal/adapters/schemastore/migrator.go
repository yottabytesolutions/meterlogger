// Package schemastore provides a unified schema migration interface for all database engines.
// Up functions are closures that capture the database connection; callers need not pass it explicitly.
package schemastore

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Migration is a single idempotent schema change.
type Migration struct {
	Version     int
	Description string
	Up          func(ctx context.Context) error
}

// Migrator applies versioned schema migrations for a named component.
type Migrator interface {
	Migrate(ctx context.Context, component string, migrations []Migration) error
}

// migrateMu serializes all Migrate calls in this process. Source goroutines migrate
// against the same shared database at startup; concurrent CREATE TABLE IF NOT EXISTS
// can fail on Postgres with a duplicate-key error on pg_type. Cross-process locking is
// out of scope: the deployment model is a single writer per database.
//
//nolint:gochecknoglobals // process-wide lock is the point; per-instance locks cannot serialize across migrators
var migrateMu sync.Mutex

// runMigrations applies every migration above current in order, stopping at the first
// failure. Recording an applied version is delegated to record, which each backend
// implements against its own ledger schema.
func runMigrations(
	ctx context.Context,
	logger *slog.Logger,
	component string,
	migrations []Migration,
	current int,
	record func(ctx context.Context, component string, version int) error,
) error {
	for _, mg := range migrations {
		if mg.Version <= current {
			continue
		}
		logger.InfoContext(
			ctx, "applying migration",
			slog.String("component", component),
			slog.Int("version", mg.Version),
			slog.String("desc", mg.Description),
		)

		if err := mg.Up(ctx); err != nil {
			return fmt.Errorf("migration %d (%s): %w", mg.Version, mg.Description, err)
		}

		if err := record(ctx, component, mg.Version); err != nil {
			return fmt.Errorf("record migration %d: %w", mg.Version, err)
		}
	}
	return nil
}
