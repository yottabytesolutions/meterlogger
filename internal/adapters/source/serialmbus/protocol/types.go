package protocol

type FrameType int

const (
	ShortFrame FrameType = iota
	ControlFrame
	LongFrame
	AckFrame
)

const (
	ShortFrameHeader         = 0x10
	ControlOrLongFrameHeader = 0x68
	AckFrameByte             = 0xE5
	StopByte                 = 0x16
)

//nolint:gochecknoglobals // protocol constant used across the package
var AckFrameBytes = []byte{AckFrameByte}

type Frame interface {
	ToBytes() []byte
	FrameType() FrameType
	Validate() bool
	Prepare() Frame
}

type AckFrameStruct struct{}

type ShortFrameStruct struct {
	Control  byte
	Address  byte
	Checksum byte
}

type ControlFrameStruct struct {
	Length      byte
	Control     byte
	Address     byte
	ControlInfo byte
	Checksum    byte
}

type LongFrameStruct struct {
	Length      byte
	Control     byte
	Address     byte
	ControlInfo byte
	Data        []byte
	Checksum    byte
}

type SlaveInformation struct {
	ID           int64
	Manufacturer string
	Version      int32
	Medium       string
	AccessNumber int32
	Status       []byte
	Signature    []byte
}

type DataRecord struct {
	DIB       []byte
	VIB       []byte
	DataValue []byte
}
type HeatTelegram struct {
	SlaveInformation SlaveInformation
	Datarecords      []DataRecord
}
