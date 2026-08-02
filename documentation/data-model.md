# Data model

> Related: [architecture.md](./architecture.md) · [configuration.md](./configuration.md)

This document describes the Go domain types and the database tables they map to.

---

## Go domain types

Domain types live in `internal/domain/`. They are plain Go structs with no framework dependencies.

### HeatTelegram

```go
// internal/domain/heat.go
type HeatTelegram struct {
    Timestamp      time.Time
    MeterId        string    // e.g. "Kamstrup (Heat)"
    SerialNo       string
    Joules         int64     // Total energy consumed (J)
    VolumeCm3      float64   // Total volume of liquid (cm³)
    SecondsCounter int64     // Operating time (s)
    Tforward       float64   // Supply temperature (°C)
    Treturn        float64   // Return temperature (°C)
    Tdiff          float64   // Temperature difference (°C)
    ActualPower    int64     // Current power consumption (W)
    MaxPower       int64     // Peak power consumption ever (W)
    ActualFlow     float64   // Current liquid flow rate
    MaxFlow        float64   // Peak liquid flow rate ever
}
```

### GridTelegram

```go
// internal/domain/grid.go
type GridTelegram struct {
    Time             time.Time
    MeterMerkType    string  // Meter brand identifier string
    Serienummer      string

    // Cumulative counters (kWh)
    UsageCounter1    float64 // Consumed off-peak
    UsageCounter2    float64 // Consumed peak
    OutputCounter1   float64 // Returned off-peak
    OutputCounter2   float64 // Returned peak

    // Current power (W)
    TotalPowerUsage  int
    TotalPowerOutput int

    // Per-phase quality
    BrownoutsP1, BrownoutsP2, BrownoutsP3 int
    SpikesP1, SpikesP2, SpikesP3           int
    VoltageP1, VoltageP2, VoltageP3        float64 // V
    CurrentP1, CurrentP2, CurrentP3        int     // A
    PowerUsageP1, PowerUsageP2, PowerUsageP3  int  // W
    PowerOutputP1, PowerOutputP2, PowerOutputP3 int // W

    // Peak demand (Belgian capaciteitstarief); zero when the meter
    // does not publish them
    AvgDemand        int       // W, current average demand (1-0:1.4.0)
    MaxDemandMonth   int       // W, running-month maximum (1-0:1.6.0)
    MaxDemandMonthAt time.Time // capture time of the maximum
}
```

Luxembourgish and Austrian meters publish only energy totals; those land in `UsageCounter1` and
`OutputCounter1` with the tariff-2 counters at zero.

### GasReading

```go
// internal/domain/gas.go
type GasReading struct {
    CapturedAt time.Time // meter-supplied capture time
    ReceivedAt time.Time // when the carrying telegram was read
    Channel    int       // M-Bus channel (1 to 4), assigned by installation order
    DeviceType int       // EN 13757-3 medium code: 3 = gas
    SerialNo   string
    ReadingM3  float64   // cumulative gas volume (m³)
}
```

Gas readings arrive as M-Bus subdevice lines inside the grid meter's P1 telegrams; see
[meter-types.md](./meter-types.md#m-bus-subdevices-gas). Two timestamps matter:

- **CapturedAt** is when the gas meter took the reading, as reported in the telegram. The meter
  captures a new value every 5 minutes (DSMR 5) or hourly (DSMR 4 and older).
- **ReceivedAt** is when MeterLogger read the telegram that carried the reading. Telegrams repeat
  the last capture every second, so ReceivedAt advances while CapturedAt stays fixed.

**Deduplication rule:** the service stores a row only when CapturedAt changes for a given channel.
Repeated telegrams carrying the same capture are dropped. Expect one row per capture interval, not
one per telegram.

**Device type codes** follow EN 13757-3: `3` is gas. Water and thermal subdevices have their own
types below; a slave e-meter (code `2`) is never stored from the master's telegram.

### WaterReading and ThermalReading

```go
// internal/domain/subdevice.go
type WaterReading struct {
    CapturedAt time.Time // meter-supplied capture time
    ReceivedAt time.Time // when the carrying telegram was read
    Channel    int       // M-Bus channel (1 to 4), assigned by installation order
    DeviceType int       // 6 = warm water, 7 = water
    SerialNo   string
    ReadingM3  float64   // cumulative water volume (m³)
}

type ThermalReading struct {
    CapturedAt time.Time // meter-supplied capture time
    ReceivedAt time.Time // when the carrying telegram was read
    Channel    int       // M-Bus channel (1 to 4), assigned by installation order
    DeviceType int       // 4 = heat, 10/11 = cooling, 12 = heat/cool combined
    SerialNo   string
    ReadingGJ  float64   // cumulative thermal energy (GJ)
}
```

Water and thermal readings arrive on the same M-Bus subdevice lines as gas and follow the same
timestamps and deduplication rule. Water must be reported in m3 and thermal in GJ; readings with
any other unit are skipped with a warning.

### EnvoySolarData

```go
// internal/domain/solar.go
type EnvoySolarData struct {
    ReadingTime  time.Time
    ProductionWh float64          // Lifetime production (Wh)
    Watt         float64          // Current production (W)
    PanelCount   int              // Number of active micro-inverters
    EnvoySerial  string
    Inverters    []InverterDetails
}

type InverterDetails struct {
    SerialNumber      string
    Chaneid           int
    Producing         bool
    Operating         bool
    Phase             string    // "ph-a", "ph-b", or "ph-c"
    Communicating     bool
    DeviceStatus      []string
    ReportTime        time.Time
    LastReportedWatts int
    MaxReportWatts    int

    // Electrical measurements from /ivp/pdm/device_data (base units).
    DCVoltage    float64
    DCCurrent    float64
    ACVoltage    float64
    ACCurrent    float64
    ACFrequency  float64
    TemperatureC int
    LeadingVArs  int
    LaggingVArs  int
    WhToday      int
    WhYesterday  int
    WhWeek       int
    WhLifetime   float64
    RSSI         int
    ISSI         int
}
```

The electrical fields come from `/ivp/pdm/device_data`, merged per inverter by
serial number. The Envoy reports voltage, current, and frequency as milli-units
(mV, mA, mHz); the reader converts them to base units (V, A, Hz) and converts
lifetime joules to watt-hours. These fields are zero when device_data has not
yet reported a reading for a panel. Freshness matches the watts: both track the
microinverter powerline report, roughly every 5 minutes per panel, staggered.

### DucoBox types

```go
// internal/domain/ventilation.go

type DucoBoxStatus struct {
    EnergyCalib    EnergyCalib
    EnergyFan      EnergyFan
    EnergyInfo     EnergyInfo
    General        General
    WeatherStation WeatherStation
}

// Fan metrics
type EnergyFan struct {
    ExhaustFanPressActual, ExhaustFanPressTarget int
    ExhaustFanPwmLevel, ExhaustFanPwmPercentage, ExhaustFanSpeed int
    SupplyFanPressActual, SupplyFanPressTarget int
    SupplyFanPwmLevel, SupplyFanPwmPercentage, SupplyFanSpeed int
}

// Temperatures and protection state
type EnergyInfo struct {
    BypassStatus, FilterRemainingTime int
    FrostProtHeaterLevel, FrostProtPressReduct int
    FrostProtState bool
    TempEHA, TempETA, TempODA, TempSUP int  // exhaust/extract/outdoor/supply (°C × 10)
}

// Shared base for all node types
type BaseDucoNodeStatus struct {
    Node, SubType, Addr, Sub, Prnt, Asso int
    DevType, Netw, Location, State string
    Cntdwn, Mode, Ovrl, Snsr, Cerr, Show, Link int
    Swversion, Serialnb string
}

type DucoNodeBoxStatus struct {
    BaseDucoNodeStatus
    Trgt, Actl int     // target/actual ventilation level
    Rh   float64       // relative humidity %
    Temp float64       // temperature °C
    Co2  float64       // CO₂ ppm
}

type DucoNodeBoxValveStatus struct {
    BaseDucoNodeStatus
    Trgt, Actl int
}

type DucoRFSensorStatus struct {
    BaseDucoNodeStatus
    Temp, Co2, Rh float64
    RssiN2M, RssiN2H int  // RSSI direct / with hops
    HopVia int
}
```

---

## QuestDB tables

QuestDB uses the [InfluxDB line protocol](https://questdb.io/docs/reference/api/ilp/overview/) for ingestion. Each table
is populated by a dedicated writer in `internal/adapters/sink/qdb/`.

All tables have a `timestamp` designated timestamp column (QuestDB's primary time index).

### heat_meter (configurable name)

Written by `qdb_heat_writer.go`.

| Column      | Type      | Notes                            |
|-------------|-----------|----------------------------------|
| `device`    | Symbol    | `"Multical {MeterId}"`           |
| `serial`    | Symbol    |                                  |
| `location`  | Symbol    | Always `"meterkast"` (hardcoded) |
| `power`     | Long      | ActualPower (W)                  |
| `energy`    | Double    | Joules × 1e-9 (GJ)               |
| `t1`        | Double    | Tforward (°C)                    |
| `t2`        | Double    | Treturn (°C)                     |
| `t1mint2`   | Double    | Tdiff (°C)                       |
| `volume`    | Double    | VolumeCm3                        |
| `seconds`   | Long      | SecondsCounter                   |
| `max_flow`  | Double    | MaxFlow                          |
| `max_power` | Long      | MaxPower                         |
| `timestamp` | Timestamp | From telegram.Timestamp          |

### grid_meter (configurable name)

Written by `qdb_grid_writer.go`.

| Column                | Type      | Notes                 |
|-----------------------|-----------|-----------------------|
| `MeterMerkType`       | Symbol    |                       |
| `Serienummer`         | Symbol    |                       |
| `UsageCounter1`       | Double    | kWh off-peak          |
| `UsageCounter2`       | Double    | kWh peak              |
| `OutputCounter1`      | Double    | kWh returned off-peak |
| `OutputCounter2`      | Double    | kWh returned peak     |
| `VoltageP1/P2/P3`     | Double    | Volts                 |
| `TotalPowerUsage`     | Long      | W                     |
| `TotalPowerOutput`    | Long      | W                     |
| `BrownoutsP1/P2/P3`   | Long      |                       |
| `SpikesP1/P2/P3`      | Long      |                       |
| `CurrentP1/P2/P3`     | Long      | A                     |
| `PowerUsageP1/P2/P3`  | Long      | W                     |
| `PowerOutputP1/P2/P3` | Long      | W                     |
| `AvgDemand`           | Long      | W, Belgian peak demand; 0 elsewhere |
| `MaxDemandMonth`      | Long      | W, running-month maximum; 0 elsewhere |
| `MaxDemandMonthAt`    | Timestamp | Only written when the meter publishes peak demand |
| `timestamp`           | Timestamp | From telegram.Time    |

The SQL sinks (Postgres, TimescaleDB, MySQL, TDEngine, ClickHouse) store the same peak demand
fields as nullable columns `avg_demand`, `max_demand_month`, and `max_demand_month_at`, added by
grid schema migration version 2. `max_demand_month_at` is NULL when the meter does not publish
peak demand.

### gas_meter (configurable name)

Written when `Grid.Gas.Enabled` is set; name from `Grid.Gas.Measurement`. One row per new gas
meter capture (see the deduplication rule above).

| Column        | Type      | Notes                                        |
|---------------|-----------|----------------------------------------------|
| `serial_no`   | Symbol    | Gas meter serial number                      |
| `channel`     | Long      | M-Bus channel (1 to 4)                       |
| `device_type` | Long      | EN 13757-3 medium code, `3` for gas          |
| `received_at` | Timestamp | When the carrying telegram was read          |
| `reading_m3`  | Double    | Cumulative gas volume (m³)                   |
| `timestamp`   | Timestamp | CapturedAt: meter-supplied capture time      |

### water_meter (configurable name)

Written when `Grid.Water.Enabled` is set; name from `Grid.Water.Measurement`. One row per new
water meter capture, deduplicated exactly like gas.

| Column        | Type      | Notes                                        |
|---------------|-----------|----------------------------------------------|
| `serial_no`   | Symbol    | Water meter serial number                    |
| `channel`     | Long      | M-Bus channel (1 to 4)                       |
| `device_type` | Long      | `6` warm water, `7` water                    |
| `received_at` | Timestamp | When the carrying telegram was read          |
| `reading_m3`  | Double    | Cumulative water volume (m³)                 |
| `timestamp`   | Timestamp | CapturedAt: meter-supplied capture time      |

### thermal_meter (configurable name)

Written when `Grid.Thermal.Enabled` is set; name from `Grid.Thermal.Measurement`. One row per new
heat or cooling meter capture, deduplicated exactly like gas.

| Column        | Type      | Notes                                          |
|---------------|-----------|------------------------------------------------|
| `serial_no`   | Symbol    | Thermal meter serial number                    |
| `channel`     | Long      | M-Bus channel (1 to 4)                         |
| `device_type` | Long      | `4` heat, `10`/`11` cooling, `12` heat/cool    |
| `received_at` | Timestamp | When the carrying telegram was read            |
| `reading_gj`  | Double    | Cumulative thermal energy (GJ)                 |
| `timestamp`   | Timestamp | CapturedAt: meter-supplied capture time        |

### solar (configurable name)

Written by `qdb_solar_writer.go`.

| Column                | Type      | Notes                                      |
|-----------------------|-----------|--------------------------------------------|
| `EnvoySerialNumber`   | Symbol    |                                            |
| `ProductionWattHours` | Double    | Lifetime Wh                                |
| `ProductionWatt`      | Double    | Current W                                  |
| `ProductionVoltage`   | Long      | Contains PanelCount (naming is misleading) |
| `timestamp`           | Timestamp | From data.ReadingTime                      |

### solar_inverters (configurable base name + `_inverters`)

One row per inverter per reading.

| Column                 | Type      | Notes                                   |
|------------------------|-----------|-----------------------------------------|
| `InverterSerialNumber` | Symbol    |                                         |
| `EnvoySerialNumber`    | String    | Parent gateway                          |
| `ChannelID`            | Long      | inverter.Chaneid                        |
| `Operating`            | Boolean   |                                         |
| `Communicating`        | Boolean   |                                         |
| `Producing`            | Boolean   |                                         |
| `Phase`                | String    | "ph-a", etc.                            |
| `Voltage`              | Long      | Contains Chaneid (naming is misleading) |
| `Status`               | String    | Joined device status codes              |
| `Watts`                | Long      | LastReportedWatts                       |
| `PeakWatts`            | Long      | MaxReportWatts                          |
| `DCVoltage`            | Double    | device_data, V                          |
| `DCCurrent`            | Double    | device_data, A                          |
| `ACVoltage`            | Double    | device_data, V                          |
| `ACCurrent`            | Double    | device_data, A                          |
| `ACFrequency`          | Double    | device_data, Hz                         |
| `TemperatureC`         | Long      | device_data, °C                         |
| `LeadingVArs`          | Long      | device_data                             |
| `LaggingVArs`          | Long      | device_data                             |
| `WhToday`              | Long      | device_data                             |
| `WhYesterday`          | Long      | device_data                             |
| `WhWeek`               | Long      | device_data                             |
| `WhLifetime`           | Double    | device_data, Wh                         |
| `RSSI`                 | Long      | device_data, powerline link            |
| `ISSI`                 | Long      | device_data, powerline link            |
| `timestamp`            | Timestamp | From inverter.ReportTime                |

The SQL sinks (Postgres, MySQL, TimescaleDB, TDEngine, ClickHouse) carry the
same fields as snake_case columns (`dc_voltage`, `ac_frequency`, `status`,
etc.), added to the `_inverters` table by solar schema migration version 2. The
`status` column holds the joined device status codes, which version 1 stored
only in QuestDB.

### ventilation_box_general (configurable base + `_box_general`)

One row per box status read, written by `qdb_ventilation_writer.go`.

Contains all `EnergyCalib`, `EnergyFan`, `EnergyInfo`, and `General` fields flattened to individual columns.
`timestamp` = `time.Now()` at time of write.

### ventilation_node (configurable base + `_node`)

For `DucoRFSensorStatus` nodes (UCCO2, UCRH).

| Column                                           | Type      | Notes             |
|--------------------------------------------------|-----------|-------------------|
| `node`                                           | Symbol    | Node ID as string |
| `location`                                       | Symbol    |                   |
| `device`                                         | Symbol    | DevType           |
| `connection_type`                                | Symbol    | Netw              |
| `serialnumber`                                   | Symbol    |                   |
| `sw_version`                                     | Symbol    |                   |
| `mode`                                           | String    |                   |
| `state`                                          | String    |                   |
| `rssi_direct`                                    | Long      | RssiN2M           |
| `rssi_with_hops`                                 | Long      | RssiN2H           |
| `hop_via`                                        | Long      |                   |
| `snsr`, `cerr`, `ovrl`, `cntdwn`, `show`, `link` | Long      |                   |
| `co2`                                            | Double    | ppm               |
| `temp`                                           | Double    | °C                |
| `humidity`                                       | Double    | %                 |
| `timestamp`                                      | Timestamp | time.Now()        |

### ventilation_box_node (configurable base + `_box_node`)

For `DucoNodeBoxStatus` nodes (BOX type). Same symbol columns plus:

| Column     | Type   | Notes                    |
|------------|--------|--------------------------|
| `trgt`     | Long   | Target ventilation level |
| `actl`     | Long   | Actual ventilation level |
| `co2`      | Double | ppm                      |
| `temp`     | Double | °C                       |
| `humidity` | Double | %                        |

### ventilation_valve (configurable base + `_valve`)

For `DucoNodeBoxValveStatus` nodes (VLV type). Same symbol columns plus:

| Column | Type | Notes           |
|--------|------|-----------------|
| `trgt` | Long | Target position |
| `actl` | Long | Actual position |

---

## SQL sink schemas (PostgreSQL, MySQL, TimescaleDB, ClickHouse, TDEngine)

The five SQL sinks (PostgreSQL, MySQL, TimescaleDB, ClickHouse, TDEngine) share the same logical column names and
types. The `schemastore` migration framework applies engine-specific DDL adjustments (e.g. `DATETIME(6)` vs `TIMESTAMPTZ`,
`MergeTree` engine for ClickHouse). TimescaleDB uses `create_hypertable()` on the timestamp column in addition to the
standard PostgreSQL schema.

Refer to the `internal/adapters/sink/<backend>/` package for the exact DDL used by each engine.

### Gas table (SQL sinks and ClickHouse)

When `Grid.Gas.Enabled` is set, the SQL sinks and ClickHouse create a table named after
`Grid.Gas.Measurement` (default `gas_meter`):

| Column        | Type      | Notes                                   |
|---------------|-----------|-----------------------------------------|
| `ts`          | timestamp | CapturedAt: meter-supplied capture time |
| `received_at` | timestamp | When the carrying telegram was read     |
| `channel`     | int       | M-Bus channel (1 to 4)                  |
| `device_type` | int       | EN 13757-3 medium code, `3` for gas     |
| `serial_no`   | text      | Gas meter serial number                 |
| `reading_m3`  | double    | Cumulative gas volume (m³)              |

### Water and thermal tables (SQL sinks and ClickHouse)

When `Grid.Water.Enabled` or `Grid.Thermal.Enabled` is set, the SQL sinks and ClickHouse create
tables named after `Grid.Water.Measurement` (default `water_meter`) and `Grid.Thermal.Measurement`
(default `thermal_meter`). Both share the gas table shape:

| Column        | Type      | Notes                                                      |
|---------------|-----------|------------------------------------------------------------|
| `ts`          | timestamp | CapturedAt: meter-supplied capture time                    |
| `received_at` | timestamp | When the carrying telegram was read                        |
| `channel`     | int       | M-Bus channel (1 to 4)                                     |
| `device_type` | int       | Water: `6`/`7`. Thermal: `4`, `10`, `11`, `12`             |
| `serial_no`   | text      | Meter serial number                                        |
| `reading_m3`  | double    | Water table only: cumulative volume (m³)                   |
| `reading_gj`  | double    | Thermal table only: cumulative energy (GJ)                 |

Deduplication happens in the service before the write; the stores insert what they are given.

---

## Known data model issues

These are pre-existing quirks in the codebase worth being aware of:

1. **Solar `ProductionVoltage` column** stores `PanelCount`, not a voltage. The naming is a copy-paste artifact in
   `qdb_solar_writer.go`.
2. **Solar inverter `Voltage` column** stores `Chaneid`, not a voltage. Same cause.
3. **Heat `location` symbol** is hardcoded to `"meterkast"` in the writer rather than coming from config.
4. **Ventilation `timestamp`** uses `time.Now()` (write time) rather than any timestamp from the DucoBox API response.
