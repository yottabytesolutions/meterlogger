# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [1.5.2] - 2026-08-02

### Fixed

- Per-inverter electrical fields (added in 1.5.0) landed as zeros. The
  `/ivp/pdm/device_data` response mixes scalar keys (`deviceCount`,
  `deviceDataLimit`) in at the top level alongside the device entries, so
  decoding into `map[string]DeviceDataDevice` failed on the first number and the
  reader fell back to empty electrical fields. The decoder now reads the body as
  raw messages and keeps only the entries that parse as a device, skipping the
  scalars and non-device values. Verified live against the gateway: all panels
  report DC/AC voltage and current, frequency, temperature, and energy again.
- A service that hit its consecutive-error threshold terminated the process with
  SIGTERM, which the normal shutdown path handled and exited 0, making a fatal
  condition look like a clean stop to Kubernetes and alerting. The process now
  exits non-zero when a service terminates on an unrecoverable error.

## [1.5.1] - 2026-08-02

### Fixed

- Per-inverter rows are no longer written repeatedly between microinverter
  reports. Panels report over powerline roughly every five minutes, staggered,
  so a poll interval shorter than that previously stored many identical
  `<measurement>_inverters` rows per panel (same report time, same values). The
  solar service now stores exactly one row per panel per new report, keyed on
  the panel report time, regardless of poll rate. The gateway aggregate is
  unchanged and still stored on every poll. State is in memory and resets on
  restart, so the first poll after a restart writes each panel's current report
  once even if unchanged.

## [1.5.0] - 2026-08-02

### Added

- Per-inverter electrical data from the Enphase Envoy `/ivp/pdm/device_data`
  endpoint, merged into the `<measurement>_inverters` rows: DC and AC voltage
  and current, AC frequency, panel temperature, leading and lagging reactive
  power, energy counters (today, yesterday, week, lifetime), and powerline
  link quality (RSSI, ISSI). Milli-units from the Envoy are converted to base
  units (V, A, Hz) and joules to watt-hours. Owner credentials suffice; no
  installer token is required. The joined device status is now also stored in
  the SQL sinks, not only QuestDB. New columns are added automatically by solar
  schema migration v2. A `device_data` fetch failure degrades gracefully: the
  gateway aggregate and per-panel watts still collect, with zero electrical
  fields, so older firmware without the endpoint keeps working.

## [1.4.0] - 2026-08-02

### Fixed

- QuestDB heat power and maximum power were stored three orders of magnitude
  too small: the writer still divided by the milliwatt units of the M-Bus
  library used before the gombus migration. Existing QuestDB heat history
  shows a step change at this release.

### Added

- Belgian (Fluvius eMUCS) grid meter support: version line on `0-0:96.1.4`,
  gas subdevices on `0-n:24.2.3` (volume not temperature corrected), decimal
  phase currents, and peak demand (capaciteitstarief) fields stored in three
  new grid columns: `avg_demand`, `max_demand_month`, `max_demand_month_at`
  (grid schema migration v2, added automatically).
- Water and thermal meter readings from the grid meter's P1 port
  (`Grid.Water.Enabled`, `Grid.Thermal.Enabled`), alongside the existing gas
  support. Water meters (device types 6 and 7, common on Belgian Fluvius
  installs) store to `Grid.Water.Measurement` (default `water_meter`);
  heat and cooling meters (device types 4, 10, 11, 12) store to
  `Grid.Thermal.Measurement` (default `thermal_meter`). Readings are
  deduplicated on the meter-supplied capture time, exactly like gas. Slave
  e-meters (device type 2) are never stored from the master's telegram: read
  them from their own P1 port.
- Encrypted DLMS telegram support for Luxembourgish Smarty and Austrian
  Sagemcom T210-D meters (EVN, Energienetze Steiermark) via
  `Grid.DecryptionKey` and `Grid.AuthenticationKey`. Frames are AES-128-GCM
  decrypted and fed through the normal telegram path. Telegrams with energy
  totals only (`1.8.0`/`2.8.0`) and the `0-0:42.0.0` equipment id are
  accepted. Wiener Netze raw DLMS push is not supported.
- SML reader for German electricity meters (EMH eHZ and mMe4.0, ISKRA
  MT681, EasyMeter Q3A/Q3B, eBZ DD3 SM variant, Holley DTZ541) over an IR
  read head, selected with `Grid.Reader: sml` (default stays `dsmr`).
  Accepts both the standard X-25 frame CRC and the Holley Kermit variant.
  Works with factory-state meters that send only the energy total.
- MQTT sink (`MQTT.Enabled`) that publishes every reading as flat JSON on
  `<TopicPrefix>/<measurement>` and announces all sensors to Home Assistant
  via retained MQTT discovery messages: one device per meter, correct
  device/state classes and units, availability via a last-will status topic.
  Grid, gas, and solar sensors slot straight into the Home Assistant Energy
  dashboard; heat energy is published in kWh (converted from joules) for the
  same reason. Water and thermal subdevice readings are announced too. See documentation/deployment.md, section "Home Assistant".


## [1.3.0] - 2026-08-01

### Added

- Optical (IR eye) reader for Kamstrup Multical heat meters via the KMP
  protocol, selected with `Heat.Reader: optical` (default stays `mbus`).
  Supports the KMP generation (Multical 402, 403, 601, 602, 603, 801, 803).
- Optical reader for the pre-KMP Kamstrup Multical 401 and 66C, selected
  with `Heat.Reader: optical401`. Sends the ASCII poll at 300 baud and reads
  the fixed ten-field telegram at 1200 baud. Value scaling is configurable
  via the `Heat.Optical401` keys; defaults fit common Dutch district heating
  installs.
- Gas meter readings from the grid meter's P1 port (`Grid.Gas.Enabled`). The
  gas meter is attached to the electricity meter over M-Bus; the grid source
  stores its readings to a separate table (`Grid.Gas.Measurement`, default
  `gas_meter`), deduplicated on the meter-supplied capture time.
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
