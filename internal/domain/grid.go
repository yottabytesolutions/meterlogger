// Package domain holds the core data types and adapter interfaces for
// MeterLogger. Every other package in the project either implements a domain
// interface (sources and sinks) or coordinates between them (services).
//
// Telegram and reading types describe a single meter measurement. Reader
// interfaces are the abstraction over physical or network sources.
// Repository interfaces are the abstraction over the storage backends. The
// service layer wires a reader to a repository and depends only on this
// package.
package domain

import (
	"context"
	"time"
)

// GridTelegram is one decoded reading from a DSMR P1 grid meter.
// Counter fields are kWh totals; instantaneous power fields are in watts;
// voltage in volts; current in amps.
type GridTelegram struct {
	Time             time.Time
	MeterMerkType    string
	Serienummer      string
	UsageCounter1    float64
	UsageCounter2    float64
	OutputCounter1   float64
	OutputCounter2   float64
	TotalPowerUsage  int
	TotalPowerOutput int
	BrownoutsP1      int
	BrownoutsP2      int
	BrownoutsP3      int
	SpikesP1         int
	SpikesP2         int
	SpikesP3         int
	VoltageP1        float64
	VoltageP2        float64
	VoltageP3        float64
	CurrentP1        int
	CurrentP2        int
	CurrentP3        int
	PowerUsageP1     int
	PowerUsageP2     int
	PowerUsageP3     int
	PowerOutputP1    int
	PowerOutputP2    int
	PowerOutputP3    int

	// MBusDevices carries the readings of meters attached over M-Bus
	// (gas, water, thermal), when present in the telegram.
	MBusDevices []MBusDeviceReading

	/* Samenstelling van een P1 bericht
	   Meter identificatie

	   P1 Protocol versie // 1-3:0.2.8(50)
	   Timestamp van de meting // 0-0:1.0.0(191130210919W)
	   Serienummer meter // 0-0:96.1.1(4530303334303037343337383430323139)
	   Verbruik Tarief 1 // 1-0:1.8.1(000239.922*kWh)
	   Verbruik Tarief 2 // 1-0:1.8.2(000239.621*kWh)
	   Teruggeleverd Tarief 1 // 1-0:2.8.1(000003.448*kWh)
	   Teruggeleverd Tarief 2 // 1-0:2.8.2(000000.000*kWh)
	   Huidige Tarief // 0-0:96.14.0(0001)
	   Totaal opgenomen vermogen // 1-0:1.7.0(00.577*kW)
	   Huidige teruglevering in watt // 1-0:2.7.0(00.000*kW)
	   Totaal aantal storingen // 0-0:96.7.21(00009)
	   Totaal aantal lange storingen // 0-0:96.7.9(00010)
	   Logboek van lange storingen // 1-0:99.97.0(8)(0-0:96.7.19)(190626203404S)(0000517984*s)(190626205653S)(0000000346*s)(190626210701S)(0000000381*s)(190626211944S)(0000000526*s)(190626213252S)(0000000426*s)(190627005449S)(0000000240*s)(191102152533W)(0000000830*s)(191102155054W)(0000001331*s)
	   Aantal brownouts fase 1 // 1-0:32.32.0(00000)
	   Aantal brownouts fase 2 // 1-0:52.32.0(00000)
	   Aantal brownouts fase 3 // 1-0:72.32.0(00000)
	   Aantal voltage pieken fase 1 // 1-0:32.36.0(00001)
	   Aantal voltage pieken fase 2 // 1-0:52.36.0(00001)
	   Aantal voltage pieken fase 3 // 1-0:72.36.0(00001)
	   Bericht // 0-0:96.13.0()
	   Huidig voltage in fase 1 // 1-0:32.7.0(227.4*V)
	   Huidig voltage in fase 2 // 1-0:52.7.0(227.2*V)
	   Huidig voltage in fase 3 // 1-0:72.7.0(228.2*V)
	   Huidige stroom in fase 1 // 1-0:31.7.0(001*A)
	   Huidige stroom in fase 2 // 1-0:51.7.0(000*A)
	   Huidige stroom in fase 3 // 1-0:71.7.0(001*A)
	   Opgenomen vermogen fase 1 // 1-0:21.7.0(00.298*kW)
	   Opgenomen vermogen fase 2 // 1-0:41.7.0(00.054*kW)
	   Opgenomen vermogen fase 3 // 1-0:61.7.0(00.223*kW)
	   Geleverd vermogen fase 1 // 1-0:22.7.0(00.000*kW)
	   Geleverd vermogen fase 2 // 1-0:42.7.0(00.000*kW)
	   Geleverd vermogen fase 3 // 1-0:62.7.0(00.000*kW)
	*/
}

// GridTelegramReader reads grid meter telegrams. The reader owns its output:
// Telegrams returns the channel on which successfully parsed telegrams are
// delivered, and the reader closes that channel when ReadGridTelegrams
// returns. ReadGridTelegrams runs until ctx is cancelled or a non-recoverable
// error occurs and must be called at most once.
type GridTelegramReader interface {
	Telegrams() <-chan GridTelegram
	ReadGridTelegrams(ctx context.Context) error
}

// GridTelegramRepository writes grid meter telegrams to a storage backend.
// StoreGridTelegram queues or writes a single reading. Flush forces any
// buffered data to the backend. Close releases all underlying resources.
type GridTelegramRepository interface {
	StoreGridTelegram(ctx context.Context, meterBericht GridTelegram) error
	Flush(ctx context.Context) error
	Close() error
}
