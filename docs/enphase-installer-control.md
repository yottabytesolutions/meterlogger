# Enphase Envoy: installer access, high-res data, inverter control

## Plan

Two features on the Enphase solar source:
1. High-resolution aggregate solar via the `/stream/meter` SSE stream.
2. Whole-array power on/off control via the installer token.

Status is tracked in the `## Status` section at the bottom.

## Live sweep results (2026-08-02)

Ground truth, verified against the production Envoy with real owner and
installer tokens. Trust these over the handover where they conflict.

- Envoy: `https://envoy.home.jvthert.nl`, serial `122308135614` (read from
  unauthenticated `/info`), firmware `D8.3.5528`, `<web-tokens>true`.
- No local auth. HTTP :80 closed. HTTPS 401 carries no `WWW-Authenticate`
  header, so no digest/basic challenge exists. The serial-based password
  calculator does not work on D8. Every request needs a cloud-minted JWT.
  Cloud is touched only to mint a token; all reads and writes then run on the
  LAN. Owner token ~6 months, installer token ~12 hours.
- Token claims confirm the level: owner `enphaseUser=owner`, installer
  `enphaseUser=installer` (`level=3` in the mod endpoint errors).

### What installer unlocks that owner does not

| Endpoint | Owner | Installer | Meaning |
|---|---|---|---|
| `/ivp/mod/{eid}/mode/power` | 401 | 200 | power control (see control finding) |
| `/ivp/peb/devstatus` | 401 | 200 | per-PCU detailed status counters |
| `/installer/agf/index.json` | 401 | 200 | selected grid profile |
| `/installer/agf/inverters_status.json` | 401 | 200 | per-inverter admin_state, phase |

Everything else the project reads or would read is already 200 for owner:
`/production.json`, `/api/v1/production/inverters`, `/inventory.json`,
`/ivp/meters/readings`, `/ivp/ss/pcs_settings`, `/ivp/ss/dpel`,
`/ivp/sc/status`, `/ivp/livedata/status`, `/installer/agf/details.json`,
`/admin/lib/tariff`. Data collection needs no installer token; only control
and the grid-profile/devstatus extras do.

### CONTROL FINDING: per-panel shutdown is not possible on this hardware

This corrects the handover. Per-panel `/ivp/mod/{chaneid}/mode/power` is
rejected even at installer level, on all 11 panels:

```
GET /ivp/mod/1627390225/mode/power
{ "error" : "power mode get: not valid for (eid=0x61000111) level=3 devtype=1"}
```

`devtype=1` is a PCU microinverter. The firmware does not expose per-device
power mode for microinverters. Only the gateway-wide record works:

```
GET /ivp/mod/603980032/mode/power
{ "powerForcedOff": false, "pvPowerForcedOff": false, "enchargePowerForcedOff": false}
```

So control is all-or-nothing: the whole array off or on, via EID `603980032`,
with fields `powerForcedOff` (everything), `pvPowerForcedOff` (PV only),
`enchargePowerForcedOff` (battery only; no battery here). For the
salderingsregeling use case (stop exporting when prices are negative) this is
exactly the right lever. Individual-panel control would not help anyway.

### High-resolution data

- `/stream/meter` SSE (owner token) is the sub-second aggregate source.
  Feature 1. No installer needed.
- `/ivp/livedata/status` reports "Live stream not enabled. Enable using
  /ivp/livedata/stream/ API" so livedata is not a drop-in alternative.
- Per-panel data stays capped at ~5 min by the powerline duty cycle. Not
  improvable. `/api/v1/production/inverters` (owner) already covers it.

## Freshness (measured 2026-08-02)

`/ivp/pdm/device_data` and `/api/v1/production/inverters` refresh at the same
cadence: 10 fresh per-panel reports each over 7 minutes, i.e. ~5 min per panel,
staggered. The `duration: 906` in device_data is the reading-interval length,
not the refresh rate. So the rich electrical fields are exactly as fresh as the
watts. No sub-5-minute per-panel data exists without CTs.

## Done: richer per-inverter reader

Shipped in this change set:
- `domain.InverterDetails` enriched with DC/AC voltage and current, frequency,
  temperature, VArs, Wh today/yesterday/week, lifetime Wh, RSSI, ISSI.
- Reader queries `/ivp/pdm/device_data`, merges by serial, converts milli-units
  to base units and joules to Wh.
- All sinks extended: QuestDB (new ILP columns), sqlsink v2 migration covering
  Postgres/MySQL/TimescaleDB/TDEngine, ClickHouse v2 ALTER migration, MQTT
  payload plus HA discovery sensors. `status` (device_status) now also stored in
  the SQL sinks, not only QuestDB.
- Migrations auto-run on deploy; existing `_inverters` tables gain the columns.

## Status

- Live sweep: done (2026-08-02). Findings above.
- Richer per-inverter reader + schema across all sinks: DONE. Build, tests, lint
  green. Not yet released (needs a version bump + tag).
- Whole-array control (MQTT switch + CLI): not started. Blocked on a live
  write-test authorization to confirm restart delay.
- Per-panel control: dropped, not achievable on this hardware (devtype=1).
- `/stream/meter` consumer: dropped, CTs disabled so the stream reads zero.
