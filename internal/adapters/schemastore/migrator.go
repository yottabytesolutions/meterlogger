// Package schemastore provides a unified schema migration interface for all database engines.
// Up functions are closures that capture the database connection; callers need not pass it explicitly.
package schemastore

import "context"

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
