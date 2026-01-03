package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeProvider_Name(t *testing.T) {
	p := &ClaudeProvider{}
	if got := p.Name(); got != "Claude" {
		t.Errorf("Name() = %q, want %q", got, "Claude")
	}
}

func TestClaudeProvider_FetchUsage_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != claudeAPIPath {
			t.Errorf("expected path %s, got %s", claudeAPIPath, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("expected Authorization 'Bearer test-token', got %q", auth)
		}
		if beta := r.Header.Get("anthropic-beta"); beta != claudeAnthropicBeta {
			t.Errorf("expected anthropic-beta %q, got %q", claudeAnthropicBeta, beta)
		}
		if accept := r.Header.Get("Accept"); accept != "application/json" {
			t.Errorf("expected Accept 'application/json', got %q", accept)
		}
		if ua := r.Header.Get("User-Agent"); ua != "ai-meter/0.1.0" {
			t.Errorf("expected User-Agent 'ai-meter/0.1.0', got %q", ua)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"five_hour": {
				"utilization": 24.0,
				"resets_at": "2026-01-02T19:59:59+00:00"
			},
			"seven_day": {
				"utilization": 36.0,
				"resets_at": "2026-01-08T06:59:59+00:00"
			}
		}`))
	}))
	defer server.Close()

	// Create temp credentials
	tempDir := t.TempDir()
	credDir := filepath.Join(tempDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(credDir, "claude-user@example.com.json")
	credsJSON := `{"access_token": "test-token", "refresh_token": "refresh-token", "type": "claude"}`
	if err := os.WriteFile(credsPath, []byte(credsJSON), 0600); err != nil {
		t.Fatal(err)
	}

	// Create provider with test config
	p := &ClaudeProvider{
		homeDir: tempDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Check 5-hour row
	if rows[0].Provider != "Claude (user@example.com)" {
		t.Errorf("row[0].Provider = %q, want %q", rows[0].Provider, "Claude (user@example.com)")
	}
	if rows[0].Label != "5-hour" {
		t.Errorf("row[0].Label = %q, want %q", rows[0].Label, "5-hour")
	}
	if rows[0].UsagePercent != 24.0 {
		t.Errorf("row[0].UsagePercent = %f, want %f", rows[0].UsagePercent, 24.0)
	}
	if rows[0].IsWarning {
		t.Error("row[0].IsWarning = true, want false")
	}

	// Check 7-day row
	if rows[1].Provider != "Claude (user@example.com)" {
		t.Errorf("row[1].Provider = %q, want %q", rows[1].Provider, "Claude (user@example.com)")
	}
	if rows[1].Label != "7-day" {
		t.Errorf("row[1].Label = %q, want %q", rows[1].Label, "7-day")
	}
	if rows[1].UsagePercent != 36.0 {
		t.Errorf("row[1].UsagePercent = %f, want %f", rows[1].UsagePercent, 36.0)
	}
	if rows[1].IsWarning {
		t.Error("row[1].IsWarning = true, want false")
	}

	// Verify reset times are parsed correctly
	expectedFiveHour := time.Date(2026, 1, 2, 19, 59, 59, 0, time.UTC)
	if !rows[0].ResetTime.Equal(expectedFiveHour) {
		t.Errorf("row[0].ResetTime = %v, want %v", rows[0].ResetTime, expectedFiveHour)
	}

	expectedSevenDay := time.Date(2026, 1, 8, 6, 59, 59, 0, time.UTC)
	if !rows[1].ResetTime.Equal(expectedSevenDay) {
		t.Errorf("row[1].ResetTime = %v, want %v", rows[1].ResetTime, expectedSevenDay)
	}
}

func TestClaudeProvider_FetchUsage_MissingCreds(t *testing.T) {
	tempDir := t.TempDir()

	p := &ClaudeProvider{
		homeDir: tempDir,
		baseURL: "http://localhost:9999",
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 warning row, got %d", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("row[0].IsWarning = false, want true")
	}
	if rows[0].Provider != "Claude" {
		t.Errorf("row[0].Provider = %q, want %q", rows[0].Provider, "Claude")
	}
	expectedPattern := filepath.Join(tempDir, ".cli-proxy-api", "claude-*.json")
	expectedMsg := "No credential files found matching " + expectedPattern
	if rows[0].WarningMsg != expectedMsg {
		t.Errorf("row[0].WarningMsg = %q, want %q", rows[0].WarningMsg, expectedMsg)
	}
}

func TestClaudeProvider_FetchUsage_MalformedCreds(t *testing.T) {
	tempDir := t.TempDir()
	credDir := filepath.Join(tempDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(credDir, "claude-user@example.com.json")
	if err := os.WriteFile(credsPath, []byte("not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	p := &ClaudeProvider{
		homeDir: tempDir,
		baseURL: "http://localhost:9999",
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 warning row, got %d", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("row[0].IsWarning = false, want true")
	}
	if rows[0].Provider != "Claude (user@example.com)" {
		t.Errorf("row[0].Provider = %q, want %q", rows[0].Provider, "Claude (user@example.com)")
	}
	if rows[0].WarningMsg == "" {
		t.Error("row[0].WarningMsg should contain parse error")
	}
}

func TestClaudeProvider_FetchUsage_EmptyToken(t *testing.T) {
	tempDir := t.TempDir()
	credDir := filepath.Join(tempDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(credDir, "claude-user@example.com.json")
	credsJSON := `{"access_token": "", "refresh_token": "refresh-token", "type": "claude"}`
	if err := os.WriteFile(credsPath, []byte(credsJSON), 0600); err != nil {
		t.Fatal(err)
	}

	p := &ClaudeProvider{
		homeDir: tempDir,
		baseURL: "http://localhost:9999",
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 warning row, got %d", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("row[0].IsWarning = false, want true")
	}
	if rows[0].WarningMsg != "failed to load credentials: no access token found in credentials" {
		t.Errorf("row[0].WarningMsg = %q, want %q", rows[0].WarningMsg, "failed to load credentials: no access token found in credentials")
	}
}

func TestClaudeProvider_FetchUsage_APIError(t *testing.T) {
	// Create mock server that returns 401
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	// Create temp credentials
	tempDir := t.TempDir()
	credDir := filepath.Join(tempDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(credDir, "claude-user@example.com.json")
	credsJSON := `{"access_token": "invalid-token", "type": "claude"}`
	if err := os.WriteFile(credsPath, []byte(credsJSON), 0600); err != nil {
		t.Fatal(err)
	}

	p := &ClaudeProvider{
		homeDir: tempDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 warning row, got %d", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("row[0].IsWarning = false, want true")
	}
	if rows[0].Provider != "Claude (user@example.com)" {
		t.Errorf("row[0].Provider = %q, want %q", rows[0].Provider, "Claude (user@example.com)")
	}
	expectedMsg := "authentication failed (token may be expired)"
	if rows[0].WarningMsg != expectedMsg {
		t.Errorf("row[0].WarningMsg = %q, want %q", rows[0].WarningMsg, expectedMsg)
	}
}

func TestClaudeProvider_FetchUsage_APIError403Revoked(t *testing.T) {
	// Create mock server that returns 403 with revoked message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"permission_error","message":"OAuth token has been revoked."}}`))
	}))
	defer server.Close()

	// Create temp credentials
	tempDir := t.TempDir()
	credDir := filepath.Join(tempDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(credDir, "claude-user@example.com.json")
	credsJSON := `{"access_token": "invalid-token", "type": "claude"}`
	if err := os.WriteFile(credsPath, []byte(credsJSON), 0600); err != nil {
		t.Fatal(err)
	}

	p := &ClaudeProvider{
		homeDir: tempDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 warning row, got %d", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("row[0].IsWarning = false, want true")
	}
	expectedMsg := "authentication failed (token revoked)"
	if rows[0].WarningMsg != expectedMsg {
		t.Errorf("row[0].WarningMsg = %q, want %q", rows[0].WarningMsg, expectedMsg)
	}
}

func TestClaudeProvider_FetchUsage_RefreshesTokenOn401(t *testing.T) {
	refreshCalls := 0
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls++
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to parse refresh payload: %v", err)
		}
		if payload["grant_type"] != "refresh_token" {
			t.Errorf("expected grant_type refresh_token, got %q", payload["grant_type"])
		}
		if payload["refresh_token"] != "refresh-token" {
			t.Errorf("expected refresh_token refresh-token, got %q", payload["refresh_token"])
		}
		if payload["client_id"] != claudeClientID {
			t.Errorf("expected client_id %q, got %q", claudeClientID, payload["client_id"])
		}
		if payload["scope"] != "user:profile user:inference user:sessions:claude_code" {
			t.Errorf("unexpected scope: %q", payload["scope"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600,"scope":"user:profile user:inference user:sessions:claude_code"}`))
	}))
	defer refreshServer.Close()

	usageCalls := 0
	usageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usageCalls++
		if r.Header.Get("Authorization") != "Bearer new-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"five_hour": {
				"utilization": 10.0,
				"resets_at": "2026-01-02T19:59:59+00:00"
			},
			"seven_day": {
				"utilization": 20.0,
				"resets_at": "2026-01-08T06:59:59+00:00"
			}
		}`))
	}))
	defer usageServer.Close()

	tempDir := t.TempDir()
	credDir := filepath.Join(tempDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(credDir, "claude-user@example.com.json")
	credsJSON := `{"access_token": "old-token", "refresh_token": "refresh-token", "type": "claude"}`
	if err := os.WriteFile(credsPath, []byte(credsJSON), 0600); err != nil {
		t.Fatal(err)
	}

	p := &ClaudeProvider{
		homeDir:  tempDir,
		baseURL:  usageServer.URL,
		tokenURL: refreshServer.URL,
		client:   &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if refreshCalls != 1 {
		t.Errorf("expected 1 refresh call, got %d", refreshCalls)
	}
	if usageCalls != 2 {
		t.Errorf("expected 2 usage calls (401 + retry), got %d", usageCalls)
	}
	for _, row := range rows {
		if row.IsWarning {
			t.Errorf("unexpected warning row: %s", row.WarningMsg)
		}
	}

	updated, err := os.ReadFile(credsPath)
	if err != nil {
		t.Fatalf("failed to read updated credentials: %v", err)
	}
	var updatedCred claudeCredentials
	if err := json.Unmarshal(updated, &updatedCred); err != nil {
		t.Fatalf("failed to parse updated credentials: %v", err)
	}
	if updatedCred.AccessToken != "new-token" {
		t.Errorf("updated access_token = %q, want %q", updatedCred.AccessToken, "new-token")
	}
	if updatedCred.RefreshToken != "new-refresh" {
		t.Errorf("updated refresh_token = %q, want %q", updatedCred.RefreshToken, "new-refresh")
	}
	if updatedCred.LastRefresh == "" {
		t.Error("expected last_refresh to be set in credentials")
	}
	if updatedCred.Expired == "" {
		t.Error("expected expired to be set in credentials")
	}
}

func TestClaudeProvider_FetchUsage_APIError500(t *testing.T) {
	// Create mock server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	// Create temp credentials
	tempDir := t.TempDir()
	credDir := filepath.Join(tempDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(credDir, "claude-user@example.com.json")
	credsJSON := `{"access_token": "test-token", "refresh_token": "refresh-token", "type": "claude"}`
	if err := os.WriteFile(credsPath, []byte(credsJSON), 0600); err != nil {
		t.Fatal(err)
	}

	p := &ClaudeProvider{
		homeDir: tempDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected 1 warning row, got %d", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("row[0].IsWarning = false, want true")
	}
	expectedMsg := `API returned status 500: {"error": "internal server error"}`
	if rows[0].WarningMsg != expectedMsg {
		t.Errorf("row[0].WarningMsg = %q, want %q", rows[0].WarningMsg, expectedMsg)
	}
}

func TestClaudeProvider_FetchUsage_MalformedTimestamp(t *testing.T) {
	// Create mock server that returns invalid timestamps
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"five_hour": {
				"utilization": 24.0,
				"resets_at": "not-a-valid-timestamp"
			},
			"seven_day": {
				"utilization": 36.0,
				"resets_at": "2026-01-08T06:59:59+00:00"
			}
		}`))
	}))
	defer server.Close()

	// Create temp credentials
	tempDir := t.TempDir()
	credDir := filepath.Join(tempDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(credDir, "claude-user@example.com.json")
	credsJSON := `{"access_token": "test-token", "refresh_token": "refresh-token", "type": "claude"}`
	if err := os.WriteFile(credsPath, []byte(credsJSON), 0600); err != nil {
		t.Fatal(err)
	}

	p := &ClaudeProvider{
		homeDir: tempDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	// Should return 2 rows: 1 warning for malformed timestamp, 1 data row for valid entry
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// First row should be a warning about parse error
	if !rows[0].IsWarning {
		t.Error("row[0].IsWarning = false, want true")
	}
	if rows[0].Label != "5-hour" {
		t.Errorf("row[0].Label = %q, want %q", rows[0].Label, "5-hour")
	}

	// Second row should be valid data
	if rows[1].IsWarning {
		t.Error("row[1].IsWarning = true, want false")
	}
	if rows[1].Label != "7-day" {
		t.Errorf("row[1].Label = %q, want %q", rows[1].Label, "7-day")
	}
	if rows[1].UsagePercent != 36.0 {
		t.Errorf("row[1].UsagePercent = %f, want %f", rows[1].UsagePercent, 36.0)
	}
}

func TestClaudeProvider_FetchUsage_EmptyResetTime(t *testing.T) {
	// Create mock server that returns an empty reset time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"five_hour": {
				"utilization": 12.0,
				"resets_at": ""
			},
			"seven_day": {
				"utilization": 34.0,
				"resets_at": "2026-01-08T06:59:59+00:00"
			}
		}`))
	}))
	defer server.Close()

	// Create temp credentials
	tempDir := t.TempDir()
	credDir := filepath.Join(tempDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(credDir, "claude-user@example.com.json")
	credsJSON := `{"access_token": "test-token", "refresh_token": "refresh-token", "type": "claude"}`
	if err := os.WriteFile(credsPath, []byte(credsJSON), 0600); err != nil {
		t.Fatal(err)
	}

	p := &ClaudeProvider{
		homeDir: tempDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := p.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	if rows[0].IsWarning {
		t.Fatalf("row[0].IsWarning = true, want false")
	}
	if !rows[0].ResetTime.IsZero() {
		t.Fatalf("row[0].ResetTime = %v, want zero", rows[0].ResetTime)
	}
	if rows[1].IsWarning {
		t.Fatalf("row[1].IsWarning = true, want false")
	}
}

func TestNewClaudeProvider(t *testing.T) {
	p, err := NewClaudeProvider()
	if err != nil {
		t.Fatalf("NewClaudeProvider() error = %v", err)
	}

	if p.baseURL != claudeDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", p.baseURL, claudeDefaultBaseURL)
	}
	if p.client == nil {
		t.Error("client is nil")
	}
	if p.client.Timeout != claudeTimeout {
		t.Errorf("client.Timeout = %v, want %v", p.client.Timeout, claudeTimeout)
	}
	if p.homeDir == "" {
		t.Error("homeDir is empty")
	}
}
