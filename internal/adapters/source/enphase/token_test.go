package enphase

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// testJWTHeader is the base64url-encoded header for test JWTs (alg:HS256, typ:JWT).
	testJWTHeader = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
	// testJWTFarFuturePayload is a JWT payload with exp far in the future.
	testJWTFarFuturePayload = "eyJzdWIiOiJ0ZXN0IiwiZXhwIjo5OTk5OTk5OTk5fQ"
	// testLoginPath is the Enphase login API path.
	testLoginPath = "/login/login.json"
	// testSessionID is a fake Enphase session identifier used in mock login responses.
	testSessionID = "sess123"
	// testSessionIDKey is the JSON key for the session identifier in login responses.
	testSessionIDKey = "session_id"
)

// makeExpiringToken creates a RegisteredClaims JWT with ExpiresAt already past
// (within the tokenRefreshBuffer window), so ensureToken triggers a refresh.
func makeExpiringToken() *jwt.Token {
	// exp=1 means expired in 1970 - well within the refresh buffer.
	payload := "eyJzdWIiOiJ0ZXN0IiwiZXhwIjoxfQ"
	tokenStr := testJWTHeader + "." + payload + ".signature"
	token, _, _ := new(jwt.Parser).ParseUnverified(tokenStr, &jwt.RegisteredClaims{})
	return token
}

func makeAuthServer(t *testing.T, sessionJSON string, tokenStr string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case testLoginPath:
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(sessionJSON))
				case "/tokens":
					_, _ = w.Write([]byte(tokenStr))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
		),
	)
}

// redirectTransport redirects all requests to a test server by replacing the host.
// This allows intercepting hardcoded Enphase API URLs in fetchToken.
type redirectTransport struct {
	base   string
	client *http.Client
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	//nolint:gosec // G704: test transport; base is a test server URL, not user-controlled
	baseReq, _ := http.NewRequestWithContext(req.Context(), req.Method, t.base+req.URL.Path, req.Body)
	baseReq.Header = req.Header
	//nolint:gosec // G704: test transport; base is a test server URL, not user-controlled
	return t.client.Do(baseReq)
}

func withRedirectClient(t *testing.T, server *httptest.Server) func() {
	t.Helper()
	origEnvoy := httpClient
	origCloud := cloudClient
	redirect := &http.Client{Transport: &redirectTransport{base: server.URL, client: server.Client()}}
	httpClient = redirect
	cloudClient = redirect
	return func() {
		httpClient = origEnvoy
		cloudClient = origCloud
	}
}

func TestFetchToken_Success(t *testing.T) {
	validTokenStr := testJWTHeader + "." + testJWTFarFuturePayload + ".sig"
	sessionResp, _ := json.Marshal(map[string]string{testSessionIDKey: testSessionID})

	server := makeAuthServer(t, string(sessionResp), validTokenStr)
	defer server.Close()
	defer withRedirectClient(t, server)()

	token, err := fetchToken(
		context.Background(), Config{User: "user@example.com", Password: "pass", Serial: testEnvoySerial},
	)
	if err != nil {
		t.Fatalf("fetchToken() error: %v", err)
	}
	if token == nil {
		t.Error("fetchToken() returned nil token")
	}
}

func TestFetchToken_LoginError(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("{}"))
			},
		),
	)
	defer server.Close()
	defer withRedirectClient(t, server)()

	_, err := fetchToken(
		context.Background(), Config{User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
	)
	if err == nil {
		t.Error("fetchToken() should return error on login failure")
	}
}

func TestFetchToken_InvalidLoginJSON(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == testLoginPath {
					_, _ = w.Write([]byte("not-json"))
				}
			},
		),
	)
	defer server.Close()
	defer withRedirectClient(t, server)()

	_, err := fetchToken(
		context.Background(), Config{User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
	)
	if err == nil {
		t.Error("fetchToken() should return error on invalid login JSON")
	}
}

func TestFetchToken_TokenFetchError(t *testing.T) {
	sessionResp, _ := json.Marshal(map[string]string{testSessionIDKey: testSessionID})
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case testLoginPath:
					_, _ = w.Write(sessionResp)
				case "/tokens":
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte("unauthorized"))
				}
			},
		),
	)
	defer server.Close()
	defer withRedirectClient(t, server)()

	_, err := fetchToken(
		context.Background(), Config{User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
	)
	if err == nil {
		t.Fatal("fetchToken() should return error on non-200 token response")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("fetchToken() error should include status and body snippet, got: %v", err)
	}
}

func TestFetchToken_MissingSessionID(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == testLoginPath {
					_, _ = w.Write([]byte("{}"))
				}
			},
		),
	)
	defer server.Close()
	defer withRedirectClient(t, server)()

	_, err := fetchToken(
		context.Background(), Config{User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
	)
	if err == nil {
		t.Fatal("fetchToken() should return error when session_id is missing")
	}
	if !strings.Contains(err.Error(), "session_id") {
		t.Errorf("fetchToken() error should mention session_id, got: %v", err)
	}
}

func TestBodySnippet_Truncates(t *testing.T) {
	long := strings.Repeat("x", maxErrorBodySnippet+10)
	got := bodySnippet([]byte(long))
	if len(got) != maxErrorBodySnippet+3 || !strings.HasSuffix(got, "...") {
		t.Errorf("bodySnippet() did not truncate as expected, len=%d", len(got))
	}
	if bodySnippet([]byte("short")) != "short" {
		t.Error("bodySnippet() should return short bodies unchanged")
	}
}

// TestEnvoyClient_AcceptsSelfSignedTLS proves the local Envoy client still accepts
// a self-signed certificate, matching the LAN device behavior.
func TestEnvoyClient_AcceptsSelfSignedTLS(t *testing.T) {
	server := httptest.NewTLSServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("{}"))
			},
		),
	)
	defer server.Close()

	body, err := queryEnvoy(context.Background(), server.URL, "token", testLogger())
	if err != nil {
		t.Fatalf("envoy client should accept self-signed TLS, got error: %v", err)
	}
	if string(body) != "{}" {
		t.Errorf("unexpected body: %q", body)
	}
}

// TestCloudClient_RejectsSelfSignedTLS proves the cloud client verifies TLS
// certificates, so credentials never go over an unverified connection.
func TestCloudClient_RejectsSelfSignedTLS(t *testing.T) {
	server := httptest.NewTLSServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("{}"))
			},
		),
	)
	defer server.Close()

	req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if reqErr != nil {
		t.Fatalf("failed to build request: %v", reqErr)
	}
	resp, err := cloudClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("cloud client should reject self-signed TLS")
	}
	var certErr *tls.CertificateVerificationError
	if !errors.As(err, &certErr) {
		t.Errorf("expected certificate verification error, got: %v", err)
	}
}

func TestEnsureToken_ExpiringToken_RefreshFails(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("{}"))
			},
		),
	)
	defer server.Close()
	defer withRedirectClient(t, server)()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: testEnvoyURL, User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
		logger: testLogger(),
		token:  makeExpiringToken(),
	}

	if err := reader.ensureToken(context.Background()); err == nil {
		t.Error("ensureToken() should return error when token refresh fails")
	}
}

func TestEnsureToken_NilToken_FetchFails(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("{}"))
			},
		),
	)
	defer server.Close()
	defer withRedirectClient(t, server)()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: testEnvoyURL, User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
		logger: testLogger(),
		token:  nil,
	}

	if err := reader.ensureToken(context.Background()); err == nil {
		t.Error("ensureToken() should return error when token fetch fails")
	}
}

func TestEnsureToken_NilExpiresAt_RefreshFails(t *testing.T) {
	// JWT with RegisteredClaims but no ExpiresAt - triggers the ExpiresAt == nil branch.
	payload := "eyJzdWIiOiJ0ZXN0In0"
	tokenStr := testJWTHeader + "." + payload + ".sig"
	token, _, _ := new(jwt.Parser).ParseUnverified(tokenStr, &jwt.RegisteredClaims{})

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("{}"))
			},
		),
	)
	defer server.Close()
	defer withRedirectClient(t, server)()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: testEnvoyURL, User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
		logger: testLogger(),
		token:  token,
	}

	if err := reader.ensureToken(context.Background()); err == nil {
		t.Error("ensureToken() should return error when nil ExpiresAt triggers refresh and refresh fails")
	}
}

func TestEnsureToken_ExpiringToken_RefreshSucceeds(t *testing.T) {
	validTokenStr := testJWTHeader + "." + testJWTFarFuturePayload + ".sig"
	sessionResp, _ := json.Marshal(map[string]string{testSessionIDKey: testSessionID})

	server := makeAuthServer(t, string(sessionResp), validTokenStr)
	defer server.Close()
	defer withRedirectClient(t, server)()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: testEnvoyURL, User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
		logger: testLogger(),
		token:  makeExpiringToken(),
	}

	if err := reader.ensureToken(context.Background()); err != nil {
		t.Errorf("ensureToken() unexpected error after successful refresh: %v", err)
	}
	if reader.token == nil {
		t.Error("ensureToken() did not update token")
	}
}

func TestReadEnvoySolarData_EnsureTokenFails(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
		),
	)
	defer server.Close()
	defer withRedirectClient(t, server)()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: server.URL, User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
		logger: testLogger(),
		token:  nil,
	}

	if _, err := reader.ReadEnvoySolarData(context.Background()); err == nil {
		t.Error("ReadEnvoySolarData() should return error when ensureToken fails")
	}
}
