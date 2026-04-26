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

func fetchToken(ctx context.Context, user string, password string, serial string) (*jwt.Token, error) {
	// Retrieve session id
	loginData := LoginData{
		User: map[string]string{
			"email": user, "password": password,
		},
	}
	jsonData, _ := json.Marshal(loginData)

	req, loginErr := http.NewRequestWithContext(
		ctx, http.MethodPost, enphaseAuthURL, bytes.NewBuffer(jsonData),
	)
	if loginErr != nil {
		return nil, loginErr
	}
	req.Header.Set("Content-Type", "application/json")

	resp, loginErr := httpClient.Do(req)
	if loginErr != nil {
		return nil, loginErr
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)

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
	jsonData, marshalErr := json.Marshal(
		TokenData{
			SessionID: sessionID,
			SerialNum: serial,
			Username:  user,
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

	resp2, tokenErr := httpClient.Do(req2)
	if tokenErr != nil {
		return nil, tokenErr
	}

	defer func() {
		_ = resp2.Body.Close()
	}()

	body, _ = io.ReadAll(resp2.Body)
	tokenstring := string(body)
	token, _, err := new(jwt.Parser).ParseUnverified(tokenstring, &jwt.RegisteredClaims{})
	return token, err
}
