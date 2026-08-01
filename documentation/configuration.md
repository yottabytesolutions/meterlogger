# Configuration

> Related: [architecture.md](./architecture.md) · [deployment.md](./deployment.md) · [meter-types.md](./meter-types.md)

MeterLogger is configured via a YAML file (default: `~/.meterlogger.yaml`) with optional overrides from environment
variables and CLI flags. [Viper](https://github.com/spf13/viper) is used for loading; keys are case-insensitive.

A ready-to-copy starting point covering every source and sink is provided at
[`config.example.yaml`](../config.example.yaml) in the repo root:

```sh
cp config.example.yaml config.yaml
# edit config.yaml, then:
meterlogger --config ./config.yaml --source grid
```

## Precedence order (highest → lowest)

1. CLI flags (`--envoy-url`, `--debug`)
2. Environment variables
3. Config file
4. Built-in defaults

---

## Enabling and disabling sources

Each source (meter type) has an `Enabled` flag. All sources with `Enabled: true` start concurrently in the same
process. At least one source must be enabled; the process exits with a fatal error if none are.

---

## Enabling and disabling sinks

Each sink (database backend) also has an `Enabled` flag. Every enabled source writes to **all** enabled sinks
simultaneously. If one sink fails for a measurement, the error is logged and the other sinks continue receiving data.

At least one sink must be enabled. The process exits with a fatal error if all sinks are disabled.

> **Breaking change:** `QuestDB.Enabled` no longer defaults to `true`. Every sink must now be explicitly enabled
> with `Enabled: true`. Old configs that rely on the implicit default will start with no sinks and exit immediately.
> Add `QuestDB: { Enabled: true }` (or the equivalent YAML block) to restore the previous behaviour.

### Supported sinks

| Config key    | Backend                      | Default port |   Auto-migration   |
|---------------|------------------------------|:------------:|:------------------:|
| `QuestDB`     | QuestDB (ILP/TCP)            |     9009     | automatic via ILP  |
| `Postgres`    | PostgreSQL                   |     5432     |        yes         |
| `MySQL`       | MySQL / MariaDB              |     3306     |        yes         |
| `TimescaleDB` | TimescaleDB (PostgreSQL ext) |     5432     |        yes         |
| `ClickHouse`  | ClickHouse                   |     9000     |        yes         |
| `TDEngine`    | TDEngine                     |     6041     |        yes         |
| `Stdout`      | Log output (debug only)      |      -       |        n/a         |

The `Stdout` sink logs every record instead of persisting it. Use it to inspect meter data during
setup or debugging. It is not for production.

---

## Schema management

When a SQL sink (PostgreSQL, MySQL, TimescaleDB, ClickHouse, or TDEngine) is enabled, MeterLogger automatically creates
and migrates the required tables on startup. The app must be granted schema management rights (`CREATE TABLE`, `INSERT`,
`ALTER TABLE`) on the target database.

Migration history is tracked in `meterlogger_schema_migrations` - do not drop this table.

---

## Health and metrics server

A small HTTP server exposes Kubernetes-friendly endpoints:

| Endpoint   | Purpose                                                                                                       |
|------------|---------------------------------------------------------------------------------------------------------------|
| `/healthz` | Liveness probe - 503 if any registered sink has been failing for `HTTPServer.LivenessFailureThreshold` (90s) |
| `/readyz`  | Readiness probe - 503 if any registered sink is currently down                                                |
| `/metrics` | Prometheus metrics in text exposition format                                                                  |

Port defaults to `8080`, configurable via `HTTPServer.Port`. The liveness threshold defaults to `90s`,
configurable via `HTTPServer.LivenessFailureThreshold`. See
[observability.md - Liveness detail](./observability.md#liveness-detail) for the rationale.

### Prometheus metrics

| Metric                                    | Type    | Labels           |
|-------------------------------------------|---------|------------------|
| `meterlogger_reads_total`                 | counter | `source`         |
| `meterlogger_read_errors_total`           | counter | `source`         |
| `meterlogger_writes_total`                | counter | `sink`, `source` |
| `meterlogger_write_errors_total`          | counter | `sink`, `source` |
| `meterlogger_last_read_timestamp_seconds` | gauge   | `source`         |

---

## Full annotated config file

```yaml
# Enable debug-level logging.
Debug: false

# How often to flush buffered data to QuestDB.
# Uses Go duration syntax: 5s, 1m, 500ms, etc.
FlushInterval: 10s

# ── HTTP health / metrics server ────────────────────────────
HTTPServer:
  Port: 8080                       # /healthz, /readyz, /metrics
  LivenessFailureThreshold: 90s    # /healthz flips to 503 once any sink has
                                   # been continuously unhealthy this long.
                                   # /readyz still flips on the first failure.

# ── QuestDB sink ─────────────────────────────────────────────
# Enabled defaults to false; every sink must be enabled explicitly.
QuestDB:
  Enabled: true
  Host: questdb              # hostname or IP
  Port: 9009                 # ILP (InfluxDB line protocol) TCP port
  User: admin
  Password: quest

# ── PostgreSQL sink ──────────────────────────────────────────
# Tables are created/migrated automatically on startup.
Postgres:
  Enabled: false
  Host: localhost
  Port: 5432
  User: meterlogger
  Password: secret
  Database: meterlogger
  SSLMode: disable           # disable | require | verify-full

# ── MySQL sink ───────────────────────────────────────────────
# Tables are created/migrated automatically on startup.
MySQL:
  Enabled: false
  Host: localhost
  Port: 3306
  User: meterlogger
  Password: secret
  Database: meterlogger

# ── TimescaleDB sink ─────────────────────────────────────────
# PostgreSQL-compatible; uses the same schema as Postgres.
# Tables are created/migrated automatically on startup.
TimescaleDB:
  Enabled: false
  Host: localhost
  Port: 5432
  User: meterlogger
  Password: secret
  Database: meterlogger
  SSLMode: disable           # disable | require | verify-full

# ── ClickHouse sink ───────────────────────────────────────────
# Tables are created/migrated automatically on startup.
ClickHouse:
  Enabled: false
  Host: localhost
  Port: 9000
  User: default
  Password: ""
  Database: meterlogger

# ── TDEngine sink ─────────────────────────────────────────────
# Tables are created/migrated automatically on startup.
TDEngine:
  Enabled: false
  Host: localhost
  Port: 6041
  User: root
  Password: taosdata
  Database: meterlogger

# ── OpenTelemetry tracing ──────────────────────────────────
# Emit traces to an OTLP/gRPC collector. Logs auto-correlate via traceID/spanID.
OTEL:
  Enabled: false
  CollectorAddr: "otel-collector:4317"  # gRPC, no TLS
  ServiceName: "meterlogger-heat"

# ── Continuous profiling (Grafana Pyroscope) ────────────────
Profiling:
  Enabled: false
  ServerAddr: "http://pyroscope:4040"
  ServiceName: "meterlogger-heat"
  BasicAuthUser: ""
  BasicAuthPassword: ""

# ── Heat meter (M-Bus over serial) ─────────────────────────
Heat:
  Enabled: true
  Measurement: heat_meter      # table name in all enabled sinks
  Reader: mbus                 # mbus (default), optical, or optical401
  SerialInterface: /dev/ttyUSB0
  MbusAddress: 1               # M-Bus device address (1 to 250), mbus reader only
  ScrapeInterval: 30s

# Alternative: Kamstrup Multical over the optical (IR) eye. The IR head is a
# USB serial device; the link runs at 1200 baud, 8 data bits, no parity,
# 2 stop bits (fixed in code). See meter-types.md for supported models.
# Heat:
#   Enabled: true
#   Measurement: heat_meter
#   Reader: optical
#   SerialInterface: /dev/ttyUSB0
#   ScrapeInterval: 30s

# Alternative: Kamstrup Multical 401 or 66C (pre-KMP) over the optical eye.
# The meter is battery powered; keep ScrapeInterval at 5m or longer to spare
# the battery. The Optical401 scaling depends on the meter's configuration
# code; the defaults fit common Dutch district heating installs. Verify one
# reading against the meter LCD and adjust the decimals if values are off by
# a factor of ten. See meter-types.md for the protocol details.
# Heat:
#   Enabled: true
#   Measurement: heat_meter
#   Reader: optical401
#   SerialInterface: /dev/ttyUSB0
#   ScrapeInterval: 5m
#   Optical401:
#     EnergyUnit: GJ             # GJ (default), kWh, or MWh
#     EnergyDecimals: 3          # raw/1000 GJ
#     VolumeDecimals: 3          # raw/1000 m3
#     PowerDecimals: 1           # raw/10 kW
#     FlowDecimals: 1            # raw/10 l/h

# ── Grid meter (DSMR P1 over serial) ───────────────────────
Grid:
  Enabled: true
  Measurement: grid_meter
  SerialInterface: /dev/ttyUSB1
  # No ScrapeInterval: the meter pushes data every second
  # (Luxembourg Smarty meters push every 10 seconds)

  # Only for encrypted meters (Luxembourg Smarty, Austrian Sagemcom T210-D):
  # the 32-hex-char decryption key from your grid operator. Leave unset for
  # plaintext meters (NL, BE).
  # DecryptionKey: "00000000000000000000000000000000"
  # AuthenticationKey defaults to the fixed Luxembourg AK; Austrian users
  # override it with their GAK when their operator uses one.
  # AuthenticationKey: "00112233445566778899AABBCCDDEEFF"  # gitleaks:allow public spec constant

  # Gas meter readings carried in the same P1 telegrams. Not a standalone
  # source: one process reads both power and gas from the P1 port.
  Gas:
    Enabled: false
    Measurement: gas_meter       # table name in all enabled sinks

# ── Solar (Enphase Envoy HTTP API) ─────────────────────────
Enphase:
  Enabled: false               # disable if no Enphase system is present
  Measurement: solar
  EnvoyURL: https://192.168.1.100
  User: owner@example.com
  Password: secret
  Serial: 122140012345         # Envoy gateway serial number
  ScrapeInterval: 5m

# ── Ventilation (DucoBox HTTP API) ─────────────────────────
Ventilation:
  Enabled: false               # disable if no DucoBox is present
  MeasurementBaseName: ventilation
  ScrapeInterval: 1m
  HostURL: http://192.168.1.200:8080
  Nodes:
    - 1
    - 2
    - 3
```

---

## Environment variable mapping

Every config key can be set via an environment variable. No config file is needed; an env-only
setup works. Key rules:

- Dots replaced with underscores: `QuestDB.Host` → `QUESTDB_HOST`
- Case-insensitive match, but uppercase is conventional

Common overrides for Docker/Kubernetes deployments:

```env
QUESTDB_ENABLED=true
QUESTDB_HOST=questdb
QUESTDB_PORT=9009
QUESTDB_USER=admin
QUESTDB_PASSWORD=secret

POSTGRES_ENABLED=true
POSTGRES_HOST=postgres
POSTGRES_PORT=5432
POSTGRES_USER=meterlogger
POSTGRES_PASSWORD=secret
POSTGRES_DATABASE=meterlogger
POSTGRES_SSLMODE=require

MYSQL_ENABLED=true
MYSQL_HOST=mysql
MYSQL_PORT=3306
MYSQL_USER=meterlogger
MYSQL_PASSWORD=secret
MYSQL_DATABASE=meterlogger

TIMESCALEDB_ENABLED=true
TIMESCALEDB_HOST=timescaledb
TIMESCALEDB_PORT=5432
TIMESCALEDB_USER=meterlogger
TIMESCALEDB_PASSWORD=secret
TIMESCALEDB_DATABASE=meterlogger
TIMESCALEDB_SSLMODE=require

CLICKHOUSE_ENABLED=true
CLICKHOUSE_HOST=clickhouse
CLICKHOUSE_PORT=9000
CLICKHOUSE_USER=default
CLICKHOUSE_PASSWORD=
CLICKHOUSE_DATABASE=meterlogger

TDENGINE_ENABLED=true
TDENGINE_HOST=tdengine
TDENGINE_PORT=6041
TDENGINE_USER=root
TDENGINE_PASSWORD=taosdata
TDENGINE_DATABASE=meterlogger

HEAT_ENABLED=true
HEAT_READER=mbus
HEAT_SERIALINTERFACE=/dev/ttyUSB0
HEAT_MBUSADDRESS=1
HEAT_SCRAPEINTERVAL=30s

GRID_ENABLED=true
GRID_SERIALINTERFACE=/dev/ttyUSB1
GRID_GAS_ENABLED=true
GRID_GAS_MEASUREMENT=gas_meter

FLUSHINTERVAL=10s
HTTPSERVER_PORT=8080
```

---

## CLI flags

| Flag          | Short | Description                                                   |
|---------------|-------|---------------------------------------------------------------|
| `--config`    |       | Path to config file (overrides default `~/.meterlogger.yaml`) |
| `--envoy-url` | `-e`  | Override `Enphase.EnvoyURL` from config                       |
| `--debug`     | `-d`  | Enable debug logging                                          |
| `--source`    | `-s`  | Run only this source: `heat`, `grid`, `solar`, `ventilation`  |

---

## Configuration fields per source

### Heat

| Key                    | Type     | Notes                               |
|------------------------|----------|-------------------------------------|
| `Heat.Enabled`         | bool     | Set `true` to activate              |
| `Heat.Reader`          | string   | `mbus` (default), `optical` (Kamstrup KMP IR eye), or `optical401` (Multical 401/66C IR eye) |
| `Heat.SerialInterface` | string   | e.g. `/dev/ttyUSB0`                 |
| `Heat.MbusAddress`     | int      | M-Bus device address, default `1`; `mbus` reader only |
| `Heat.ScrapeInterval`  | duration | How often to poll the meter         |
| `Heat.Measurement`     | string   | Table name in all enabled sinks     |

The `Heat.Optical401` keys apply to the `optical401` reader only. The Multical 401/66C sends
bare digit fields; the unit and decimal position depend on the meter's CCC configuration code
and cannot be read from the telegram. The defaults fit common Dutch district heating installs.
Calibration: verify one reading against the meter LCD and adjust the decimals if a value is
off by a factor of ten.

| Key                                | Type   | Notes                                      |
|------------------------------------|--------|--------------------------------------------|
| `Heat.Optical401.EnergyUnit`       | string | `GJ` (default), `kWh`, or `MWh`            |
| `Heat.Optical401.EnergyDecimals`   | int    | 0 to 4, default `3` (raw/1000 GJ)          |
| `Heat.Optical401.VolumeDecimals`   | int    | 0 to 4, default `3` (raw/1000 m3)          |
| `Heat.Optical401.PowerDecimals`    | int    | 0 to 4, default `1` (raw/10 kW)            |
| `Heat.Optical401.FlowDecimals`     | int    | 0 to 4, default `1` (raw/10 l/h)           |

### Grid

| Key                      | Type   | Notes                                                        |
|--------------------------|--------|--------------------------------------------------------------|
| `Grid.Enabled`           | bool   | Set `true` to activate                                       |
| `Grid.SerialInterface`   | string | e.g. `/dev/ttyUSB1`                                          |
| `Grid.Measurement`       | string | Table name in all enabled sinks                              |
| `Grid.DecryptionKey`     | string | 32 hex chars. Enables decryption of encrypted DLMS telegrams (Luxembourg, Austria). Empty (default) reads plaintext only |
| `Grid.AuthenticationKey` | string | 32 hex chars. Default `00112233445566778899AABBCCDDEEFF`, the fixed Luxembourg AK. Austrian users set their GAK |

#### Encrypted meters (Luxembourg, Austria)

Luxembourgish Smarty meters and Austrian Sagemcom T210-D meters push AES-GCM
encrypted DLMS frames instead of plaintext telegrams. Set `Grid.DecryptionKey`
to the 16-byte key (32 hex characters) for your meter:

- **Luxembourg**: request the key from your DSO (Creos and the other Smarty
  operators hand it out to the registered customer, via their customer portal
  or support desk). Leave `Grid.AuthenticationKey` at its default; all Smarty
  meters share the fixed AK.
- **Austria (EVN, Energienetze Steiermark)**: request the key through your
  grid operator's customer portal (EVN calls it the key for the
  Kundenschnittstelle). If your operator also hands out a global
  authentication key (GAK), set it as `Grid.AuthenticationKey`.

Without a configured key the reader fails fast on the first encrypted frame
with a message telling you to request the key. Belgian meters need no
configuration at all: the Fluvius telegram dialect is detected automatically.

#### Grid.Gas

Many Dutch installations have the gas meter attached to the electricity meter over M-Bus. The gas
readings then arrive on the P1 port, inside the same telegrams the grid source already reads. Set
`Grid.Gas.Enabled: true` to store them.

This is **not** a standalone source. One process reads both power and gas from the P1 port; there is
no `--source gas` value and no separate serial connection. Gas readings only flow when the grid
source runs (via `Grid.Enabled: true` or `--source grid`).

| Key                    | Type   | Default     | Notes                           |
|------------------------|--------|-------------|---------------------------------|
| `Grid.Gas.Enabled`     | bool   | `false`     | Set `true` to store gas readings |
| `Grid.Gas.Measurement` | string | `gas_meter` | Table name in all enabled sinks |

```yaml
Grid:
  Enabled: true
  Measurement: grid_meter
  SerialInterface: /dev/ttyUSB1
  Gas:
    Enabled: true
    Measurement: gas_meter
```

The gas meter updates its value every 5 minutes (DSMR 5) or hourly (DSMR 4 and older), while the
telegram repeats the last capture far more often. A row is stored only when the meter reports a new
capture, so expect one row per 5 minutes or per hour, not one per second. See
[data-model.md](./data-model.md#gas_meter-configurable-name) for the table layout and
[meter-types.md](./meter-types.md#m-bus-subdevices-gas) for the protocol details.

### Solar (Enphase)

| Key                      | Type     | Notes                                    |
|--------------------------|----------|------------------------------------------|
| `Enphase.Enabled`        | bool     | Set `true` to activate                   |
| `Enphase.EnvoyURL`       | string   | Full base URL including scheme           |
| `Enphase.User`           | string   | Enlighten cloud account email            |
| `Enphase.Password`       | string   | Enlighten cloud account password         |
| `Enphase.Serial`         | string   | Envoy gateway serial (printed on device) |
| `Enphase.ScrapeInterval` | duration | 5m is a sensible default                 |
| `Enphase.Measurement`    | string   | Table name in all enabled sinks          |

### Ventilation (DucoBox)

| Key                               | Type     | Notes                                  |
|-----------------------------------|----------|----------------------------------------|
| `Ventilation.Enabled`             | bool     | Set `true` to activate                 |
| `Ventilation.HostURL`             | string   | DucoBox HTTP base URL                  |
| `Ventilation.Nodes`               | []int    | Node IDs to poll (see DucoBox web UI)  |
| `Ventilation.ScrapeInterval`      | duration | How often to poll                      |
| `Ventilation.MeasurementBaseName` | string   | Prefix for tables in all enabled sinks |

---

## Configuration fields per sink

### QuestDB

| Key                | Type   | Default | Notes                   |
|--------------------|--------|---------|-------------------------|
| `QuestDB.Enabled`  | bool   | `false` | Must be set explicitly  |
| `QuestDB.Host`     | string |         | Hostname or IP          |
| `QuestDB.Port`     | int    | 9009    | ILP TCP port            |
| `QuestDB.User`     | string |         |                         |
| `QuestDB.Password` | string |         |                         |

### PostgreSQL

| Key                 | Type   | Default   | Notes                               |
|---------------------|--------|-----------|-------------------------------------|
| `Postgres.Enabled`  | bool   | `false`   |                                     |
| `Postgres.Host`     | string |           |                                     |
| `Postgres.Port`     | int    | 5432      |                                     |
| `Postgres.User`     | string |           |                                     |
| `Postgres.Password` | string |           |                                     |
| `Postgres.Database` | string |           |                                     |
| `Postgres.SSLMode`  | string | `disable` | `disable`, `require`, `verify-full` |

### MySQL

| Key              | Type   | Default | Notes |
|------------------|--------|---------|-------|
| `MySQL.Enabled`  | bool   | `false` |       |
| `MySQL.Host`     | string |         |       |
| `MySQL.Port`     | int    | 3306    |       |
| `MySQL.User`     | string |         |       |
| `MySQL.Password` | string |         |       |
| `MySQL.Database` | string |         |       |

### TimescaleDB

| Key                    | Type   | Default   | Notes                               |
|------------------------|--------|-----------|-------------------------------------|
| `TimescaleDB.Enabled`  | bool   | `false`   |                                     |
| `TimescaleDB.Host`     | string |           |                                     |
| `TimescaleDB.Port`     | int    | 5432      |                                     |
| `TimescaleDB.User`     | string |           |                                     |
| `TimescaleDB.Password` | string |           |                                     |
| `TimescaleDB.Database` | string |           |                                     |
| `TimescaleDB.SSLMode`  | string | `disable` | `disable`, `require`, `verify-full` |

### ClickHouse

| Key                   | Type   | Default   | Notes |
|-----------------------|--------|-----------|-------|
| `ClickHouse.Enabled`  | bool   | `false`   |       |
| `ClickHouse.Host`     | string |           |       |
| `ClickHouse.Port`     | int    | 9000      |       |
| `ClickHouse.User`     | string | `default` |       |
| `ClickHouse.Password` | string |           |       |
| `ClickHouse.Database` | string |           |       |

### TDEngine

| Key                 | Type   | Default    | Notes         |
|---------------------|--------|------------|---------------|
| `TDEngine.Enabled`  | bool   | `false`    |               |
| `TDEngine.Host`     | string |            |               |
| `TDEngine.Port`     | int    | 6041       | REST API port |
| `TDEngine.User`     | string | `root`     |               |
| `TDEngine.Password` | string | `taosdata` |               |
| `TDEngine.Database` | string |            |               |

### Stdout (debug)

| Key              | Type | Default | Notes                                          |
|------------------|------|---------|------------------------------------------------|
| `Stdout.Enabled` | bool | `false` | Logs records instead of persisting them        |

Debug sink. Not for production.

---

## Observability configuration

### OTEL (OpenTelemetry tracing)

Enables distributed tracing via OTLP/gRPC. Traces are emitted for each read+store cycle. When a span is active,
`traceID` and `spanID` are automatically added to every structured log line, enabling log-to-trace correlation in
Grafana, Jaeger, or any compatible backend.

| Key                  | Type   | Default | Notes                                           |
|----------------------|--------|---------|-------------------------------------------------|
| `OTEL.Enabled`       | bool   | `false` | Set `true` to enable tracing export             |
| `OTEL.CollectorAddr` | string |         | gRPC endpoint, e.g. `otel-collector:4317`       |
| `OTEL.ServiceName`   | string |         | Service name in traces, e.g. `meterlogger-heat` |

Config file:

```yaml
OTEL:
  Enabled: true
  CollectorAddr: "otel-collector:4317"
  ServiceName: "meterlogger-heat"
```

Environment variables:

```env
OTEL_ENABLED=true
OTEL_COLLECTORADDR=otel-collector:4317
OTEL_SERVICENAME=meterlogger-heat
```

### Profiling (Grafana Pyroscope)

Enables continuous profiling via the Grafana Pyroscope Go SDK. Pushes CPU, allocation, goroutine, mutex, and block
profiles. The SDK is pure Go and works with the scratch Docker image without changes.

| Key                           | Type   | Default | Notes                                              |
|-------------------------------|--------|---------|----------------------------------------------------|
| `Profiling.Enabled`           | bool   | `false` | Set `true` to enable profiling export              |
| `Profiling.ServerAddr`        | string |         | Pyroscope server URL, e.g. `http://pyroscope:4040` |
| `Profiling.ServiceName`       | string |         | Application name in Pyroscope                      |
| `Profiling.BasicAuthUser`     | string |         | Leave empty if no auth required                    |
| `Profiling.BasicAuthPassword` | string |         | Leave empty if no auth required                    |

Config file:

```yaml
Profiling:
  Enabled: true
  ServerAddr: "http://pyroscope:4040"
  ServiceName: "meterlogger-heat"
  BasicAuthUser: ""
  BasicAuthPassword: ""
```

Environment variables:

```env
PROFILING_ENABLED=true
PROFILING_SERVERADDR=http://pyroscope:4040
PROFILING_SERVICENAME=meterlogger-heat
```
