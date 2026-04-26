package timescaledb_test

import (
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func timescaleDummyHeat() domain.HeatTelegram {
	return domain.HeatTelegram{Timestamp: time.Now(), MeterID: "m1", SerialNo: "s1"}
}

func timescaleDummyGrid() domain.GridTelegram {
	return domain.GridTelegram{Time: time.Now()}
}

func timescaleDummySolar() domain.EnvoySolarData {
	return domain.EnvoySolarData{ReadingTime: time.Now(), EnvoySerial: "e1"}
}

func timescaleDummySolarWithInverter() domain.EnvoySolarData {
	return domain.EnvoySolarData{
		ReadingTime: time.Now(),
		EnvoySerial: "e1",
		Inverters:   []domain.InverterDetails{{SerialNumber: "inv1", ReportTime: time.Now()}},
	}
}

func timescaleDummyBoxStatus() domain.DucoBoxStatus {
	return domain.DucoBoxStatus{}
}

func timescaleDummyRFSensor() domain.DucoRFSensorStatus {
	return domain.DucoRFSensorStatus{}
}

func timescaleDummyBoxNode() domain.DucoNodeBoxStatus {
	return domain.DucoNodeBoxStatus{}
}

func timescaleDummyValveNode() domain.DucoNodeBoxValveStatus {
	return domain.DucoNodeBoxValveStatus{}
}
