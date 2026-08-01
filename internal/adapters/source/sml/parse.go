package sml

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// The payload is not parsed as a full SML message tree. After frame
// validation the payload is scanned for value-list entries, recognizable
// as 77 07 <6-byte OBIS object name>, and only the entry tail (status,
// valTime, unit, scaler, value) is TL-decoded. This keeps the parser
// independent of the surrounding GetListResponse structure, which varies
// between vendors, and makes unknown entries free to ignore.

// obisCode is the 6-byte OBIS object name as sent on the wire.
type obisCode [6]byte

//nolint:gochecknoglobals // immutable OBIS object names, effectively constants
var (
	obisImportTotal   = obisCode{0x01, 0x00, 0x01, 0x08, 0x00, 0xFF} // 1-0:1.8.0
	obisImportTariff1 = obisCode{0x01, 0x00, 0x01, 0x08, 0x01, 0xFF} // 1-0:1.8.1
	obisImportTariff2 = obisCode{0x01, 0x00, 0x01, 0x08, 0x02, 0xFF} // 1-0:1.8.2
	obisExportTotal   = obisCode{0x01, 0x00, 0x02, 0x08, 0x00, 0xFF} // 1-0:2.8.0
	obisExportTariff1 = obisCode{0x01, 0x00, 0x02, 0x08, 0x01, 0xFF} // 1-0:2.8.1
	obisExportTariff2 = obisCode{0x01, 0x00, 0x02, 0x08, 0x02, 0xFF} // 1-0:2.8.2
	obisPowerTotal    = obisCode{0x01, 0x00, 0x10, 0x07, 0x00, 0xFF} // 1-0:16.7.0 signed W
	obisPowerL1       = obisCode{0x01, 0x00, 0x24, 0x07, 0x00, 0xFF} // 1-0:36.7.0 signed W
	obisPowerL2       = obisCode{0x01, 0x00, 0x38, 0x07, 0x00, 0xFF} // 1-0:56.7.0 signed W
	obisPowerL3       = obisCode{0x01, 0x00, 0x4C, 0x07, 0x00, 0xFF} // 1-0:76.7.0 signed W
	obisPowerPlusL1   = obisCode{0x01, 0x00, 0x15, 0x07, 0x00, 0xFF} // 1-0:21.7.0 signed W
	obisPowerPlusL2   = obisCode{0x01, 0x00, 0x29, 0x07, 0x00, 0xFF} // 1-0:41.7.0 signed W
	obisPowerPlusL3   = obisCode{0x01, 0x00, 0x3D, 0x07, 0x00, 0xFF} // 1-0:61.7.0 signed W
	obisVoltageL1     = obisCode{0x01, 0x00, 0x20, 0x07, 0x00, 0xFF} // 1-0:32.7.0 V
	obisVoltageL2     = obisCode{0x01, 0x00, 0x34, 0x07, 0x00, 0xFF} // 1-0:52.7.0 V
	obisVoltageL3     = obisCode{0x01, 0x00, 0x48, 0x07, 0x00, 0xFF} // 1-0:72.7.0 V
	obisCurrentL1     = obisCode{0x01, 0x00, 0x1F, 0x07, 0x00, 0xFF} // 1-0:31.7.0 A
	obisCurrentL2     = obisCode{0x01, 0x00, 0x33, 0x07, 0x00, 0xFF} // 1-0:51.7.0 A
	obisCurrentL3     = obisCode{0x01, 0x00, 0x47, 0x07, 0x00, 0xFF} // 1-0:71.7.0 A
	obisServerID      = obisCode{0x01, 0x00, 0x00, 0x00, 0x09, 0xFF} // 1-0:0.0.9 octet string
	obisManufacturer  = obisCode{0x81, 0x81, 0xC7, 0x82, 0x03, 0xFF} // 129-129:199.130.3 ASCII
)

var errMissingImportTotal = errors.New("SML file has no 1-0:1.8.0 import energy entry")

// entryValue is one decoded value-list entry.
type entryValue struct {
	num    float64 // scaled numeric value, when the entry carries a number
	hasNum bool
	octets []byte // raw bytes, when the entry carries an octet string
}

const (
	entryMarker    = 0x77 // TL of a 7-element SML_ListEntry
	obisMarker     = 0x07 // TL of the 6-byte object name octet string
	whPerKWh       = 1000
	scalerBase     = 10
	entryHeaderLen = 2 + len(obisCode{})
)

// scanEntries walks the payload and decodes every recognizable value-list
// entry. Undecodable entry tails are skipped; later duplicates win.
func scanEntries(payload []byte) map[obisCode]entryValue {
	entries := make(map[obisCode]entryValue)
	for i := 0; i+entryHeaderLen <= len(payload); i++ {
		if payload[i] != entryMarker || payload[i+1] != obisMarker {
			continue
		}
		var code obisCode
		copy(code[:], payload[i+2:i+entryHeaderLen])
		v, err := decodeEntryTail(payload[i+entryHeaderLen:])
		if err != nil {
			continue
		}
		entries[code] = v
	}
	return entries
}

// decodeEntryTail decodes status, valTime, unit, scaler, and value of a
// list entry, applying the scaler to numeric values. The unit is decoded
// but not enforced; OBIS semantics determine the interpretation.
func decodeEntryTail(buf []byte) (entryValue, error) {
	pos := 0
	for range 2 { // status and valTime, both unused
		n, err := skipValue(buf[pos:])
		if err != nil {
			return entryValue{}, err
		}
		pos += n
	}
	unit, err := decodeValue(buf[pos:]) // unit, decoded for completeness but unused
	if err != nil {
		return entryValue{}, err
	}
	pos += unit.size
	scalerVal, err := decodeValue(buf[pos:])
	if err != nil {
		return entryValue{}, err
	}
	pos += scalerVal.size
	scaler := 0
	if scalerVal.typ == typeInt && !scalerVal.absent {
		scaler = int(scalerVal.i)
	}
	val, err := decodeValue(buf[pos:])
	if err != nil {
		return entryValue{}, err
	}
	switch val.typ {
	case typeInt:
		return entryValue{num: float64(val.i) * math.Pow(scalerBase, float64(scaler)), hasNum: true}, nil
	case typeUint:
		return entryValue{num: float64(val.u) * math.Pow(scalerBase, float64(scaler)), hasNum: true}, nil
	case typeOctet:
		return entryValue{octets: val.octets}, nil
	default:
		return entryValue{}, fmt.Errorf("unsupported value type 0x%02X", val.typ)
	}
}

// parsePayload turns a validated SML payload into a grid telegram. Only
// 1-0:1.8.0 is required; every other entry is optional because meters in
// factory state send nothing but the import total until the owner enters
// the meter PIN and enables the extended info mode.
func parsePayload(payload []byte, receivedAt time.Time) (domain.GridTelegram, error) {
	entries := scanEntries(payload)

	importTotal, ok := numEntry(entries, obisImportTotal)
	if !ok {
		return domain.GridTelegram{}, errMissingImportTotal
	}

	telegram := domain.GridTelegram{Time: receivedAt}
	applyEnergy(entries, &telegram, importTotal)
	applyPower(entries, &telegram)
	applyPhases(entries, &telegram)
	applyIdentity(entries, &telegram)
	return telegram, nil
}

// numEntry returns the scaled numeric value of an entry, if present.
func numEntry(entries map[obisCode]entryValue, code obisCode) (float64, bool) {
	v, ok := entries[code]
	if !ok || !v.hasNum {
		return 0, false
	}
	return v.num, true
}

// applyEnergy fills the kWh counters. SML energy registers are in Wh.
// With per-tariff registers present they map to counters 1 and 2 like the
// DSMR reader fills them; a meter without tariffs reports only the total,
// which then lands in counter 1 with counter 2 staying zero.
func applyEnergy(entries map[obisCode]entryValue, t *domain.GridTelegram, importTotal float64) {
	if tariff1, ok := numEntry(entries, obisImportTariff1); ok {
		t.UsageCounter1 = tariff1 / whPerKWh
		if tariff2, ok2 := numEntry(entries, obisImportTariff2); ok2 {
			t.UsageCounter2 = tariff2 / whPerKWh
		}
	} else {
		t.UsageCounter1 = importTotal / whPerKWh
	}
	if tariff1, ok := numEntry(entries, obisExportTariff1); ok {
		t.OutputCounter1 = tariff1 / whPerKWh
		if tariff2, ok2 := numEntry(entries, obisExportTariff2); ok2 {
			t.OutputCounter2 = tariff2 / whPerKWh
		}
	} else if total, hasTotal := numEntry(entries, obisExportTotal); hasTotal {
		t.OutputCounter1 = total / whPerKWh
	}
}

// applyPower fills the total power fields from the signed 1-0:16.7.0
// register: positive is import, negative is export.
func applyPower(entries map[obisCode]entryValue, t *domain.GridTelegram) {
	if w, ok := numEntry(entries, obisPowerTotal); ok {
		t.TotalPowerUsage, t.TotalPowerOutput = splitSignedWatts(w)
	}
}

// applyPhases fills per-phase power, voltage, and current. Both vendor
// register sets for phase power are accepted; the 21/41/61 set wins when a
// meter sends both.
func applyPhases(entries map[obisCode]entryValue, t *domain.GridTelegram) {
	phasePower := func(primary, fallback obisCode, usage, output *int) {
		w, ok := numEntry(entries, primary)
		if !ok {
			w, ok = numEntry(entries, fallback)
			if !ok {
				return
			}
		}
		*usage, *output = splitSignedWatts(w)
	}
	phasePower(obisPowerPlusL1, obisPowerL1, &t.PowerUsageP1, &t.PowerOutputP1)
	phasePower(obisPowerPlusL2, obisPowerL2, &t.PowerUsageP2, &t.PowerOutputP2)
	phasePower(obisPowerPlusL3, obisPowerL3, &t.PowerUsageP3, &t.PowerOutputP3)

	if v, ok := numEntry(entries, obisVoltageL1); ok {
		t.VoltageP1 = v
	}
	if v, ok := numEntry(entries, obisVoltageL2); ok {
		t.VoltageP2 = v
	}
	if v, ok := numEntry(entries, obisVoltageL3); ok {
		t.VoltageP3 = v
	}
	if a, ok := numEntry(entries, obisCurrentL1); ok {
		t.CurrentP1 = int(math.Round(a))
	}
	if a, ok := numEntry(entries, obisCurrentL2); ok {
		t.CurrentP2 = int(math.Round(a))
	}
	if a, ok := numEntry(entries, obisCurrentL3); ok {
		t.CurrentP3 = int(math.Round(a))
	}
}

// applyIdentity fills the meter identification fields. The server ID is a
// binary octet string and is stored hex-encoded; the manufacturer entry is
// plain ASCII.
func applyIdentity(entries map[obisCode]entryValue, t *domain.GridTelegram) {
	if v, ok := entries[obisServerID]; ok && len(v.octets) > 0 {
		t.Serienummer = hex.EncodeToString(v.octets)
	}
	if v, ok := entries[obisManufacturer]; ok && len(v.octets) > 0 {
		t.MeterMerkType = string(v.octets)
	}
}

// splitSignedWatts splits a signed power reading into the usage and output
// magnitudes the telegram carries.
func splitSignedWatts(w float64) (int, int) {
	if w >= 0 {
		return int(math.Round(w)), 0
	}
	return 0, int(math.Round(-w))
}
