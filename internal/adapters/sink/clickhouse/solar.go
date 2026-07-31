package clickhouse

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// SolarStore implements domain.EnvoySolarRepository for ClickHouse.
type SolarStore struct {
	db     *DB
	table  string
	logger *slog.Logger
	buf    batchBuffer[domain.EnvoySolarData]
}

// NewSolarStore creates and migrates a SolarStore.
func NewSolarStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*SolarStore, error) {
	m := schemastore.NewClickHouseMigrator(db.db, logger)
	if err := m.Migrate(ctx, "clickhouse_solar_"+table, solarMigrations(db.db, table)); err != nil {
		return nil, fmt.Errorf("clickhouse solar migration: %w", err)
	}
	return &SolarStore{db: db, table: table, logger: logger}, nil
}

// StoreEnvoySolarData buffers solar data for ClickHouse.
func (s *SolarStore) StoreEnvoySolarData(ctx context.Context, d domain.EnvoySolarData) error {
	warnDropped(ctx, s.logger, s.table, s.buf.add(d))
	return nil
}

// Flush performs a batch insert into ClickHouse. On failure the batch is
// re-queued for the next flush.
func (s *SolarStore) Flush(ctx context.Context) error {
	batch := s.buf.take()
	if len(batch) == 0 {
		return nil
	}
	if err := s.insertBatch(ctx, batch); err != nil {
		warnDropped(ctx, s.logger, s.table, s.buf.requeue(batch))
		return err
	}
	return nil
}

func (s *SolarStore) insertBatch(ctx context.Context, batch []domain.EnvoySolarData) error {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clickhouse transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Table names come from config, not user input.
	solarStmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s (ts, envoy_serial, production_wh, watt, panel_count) VALUES (?,?,?,?,?)`,
			s.table,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare solar insert: %w", err)
	}
	defer func() { _ = solarStmt.Close() }()

	invStmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s_inverters
        (ts, envoy_serial, inverter_serial, channel_id, operating, communicating,
         producing, phase, watts, peak_watts)
        VALUES (?,?,?,?,?,?,?,?,?,?)`, s.table,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare inverter insert: %w", err)
	}
	defer func() { _ = invStmt.Close() }()

	for _, d := range batch {
		_, err = solarStmt.ExecContext(ctx, d.ReadingTime, d.EnvoySerial, d.ProductionWh, d.Watt, d.PanelCount)
		if err != nil {
			return fmt.Errorf("exec solar batch insert: %w", err)
		}

		for _, inv := range d.Inverters {
			_, err = invStmt.ExecContext(
				ctx,
				inv.ReportTime, d.EnvoySerial, inv.SerialNumber, inv.Chaneid,
				inv.Operating, inv.Communicating, inv.Producing,
				inv.Phase, inv.LastReportedWatts, inv.MaxReportWatts,
			)
			if err != nil {
				return fmt.Errorf("exec inverter batch insert: %w", err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit clickhouse solar batch: %w", err)
	}
	return nil
}

// Close flushes pending rows with a bounded timeout. The shared DB is closed
// via DB.Close().
func (s *SolarStore) Close() error { return closeWithFinalFlush(s.Flush) }
