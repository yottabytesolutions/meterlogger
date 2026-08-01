package sqlsink

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
)

type migrator interface {
	Migrate(ctx context.Context, component string, migrations []schemastore.Migration) error
}

const (
	typeBigInt = "BIGINT"
	typeInt    = "INT"
	typeDouble = "DOUBLE"
)

// Dialect captures everything that differs between the SQL sink backends.
// Its name doubles as health check name, migration ledger key prefix and
// log message prefix, so it must stay stable for existing deployments.
type Dialect struct {
	name        string
	displayName string
	driver      string
	placeholder schemastore.PlaceholderStyle
	typeName    func(columnKind) string
	notNull     bool
	// quoteChar wraps column identifiers in DDL and inserts so reserved
	// words like the duco "show" column work on every dialect.
	quoteChar   string
	newMigrator func(db *sql.DB, logger *slog.Logger) migrator
	postCreate  func(ctx context.Context, db *sql.DB, table string) error
}

func (d Dialect) placeholders(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		if d.placeholder == schemastore.QuestionPlaceholder {
			b.WriteByte('?')
		} else {
			b.WriteString("$" + strconv.Itoa(i))
		}
	}
	return b.String()
}

func postgresTypeName(k columnKind) string {
	switch k {
	case kindTimestamp:
		return "TIMESTAMPTZ"
	case kindText, kindShortText:
		return "TEXT"
	case kindDouble:
		return "DOUBLE PRECISION"
	case kindBigInt:
		return typeBigInt
	case kindInt:
		return typeInt
	case kindBool:
		return "BOOLEAN"
	}
	panic(fmt.Sprintf("unknown column kind %d", k))
}

func mysqlTypeName(k columnKind) string {
	switch k {
	case kindTimestamp:
		return "DATETIME(6)"
	case kindText:
		return "VARCHAR(255)"
	case kindShortText:
		return "VARCHAR(50)"
	case kindDouble:
		return typeDouble
	case kindBigInt:
		return typeBigInt
	case kindInt:
		return typeInt
	case kindBool:
		return "TINYINT(1)"
	}
	panic(fmt.Sprintf("unknown column kind %d", k))
}

func tdengineTypeName(k columnKind) string {
	switch k {
	case kindTimestamp:
		return "TIMESTAMP"
	case kindText:
		return "NCHAR(255)"
	case kindShortText:
		return "NCHAR(64)"
	case kindDouble:
		return typeDouble
	case kindBigInt:
		return typeBigInt
	case kindInt:
		return typeInt
	case kindBool:
		return "BOOL"
	}
	panic(fmt.Sprintf("unknown column kind %d", k))
}

func newSQLMigrator(placeholder schemastore.PlaceholderStyle) func(db *sql.DB, logger *slog.Logger) migrator {
	return func(db *sql.DB, logger *slog.Logger) migrator {
		return schemastore.NewSQLMigrator(db, placeholder, logger)
	}
}

// PostgresDialect returns the dialect for plain PostgreSQL.
func PostgresDialect() Dialect {
	return Dialect{
		name:        "postgres",
		displayName: "PostgreSQL",
		driver:      "pgx",
		placeholder: schemastore.DollarPlaceholder,
		typeName:    postgresTypeName,
		notNull:     true,
		quoteChar:   `"`,
		newMigrator: newSQLMigrator(schemastore.DollarPlaceholder),
		postCreate:  nil,
	}
}

// TimescaleDBDialect returns the PostgreSQL dialect with TimescaleDB's
// component keys and a post-create hook that turns tables into hypertables.
func TimescaleDBDialect() Dialect {
	d := PostgresDialect()
	d.name = "timescaledb"
	d.displayName = "TimescaleDB"
	d.postCreate = createHypertable
	return d
}

func createHypertable(ctx context.Context, db *sql.DB, table string) error {
	// table name comes from config, not user HTTP input.
	_, err := db.ExecContext(
		ctx,
		fmt.Sprintf(`SELECT create_hypertable('%s', 'ts', if_not_exists => TRUE)`, table),
	)
	return err
}

// MySQLDialect returns the dialect for MySQL.
func MySQLDialect() Dialect {
	return Dialect{
		name:        "mysql",
		displayName: "MySQL",
		driver:      "mysql",
		placeholder: schemastore.QuestionPlaceholder,
		typeName:    mysqlTypeName,
		notNull:     true,
		quoteChar:   "`",
		newMigrator: newSQLMigrator(schemastore.QuestionPlaceholder),
		postCreate:  nil,
	}
}

// TDEngineDialect returns the dialect for TDEngine via its REST driver.
func TDEngineDialect() Dialect {
	return Dialect{
		name:        "tdengine",
		displayName: "TDEngine",
		driver:      "taosRestful",
		placeholder: schemastore.QuestionPlaceholder,
		typeName:    tdengineTypeName,
		notNull:     false,
		quoteChar:   "`",
		newMigrator: func(db *sql.DB, logger *slog.Logger) migrator {
			return schemastore.NewTDEngineMigrator(db, logger)
		},
		postCreate: nil,
	}
}

// quoteIdent wraps a column name in the dialect's identifier quotes.
func (d Dialect) quoteIdent(name string) string {
	return d.quoteChar + name + d.quoteChar
}
