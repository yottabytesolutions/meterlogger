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
}
```

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
}
```

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
| `timestamp`           | Timestamp | From telegram.Time    |

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
| `timestamp`            | Timestamp | From inverter.ReportTime                |

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

---

## Known data model issues

These are pre-existing quirks in the codebase worth being aware of:

1. **Solar `ProductionVoltage` column** stores `PanelCount`, not a voltage. The naming is a copy-paste artifact in
   `qdb_solar_writer.go`.
2. **Solar inverter `Voltage` column** stores `Chaneid`, not a voltage. Same cause.
3. **Heat `location` symbol** is hardcoded to `"meterkast"` in the writer rather than coming from config.
4. **Ventilation `timestamp`** uses `time.Now()` (write time) rather than any timestamp from the DucoBox API response.
