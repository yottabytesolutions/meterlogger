package domain

import (
	"context"
	"errors"
)

// ErrUnknownDevType is returned when the DucoBox reports a device type this
// application does not recognise. Callers detect it with errors.Is and skip
// the node instead of treating it as a failure.
var ErrUnknownDevType = errors.New("unknown devtype")

// DucoBoxStatus represents the status of the main DucoBox unit.
type DucoBoxStatus struct {
	EnergyCalib    EnergyCalib
	EnergyFan      EnergyFan
	EnergyInfo     EnergyInfo
	General        General
	WeatherStation WeatherStation
}

// EnergyCalib represents calibration data for the DucoBox.
type EnergyCalib struct {
	CalibKinZone1       int
	CalibKinZone2       int
	CalibKout           int
	CalibPinInternZone1 int
	CalibPinInternZone2 int
	CalibPinMaxZone1    int
	CalibPinMaxZone2    int
	CalibPinXZone1      int
	CalibPinXZone2      int
	CalibPout           int
	CalibPoutMax        int
	CalibQinZone1       int
	CalibQinZone2       int
	CalibQout           int
	CalibState          string
}

// EnergyFan represents fan-related data.
type EnergyFan struct {
	ExhaustFanPressActual   int
	ExhaustFanPressTarget   int
	ExhaustFanPwmLevel      int
	ExhaustFanPwmPercentage int
	ExhaustFanSpeed         int
	SupplyFanPressActual    int
	SupplyFanPressTarget    int
	SupplyFanPwmLevel       int
	SupplyFanPwmPercentage  int
	SupplyFanSpeed          int
}

// EnergyInfo represents additional energy-related information.
type EnergyInfo struct {
	BypassStatus         int
	FilterRemainingTime  int
	FrostProtHeaterLevel int
	FrostProtPressReduct int
	FrostProtState       bool
	TempEHA              int
	TempETA              int
	TempODA              int
	TempSUP              int
}

// General represents general information about the DucoBox.
type General struct {
	InstallerState string
	RFHomeID       string
	Time           int64
}

// WeatherStation represents weather station data.
type WeatherStation struct {
	Present bool
}

// BaseDucoNodeStatus contains common fields for different node types.
type BaseDucoNodeStatus struct {
	Node      int
	DevType   string
	SubType   int
	Netw      string
	Addr      int
	Sub       int
	Prnt      int
	Asso      int
	Location  string
	State     string
	Cntdwn    int
	Mode      string
	Ovrl      int
	Snsr      int
	Cerr      int
	Swversion string
	Serialnb  string
	Show      int
	Link      int
}

// NodeDevType returns the device type identifier, implementing DucoNodeStatus.
func (b BaseDucoNodeStatus) NodeDevType() string { return b.DevType }

// DucoNodeBoxStatus represents a node with box status.
type DucoNodeBoxStatus struct {
	BaseDucoNodeStatus

	Trgt int
	Actl int
	Rh   float64
	Temp float64
	Co2  float64
}

// DucoNodeBoxValveStatus represents a valve node.
type DucoNodeBoxValveStatus struct {
	BaseDucoNodeStatus

	Trgt int
	Actl int
}

// DucoRFSensorStatus represents an RF sensor node.
type DucoRFSensorStatus struct {
	BaseDucoNodeStatus

	Temp    float64
	Co2     float64
	Rh      float64
	RssiN2M int
	HopVia  int
	RssiN2H int
}

// DucoNodeStatus is the type-safe union of node statuses returned by DucoBox nodes.
// It is implemented by DucoNodeBoxStatus, DucoNodeBoxValveStatus, and DucoRFSensorStatus.
type DucoNodeStatus interface {
	NodeDevType() string
}

// DucoReader defines the interface for reading data from the DucoBox.
type DucoReader interface {
	ReadBoxStatus(ctx context.Context) (DucoBoxStatus, error)
	ReadNodeStatus(ctx context.Context, nodeID int) (DucoNodeStatus, error)
}

// DucoRepository defines the interface for storing Duco data.
type DucoRepository interface {
	StoreBoxStatus(ctx context.Context, boxStatus DucoBoxStatus) error
	StoreNodeData(ctx context.Context, nodeData DucoNodeStatus) error
	Flush(ctx context.Context) error
	Close() error
}
