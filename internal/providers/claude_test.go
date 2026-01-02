package providers

import (
	"context"
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
	claudeDir := filepath.Join(tempDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(claudeDir, ".credentials.json")
	credsJSON := `{"claudeAiOauth": {"accessToken": "test-token", "expiresAt": 1767396165210}}`
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
	if rows[0].Provider != "Claude" {
		t.Errorf("row[0].Provider = %q, want %q", rows[0].Provider, "Claude")
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
	if rows[1].Provider != "Claude" {
		t.Errorf("row[1].Provider = %q, want %q", rows[1].Provider, "Claude")
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
	expectedPath := filepath.Join(tempDir, ".claude", ".credentials.json")
	if rows[0].WarningMsg != "credentials file not found: "+expectedPath {
		t.Errorf("row[0].WarningMsg = %q, want to contain path %q", rows[0].WarningMsg, expectedPath)
	}
}

func TestClaudeProvider_FetchUsage_MalformedCreds(t *testing.T) {
	tempDir := t.TempDir()
	claudeDir := filepath.Join(tempDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(claudeDir, ".credentials.json")
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
	if rows[0].Provider != "Claude" {
		t.Errorf("row[0].Provider = %q, want %q", rows[0].Provider, "Claude")
	}
	if rows[0].WarningMsg == "" {
		t.Error("row[0].WarningMsg should contain parse error")
	}
}

func TestClaudeProvider_FetchUsage_EmptyToken(t *testing.T) {
	tempDir := t.TempDir()
	claudeDir := filepath.Join(tempDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(claudeDir, ".credentials.json")
	credsJSON := `{"claudeAiOauth": {"accessToken": "", "expiresAt": 1767396165210}}`
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
	if rows[0].WarningMsg != "no access token found in credentials" {
		t.Errorf("row[0].WarningMsg = %q, want %q", rows[0].WarningMsg, "no access token found in credentials")
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
	claudeDir := filepath.Join(tempDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(claudeDir, ".credentials.json")
	credsJSON := `{"claudeAiOauth": {"accessToken": "invalid-token", "expiresAt": 1767396165210}}`
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
	if rows[0].Provider != "Claude" {
		t.Errorf("row[0].Provider = %q, want %q", rows[0].Provider, "Claude")
	}
	if rows[0].WarningMsg != "API returned status 401" {
		t.Errorf("row[0].WarningMsg = %q, want %q", rows[0].WarningMsg, "API returned status 401")
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
	claudeDir := filepath.Join(tempDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}

	credsPath := filepath.Join(claudeDir, ".credentials.json")
	credsJSON := `{"claudeAiOauth": {"accessToken": "test-token", "expiresAt": 1767396165210}}`
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
	if rows[0].WarningMsg != "API returned status 500" {
		t.Errorf("row[0].WarningMsg = %q, want %q", rows[0].WarningMsg, "API returned status 500")
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
