# Troubleshooting

> Related: [configuration.md](./configuration.md) · [deployment.md](./deployment.md) ·
> [observability.md](./observability.md) · [meter-types.md](./meter-types.md)

Common problems when getting MeterLogger running for the first time, and how to diagnose them.

---

## Serial port permission errors (heat / grid)

Both the heat meter (M-Bus) and grid meter (DSMR P1) sources open a serial device such as
`/dev/ttyUSB0`. A typical failure looks like:

```
permission denied opening /dev/ttyUSB0
```

or, from `serial.OpenPort`, an underlying `open /dev/ttyUSB0: permission denied`.

### Running directly on a host (no container)

The serial device is normally owned by `root:dialout` (Linux) or `root:wheel` (macOS) with group
read/write permissions. Add your user to the group that owns the device:

```sh
ls -l /dev/ttyUSB0
# crw-rw---- 1 root dialout 188, 0 Jan 1 12:00 /dev/ttyUSB0

sudo usermod -aG dialout $USER
# log out and back in for the group change to take effect
```

Alternatively, install a udev rule that grants access without adding users to a group, for example
a rule matching the USB-to-serial adapter's vendor/product ID with `MODE="0666"`.

### Running in a container

The container process does not automatically see host devices. Pass the device explicitly, as
documented in [deployment.md - Docker Compose](./deployment.md#docker-compose--full-example):

```yaml
devices:
  - /dev/ttyUSB0:/dev/ttyUSB0
device_cgroup_rules:
  - 'c 188:* rmw'
```

If the container still reports permission denied, check that the container's runtime user has
access to the device node inside the container (`ls -l /dev/ttyUSB0` from inside the container).
The published image runs as a non-root user; group `188` (`dialout` major number for USB serial)
must be reachable by that user, which `device_cgroup_rules` grants at the cgroup level regardless
of in-container user/group mapping.

### Device path changes after reboot or reconnect

USB serial adapters are not guaranteed to keep the same `/dev/ttyUSBx` number across reboots or
reconnects, especially with more than one adapter attached. Use a stable path instead, for example
`/dev/serial/by-id/usb-...`, and point `Heat.SerialInterface` / `Grid.SerialInterface` at that path.

---

## Enphase Envoy authentication failures

The solar source logs an error like:

```
Failed to authenticate with Enphase API. Statuscode:401 Unauthorized. API Response:...
```

This comes from the login step against Enphase's cloud service (`enlighten.enphaseenergy.com`),
not the local Envoy device. See [meter-types.md - Authentication](./meter-types.md#authentication)
for the full token flow. Check, in order:

1. **`Enphase.User` / `Enphase.Password`** - these are the Enlighten cloud account credentials
   (the same login used at enlighten.enphaseenergy.com), not local Envoy credentials.
2. **`Enphase.Serial`** - the Envoy gateway serial number, printed on the device. A mismatched
   serial causes the token exchange (`entrez.enphaseenergy.com/tokens`) to fail even after a
   successful login.
3. **`Enphase.EnvoyURL`** - must be reachable on the local network and include the scheme, e.g.
   `https://192.168.1.100`. This is used for the actual data queries once a token is obtained; it
   is not involved in authentication itself, but a wrong or unreachable URL surfaces as connection
   errors on the subsequent `production.json` / `inventory.json` calls rather than an auth error.

Since the Envoy uses a self-signed certificate, TLS verification is disabled for calls to
`EnvoyURL`. A failure at that stage is a network or URL problem, not a certificate problem.

If the cloud login itself fails outright (network unreachable, DNS failure), MeterLogger cannot
refresh the Envoy token even if the Envoy itself is reachable - the cloud round trip is required to
obtain and periodically refresh the JWT.

---

## Database sink connection errors

Each SQL sink logs a specific message when the connection fails at startup, for example:

```
failed to connect to PostgreSQL
failed to connect to MySQL
failed to connect to TimescaleDB
failed to connect to ClickHouse
failed to connect to TDEngine
```

The attached error gives the underlying cause (`connection refused`, `no such host`,
`authentication failed`, etc.). Common causes:

- **Wrong host/port.** In Docker Compose, the sink's config `Host` must be the service name (e.g.
  `questdb`, `postgres`), not `localhost` - the meterlogger container and the database run as
  separate containers on the same Docker network. `localhost` from inside the meterlogger container
  refers to the meterlogger container itself.
- **Wrong credentials.** Check `User` / `Password` / `Database` against what the database container
  was started with (e.g. `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` environment variables
  on the Postgres image).
- **Database not yet ready.** If the database container is still initializing when meterlogger
  starts, the connection attempt fails immediately - there is no built-in retry/backoff on startup.
  Use `depends_on` with a healthcheck condition in Docker Compose, or restart the meterlogger
  container (`restart: always` retries on the next crash).
- **SSL mode mismatch** (PostgreSQL / TimescaleDB only). `SSLMode` must match what the server
  requires: `disable`, `require`, or `verify-full`. See
  [configuration.md - Configuration fields per sink](./configuration.md#configuration-fields-per-sink).

For a QuestDB connection problem, note there is no explicit "failed to connect" log at startup -
QuestDB uses ILP/TCP and reports failures through `/readyz` and the write-error metrics described
below.

---

## "At least one source/sink must be enabled" startup errors

MeterLogger refuses to start under two conditions, both fatal:

```
no sinks enabled; set Enabled: true for at least one sink
no sources enabled in configuration; set Enabled: true for at least one source or use --source
```

- **No sinks enabled**: none of `QuestDB.Enabled`, `Postgres.Enabled`, `MySQL.Enabled`,
  `TimescaleDB.Enabled`, `ClickHouse.Enabled`, `TDEngine.Enabled` is `true`. Note that
  `QuestDB.Enabled` no longer defaults to `true` - every sink must be explicitly enabled. See
  [configuration.md - Enabling and disabling sinks](./configuration.md#enabling-and-disabling-sinks).
- **No sources enabled**: none of `Heat.Enabled`, `Grid.Enabled`, `Enphase.Enabled`,
  `Ventilation.Enabled` is `true`, and no `--source` flag was passed. Either set the relevant
  `Enabled: true` in the config, or pass `--source heat|grid|solar|ventilation` explicitly. Per the
  one-container-per-source model, `--source` selects which already-enabled source actually runs -
  see [deployment.md - Container isolation model](./deployment.md#container-isolation-model).

An invalid `--source` value (anything other than `heat`, `grid`, `solar`, `ventilation`) is also
fatal and logs `invalid --source value`.

---

## Confirming the collector is actually polling

Once MeterLogger starts without a fatal error, verify it is actually reading and writing data:

1. **Check `/healthz` and `/readyz`.** `/readyz` returns `503` if a configured sink is currently
   unreachable; `/healthz` only flips after sustained failure (`HTTPServer.LivenessFailureThreshold`,
   default 90s). See [observability.md - HTTP endpoints](./observability.md#http-endpoints).

   ```sh
   curl -s http://localhost:8080/readyz
   curl -s http://localhost:8080/healthz
   ```

2. **Check Prometheus metrics.** `/metrics` exposes `meterlogger_reads_total` and
   `meterlogger_last_read_timestamp_seconds` per source, and `meterlogger_writes_total` /
   `meterlogger_write_errors_total` per sink. A source that never increments `_reads_total`, or a
   `_last_read_timestamp_seconds` that stops advancing, means the read loop is stuck or the
   hardware/API is unreachable - not a sink problem. See
   [observability.md - Prometheus metrics](./observability.md#prometheus-metrics).

   ```sh
   curl -s http://localhost:8080/metrics | grep meterlogger_reads_total
   curl -s http://localhost:8080/metrics | grep meterlogger_last_read_timestamp_seconds
   ```

3. **Run with `--debug`.** Debug-level logging shows each read and store attempt. Useful when
   metrics look fine but the expected data is not appearing in the sink (e.g. wrong `Measurement`
   table name).

4. **Check container restarts.** Because each source runs in its own container, a source that
   keeps crash-looping is itself a signal that the read loop is failing fatally rather than
   producing occasional errors. See
   [observability.md - Container health as an observability signal](./observability.md#container-health-as-an-observability-signal).
