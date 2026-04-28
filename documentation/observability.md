# Observability

> Related: [deployment.md](./deployment.md) · [configuration.md](./configuration.md)

MeterLogger exposes health, readiness, and metrics endpoints out of the box.
Every container instance - one per source - has its own independent HTTP server.

## HTTP endpoints

The health server starts automatically on `HTTPServer.Port` (default `8080`).

| Endpoint   | Method | Description                                                                                                        |
|------------|--------|--------------------------------------------------------------------------------------------------------------------|
| `/healthz` | GET    | **Liveness** - returns `200 OK` as long as the process is running                                                  |
| `/readyz`  | GET    | **Readiness** - returns `200 OK` if all enabled sinks are reachable; `503 Service Unavailable` if any sink is down |
| `/metrics` | GET    | Prometheus metrics in text exposition format                                                                       |

### Readiness detail

`/readyz` checks reachability of every enabled sink. SQL sinks (PostgreSQL, MySQL, TimescaleDB, ClickHouse,
TDEngine) use a database ping issued over the shared connection pool - no fresh TCP connection or DNS lookup
is required. QuestDB reports the outcome of the most recent `Flush` against its persistent ILP/TCP sender;
this reuses the already-open connection instead of dialling a new one per probe. Each check runs with a
1-second timeout; if any sink fails, the endpoint returns `503 Service Unavailable` with details in the JSON
body.

---

## Prometheus metrics

Scrape `/metrics` with Prometheus. All metrics carry a `source` label (heat, grid, solar, ventilation)
and a `sink` label where applicable.

| Metric                                    | Type    | Labels           | Description                                |
|-------------------------------------------|---------|------------------|--------------------------------------------|
| `meterlogger_reads_total`                 | Counter | `source`         | Total successful reads from the source     |
| `meterlogger_read_errors_total`           | Counter | `source`         | Total read errors (triggers restart)       |
| `meterlogger_writes_total`                | Counter | `sink`, `source` | Total successful writes to a sink          |
| `meterlogger_write_errors_total`          | Counter | `sink`, `source` | Total write errors per sink                |
| `meterlogger_last_read_timestamp_seconds` | Gauge   | `source`         | Unix timestamp of the last successful read |

### Example Prometheus scrape config

```yaml
scrape_configs:
  - job_name: meterlogger
    static_configs:
      - targets:
          - meterlogger-heat:8080
          - meterlogger-grid:8080
          - meterlogger-solar:8080
          - meterlogger-ventilation:8080
```

### Useful alerts

```yaml
# Source has not produced a reading in 5 minutes
- alert: MeterloggerSourceStale
  expr: time() - meterlogger_last_read_timestamp_seconds > 300
  labels:
    severity: warning
  annotations:
    summary: "{{ $labels.source }} has not read in {{ $value | humanizeDuration }}"

# Read errors are occurring
- alert: MeterloggerReadErrors
  expr: rate(meterlogger_read_errors_total[5m]) > 0
  labels:
    severity: warning
  annotations:
    summary: "{{ $labels.source }} is producing read errors"

# Write errors to a specific sink
- alert: MeterloggerWriteErrors
  expr: rate(meterlogger_write_errors_total[5m]) > 0
  labels:
    severity: warning
  annotations:
    summary: "{{ $labels.source }} → {{ $labels.sink }} write errors"
```

---

## Container health as an observability signal

Because each source runs in its own container with `restart: always`, **a restarting container is
itself an observable health event** - no Prometheus needed for basic fault detection.

```sh
# Docker: see restart counts and status
docker ps --format "table {{.Names}}\t{{.Status}}"
# meterlogger-heat         Up 2 hours (healthy)
# meterlogger-grid         Up 2 hours (healthy)
# meterlogger-solar        Restarting (1) 3 seconds ago
# meterlogger-ventilation  Up 2 hours (healthy)

# Kubernetes: see restart counts
kubectl get pods -l app=meterlogger
# meterlogger-solar-6d9b4   0/1  CrashLoopBackOff  4  8m
```

This approach surfaces failures at the infrastructure level, where operators already look, without
requiring a running metrics stack. Prometheus adds fine-grained signal once the basics are in place.

### Visibility summary

| Signal                                    | Tool needed                    | What it shows                                |
|-------------------------------------------|--------------------------------|----------------------------------------------|
| Container restart                         | None                           | A source is failing; container is recovering |
| `/readyz` → 503                           | Any HTTP client                | A sink (DB) is unreachable                   |
| `meterlogger_read_errors_total`           | Prometheus                     | Read errors by source over time              |
| `meterlogger_last_read_timestamp_seconds` | Prometheus + Grafana           | Gaps in data collection                      |
| Docker `HEALTHCHECK` status               | `docker inspect` / `docker ps` | In-container readiness check                 |

---

## Distributed tracing (OpenTelemetry)

Enable OTLP tracing and metrics export by adding to your config:

```yaml
OTEL:
  Enabled: true
  CollectorAddr: "otel-collector:4317"  # gRPC, no TLS
  ServiceName: "meterlogger-heat"
```

Traces are emitted for each read+store cycle. Span errors are recorded with `span.RecordError` and
`codes.Error` so they appear as failed spans in your tracing backend.

### Log-to-trace correlation

`internal/tracedslog` wraps the structured logger with a custom `slog.Handler`. When a span is active
in the request context, it automatically appends `traceID` and `spanID` attributes to every log record:

```
time=2024-01-15T10:30:00Z level=INFO msg="stored heat telegram" traceID=4bf92f3577b34da6a3ce929d0e0e4736 spanID=00f067aa0ba902b7
```

No code changes are needed in adapters or services. Any `logger.InfoContext(ctx, ...)` call that carries
an active span will include the trace attributes automatically. This enables direct log-to-trace linking
in Grafana, Jaeger, or any LGTM-compatible backend.

---

## Continuous profiling (Pyroscope)

Enable Grafana Pyroscope continuous profiling:

```yaml
Profiling:
  Enabled: true
  ServerAddr: "http://pyroscope:4040"
  ServiceName: "meterlogger-heat"
  BasicAuthUser: ""       # leave empty if no auth
  BasicAuthPassword: ""
```

CPU, allocation, goroutine, mutex, and block profiles are pushed to the Pyroscope server. The
Go SDK is pure-Go, so it works with the scratch Docker image without any changes.

---

## Docker health check command

The `meterlogger` binary has a `healthcheck` subcommand that probes `/readyz` on the local
instance. See [deployment.md - Health checks](./deployment.md#health-checks) for usage.
