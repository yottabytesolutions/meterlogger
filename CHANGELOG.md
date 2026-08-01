# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- Optical (IR eye) reader for Kamstrup Multical heat meters via the KMP
  protocol, selected with `Heat.Reader: optical` (default stays `mbus`).
  Supports the KMP generation (Multical 402, 403, 601, 602, 603, 801, 803);
  the MC 66C and MC 401 use different optical protocols and are not
  supported.
- Systemd template unit and setup recipe for running sources without
  containers (`deploy/systemd/`).

## [1.2.0] - 2026-08-01

### Fixed

- The MySQL ventilation sink failed its schema migration because the node
  table has a column named `show`, a reserved word. All generated SQL now
  quotes column identifiers per dialect.
- The ClickHouse solar and ventilation sinks silently lost data: the driver
  supports one prepared batch per transaction, so flushes spanning several
  tables dropped every batch after the first. Each table now flushes in its
  own transaction. Both bugs were caught by the new integration tests.

### Added

- `meterlogger validate` checks the configuration and exits nonzero on problems;
  `--ping` also connects to every enabled sink and reports per-sink health.
- `meterlogger probe --source <name>` takes a single reading from one source and
  prints it as JSON, for commissioning and hardware checks.
- Kubernetes deployment example in `deploy/kubernetes.yaml` with probe wiring
  and serial device access notes.
- Weekly govulncheck scan and an integration test suite that runs the SQL and
  ClickHouse sinks against real databases in CI.
- CI enforces the 80 percent coverage floor.

### Changed

- Release notes are generated from this changelog; tagging a version without a
  changelog entry fails the release.
- Agent instructions moved from CLAUDE.md to AGENTS.md.

## [1.1.1] - 2026-07-31

### Fixed

- `Heat.MbusAddress` defaults to `1`, the address the reader always polled
  before it became configurable, so configs without the key keep working.

## [1.1.0] - 2026-07-31

### Fixed

- The heat reader now polls the configured `Heat.MbusAddress` instead of always
  polling address 1.
- ClickHouse no longer loses buffered rows when a flush fails or the process
  stops; batches are re-queued and flushed on close.
- A serial stream ending (EOF) on the grid meter now restarts the container
  instead of leaving a healthy-looking process with no data flow.
- One failing sink no longer kills the ventilation service immediately; all
  services tolerate transient errors the same way.
- Undecodable meter values (BCD filler) surface as errors instead of zeros.
- Database writes carry a timeout so a hung connection cannot stall a service.
- Passwords with special characters work in every sink connection string.

### Changed

- The Enphase cloud login verifies TLS certificates. Local Envoy requests still
  accept the device's self-signed certificate.
- M-Bus reads use the gombus client with incremental frame reassembly; readings
  arrive about ten seconds sooner per cycle.
- `Heat.MbusAddress` must be 1 to 250; other values fail at startup.
- The four SQL sinks share one implementation; schema migrations and table
  layouts are unchanged.
- Environment variables can configure every key without a config file, for
  example `QUESTDB_ENABLED=true`.
- `meterlogger --version` prints the build version.

### Added

- Stdout debug sink (`Stdout.Enabled`) that logs readings instead of storing them.

## [1.0.0] - 2026-07-30

First public, open-source release.

- Reads heat meters (M-Bus/serial), grid meters (DSMR P1/serial), solar production
  (Enphase Envoy HTTP API), and ventilation data (DucoBox HTTP API).
- Stores readings to QuestDB, Postgres, MySQL, ClickHouse, TDEngine, TimescaleDB, or stdout,
  with all enabled sinks written to concurrently.
- Ports-and-adapters architecture: domain core has no IO dependencies, adapters implement
  domain interfaces for each source and sink.
- Exposes Prometheus metrics and `/healthz`/`/readyz` HTTP endpoints for liveness and readiness.
