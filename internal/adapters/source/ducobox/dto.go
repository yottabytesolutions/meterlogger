package ducobox

import (
	"strings"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// ducoBoxStatusDTO is the API response shape for /boxinfoget.
type ducoBoxStatusDTO struct {
	EnergyCalib    energyCalibDTO    `json:"EnergyCalib"`
	EnergyFan      energyFanDTO      `json:"EnergyFan"`
	EnergyInfo     energyInfoDTO     `json:"EnergyInfo"`
	General        generalDTO        `json:"General"`
	WeatherStation weatherStationDTO `json:"WeatherStation"`
}

type energyCalibDTO struct {
	CalibKinZone1       int    `json:"CalibKinZone1"`
	CalibKinZone2       int    `json:"CalibKinZone2"`
	CalibKout           int    `json:"CalibKout"`
	CalibPinInternZone1 int    `json:"CalibPinInternZone1"`
	CalibPinInternZone2 int    `json:"CalibPinInternZone2"`
	CalibPinMaxZone1    int    `json:"CalibPinMaxZone1"`
	CalibPinMaxZone2    int    `json:"CalibPinMaxZone2"`
	CalibPinXZone1      int    `json:"CalibPinXZone1"`
	CalibPinXZone2      int    `json:"CalibPinXZone2"`
	CalibPout           int    `json:"CalibPout"`
	CalibPoutMax        int    `json:"CalibPoutMax"`
	CalibQinZone1       int    `json:"CalibQinZone1"`
	CalibQinZone2       int    `json:"CalibQinZone2"`
	CalibQout           int    `json:"CalibQout"`
	CalibState          string `json:"CalibState"`
}

type energyFanDTO struct {
	ExhaustFanPressActual   int `json:"ExhaustFanPressActual"`
	ExhaustFanPressTarget   int `json:"ExhaustFanPressTarget"`
	ExhaustFanPwmLevel      int `json:"ExhaustFanPwmLevel"`
	ExhaustFanPwmPercentage int `json:"ExhaustFanPwmPercentage"`
	ExhaustFanSpeed         int `json:"ExhaustFanSpeed"`
	SupplyFanPressActual    int `json:"SupplyFanPressActual"`
	SupplyFanPressTarget    int `json:"SupplyFanPressTarget"`
	SupplyFanPwmLevel       int `json:"SupplyFanPwmLevel"`
	SupplyFanPwmPercentage  int `json:"SupplyFanPwmPercentage"`
	SupplyFanSpeed          int `json:"SupplyFanSpeed"`
}

type energyInfoDTO struct {
	BypassStatus         int  `json:"BypassStatus"`
	FilterRemainingTime  int  `json:"FilterRemainingTime"`
	FrostProtHeaterLevel int  `json:"FrostProtHeaterLevel"`
	FrostProtPressReduct int  `json:"FrostProtPressReduct"`
	FrostProtState       bool `json:"FrostProtState"`
	TempEHA              int  `json:"TempEHA"`
	TempETA              int  `json:"TempETA"`
	TempODA              int  `json:"TempODA"`
	TempSUP              int  `json:"TempSUP"`
}

type generalDTO struct {
	InstallerState string `json:"InstallerState"`
	RFHomeID       string `json:"RFHomeID"`
	Time           int64  `json:"Time"`
}

type weatherStationDTO struct {
	Present bool `json:"Present"`
}

type baseNodeStatusDTO struct {
	Node      int    `json:"node"`
	DevType   string `json:"devtype"`
	SubType   int    `json:"subtype"`
	Netw      string `json:"netw"`
	Addr      int    `json:"addr"`
	Sub       int    `json:"sub"`
	Prnt      int    `json:"prnt"`
	Asso      int    `json:"asso"`
	Location  string `json:"location"`
	State     string `json:"state"`
	Cntdwn    int    `json:"cntdwn"`
	Mode      string `json:"mode"`
	Ovrl      int    `json:"ovrl"`
	Snsr      int    `json:"snsr"`
	Cerr      int    `json:"cerr"`
	Swversion string `json:"swversion"`
	Serialnb  string `json:"serialnb"`
	Show      int    `json:"show"`
	Link      int    `json:"link"`
}

type nodeBoxStatusDTO struct {
	baseNodeStatusDTO

	Trgt int     `json:"trgt"`
	Actl int     `json:"actl"`
	Rh   float64 `json:"rh"`
	Temp float64 `json:"temp"`
	Co2  float64 `json:"co2"`
}

type nodeBoxValveStatusDTO struct {
	baseNodeStatusDTO

	Trgt int `json:"trgt"`
	Actl int `json:"actl"`
}

type rfSensorStatusDTO struct {
	baseNodeStatusDTO

	Temp    float64 `json:"temp"`
	Co2     float64 `json:"co2"`
	Rh      float64 `json:"rh"`
	RssiN2M int     `json:"rssi_n2m"`
	HopVia  int     `json:"hop_via"`
	RssiN2H int     `json:"rssi_n2h"`
}

func mapBoxStatus(dto ducoBoxStatusDTO) domain.DucoBoxStatus {
	return domain.DucoBoxStatus{
		EnergyCalib: domain.EnergyCalib{
			CalibKinZone1:       dto.EnergyCalib.CalibKinZone1,
			CalibKinZone2:       dto.EnergyCalib.CalibKinZone2,
			CalibKout:           dto.EnergyCalib.CalibKout,
			CalibPinInternZone1: dto.EnergyCalib.CalibPinInternZone1,
			CalibPinInternZone2: dto.EnergyCalib.CalibPinInternZone2,
			CalibPinMaxZone1:    dto.EnergyCalib.CalibPinMaxZone1,
			CalibPinMaxZone2:    dto.EnergyCalib.CalibPinMaxZone2,
			CalibPinXZone1:      dto.EnergyCalib.CalibPinXZone1,
			CalibPinXZone2:      dto.EnergyCalib.CalibPinXZone2,
			CalibPout:           dto.EnergyCalib.CalibPout,
			CalibPoutMax:        dto.EnergyCalib.CalibPoutMax,
			CalibQinZone1:       dto.EnergyCalib.CalibQinZone1,
			CalibQinZone2:       dto.EnergyCalib.CalibQinZone2,
			CalibQout:           dto.EnergyCalib.CalibQout,
			CalibState:          dto.EnergyCalib.CalibState,
		},
		EnergyFan: domain.EnergyFan{
			ExhaustFanPressActual:   dto.EnergyFan.ExhaustFanPressActual,
			ExhaustFanPressTarget:   dto.EnergyFan.ExhaustFanPressTarget,
			ExhaustFanPwmLevel:      dto.EnergyFan.ExhaustFanPwmLevel,
			ExhaustFanPwmPercentage: dto.EnergyFan.ExhaustFanPwmPercentage,
			ExhaustFanSpeed:         dto.EnergyFan.ExhaustFanSpeed,
			SupplyFanPressActual:    dto.EnergyFan.SupplyFanPressActual,
			SupplyFanPressTarget:    dto.EnergyFan.SupplyFanPressTarget,
			SupplyFanPwmLevel:       dto.EnergyFan.SupplyFanPwmLevel,
			SupplyFanPwmPercentage:  dto.EnergyFan.SupplyFanPwmPercentage,
			SupplyFanSpeed:          dto.EnergyFan.SupplyFanSpeed,
		},
		EnergyInfo: domain.EnergyInfo{
			BypassStatus:         dto.EnergyInfo.BypassStatus,
			FilterRemainingTime:  dto.EnergyInfo.FilterRemainingTime,
			FrostProtHeaterLevel: dto.EnergyInfo.FrostProtHeaterLevel,
			FrostProtPressReduct: dto.EnergyInfo.FrostProtPressReduct,
			FrostProtState:       dto.EnergyInfo.FrostProtState,
			TempEHA:              dto.EnergyInfo.TempEHA,
			TempETA:              dto.EnergyInfo.TempETA,
			TempODA:              dto.EnergyInfo.TempODA,
			TempSUP:              dto.EnergyInfo.TempSUP,
		},
		General: domain.General{
			InstallerState: dto.General.InstallerState,
			RFHomeID:       dto.General.RFHomeID,
			Time:           dto.General.Time,
		},
		WeatherStation: domain.WeatherStation{
			Present: dto.WeatherStation.Present,
		},
	}
}

func mapBaseNodeStatus(dto baseNodeStatusDTO) domain.BaseDucoNodeStatus {
	return domain.BaseDucoNodeStatus{
		Node:      dto.Node,
		DevType:   dto.DevType,
		SubType:   dto.SubType,
		Netw:      dto.Netw,
		Addr:      dto.Addr,
		Sub:       dto.Sub,
		Prnt:      dto.Prnt,
		Asso:      dto.Asso,
		Location:  dto.Location,
		State:     dto.State,
		Cntdwn:    dto.Cntdwn,
		Mode:      dto.Mode,
		Ovrl:      dto.Ovrl,
		Snsr:      dto.Snsr,
		Cerr:      dto.Cerr,
		Swversion: dto.Swversion,
		Serialnb:  dto.Serialnb,
		Show:      dto.Show,
		Link:      dto.Link,
	}
}

func mapNodeBoxStatus(dto nodeBoxStatusDTO) domain.DucoNodeBoxStatus {
	return domain.DucoNodeBoxStatus{
		BaseDucoNodeStatus: mapBaseNodeStatus(dto.baseNodeStatusDTO),
		Trgt:               dto.Trgt,
		Actl:               dto.Actl,
		Rh:                 dto.Rh,
		Temp:               dto.Temp,
		Co2:                dto.Co2,
	}
}

func mapNodeBoxValveStatus(dto nodeBoxValveStatusDTO) domain.DucoNodeBoxValveStatus {
	return domain.DucoNodeBoxValveStatus{
		BaseDucoNodeStatus: mapBaseNodeStatus(dto.baseNodeStatusDTO),
		Trgt:               dto.Trgt,
		Actl:               dto.Actl,
	}
}

// mapRFSensorStatus maps the DTO and applies device-type validation
// (swaps sensor readings when the device type and non-zero field don't match).
func mapRFSensorStatus(dto rfSensorStatusDTO) domain.DucoRFSensorStatus {
	result := domain.DucoRFSensorStatus{
		BaseDucoNodeStatus: mapBaseNodeStatus(dto.baseNodeStatusDTO),
		Temp:               dto.Temp,
		Co2:                dto.Co2,
		Rh:                 dto.Rh,
		RssiN2M:            dto.RssiN2M,
		HopVia:             dto.HopVia,
		RssiN2H:            dto.RssiN2H,
	}
	// Apply device-type quirk correction.
	switch strings.ToUpper(dto.DevType) {
	case devTypeUCRH:
		if result.Co2 > 0 && result.Rh == 0 {
			result.Rh, result.Co2 = result.Co2, 0
		}
	case devTypeUCCO2:
		if result.Rh > 0 && result.Co2 == 0 {
			result.Co2, result.Rh = result.Rh, 0
		}
	}
	return result
}
