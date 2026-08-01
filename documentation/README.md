# MeterLogger Documentation

MeterLogger is a Go service that reads data from utility meters and stores it in one or more time-series or relational
databases. It is designed to run as **one container per source** - each container handles exactly one meter type,
providing fault isolation and visible restarts. See [deployment.md](./deployment.md) for details.

## Documents in this folder

| File                                   | Contents                                                           |
|----------------------------------------|--------------------------------------------------------------------|
| [architecture.md](./architecture.md)   | Package structure, dependency graph, design patterns               |
| [configuration.md](./configuration.md) | All configuration options with annotated examples                  |
| [meter-types.md](./meter-types.md)     | Per-meter-type deep dive: protocols, hardware, wiring              |
| [data-model.md](./data-model.md)       | Domain types and database table schemas                            |
| [deployment.md](./deployment.md)       | Building, running locally, Docker, docker-compose, Kubernetes, OTel, Pyroscope |
| [observability.md](./observability.md) | Health endpoints, Prometheus metrics, tracing, profiling, alerting |
| [troubleshooting.md](./troubleshooting.md) | Common first-run problems and how to diagnose them             |

## Supported meter types

| Type          | Protocol               | Hardware interface     |
|---------------|------------------------|------------------------|
| `heat`        | M-Bus or optical (serial) | USB-to-M-Bus converter or IR read head |
| `grid`        | DSMR P1 (NL, BE, LU, AT) or SML (serial) | USB-to-P1 cable or IR read head |

| `solar`       | Enphase Envoy HTTP API | LAN / WiFi             |
| `ventilation` | DucoBox HTTP API       | LAN / WiFi             |

## Supported sinks

| Sink        | Type                  | Auto-migration    | Notes                                   |
|-------------|-----------------------|-------------------|-----------------------------------------|
| QuestDB     | time-series (ILP/TCP) | automatic via ILP | Must be explicitly enabled              |
| PostgreSQL  | relational            | yes               | Standard SQL; recommended for analytics |
| MySQL       | relational            | yes               |                                         |
| TimescaleDB | PostgreSQL extension  | yes               | Same schema as PostgreSQL               |
| ClickHouse  | column-store (OLAP)   | yes               | High-throughput analytics workloads     |
| TDEngine    | time-series           | yes               | Lightweight IoT time-series engine      |
| Stdout      | debug (log output)    | n/a               | `Stdout.Enabled: true`; not for production |

All enabled sinks receive every write concurrently. At least one sink must be enabled.

## Quick orientation

```
cmd/meterlogger/        ← main entrypoint, CLI wiring, config
internal/
  domain/               ← data types + reader/repository interfaces
  service/              ← polling loops, orchestration
  adapters/
    source/
      gridmeter/        ← DSMR P1 serial reader
      sml/              ← SML (German meters) IR serial reader
      serialmbus/       ← M-Bus serial reader + protocol layer
      kamstrup/         ← Kamstrup KMP optical reader
      multical401/      ← Multical 401/66C optical reader
      enphase/          ← Enphase Envoy HTTP reader
      ducobox/          ← DucoBox HTTP reader
    sink/
      qdb/              ← QuestDB sink
      postgres/         ← PostgreSQL sink (with auto-migration)
      mysql/            ← MySQL sink (with auto-migration)
      timescaledb/      ← TimescaleDB sink (with auto-migration)
      clickhouse/       ← ClickHouse sink (with auto-migration)
      tdengine/         ← TDEngine sink (with auto-migration)
      multisink/        ← fan-out: writes to all enabled sinks
      stdout/           ← debug sink (structured log output)
    schemastore/        ← shared schema migration framework (all engines)
internal/
  healthserver/         ← HTTP /healthz, /readyz, /metrics
  metrics/              ← Prometheus counter/gauge definitions
  tracedslog/           ← slog.Handler that injects traceID+spanID when a span is active
  debuglog/             ← conditional debug logging helper
```

The flow for every meter type is identical:

```
reader (adapter) → service (polling loop) → multisink → [QuestDB, PostgreSQL, MySQL, TimescaleDB, ClickHouse, TDEngine]
```

See [architecture.md](./architecture.md) for a full diagram and [configuration.md](./configuration.md) for all
configuration options.
