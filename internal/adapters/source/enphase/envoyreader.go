// Package enphase implements the Enphase Envoy solar reader. It obtains a
// short-lived JWT from enphaseenergy.com using the configured credentials,
// then queries the local Envoy gateway over HTTPS for production and
// inverter inventory data. Token refresh, TLS handling, and HTTP client
// tuning live here so the rest of the project sees only domain types.
package enphase

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const (
	productionDataURL  = "%s/production.json?details=1"
	inventoryDataURL   = "%s/inventory.json"
	inverterDataURL    = "%s/api/v1/production/inverters"
	deviceDataURL      = "%s/ivp/pdm/device_data"
	httpClientTimeout  = 10 * time.Second
	tokenRefreshBuffer = 3600 * time.Second // duration before expiry to refresh token

	// HTTP transport tuning. Envoy is a single host on the LAN, so a small pool
	// with a long idle timeout keeps the TCP/TLS connection alive across polls
	// and avoids re-resolving DNS and re-negotiating TLS on every read.
	httpIdleConnTimeout     = 10 * time.Minute
	httpMaxIdleConns        = 4
	httpMaxIdleConnsPerHost = 4
	httpDialTimeout         = 5 * time.Second
	httpDialKeepAlive       = 60 * time.Second
	httpTLSHandshakeTimeout = 5 * time.Second
	httpExpectContinue      = 1 * time.Second

	productionTypeInverters = "inverters"
	deviceTypePCU           = "PCU"
	deviceNamePCU           = "pcu"

	// milliUnit converts the Envoy's milli-scaled integers (mV, mA, mHz) to
	// base units (V, A, Hz).
	milliUnit = 1000.0
	// joulesPerWattHour converts joules to watt-hours (1 Wh = 3600 J).
	joulesPerWattHour = 3600.0
)

// Config holds the connection parameters for an Enphase Envoy gateway.
type Config struct {
	EnvoyURL string
	User     string
	Password string
	Serial   string
}

type EnvoyReader struct {
	cfg    Config
	logger *slog.Logger
	token  *jwt.Token
}

//nolint:gochecknoglobals // shared HTTP client for local Envoy requests
var httpClient = newEnvoyHTTPClient()

//nolint:gochecknoglobals // shared HTTP client for enphaseenergy.com cloud requests
var cloudClient = &http.Client{Timeout: httpClientTimeout}

// newEnvoyHTTPClient constructs the shared Envoy HTTP client with a tuned
// Transport. Keeping idle connections alive across polls avoids repeated DNS
// lookups and TLS handshakes against the Envoy device.
func newEnvoyHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   httpDialTimeout,
			KeepAlive: httpDialKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          httpMaxIdleConns,
		MaxIdleConnsPerHost:   httpMaxIdleConnsPerHost,
		IdleConnTimeout:       httpIdleConnTimeout,
		TLSHandshakeTimeout:   httpTLSHandshakeTimeout,
		ExpectContinueTimeout: httpExpectContinue,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // G402: InsecureSkipVerify required for local Enphase Envoy device
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   httpClientTimeout,
	}
}

func (e *EnvoyReader) ReadEnvoySolarData(ctx context.Context) (domain.EnvoySolarData, error) {
	if err := e.ensureToken(ctx); err != nil {
		e.logger.ErrorContext(ctx, "Failed to get a valid token", slog.Any("error", err))
		return domain.EnvoySolarData{}, err
	}

	meterData, err := e.getMeterData(ctx)
	if err != nil {
		e.logger.ErrorContext(ctx, "Failed to get meter data", slog.Any("error", err))
		return domain.EnvoySolarData{}, err
	}

	inventoryData, err := e.getInventoryData(ctx)
	if err != nil {
		e.logger.ErrorContext(ctx, "Failed to get inventory data", slog.Any("error", err))
		return domain.EnvoySolarData{}, err
	}

	inverterData, err := e.getInverterData(ctx)
	if err != nil {
		e.logger.ErrorContext(ctx, "Failed to get inverter data", slog.Any("error", err))
		return domain.EnvoySolarData{}, err
	}

	// device_data is supplementary: it enriches inverter rows with electrical
	// readings but the aggregate and per-panel watts come from the endpoints
	// above. A failure here (older firmware without the endpoint, or a stalled
	// Envoy) must not take down solar collection. Log and continue with empty
	// electrical fields.
	deviceData, err := e.getDeviceData(ctx)
	if err != nil {
		e.logger.WarnContext(ctx, "Failed to get device data, continuing without electrical fields",
			slog.Any("error", err))
		deviceData = nil
	}

	var inverterProductionData *Production
	for _, production := range meterData.Production {
		if production.Type == productionTypeInverters {
			inverterProductionData = &production
			break
		}
	}

	serialLookup := make(map[string]Device)
	for _, device := range *inventoryData {
		if device.Type == deviceTypePCU {
			for _, inv := range device.Devices {
				serialLookup[inv.SerialNum] = inv
			}
		}
	}

	if inverterProductionData == nil {
		e.logger.ErrorContext(ctx, "Inverter Production data not found")
		return domain.EnvoySolarData{}, errors.New("inverters data not found")
	}

	deviceLookup := deviceDataBySerial(deviceData)

	inverters := make([]domain.InverterDetails, len(*inverterData))
	for i, inverter := range *inverterData {
		invDetails := serialLookup[inverter.SerialNumber]
		inverters[i] = domain.InverterDetails{
			SerialNumber:      inverter.SerialNumber,
			Chaneid:           invDetails.Chaneid,
			Producing:         invDetails.Producing,
			Operating:         invDetails.Operating,
			Phase:             invDetails.Phase,
			Communicating:     invDetails.Communicating,
			DeviceStatus:      invDetails.DeviceStatus,
			ReportTime:        time.Unix(int64(inverter.LastReportDate), 0),
			LastReportedWatts: inverter.LastReportWatts,
			MaxReportWatts:    inverter.MaxReportWatts,
		}
		if reading, ok := deviceLookup[inverter.SerialNumber]; ok {
			applyDeviceData(&inverters[i], reading)
		}
	}

	return domain.EnvoySolarData{
		ReadingTime:  time.Unix(int64(inverterProductionData.ReadingTime), 0),
		ProductionWh: inverterProductionData.WhLifetime,
		Watt:         inverterProductionData.WNow,
		PanelCount:   inverterProductionData.ActiveCount,
		EnvoySerial:  e.cfg.Serial,
		Inverters:    inverters,
	}, nil
}

func (e *EnvoyReader) ensureToken(ctx context.Context) error {
	if e.token == nil {
		token, err := fetchToken(ctx, e.cfg)
		if err != nil {
			return err
		}
		e.token = token
	}

	return e.refreshIfExpiring(ctx)
}

func (e *EnvoyReader) refreshIfExpiring(ctx context.Context) error {
	claims, ok := e.token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return nil
	}
	if claims.ExpiresAt != nil && !claims.ExpiresAt.Time.Before(time.Now().Add(tokenRefreshBuffer)) {
		return nil
	}
	validUntil := "never"
	if claims.ExpiresAt != nil {
		validUntil = claims.ExpiresAt.Time.Format(time.RFC3339)
	}
	e.logger.InfoContext(ctx, "Token is about to expire, refreshing", slog.String("validUntil", validUntil))
	token, err := fetchToken(ctx, e.cfg)
	if err != nil {
		return err
	}
	e.token = token
	return nil
}

func (e *EnvoyReader) getInverterData(ctx context.Context) (*InverterData, error) {
	rawInverterData, err := queryEnvoy(ctx, fmt.Sprintf(inverterDataURL, e.cfg.EnvoyURL), e.token.Raw, e.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get inverter data: %w", err)
	}

	return unmarshalInverterData(rawInverterData)
}

func (e *EnvoyReader) getInventoryData(ctx context.Context) (*InventoryData, error) {
	rawInventory, err := queryEnvoy(ctx, fmt.Sprintf(inventoryDataURL, e.cfg.EnvoyURL), e.token.Raw, e.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory data: %w", err)
	}

	return unmarshalInventoryData(rawInventory)
}

func (e *EnvoyReader) getDeviceData(ctx context.Context) (DeviceData, error) {
	rawDeviceData, err := queryEnvoy(ctx, fmt.Sprintf(deviceDataURL, e.cfg.EnvoyURL), e.token.Raw, e.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get device data: %w", err)
	}

	return unmarshalDeviceData(rawDeviceData)
}

func (e *EnvoyReader) getMeterData(ctx context.Context) (*MeterReading, error) {
	rawProductionData, err := queryEnvoy(ctx, fmt.Sprintf(productionDataURL, e.cfg.EnvoyURL), e.token.Raw, e.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get meter data: %w", err)
	}
	return unmarshalMeterReading(rawProductionData)
}

func queryEnvoy(ctx context.Context, url string, token string, logger *slog.Logger) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+token)

	response, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			logger.ErrorContext(ctx, "Failed to close response body", slog.Any("error", closeErr))
		}
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code. got=%d, want=%d", response.StatusCode, http.StatusOK)
	}

	return body, nil
}

func unmarshalInverterData(body []byte) (*InverterData, error) {
	var data InverterData
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Inverter data: %w", err)
	}
	return &data, nil
}

func unmarshalMeterReading(body []byte) (*MeterReading, error) {
	var data MeterReading
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal MeterReading data: %w", err)
	}
	return &data, nil
}

func unmarshalInventoryData(body []byte) (*InventoryData, error) {
	var data InventoryData
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Inventory data: %w", err)
	}
	return &data, nil
}

// unmarshalDeviceData decodes /ivp/pdm/device_data. The response is not a pure
// map of device id to device: it also carries scalar keys at the top level
// (deviceCount, deviceDataLimit), so decoding straight into
// map[string]DeviceDataDevice fails on the first number. Decode into raw
// messages and keep only the entries that parse as a device with a name; the
// scalars and any non-object values are skipped.
func unmarshalDeviceData(body []byte) (DeviceData, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DeviceData: %w", err)
	}
	data := make(DeviceData, len(raw))
	for key, msg := range raw {
		var device DeviceDataDevice
		if err := json.Unmarshal(msg, &device); err != nil {
			continue // scalar keys such as deviceCount and deviceDataLimit
		}
		if device.DevName == "" {
			continue // not a device entry
		}
		data[key] = device
	}
	return data, nil
}

// deviceDataBySerial indexes the newest PCU channel reading per inverter
// serial. device_data is keyed by an opaque device id, so it is re-keyed by
// serial number to match the other endpoints. Non-PCU devices (eim, nsrb) and
// PCUs without a channel are skipped.
func deviceDataBySerial(data DeviceData) map[string]DeviceDataChannel {
	lookup := make(map[string]DeviceDataChannel)
	for _, device := range data {
		if device.DevName != deviceNamePCU || len(device.Channels) == 0 {
			continue
		}
		lookup[device.SerialNum] = device.Channels[0]
	}
	return lookup
}

// applyDeviceData merges the electrical measurements from one device_data
// channel into an inverter row, converting the Envoy's milli-units to base
// units and joules to watt-hours.
func applyDeviceData(inv *domain.InverterDetails, ch DeviceDataChannel) {
	r := ch.LastReading
	inv.DCVoltage = float64(r.DCVoltageMV) / milliUnit
	inv.DCCurrent = float64(r.DCCurrentMA) / milliUnit
	inv.ACVoltage = float64(r.ACVoltageMV) / milliUnit
	inv.ACCurrent = float64(r.ACCurrentMA) / milliUnit
	inv.ACFrequency = float64(r.ACFrequencyMHz) / milliUnit
	inv.TemperatureC = r.ChannelTemp
	inv.LeadingVArs = r.LeadingVArs
	inv.LaggingVArs = r.LaggingVArs
	inv.RSSI = r.RSSI
	inv.ISSI = r.ISSI
	inv.WhToday = ch.WattHours.Today
	inv.WhYesterday = ch.WattHours.Yesterday
	inv.WhWeek = ch.WattHours.Week
	inv.WhLifetime = float64(ch.Lifetime.JoulesProduced) / joulesPerWattHour
}

func NewEnvoyReader(cfg Config, logger *slog.Logger) *EnvoyReader {
	return &EnvoyReader{
		cfg:    cfg,
		logger: logger,
	}
}

type MeterReading struct {
	Production  []Production  `json:"production"`
	Consumption []Consumption `json:"consumption"`
	Storage     []Storage     `json:"storage"`
}
type Lines struct {
	WNow             float64 `json:"wNow"`
	WhLifetime       float64 `json:"whLifetime"`
	VarhLeadLifetime float64 `json:"varhLeadLifetime"`
	VarhLagLifetime  float64 `json:"varhLagLifetime"`
	VahLifetime      float64 `json:"vahLifetime"`
	RmsCurrent       float64 `json:"rmsCurrent"`
	RmsVoltage       float64 `json:"rmsVoltage"`
	ReactPwr         float64 `json:"reactPwr"`
	ApprntPwr        float64 `json:"apprntPwr"`
	PwrFactor        float64 `json:"pwrFactor"`
	WhToday          float64 `json:"whToday"`
	WhLastSevenDays  float64 `json:"whLastSevenDays"`
	VahToday         float64 `json:"vahToday"`
	VarhLeadToday    float64 `json:"varhLeadToday"`
	VarhLagToday     float64 `json:"varhLagToday"`
}
type Production struct {
	Type             string  `json:"type"`
	ActiveCount      int     `json:"activeCount"`
	ReadingTime      int     `json:"readingTime"`
	WNow             float64 `json:"wNow"`
	WhLifetime       float64 `json:"whLifetime"`
	MeasurementType  string  `json:"measurementType,omitempty"`
	VarhLeadLifetime float64 `json:"varhLeadLifetime,omitempty"`
	VarhLagLifetime  float64 `json:"varhLagLifetime,omitempty"`
	VahLifetime      float64 `json:"vahLifetime,omitempty"`
	RmsCurrent       float64 `json:"rmsCurrent,omitempty"`
	RmsVoltage       float64 `json:"rmsVoltage,omitempty"`
	ReactPwr         float64 `json:"reactPwr,omitempty"`
	ApprntPwr        float64 `json:"apprntPwr,omitempty"`
	PwrFactor        float64 `json:"pwrFactor,omitempty"`
	WhToday          float64 `json:"whToday,omitempty"`
	WhLastSevenDays  float64 `json:"whLastSevenDays,omitempty"`
	VahToday         float64 `json:"vahToday,omitempty"`
	VarhLeadToday    float64 `json:"varhLeadToday,omitempty"`
	VarhLagToday     float64 `json:"varhLagToday,omitempty"`
	Lines            []Lines `json:"lines,omitempty"`
}
type Consumption struct {
	Type             string  `json:"type"`
	ActiveCount      int     `json:"activeCount"`
	MeasurementType  string  `json:"measurementType"`
	ReadingTime      int     `json:"readingTime"`
	WNow             float64 `json:"wNow"`
	WhLifetime       float64 `json:"whLifetime"`
	VarhLeadLifetime float64 `json:"varhLeadLifetime"`
	VarhLagLifetime  float64 `json:"varhLagLifetime"`
	VahLifetime      float64 `json:"vahLifetime"`
	RmsCurrent       float64 `json:"rmsCurrent"`
	RmsVoltage       float64 `json:"rmsVoltage"`
	ReactPwr         float64 `json:"reactPwr"`
	ApprntPwr        float64 `json:"apprntPwr"`
	PwrFactor        float64 `json:"pwrFactor"`
	WhToday          float64 `json:"whToday"`
	WhLastSevenDays  float64 `json:"whLastSevenDays"`
	VahToday         float64 `json:"vahToday"`
	VarhLeadToday    float64 `json:"varhLeadToday"`
	VarhLagToday     float64 `json:"varhLagToday"`
	Lines            []Lines `json:"lines"`
}
type Storage struct {
	Type        string  `json:"type"`
	ActiveCount int     `json:"activeCount"`
	ReadingTime int     `json:"readingTime"`
	WNow        float64 `json:"wNow"`
	WhNow       float64 `json:"whNow"`
	State       string  `json:"state"`
}

type Device struct {
	PartNum        string   `json:"part_num"`
	Installed      string   `json:"installed"`
	SerialNum      string   `json:"serial_num"`
	DeviceStatus   []string `json:"device_status"`
	LastRptDate    string   `json:"last_rpt_date"`
	AdminState     int      `json:"admin_state"`
	DevType        int      `json:"dev_type"`
	CreatedDate    string   `json:"created_date"`
	ImgLoadDate    string   `json:"img_load_date"`
	ImgPnumRunning string   `json:"img_pnum_running"`
	Ptpn           string   `json:"ptpn"`
	Chaneid        int      `json:"chaneid"`
	DeviceControl  []struct {
		Gficlearset bool `json:"gficlearset"`
	} `json:"device_control"`
	Producing     bool   `json:"producing"`
	Communicating bool   `json:"communicating"`
	Provisioned   bool   `json:"provisioned"`
	Operating     bool   `json:"operating"`
	Phase         string `json:"phase"`
}

type InventoryData []struct {
	Type    string   `json:"type"`
	Devices []Device `json:"devices"`
}

type InverterData []struct {
	SerialNumber    string `json:"serialNumber"`
	LastReportDate  int    `json:"lastReportDate"`
	DevType         int    `json:"devType"`
	LastReportWatts int    `json:"lastReportWatts"`
	MaxReportWatts  int    `json:"maxReportWatts"`
}

// DeviceData is the /ivp/pdm/device_data response, a map keyed by an opaque
// device id. Only PCU (microinverter) entries carry per-panel electrical
// readings; eim and nsrb entries are ignored.
type DeviceData map[string]DeviceDataDevice

type DeviceDataDevice struct {
	DevName   string              `json:"devName"`
	SerialNum string              `json:"sn"`
	Active    bool                `json:"active"`
	Channels  []DeviceDataChannel `json:"channels"`
}

type DeviceDataChannel struct {
	ChanEid     int                   `json:"chanEid"`
	Created     int                   `json:"created"`
	WattHours   DeviceDataWattHours   `json:"wattHours"`
	Watts       DeviceDataWatts       `json:"watts"`
	LastReading DeviceDataLastReading `json:"lastReading"`
	Lifetime    DeviceDataLifetime    `json:"lifetime"`
}

type DeviceDataWattHours struct {
	Today     int `json:"today"`
	Yesterday int `json:"yesterday"`
	Week      int `json:"week"`
}

type DeviceDataWatts struct {
	Now     int `json:"now"`
	NowUsed int `json:"nowUsed"`
	Max     int `json:"max"`
}

// DeviceDataLastReading holds the most recent per-panel interval reading. The
// *MV, *MA, and *MHz fields are milli-scaled integers from the Envoy. The JSON
// tag casing is inconsistent (acVoltageINmV vs acCurrentInmA) because that is
// how the firmware spells the keys; encoding/json matches case-insensitively.
type DeviceDataLastReading struct {
	EndDate        int `json:"endDate"`
	Duration       int `json:"duration"`
	JoulesProduced int `json:"joulesProduced"`
	ACVoltageMV    int `json:"acVoltageINmV"`
	ACFrequencyMHz int `json:"acFrequencyINmHz"`
	DCVoltageMV    int `json:"dcVoltageINmV"`
	DCCurrentMA    int `json:"dcCurrentINmA"`
	ChannelTemp    int `json:"channelTemp"`
	LeadingVArs    int `json:"leadingVArs"`
	LaggingVArs    int `json:"laggingVArs"`
	ACCurrentMA    int `json:"acCurrentInmA"`
	RSSI           int `json:"rssi"`
	ISSI           int `json:"issi"`
}

type DeviceDataLifetime struct {
	CreatedTime    int   `json:"createdTime"`
	Duration       int   `json:"duration"`
	JoulesProduced int64 `json:"joulesProduced"`
}
