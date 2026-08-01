# Deployment

>
Related: [configuration.md](./configuration.md) · [architecture.md](./architecture.md) · [observability.md](./observability.md)

## Contents

- [Building](#building)
- [Container isolation model](#container-isolation-model)
- [Docker Compose - full example](#docker-compose--full-example)
- [Health checks](#health-checks)
- [Kubernetes](#kubernetes)
- [Systemd](#systemd)
- [Sink containers](#sink-containers)
- [Home Assistant](#home-assistant)
- [Observability infrastructure](#observability-infrastructure)
- [Shutdown behaviour](#shutdown-behaviour)

---

## Building

### Local binaries

```sh
make build
# Output: out/meterlogger-linux-amd64
#         out/meterlogger-linux-arm64
#         out/meterlogger-darwin-arm64
```

`CGO_ENABLED=0` produces fully static binaries. `-s -w` strips debug symbols. `CommitSHA` and `BuildDate`
are injected at build time and appear in every log line.

### Docker image

The Dockerfile is a two-stage build. The build stage compiles `cmd/meterlogger`
with `-trimpath -ldflags="-s -w"`. The runtime stage is `FROM scratch` and
contains only the meterlogger binary, CA certificates, and a non-root user
record. Timezone data is embedded into the binary via the `time/tzdata`
import.

```sh
docker build --build-arg GIT_SHA=$(git rev-parse --short HEAD) -t yottabyte/meterlogger .
```

The CI pipeline builds and pushes a multi-arch manifest (`linux/amd64` + `linux/arm64`) automatically on push to
`master`. Images are published to Docker Hub (`yottabyte/meterlogger`) and GitHub Container
Registry (`ghcr.io/yottabytesolutions/meterlogger`).

---

## Container isolation model

MeterLogger is designed to run **one container per source**. Each container handles exactly one meter type
(heat, grid, solar, or ventilation) using the `--source` flag:

```sh
meterlogger --source heat
meterlogger --source grid
meterlogger --source solar
meterlogger --source ventilation
```

### Why one container per source?

Each source reads from a distinct device or network endpoint. Failures are isolated and independent:

| Failure scenario                   | Per-source containers     | Single container      |
|------------------------------------|---------------------------|-----------------------|
| USB dongle detached (heat/grid)    | Only that container exits | Whole process exits   |
| Network outage (solar/ventilation) | Only that container exits | Whole process exits   |
| Serial port stuck                  | Only that container exits | Whole process exits   |
| Restart clearly visible            | Yes - `CrashLoopBackOff`  | No - process stays up |

### Restarts as a signal

When a source fails, the container exits and Docker/Kubernetes restarts it (`restart: always` /
`restartPolicy: Always`). This means failure is **immediately visible** without any monitoring tooling:

```sh
docker ps          # shows Restarting or Up Xm (restarted N times)
kubectl get pods   # shows CrashLoopBackOff or restart count
```

A process that swallows errors and stays alive is invisible to the orchestrator. With this model, a
restarting container is itself the health signal.

### Shared configuration

All four containers share a single config file. The `Enabled: true` flags document which sources are
active; the `--source` flag at runtime selects which one actually starts. This means you never need
four separate config files or four separate images.

```yaml
# config.yaml - one file, mounted into every container at /config.yaml
Heat:
  Enabled: true
  Measurement: warmte
  SerialInterface: /dev/ttyUSB0
  MbusAddress: 1
  ScrapeInterval: 30s

Grid:
  Enabled: true
  Measurement: stroom
  SerialInterface: /dev/ttyUSB1

Enphase:
  Enabled: true
  Measurement: solar
  EnvoyURL: http://envoy.local
  User: owner@example.com
  Password: secret
  Serial: 122308135614
  ScrapeInterval: 20s

Ventilation:
  Enabled: true
  MeasurementBaseName: ventilatie
  HostURL: http://ducobox.local
  ScrapeInterval: 60s
  Nodes: [1,2,3,4,5,6,7]

QuestDB:
  Enabled: true
  Host: questdb
  Port: 9009
  User: admin
  Password: quest

FlushInterval: 3s
```

---

## Docker Compose - full example

```yaml
services:

  meterlogger-heat:
    image: yottabyte/meterlogger:latest
    command: ["--source", "heat", "--config", "/config.yaml"]
    restart: always
    volumes:
      - ./config.yaml:/config.yaml:ro
    devices:
      - /dev/ttyUSB0:/dev/ttyUSB0
    device_cgroup_rules:
      - 'c 188:* rmw'
    ports:
      - "8081:8080"

  meterlogger-grid:
    image: yottabyte/meterlogger:latest
    command: ["--source", "grid", "--config", "/config.yaml"]
    restart: always
    volumes:
      - ./config.yaml:/config.yaml:ro
    devices:
      - /dev/ttyUSB1:/dev/ttyUSB1
    device_cgroup_rules:
      - 'c 188:* rmw'
    ports:
      - "8082:8080"

  meterlogger-solar:
    image: yottabyte/meterlogger:latest
    command: ["--source", "solar", "--config", "/config.yaml"]
    restart: always
    volumes:
      - ./config.yaml:/config.yaml:ro
    ports:
      - "8083:8080"

  meterlogger-ventilation:
    image: yottabyte/meterlogger:latest
    command: ["--source", "ventilation", "--config", "/config.yaml"]
    restart: always
    volumes:
      - ./config.yaml:/config.yaml:ro
    ports:
      - "8084:8080"
```

> **`device_cgroup_rules`** is the minimal alternative to `privileged: true`. It grants read/write
> access to USB serial devices (major 188) without elevating all capabilities.

> **Config mount:** the image runs as the non-root user `minion` with no home directory, so the
> default config location (`$HOME/.meterlogger.yaml`) does not exist in the container. Always mount
> the config at a fixed path and pass `--config`, as shown above.

Each container exposes its own health/metrics port. Map them to distinct host ports (8081 to 8084 above)
or use a reverse proxy / service mesh to route by path.

### Network exposure

- The health/metrics port binds all interfaces and has no authentication. Do not expose it publicly.
  Keep the port mapping on a trusted network or drop it and probe over the Docker network.
- Only Postgres and TimescaleDB support TLS (`SSLMode`). MySQL, ClickHouse, TDEngine, and QuestDB ILP
  connections are plaintext. Run those sinks on a trusted network, not across the public internet.

---

## Health checks

### How it works

Every MeterLogger instance starts an HTTP server (default port 8080) with three endpoints:

| Endpoint   | Purpose                                                                                                |
|------------|--------------------------------------------------------------------------------------------------------|
| `/healthz` | Liveness - `503` once a sink has been continuously down for `HTTPServer.LivenessFailureThreshold` (90s) |
| `/readyz`  | Readiness - `200 OK` when all enabled sinks are reachable, `503` if any sink is currently down         |
| `/metrics` | Prometheus metrics in text exposition format                                                           |

The split between liveness and readiness is intentional: a brief sink outage flips `/readyz` so traffic stops
hitting the pod, but `/healthz` stays green so the kubelet does not restart on every transient blip. Once
the outage exceeds the liveness threshold the pod restarts itself instead of getting stuck in a permanent
`Running but NotReady` state. See
[observability.md - Liveness detail](./observability.md#liveness-detail).

### Docker HEALTHCHECK

The `meterlogger` binary has a `healthcheck` subcommand that calls `/readyz` on the local
instance and exits `0` (healthy) or `1` (unhealthy). Because the runtime image is `scratch`,
there is no shell or `curl` - this subcommand is the only way to perform an in-container probe.

The `HEALTHCHECK` instruction is already set in the Dockerfile:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/meterlogger", "healthcheck"]
```

If the port is not the default 8080, set `HTTPSERVER_PORT` in the container environment:

```yaml
environment:
  HTTPSERVER_PORT: "9090"
```

The `healthcheck` subcommand reads this variable automatically.

Inspect container health status:

```sh
docker inspect --format='{{.State.Health.Status}}' meterlogger-heat
# healthy | unhealthy | starting
```

---

## Kubernetes

A complete worked example lives in [deploy/kubernetes.yaml](../deploy/kubernetes.yaml): a ConfigMap
with a minimal grid + QuestDB config and a single-replica Deployment with probes, resource limits,
and a restrictive security context. Copy the Deployment once per source and change the `--source`
argument; all containers can share the ConfigMap.

Key points:

- **One Deployment per source.** Same isolation model as Docker Compose: a failing source
  crash-loops its own pod and nothing else.
- **Probes.** `readinessProbe` hits `/readyz`, which flips to `503` immediately when a sink is
  down. `livenessProbe` hits `/healthz`, which only fails after the sink has been down for
  `HTTPServer.LivenessFailureThreshold` (default 90s), so the kubelet restarts the pod on
  sustained failure but never on a transient blip.
- **Serial devices (heat, grid).** A plain `hostPath` mount of `/dev/ttyUSB0` requires
  `privileged: true` because the kubelet does not whitelist the device in the cgroup. A device
  plugin (for example `squat/generic-device-plugin`) exposes the device as an extended resource
  without privilege. Network sources (solar, ventilation) need neither.
- **Do not expose port 8080 beyond the pod network.** The health/metrics endpoints have no
  authentication. Probes and Prometheus scraping reach the pod directly; no Service is needed.

---

## Systemd

For hosts without a container runtime, `deploy/systemd/meterlogger@.service` is a
template unit that runs one instance per source, matching the
one-container-per-source model.

```sh
# Install the binary from a release tarball
tar -xzf meterlogger-linux-amd64.tar.gz
sudo install -m 0755 meterlogger /usr/local/bin/meterlogger

# User, config, unit
sudo useradd --system --no-create-home --shell /usr/sbin/nologin meterlogger
sudo mkdir -p /etc/meterlogger
sudo cp config.example.yaml /etc/meterlogger/config.yaml   # then edit
sudo cp deploy/systemd/meterlogger@.service /etc/systemd/system/
sudo systemctl daemon-reload

# One instance per source
sudo systemctl enable --now meterlogger@grid
sudo systemctl enable --now meterlogger@heat
```

Check a service with `systemctl status meterlogger@grid` and
`journalctl -u meterlogger@grid -f`. Validate the config first with
`meterlogger validate --config /etc/meterlogger/config.yaml --ping`.

Every instance reads the same config file; the instance name selects the source,
so per-source `Enabled` flags are ignored, exactly like the `--source` flag in
containers. Instances cannot share the health port: give each one its own via a
drop-in, using the environment override support:

```sh
sudo systemctl edit meterlogger@heat
# [Service]
# Environment=HTTPSERVER_PORT=8081
```

The unit grants the `dialout` group for serial devices and applies standard
hardening. The service restarts itself on failure; that is the designed
recovery path for lost sink connections.

## Sink containers

### QuestDB

```yaml
services:
  questdb:
    image: questdb/questdb:latest
    ports:
      - "9000:9000"   # web console
      - "9009:9009"   # ILP ingestion
    volumes:
      - questdb-data:/root/.questdb

volumes:
  questdb-data:
```

Web console at `http://localhost:9000`. Tables are created on first write.

### PostgreSQL

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: meterlogger
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: meterlogger
    ports:
      - "5432:5432"
    volumes:
      - pg-data:/var/lib/postgresql/data

volumes:
  pg-data:
```

Set `POSTGRES_ENABLED=true`. Tables are created and migrated automatically on startup.

### TimescaleDB

```yaml
services:
  timescaledb:
    image: timescale/timescaledb:latest-pg16
    environment:
      POSTGRES_USER: meterlogger
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: meterlogger
    ports:
      - "5433:5432"
    volumes:
      - ts-data:/var/lib/postgresql/data

volumes:
  ts-data:
```

Set `TIMESCALEDB_ENABLED=true`. Hypertables are created automatically after each table.

### ClickHouse

```yaml
services:
  clickhouse:
    image: clickhouse/clickhouse-server:latest
    ports:
      - "9000:9000"   # native protocol (used by meterlogger)
      - "8123:8123"   # HTTP interface (for ad-hoc queries)
    volumes:
      - ch-data:/var/lib/clickhouse

volumes:
  ch-data:
```

Set `CLICKHOUSE_ENABLED=true` and `CLICKHOUSE_HOST=clickhouse`. Default port is `9000`.

### TDEngine

```yaml
services:
  tdengine:
    image: tdengine/tdengine:latest
    ports:
      - "6041:6041"   # REST API (used by meterlogger)
    volumes:
      - td-data:/var/lib/taos

volumes:
  td-data:
```

Set `TDENGINE_ENABLED=true` and `TDENGINE_HOST=tdengine`. MeterLogger connects via the REST API on port 6041.

---

## Home Assistant

The MQTT sink makes every meter show up in Home Assistant with zero YAML on the Home Assistant
side. MeterLogger publishes retained MQTT discovery config messages, so Home Assistant creates:

- One device per physical meter (grid meter, gas meter, heat meter, Enphase Envoy, DucoBox and
  each of its nodes), grouped by meter serial.
- Sensors with correct `device_class`, `state_class`, and units: energy counters (kWh, Wh),
  power (W), voltage, current, temperatures, CO2, humidity, and the gas counter (m³).
- Availability tracking: all sensors go unavailable when meterlogger stops or loses the broker,
  via a retained last-will message on `<TopicPrefix>/status`.

The grid energy counters, gas counter, and solar production sensor use
`state_class: total_increasing` with energy/gas device classes, so they can be selected directly
in the Home Assistant **Energy dashboard** (grid consumption/return, gas usage, solar
production). Heat energy is published in kWh (converted from the meter's joule counter) for the
same reason.

Requirements: any MQTT 3.1.1 broker reachable by both meterlogger and Home Assistant, with the
[MQTT integration](https://www.home-assistant.io/integrations/mqtt/) enabled in Home Assistant
and its discovery prefix matching `MQTT.DiscoveryPrefix` (default `homeassistant`). Mosquitto
works out of the box:

```yaml
services:
  mosquitto:
    image: eclipse-mosquitto:2
    ports:
      - "1883:1883"
    volumes:
      - ./mosquitto.conf:/mosquitto/config/mosquitto.conf
```

Minimal `mosquitto.conf` for a trusted LAN (add `password_file` for authentication):

```
listener 1883
allow_anonymous true
```

Configure each meterlogger container:

```yaml
environment:
  MQTT_ENABLED: "true"
  MQTT_BROKERURL: "tcp://mosquitto:1883"
```

Discovery for a meter is announced on the first reading that carries its serial number, so
entities appear within one scrape interval after startup. See
[configuration.md - MQTT](./configuration.md#mqtt) for the full key reference and topic scheme.

---

## Observability infrastructure

### OpenTelemetry Collector

To enable distributed tracing, run an OTel collector and point meterlogger at it:

```yaml
services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    volumes:
      - ./otel-config.yaml:/etc/otel-collector-config.yaml
    command: ["--config=/etc/otel-collector-config.yaml"]
    ports:
      - "4317:4317"   # OTLP gRPC
```

Then configure each meterlogger container:

```yaml
environment:
  OTEL_ENABLED: "true"
  OTEL_COLLECTORADDR: "otel-collector:4317"
  OTEL_SERVICENAME: "meterlogger-heat"
```

See [observability.md](./observability.md) for the tracing and log-correlation details.

### Grafana Pyroscope

For continuous profiling:

```yaml
services:
  pyroscope:
    image: grafana/pyroscope:latest
    ports:
      - "4040:4040"
```

Configure each meterlogger container:

```yaml
environment:
  PROFILING_ENABLED: "true"
  PROFILING_SERVERADDR: "http://pyroscope:4040"
  PROFILING_SERVICENAME: "meterlogger-heat"
```

The Go SDK is pure Go. No changes to the scratch Docker image are required.

---

## Shutdown behaviour

On `SIGINT` / `SIGTERM` / `SIGQUIT`:

1. The root context is cancelled.
2. All active service loops exit cleanly.
3. Deferred `repo.Close()` fires, flushing any buffered records and closing all DB connections.
4. The process exits with code `0`.

Any unrecoverable read or write error also triggers `SIGTERM` internally, causing the same clean shutdown
path. The container then restarts according to the restart policy and retries from a clean state.
