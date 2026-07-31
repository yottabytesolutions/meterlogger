# Architecture

> Related: [configuration.md](./configuration.md) · [data-model.md](./data-model.md) · [deployment.md](./deployment.md)

## Overview

MeterLogger follows a clean architecture with three explicit layers:

```mermaid
flowchart TB
    cmd["cmd/meterlogger\nwiring"]
    service["internal/service\nbusiness logic & orchestration"]
    domain["internal/domain\ninterfaces + data types"]
    adapters["internal/adapters\nI/O: serial, HTTP, databases"]
    cmd --> service
    service --> domain
    adapters --> domain
```

The domain layer defines **what** the system does (interfaces). The adapter layer defines **how** it connects to the
outside world (implementations). The service layer connects them.

Services depend only on `internal/domain` interfaces - they never import an adapter package directly. This is the core
architectural invariant.

---

## Package map

```mermaid
flowchart TD
    subgraph cmd["cmd/meterlogger/"]
        main["main.go\nCobra CLI · signal handling · meter-type routing"]
        db["db.go\ninitDBs() - shared DB connections"]
        helper["helper.go\ninterruptAwareContext() · doWork()"]
    end

    subgraph domain["internal/domain/"]
        dgrid["grid.go\nGridTelegram · GridTelegramReader · GridTelegramRepository"]
        dheat["heat.go\nHeatTelegram · HeatMeterReader · HeatMeterRepository"]
        dsolar["solar.go\nEnvoySolarData · InverterDetails · EnvoySolarReader · EnvoySolarRepository"]
        dvent["ventilation.go\nDucoBoxStatus · node structs · DucoReader · DucoRepository"]
    end

    subgraph service["internal/service/"]
        svc["service.go - Service interface"]
        svcgrid["gridloggingservice.go"]
        svcheat["heatloggingservice.go"]
        svcsolar["solarloggingservice.go"]
        svcduco["ducoservice.go"]
    end

    subgraph internal["internal/"]
        config["config/ - Config types · Load() · Validate()"]
        telemetry["telemetry/ - InitTracing() · InitProfiling()"]
        healthsrv["healthserver/ - /healthz · /readyz · /metrics"]
        metricslib["metrics/ - Prometheus counter/gauge definitions"]
        tracedslog["tracedslog/ - slog handler: injects traceID+spanID into logs"]
    end

    subgraph adapters["internal/adapters/"]
        subgraph sources["Sources"]
            gridreader["source/gridmeter/gridreader.go\nDSMR P1 serial"]
            mbusreader["source/serialmbus/mbusreader.go\nM-Bus serial via gombus client"]
            mbusconv["source/serialmbus/converters/gombus.go"]
            envoy["source/enphase/envoyreader.go\nEnphase HTTP API"]
            token["source/enphase/token.go\nJWT management"]
            duco["source/ducobox/ducobox.go\nDucoBox HTTP API"]
        end
        subgraph sinks["Sinks"]
            qdbclient["sink/qdb/common.go - DBClient"]
            qdbwriters["sink/qdb/*_writer.go"]
            sqlsink["sink/sqlsink/ - shared SQL store logic"]
            pgstore["sink/postgres/ - PostgreSQL dialect"]
            mystore["sink/mysql/ - MySQL dialect"]
            tsstore["sink/timescaledb/ - TimescaleDB dialect"]
            chstore["sink/clickhouse/ - ClickHouse"]
            tdstore["sink/tdengine/ - TDEngine dialect"]
            multisink["sink/multisink/ - fan-out wrapper"]
            stdout["sink/stdout/stdoutsink.go - debug"]
        end
        subgraph schemastore["internal/adapters/schemastore/"]
            schema["schemastore/ - shared migration framework"]
        end
    end

    mbusreader --> mbusconv
    envoy --> token
    svcgrid --> dgrid
    svcheat --> dheat
    svcsolar --> dsolar
    svcduco --> dvent
    main --> svc
    main --> config
    main --> telemetry
    main --> db
    main --> helper
    main --> healthsrv
    main --> metricslib
    main --> tracedslog
    pgstore --> schema
    mystore --> schema
    tsstore --> schema
    chstore --> schema
    tdstore --> schema
```

---

## Dependency graph

```mermaid
flowchart LR
    main["main.go"]

    subgraph heat["heat"]
        mbus["serialmbus.NewReader()"]
        conv["serialmbus/converters\ngombus → domain"]
        mbus --> conv
    end

    subgraph grid["grid"]
        greader["gridmeter.NewGridReader()"]
    end

    subgraph solar["solar"]
        envoy["enphase.NewEnvoyReader()"]
        jwt["enphase/token\nJWT management"]
        envoy --> jwt
    end

    subgraph vent["ventilation"]
        duco["ducobox.NewDucoReader()"]
    end

    subgraph shared["all meter types - sinks"]
        qdbclient["qdb.NewDBClient()"]
        qdbwriter["qdb.*Writer / qdb.*Repository"]
        pg["postgres.*Store"]
        my["mysql.*Store"]
        ts["timescaledb.*Store"]
        ch["clickhouse.*Store"]
        td["tdengine.*Store"]
        ms["multisink.*Repository\n(fan-out)"]
        qdbclient --> qdbwriter
        qdbwriter --> ms
        pg --> ms
        my --> ms
        ts --> ms
        ch --> ms
        td --> ms
    end

    svc["service.New*LoggingService()\n↓\nservice.Start(ctx)\n[uses domain interfaces only]"]
    main --> mbus
    main --> greader
    main --> envoy
    main --> duco
    main --> qdbclient
    main --> pg
    main --> my
    main --> ts
    main --> ch
    main --> td
    main --> svc
```

---

## Service lifecycle

Every meter type uses the same startup sequence:

```go
// 1. Construct reader adapter
reader := someadapter.NewReader(...)

// 2. Construct sink stores + fan-out repository
store1 := qdb.NewXxxWriter(client, tableName, logger)
store2 := postgres.NewXxxStore(ctx, pgDB, tableName, logger)
repo := multisink.NewXxxRepository([]domain.XxxRepository{store1, store2}, logger)

// 3. Construct service
svc := service.NewXxxLoggingService(reader, repo, intervals, logger)

// 4. Block until context is cancelled
startService(ctx, logger, "Human readable name", svc)

// 5. Deferred cleanup
defer repo.Close()
```

`startService` runs `svc.Start(ctx)` in a goroutine and waits for it to finish via a `sync.WaitGroup`.

---

## Polling & flushing

Every service uses two `time.Ticker` values:

| Ticker        | Controls                              | Typical interval                 |
|---------------|---------------------------------------|----------------------------------|
| `ticker`      | How often to read from the source     | 30s to 5min                      |
| `flushTicker` | How often to flush the QuestDB buffer | configurable via `FlushInterval` |

The QuestDB client accumulates records in memory (line protocol buffer) and sends them in bulk on flush. If flushing
fails, the service sends `SIGTERM` to itself to trigger a clean shutdown.

---

## Error handling strategy

| Situation                                   | Behaviour                                                        |
|---------------------------------------------|------------------------------------------------------------------|
| Fatal startup error (port open, DB connect) | `log.Fatal` - exits immediately                                  |
| Read error (heat/solar/grid)                | Logs error, increments `read_errors_total`, retries on next tick |
| Ventilation read/store error                | Logs error, sends `SIGTERM`, returns from service loop           |
| Store error (heat/solar/grid)               | Logs error, sends `SIGTERM`, returns from service loop           |
| Flush error                                 | Logs error, sends `SIGTERM`, returns from service loop           |
| Context cancelled (SIGINT/SIGTERM)          | All services stop cleanly, QuestDB connection flushed and closed |

The design is intentionally fail-fast. It relies on the container restart policy (`restart: always` in docker-compose)
to recover from transient hardware errors.

---

## Design patterns

### Interface-based adapter pattern

The domain layer defines minimal interfaces:

```go
// internal/domain/heat.go
type HeatMeterReader interface {
ReadHeatTelegram() (HeatTelegram, error)
}

type HeatMeterRepository interface {
StoreHeatTelegram(ctx context.Context, telegram HeatTelegram) error
Flush(ctx context.Context) error
}
```

Services are constructed with these interfaces. This makes the `stdout` adapter, the `qdb` adapter, and any future
adapter interchangeable without changing service code.

### Constructor injection

All structs are constructed via `NewXxx(...)` functions. There are no global state mutations after startup.

### Context propagation

`context.Context` flows from `main.go` down through services into every repository call. The context is cancelled on OS
signal, which unwinds all blocking select loops.

### Fail-fast with SIGTERM

When a service encounters a non-recoverable error it calls:

```go
_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
return
```

This triggers the signal handler in `interruptAwareContext()`, which cancels the root context, causing all services to
exit cleanly (including flushing the QuestDB buffer).

### Multi-sink fan-out

`internal/adapters/sink/multisink/` wraps a slice of repositories and forwards every write concurrently to all of them.
A single write failure is logged but does not block the other sinks. This lets you write to QuestDB, PostgreSQL, and
ClickHouse simultaneously with a single `StoreXxx` call from the service layer.
