package schemastore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

const createTDEngineMigrationsTable = `
CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations (
    ts        TIMESTAMP,
    component NCHAR(64),
    version   INT
)`

// TDEngineMigrator implements Migrator for TDEngine databases.
type TDEngineMigrator struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewTDEngineMigrator creates a TDEngineMigrator for the given database.
func NewTDEngineMigrator(db *sql.DB, logger *slog.Logger) *TDEngineMigrator {
	return &TDEngineMigrator{db: db, logger: logger}
}

// Migrate ensures the migrations table exists and applies any outstanding migrations for the given component.
// Calls are serialized process-wide via migrateMu.
func (m *TDEngineMigrator) Migrate(ctx context.Context, component string, migrations []Migration) error {
	migrateMu.Lock()
	defer migrateMu.Unlock()

	if _, err := m.db.ExecContext(ctx, createTDEngineMigrationsTable); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	current, err := m.currentVersion(ctx, component)
	if err != nil {
		return fmt.Errorf("query current version: %w", err)
	}

	return runMigrations(ctx, m.logger, component, migrations, current, m.recordVersion)
}

func (m *TDEngineMigrator) currentVersion(ctx context.Context, component string) (int, error) {
	var v sql.NullInt32
	q := `SELECT MAX(version) FROM meterlogger_schema_migrations WHERE component = ?`
	if err := m.db.QueryRowContext(ctx, q, component).Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int32), nil
}

func (m *TDEngineMigrator) recordVersion(ctx context.Context, component string, version int) error {
	q := `INSERT INTO meterlogger_schema_migrations (ts, component, version) VALUES (NOW(), ?, ?)`
	_, err := m.db.ExecContext(ctx, q, component, version)
	return err
}
