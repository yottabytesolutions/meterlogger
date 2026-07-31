package schemastore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

const createClickHouseMigrationsTable = `
CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations (
    component  String,
    version    UInt32,
    applied_at DateTime DEFAULT now()
) ENGINE = MergeTree() ORDER BY (component, version)`

// ClickHouseMigrator implements Migrator for ClickHouse databases.
type ClickHouseMigrator struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewClickHouseMigrator creates a ClickHouseMigrator for the given database.
func NewClickHouseMigrator(db *sql.DB, logger *slog.Logger) *ClickHouseMigrator {
	return &ClickHouseMigrator{db: db, logger: logger}
}

// Migrate ensures the migrations table exists and applies any outstanding migrations for the given component.
// Calls are serialized process-wide via migrateMu.
func (m *ClickHouseMigrator) Migrate(ctx context.Context, component string, migrations []Migration) error {
	migrateMu.Lock()
	defer migrateMu.Unlock()

	if _, err := m.db.ExecContext(ctx, createClickHouseMigrationsTable); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	current, err := m.currentVersion(ctx, component)
	if err != nil {
		return fmt.Errorf("query current version: %w", err)
	}

	return runMigrations(ctx, m.logger, component, migrations, current, m.recordVersion)
}

func (m *ClickHouseMigrator) currentVersion(ctx context.Context, component string) (int, error) {
	var version int
	q := `SELECT COALESCE(MAX(version), 0) FROM meterlogger_schema_migrations WHERE component = ?`
	return version, m.db.QueryRowContext(ctx, q, component).Scan(&version)
}

func (m *ClickHouseMigrator) recordVersion(ctx context.Context, component string, version int) error {
	q := `INSERT INTO meterlogger_schema_migrations (component, version) VALUES (?, ?)`
	_, err := m.db.ExecContext(ctx, q, component, version)
	return err
}
