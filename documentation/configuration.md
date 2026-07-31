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
  SerialInterface: /dev/ttyUSB0
  MbusAddress: 1               # M-Bus device address (1 to 250)
  ScrapeInterval: 30s

# ── Grid meter (DSMR P1 over serial) ───────────────────────
Grid:
  Enabled: true
  Measurement: grid_meter
  SerialInterface: /dev/ttyUSB1
  # No ScrapeInterval: the meter pushes data every second

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
HEAT_SERIALINTERFACE=/dev/ttyUSB0
HEAT_MBUSADDRESS=1
HEAT_SCRAPEINTERVAL=30s

GRID_ENABLED=true
GRID_SERIALINTERFACE=/dev/ttyUSB1

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
| `Heat.SerialInterface` | string   | e.g. `/dev/ttyUSB0`                 |
| `Heat.MbusAddress`     | int      | M-Bus device address, typically `1` |
| `Heat.ScrapeInterval`  | duration | How often to poll the meter         |
| `Heat.Measurement`     | string   | Table name in all enabled sinks     |

### Grid

| Key                    | Type   | Notes                           |
|------------------------|--------|---------------------------------|
| `Grid.Enabled`         | bool   | Set `true` to activate          |
| `Grid.SerialInterface` | string | e.g. `/dev/ttyUSB1`             |
| `Grid.Measurement`     | string | Table name in all enabled sinks |

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
