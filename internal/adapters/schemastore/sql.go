package schemastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

const createSQLMigrationsTable = `
CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations (
    component  VARCHAR(255) NOT NULL,
    version    INT          NOT NULL,
    applied_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (component, version)
)`

// PlaceholderStyle controls the SQL parameter placeholder syntax.
type PlaceholderStyle int

const (
	// DollarPlaceholder uses $1, $2, ... (PostgreSQL / TimescaleDB style).
	DollarPlaceholder PlaceholderStyle = iota
	// QuestionPlaceholder uses ?, ?, ... (MySQL/SQLite style).
	QuestionPlaceholder
)

// secondSQLPlaceholder is the index for the second SQL parameter.
const secondSQLPlaceholder = 2

// SQLMigrator implements Migrator for standard SQL databases.
type SQLMigrator struct {
	db          *sql.DB
	placeholder PlaceholderStyle
	logger      *slog.Logger
}

// NewSQLMigrator creates a SQLMigrator for the given database.
func NewSQLMigrator(db *sql.DB, placeholder PlaceholderStyle, logger *slog.Logger) *SQLMigrator {
	return &SQLMigrator{db: db, placeholder: placeholder, logger: logger}
}

// Migrate ensures the migrations table exists and applies any outstanding migrations for the given component.
//
//nolint:dupl // identical loop logic to ClickHouseMigrator/TDEngineMigrator; each has its own DDL and record strategy
func (m *SQLMigrator) Migrate(ctx context.Context, component string, migrations []Migration) error {
	if _, err := m.db.ExecContext(ctx, createSQLMigrationsTable); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	current, err := m.currentVersion(ctx, component)
	if err != nil {
		return fmt.Errorf("query current version: %w", err)
	}

	var errs []error
	for _, mg := range migrations {
		if mg.Version <= current {
			continue
		}
		m.logger.InfoContext(
			ctx, "applying migration",
			slog.String("component", component),
			slog.Int("version", mg.Version),
			slog.String("desc", mg.Description),
		)

		if upErr := mg.Up(ctx); upErr != nil {
			errs = append(errs, fmt.Errorf("migration %d (%s): %w", mg.Version, mg.Description, upErr))
			break
		}

		if recErr := m.recordVersion(ctx, component, mg.Version); recErr != nil {
			errs = append(errs, fmt.Errorf("record migration %d: %w", mg.Version, recErr))
			break
		}
	}

	return errors.Join(errs...)
}

func (m *SQLMigrator) ph(n int) string {
	if m.placeholder == QuestionPlaceholder {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

func (m *SQLMigrator) currentVersion(ctx context.Context, component string) (int, error) {
	var version int
	q := fmt.Sprintf(
		`SELECT COALESCE(MAX(version), 0) FROM meterlogger_schema_migrations WHERE component = %s`,
		m.ph(1),
	)
	return version, m.db.QueryRowContext(ctx, q, component).Scan(&version)
}

func (m *SQLMigrator) recordVersion(ctx context.Context, component string, version int) error {
	//nolint:gosec // G201: placeholder values are controlled by ph() which returns only "?" or "$N"
	q := fmt.Sprintf(
		`INSERT INTO meterlogger_schema_migrations (component, version) VALUES (%s, %s)`,
		m.ph(1), m.ph(secondSQLPlaceholder),
	)
	_, err := m.db.ExecContext(ctx, q, component, version)
	return err
}
