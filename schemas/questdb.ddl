CREATE TABLE solar
(
    "timestamp"         TIMESTAMP,
    EnvoySerialNumber   SYMBOL,
    ProductionWattHours DOUBLE,
    ProductionWatt      DOUBLE,
    PanelCount          LONG
) TIMESTAMP("timestamp")
PARTITION BY WEEK
DEDUP UPSERT KEYS(timestamp, EnvoySerialNumber);

CREATE TABLE solar_inverters
(
    "timestamp"          TIMESTAMP,
    InverterSerialNumber SYMBOL,
    EnvoySerialNumber SYMBOL,
    Status            SYMBOL,
    ChannelID            LONG,
    Operating            BOOLEAN,
    Communicating        BOOLEAN,
    Producing            BOOLEAN,
    Phase             VARCHAR,
    Watts                LONG,
    PeakWatts            LONG
) timestamp("timestamp")
PARTITION BY MONTH
DEDUP UPSERT KEYS(timestamp, InverterSerialNumber);

create table stroom
(
    "timestamp"      TIMESTAMP,
    Serienummer      SYMBOL,
    MeterMerkType    SYMBOL,
    UsageCounter1    DOUBLE,
    UsageCounter2    DOUBLE,
    OutputCounter1   DOUBLE,
    OutputCounter2   DOUBLE,
    TotalPowerUsage  LONG,
    TotalPowerOutput LONG,
    BrownoutsP1      LONG,
    BrownoutsP2      LONG,
    BrownoutsP3      LONG,
    SpikesP1         LONG,
    SpikesP2         LONG,
    SpikesP3         LONG,
    VoltageP1        DOUBLE,
    VoltageP2        DOUBLE,
    VoltageP3        DOUBLE,
    CurrentP1        LONG,
    CurrentP2        LONG,
    CurrentP3        LONG,
    PowerUsageP1     LONG,
    PowerUsageP2     LONG,
    PowerUsageP3     LONG,
    PowerOutputP1    LONG,
    PowerOutputP2    LONG,
    PowerOutputP3    LONG
) TIMESTAMP("timestamp")
PARTITION BY DAY
DEDUP UPSERT KEYS(timestamp, Serienummer, MeterMerkType);

CREATE table warmte
(
    "timestamp" TIMESTAMP,
    device      SYMBOL,
    serial      SYMBOL,
    location    SYMBOL,
    "power"     LONG,
    energy      LONG,
    t1          DOUBLE,
    t2          DOUBLE,
    t1mint2     DOUBLE,
    volume      LONG,
    hours       LONG,
    max_flow    DOUBLE,
    max_power   LONG
) TIMESTAMP("timestamp")
PARTITION BY MONTH
DEDUP UPSERT KEYS("timestamp", "serial");
