// Package ducobox implements the DucoBox ventilation reader. It calls the
// local DucoBox HTTP API and parses the JSON response into the union type
// domain.DucoNodeStatus, dispatching by the DevType field returned by the
// box. The package handles transient HTTP failures and DNS jitter on the
// home network.
package ducobox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// HTTP client tuning. A persistent client with keep-alives avoids a fresh TCP
// connection (and DNS lookup) on every poll of the DucoBox. The idle conn
// timeout is set longer than the typical poll interval so the pool reliably
// reuses connections.
const (
	httpClientTimeout       = 10 * time.Second
	httpIdleConnTimeout     = 10 * time.Minute
	httpMaxIdleConns        = 4
	httpMaxIdleConnsPerHost = 4
	httpDialTimeout         = 5 * time.Second
	httpDialKeepAlive       = 60 * time.Second
	httpTLSHandshakeTimeout = 5 * time.Second
	httpExpectContinue      = 1 * time.Second
)

// Device type strings returned by the DucoBox API in the "devtype" field.
const (
	devTypeBox   = "BOX"
	devTypeValve = "VLV"
	devTypeUCCO2 = "UCCO2"
	devTypeUCRH  = "UCRH"
)

type DucoReader struct {
	BaseURL string
	Logger  *slog.Logger
	client  *http.Client
}

func NewDucoReader(baseURL string, logger *slog.Logger) *DucoReader {
	return &DucoReader{
		BaseURL: baseURL,
		Logger:  logger,
		client:  newHTTPClient(),
	}
}

// newHTTPClient returns a tuned *http.Client with a persistent connection pool
// so that polling the same DucoBox does not re-resolve DNS on every request.
func newHTTPClient() *http.Client {
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
	}
	return &http.Client{
		Transport: transport,
		Timeout:   httpClientTimeout,
	}
}

// ReadBoxStatus fetches box-level status from the DucoBox HTTP interface.
func (dr *DucoReader) ReadBoxStatus(ctx context.Context) (domain.DucoBoxStatus, error) {
	// url is constructed from user-configured IoT device address
	url := fmt.Sprintf("%s/boxinfoget", dr.BaseURL)
	body, err := dr.doGet(ctx, url)
	if err != nil {
		dr.Logger.ErrorContext(ctx, "Failed to fetch boxinfoget", slog.Any("error", err))
		return domain.DucoBoxStatus{}, err
	}
	var dto ducoBoxStatusDTO
	if unmarshalErr := json.Unmarshal(body, &dto); unmarshalErr != nil {
		dr.Logger.ErrorContext(ctx, "Failed to unmarshal boxinfoget", slog.Any("error", unmarshalErr))
		return domain.DucoBoxStatus{}, unmarshalErr
	}
	dr.Logger.DebugContext(ctx, "Successfully fetched DucoBox status")
	return mapBoxStatus(dto), nil
}

// ReadNodeStatus fetches a single node's status by ID.
func (dr *DucoReader) ReadNodeStatus(ctx context.Context, nodeID int) (domain.DucoNodeStatus, error) {
	// url is constructed from user-configured IoT device address
	url := fmt.Sprintf("%s/nodeinfoget?node=%d", dr.BaseURL, nodeID)
	body, err := dr.doGet(ctx, url)
	if err != nil {
		dr.Logger.ErrorContext(ctx, "Failed to fetch node data", slog.Int("nodeID", nodeID), slog.Any("error", err))
		return nil, err
	}
	nodeData, parseErr := ParseDucoNodeStatus(body)
	if parseErr != nil {
		if errors.Is(parseErr, domain.ErrUnknownDevType) {
			dr.Logger.DebugContext(ctx, "Skipping unknown node")
		} else {
			dr.Logger.ErrorContext(
				ctx, "Failed to parse node data", slog.Int("nodeID", nodeID), slog.Any("error", parseErr),
			)
		}
		return nil, parseErr
	}
	return nodeData, nil
}

// doGet issues a GET against url reusing the persistent http.Client so DNS and
// TCP handshakes are amortised across calls.
func (dr *DucoReader) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := dr.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			dr.Logger.ErrorContext(ctx, "Failed to close response body", slog.Any("error", closeErr))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// ParseDucoNodeStatus parses the node data and returns the appropriate struct.
func ParseDucoNodeStatus(data []byte) (domain.DucoNodeStatus, error) {
	var base baseNodeStatusDTO
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, err
	}

	switch strings.ToUpper(base.DevType) {
	case devTypeBox:
		var dto nodeBoxStatusDTO
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return mapNodeBoxStatus(dto), nil
	case devTypeValve:
		var dto nodeBoxValveStatusDTO
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return mapNodeBoxValveStatus(dto), nil
	case devTypeUCCO2, devTypeUCRH:
		var dto rfSensorStatusDTO
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return mapRFSensorStatus(dto), nil
	default:
		return nil, fmt.Errorf("%w: %s", domain.ErrUnknownDevType, base.DevType)
	}
}
