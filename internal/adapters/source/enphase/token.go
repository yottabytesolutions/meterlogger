package enphase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

const (
	enphaseAuthURL = "https://enlighten.enphaseenergy.com/login/login.json"
	//nolint:gosec // G101: this is an API endpoint URL, not a credential
	enphaseTokenURL = "https://entrez.enphaseenergy.com/tokens"
)

type LoginData struct {
	User map[string]string `json:"user"`
}

type TokenData struct {
	SessionID string `json:"session_id"`
	SerialNum string `json:"serial_num"`
	Username  string `json:"username"`
}

func fetchToken(ctx context.Context, cfg Config) (*jwt.Token, error) {
	// Retrieve session id
	loginData := LoginData{
		User: map[string]string{
			"email": cfg.User, "password": cfg.Password,
		},
	}
	jsonData, loginMarshalErr := json.Marshal(loginData)
	if loginMarshalErr != nil {
		return nil, loginMarshalErr
	}

	req, loginErr := http.NewRequestWithContext(
		ctx, http.MethodPost, enphaseAuthURL, bytes.NewBuffer(jsonData),
	)
	if loginErr != nil {
		return nil, loginErr
	}
	req.Header.Set("Content-Type", "application/json")

	// Cloud endpoints get the verifying client. Only local Envoy requests may skip TLS verification.
	resp, loginErr := cloudClient.Do(req)
	if loginErr != nil {
		return nil, loginErr
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}

	var result map[string]any
	if unmarshalErr := json.Unmarshal(body, &result); unmarshalErr != nil {
		return nil, unmarshalErr
	}

	if resp.StatusCode != http.StatusOK {
		message, _ := result["message"].(string)
		return nil, errors.New(
			"Failed to authenticate with Enphase API. Statuscode:" + resp.Status + ". API Response:" + message,
		)
	}

	// Retrieve token
	sessionID, _ := result["session_id"].(string)
	if sessionID == "" {
		return nil, errors.New("enphase login response contains no session_id")
	}
	jsonData, marshalErr := json.Marshal(
		TokenData{
			SessionID: sessionID,
			SerialNum: cfg.Serial,
			Username:  cfg.User,
		},
	)

	if marshalErr != nil {
		return nil, marshalErr
	}

	req2, tokenErr := http.NewRequestWithContext(
		ctx, http.MethodPost, enphaseTokenURL, bytes.NewBuffer(jsonData),
	)
	if tokenErr != nil {
		return nil, tokenErr
	}
	req2.Header.Set("Content-Type", "application/json")

	resp2, tokenErr := cloudClient.Do(req2)
	if tokenErr != nil {
		return nil, tokenErr
	}

	defer func() {
		_ = resp2.Body.Close()
	}()

	body, readErr = io.ReadAll(resp2.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp2.StatusCode != http.StatusOK {
		return nil, errors.New(
			"enphase token request failed. Status: " + resp2.Status + ". Body: " + bodySnippet(body),
		)
	}
	tokenstring := string(body)
	token, _, err := new(jwt.Parser).ParseUnverified(tokenstring, &jwt.RegisteredClaims{})
	return token, err
}

const maxErrorBodySnippet = 256

// bodySnippet returns the response body truncated to a bounded length for error messages.
func bodySnippet(body []byte) string {
	if len(body) > maxErrorBodySnippet {
		return string(body[:maxErrorBodySnippet]) + "..."
	}
	return string(body)
}
