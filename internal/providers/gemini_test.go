package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGeminiProvider_Name(t *testing.T) {
	p := &GeminiProvider{}
	if got := p.Name(); got != "Gemini" {
		t.Errorf("Name() = %q, want %q", got, "Gemini")
	}
}

func TestGeminiProvider_FetchUsage_SingleAccount(t *testing.T) {
	// Set up mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != geminiEndpoint {
			t.Errorf("Expected path %s, got %s", geminiEndpoint, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Expected Authorization header, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("User-Agent") != "ai-meter/0.1.0" {
			t.Errorf("Expected User-Agent ai-meter/0.1.0, got %s", r.Header.Get("User-Agent"))
		}

		// Return mock response
		resp := geminiQuotaResponse{
			Buckets: []geminiQuotaBucket{
				{
					ModelID:           "gemini-2.5-pro",
					TokenType:         "REQUESTS",
					RemainingFraction: 0.75,
					ResetTime:         "2025-10-22T16:01:15Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Set up temp credential directory
	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create credential file
	cred := map[string]any{
		"token": map[string]string{
			"access_token": "test-token",
		},
		"project_id": "gen-lang-client-123",
	}
	credData, _ := json.Marshal(cred)
	credFile := filepath.Join(credDir, "gemini-user@example.com-gen-lang-client-123.json")
	if err := os.WriteFile(credFile, credData, 0600); err != nil {
		t.Fatal(err)
	}

	// Create provider
	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	// Fetch usage
	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	// Verify results
	if len(rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(rows))
	}

	row := rows[0]
	if row.Provider != "Gemini (user@example.com)" {
		t.Errorf("Provider = %q, want %q", row.Provider, "Gemini (user@example.com)")
	}
	if row.Label != "gemini-2.5-pro" {
		t.Errorf("Label = %q, want %q", row.Label, "gemini-2.5-pro")
	}
	// 0.75 remaining = 25% used
	if row.UsagePercent != 25.0 {
		t.Errorf("UsagePercent = %v, want %v", row.UsagePercent, 25.0)
	}
	expectedTime, _ := time.Parse(time.RFC3339, "2025-10-22T16:01:15Z")
	if !row.ResetTime.Equal(expectedTime) {
		t.Errorf("ResetTime = %v, want %v", row.ResetTime, expectedTime)
	}
	if row.IsWarning {
		t.Error("Expected IsWarning = false")
	}
}

func TestGeminiProvider_FetchUsage_FractionalSeconds(t *testing.T) {
	// Test that timestamps with fractional seconds (e.g. from API responses)
	// are correctly parsed using RFC3339Nano format
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiQuotaResponse{
			Buckets: []geminiQuotaBucket{
				{
					ModelID:           "gemini-2.5-pro",
					TokenType:         "REQUESTS",
					RemainingFraction: 0.6,
					ResetTime:         "2026-01-02T16:30:10.995545Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	cred := map[string]any{
		"token":      map[string]string{"access_token": "token"},
		"project_id": "proj",
	}
	data, _ := json.Marshal(cred)
	if err := os.WriteFile(filepath.Join(credDir, "gemini-user@test.com-proj.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(rows))
	}

	row := rows[0]
	if row.IsWarning {
		t.Errorf("Expected data row, got warning: %s", row.WarningMsg)
	}

	// Verify the fractional seconds timestamp was parsed correctly
	expectedTime, _ := time.Parse(time.RFC3339Nano, "2026-01-02T16:30:10.995545Z")
	if !row.ResetTime.Equal(expectedTime) {
		t.Errorf("ResetTime = %v, want %v", row.ResetTime, expectedTime)
	}

	// 0.6 remaining = 40% used
	if row.UsagePercent != 40.0 {
		t.Errorf("UsagePercent = %v, want %v", row.UsagePercent, 40.0)
	}
}

func TestGeminiProvider_FetchUsage_MultipleBuckets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiQuotaResponse{
			Buckets: []geminiQuotaBucket{
				{
					ModelID:           "gemini-2.5-pro",
					TokenType:         "REQUESTS",
					RemainingFraction: 0.5,
					ResetTime:         "2025-10-22T16:01:15Z",
				},
				{
					ModelID:           "gemini-2.5-flash",
					TokenType:         "REQUESTS",
					RemainingFraction: 0.9,
					ResetTime:         "2025-10-22T18:00:00Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	cred := map[string]any{
		"token":      map[string]string{"access_token": "token1"},
		"project_id": "proj-1",
	}
	credData, _ := json.Marshal(cred)
	if err := os.WriteFile(filepath.Join(credDir, "gemini-alice@test.com-proj-1.json"), credData, 0600); err != nil {
		t.Fatal(err)
	}

	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("Expected 2 rows, got %d", len(rows))
	}

	// First row: 0.5 remaining = 50% used
	if rows[0].Label != "gemini-2.5-pro" {
		t.Errorf("rows[0].Label = %q, want %q", rows[0].Label, "gemini-2.5-pro")
	}
	if rows[0].UsagePercent != 50.0 {
		t.Errorf("rows[0].UsagePercent = %v, want %v", rows[0].UsagePercent, 50.0)
	}

	// Second row: 0.9 remaining = 10% used
	if rows[1].Label != "gemini-2.5-flash" {
		t.Errorf("rows[1].Label = %q, want %q", rows[1].Label, "gemini-2.5-flash")
	}
	// Use tolerance for floating point comparison
	if diff := rows[1].UsagePercent - 10.0; diff > 0.001 || diff < -0.001 {
		t.Errorf("rows[1].UsagePercent = %v, want %v", rows[1].UsagePercent, 10.0)
	}
}

func TestGeminiProvider_FetchUsage_MultipleAccounts(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := geminiQuotaResponse{
			Buckets: []geminiQuotaBucket{
				{
					ModelID:           "gemini-2.5-pro",
					TokenType:         "REQUESTS",
					RemainingFraction: 0.8,
					ResetTime:         "2025-10-22T16:01:15Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Two accounts
	cred1 := map[string]any{
		"token":      map[string]string{"access_token": "token1"},
		"project_id": "proj-1",
	}
	cred2 := map[string]any{
		"token":      map[string]string{"access_token": "token2"},
		"project_id": "proj-2",
	}
	data1, _ := json.Marshal(cred1)
	data2, _ := json.Marshal(cred2)
	if err := os.WriteFile(filepath.Join(credDir, "gemini-alice@test.com-proj-1.json"), data1, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "gemini-bob@test.com-proj-2.json"), data2, 0600); err != nil {
		t.Fatal(err)
	}

	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	// Should have made 2 API calls
	if callCount != 2 {
		t.Errorf("Expected 2 API calls, got %d", callCount)
	}

	// Should have 2 rows (one per account)
	if len(rows) != 2 {
		t.Fatalf("Expected 2 rows, got %d", len(rows))
	}

	// Check that both accounts are represented
	providers := map[string]bool{}
	for _, row := range rows {
		providers[row.Provider] = true
	}
	if !providers["Gemini (alice@test.com)"] || !providers["Gemini (bob@test.com)"] {
		t.Errorf("Expected both accounts, got providers: %v", providers)
	}
}

func TestGeminiProvider_FetchUsage_NoCreds(t *testing.T) {
	tmpDir := t.TempDir()
	// Don't create .cli-proxy-api directory

	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: "http://unused",
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	// Should return warning about no credentials
	if len(rows) != 1 {
		t.Fatalf("Expected 1 warning row, got %d rows", len(rows))
	}

	row := rows[0]
	if !row.IsWarning {
		t.Error("Expected IsWarning = true")
	}
	if row.Provider != "Gemini" {
		t.Errorf("Provider = %q, want %q", row.Provider, "Gemini")
	}
	// Message now uses source.DisplayName() dynamically
	if !strings.Contains(row.WarningMsg, "No valid credentials found for") {
		t.Errorf("WarningMsg = %q, want prefix 'No valid credentials found for'", row.WarningMsg)
	}
}

func TestGeminiProvider_FetchUsage_MissingProjectID(t *testing.T) {
	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	// File with token but no project_id - should report a warning since it looks like
	// a Gemini credential file but is malformed
	cred := map[string]any{
		"token": map[string]string{"access_token": "token1"},
		// Missing project_id
	}
	data, _ := json.Marshal(cred)
	if err := os.WriteFile(filepath.Join(credDir, "gemini-user@example.com-proj.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: "http://unused",
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	// Should get two warnings: one about the parse failure, one about no valid creds
	if len(rows) != 2 {
		t.Fatalf("Expected 2 warning rows, got %d", len(rows))
	}

	// Check first warning is about parse failure
	if !rows[0].IsWarning {
		t.Error("Expected rows[0].IsWarning = true")
	}
	if !strings.Contains(rows[0].WarningMsg, "Failed to parse") || !strings.Contains(rows[0].WarningMsg, "missing project_id") {
		t.Errorf("Expected warning about failed parse with missing project_id, got: %s", rows[0].WarningMsg)
	}

	// Check second warning is about no valid credentials
	if !rows[1].IsWarning {
		t.Error("Expected rows[1].IsWarning = true")
	}
	if !strings.Contains(rows[1].WarningMsg, "No valid credentials found for") {
		t.Errorf("Expected warning about no valid credentials, got: %s", rows[1].WarningMsg)
	}
}

func TestGeminiProvider_RemainingFractionConversion(t *testing.T) {
	tests := []struct {
		name              string
		remainingFraction float64
		wantUsedPercent   float64
	}{
		{"75% remaining = 25% used", 0.75, 25.0},
		{"0% remaining = 100% used", 0.0, 100.0},
		{"100% remaining = 0% used", 1.0, 0.0},
		{"50% remaining = 50% used", 0.5, 50.0},
		{">100% remaining clamped = 0% used", 1.5, 0.0},
		{"<0% remaining clamped = 100% used", -0.1, 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := geminiQuotaResponse{
					Buckets: []geminiQuotaBucket{
						{
							ModelID:           "test-model",
							TokenType:         "REQUESTS",
							RemainingFraction: tt.remainingFraction,
							ResetTime:         "2025-10-22T16:01:15Z",
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			tmpDir := t.TempDir()
			credDir := filepath.Join(tmpDir, ".cli-proxy-api")
			if err := os.MkdirAll(credDir, 0755); err != nil {
				t.Fatal(err)
			}

			cred := map[string]any{
				"token":      map[string]string{"access_token": "token"},
				"project_id": "proj",
			}
			data, _ := json.Marshal(cred)
			if err := os.WriteFile(filepath.Join(credDir, "gemini-user@test.com-proj.json"), data, 0600); err != nil {
				t.Fatal(err)
			}

			provider := &GeminiProvider{
				homeDir: tmpDir,
				baseURL: server.URL,
				client:  &http.Client{Timeout: 5 * time.Second},
			}

			rows, err := provider.FetchUsage(context.Background())
			if err != nil {
				t.Fatalf("FetchUsage() error = %v", err)
			}

			if len(rows) != 1 {
				t.Fatalf("Expected 1 row, got %d", len(rows))
			}

			if rows[0].UsagePercent != tt.wantUsedPercent {
				t.Errorf("UsagePercent = %v, want %v", rows[0].UsagePercent, tt.wantUsedPercent)
			}
		})
	}
}

func TestGeminiProvider_EmptyBuckets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiQuotaResponse{
			Buckets: []geminiQuotaBucket{}, // Empty array
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	cred := map[string]any{
		"token":      map[string]string{"access_token": "token"},
		"project_id": "proj",
	}
	data, _ := json.Marshal(cred)
	if err := os.WriteFile(filepath.Join(credDir, "gemini-user@test.com-proj.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("Expected 1 warning row, got %d", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("Expected IsWarning = true")
	}
	if !strings.Contains(rows[0].WarningMsg, "Empty buckets") {
		t.Errorf("Expected warning about empty buckets, got: %s", rows[0].WarningMsg)
	}
}

func TestGeminiProvider_FetchUsage_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	cred := map[string]any{
		"token":      map[string]string{"access_token": "bad-token"},
		"project_id": "proj",
	}
	data, _ := json.Marshal(cred)
	if err := os.WriteFile(filepath.Join(credDir, "gemini-user@test.com-proj.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("Expected 1 warning row, got %d", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("Expected IsWarning = true")
	}
	if !strings.Contains(rows[0].WarningMsg, "API returned status") {
		t.Errorf("Expected API status warning, got: %s", rows[0].WarningMsg)
	}
}

func TestGeminiProvider_RefreshesTokenOn401(t *testing.T) {
	refreshCalls := 0
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls++
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/token" {
			t.Errorf("Expected path /token, got %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		if values.Get("grant_type") != "refresh_token" {
			t.Errorf("Expected grant_type refresh_token, got %s", values.Get("grant_type"))
		}
		if values.Get("refresh_token") != "refresh-token" {
			t.Errorf("Expected refresh_token refresh-token, got %s", values.Get("refresh_token"))
		}
		if values.Get("client_id") != "client-id" {
			t.Errorf("Expected client_id client-id, got %s", values.Get("client_id"))
		}
		if values.Get("client_secret") != "client-secret" {
			t.Errorf("Expected client_secret client-secret, got %s", values.Get("client_secret"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-token","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer refreshServer.Close()

	quotaCalls := 0
	quotaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		quotaCalls++
		if r.URL.Path != geminiEndpoint {
			t.Errorf("Expected path %s, got %s", geminiEndpoint, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer new-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := geminiQuotaResponse{
			Buckets: []geminiQuotaBucket{
				{
					ModelID:           "gemini-2.5-pro",
					TokenType:         "REQUESTS",
					RemainingFraction: 0.5,
					ResetTime:         "2025-10-22T16:01:15Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer quotaServer.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	cred := map[string]any{
		"token": map[string]string{
			"access_token":  "expired-token",
			"refresh_token": "refresh-token",
			"client_id":     "client-id",
			"client_secret": "client-secret",
			"token_uri":     refreshServer.URL + "/token",
		},
		"project_id": "proj",
	}
	data, _ := json.Marshal(cred)
	credPath := filepath.Join(credDir, "gemini-user@test.com-proj.json")
	if err := os.WriteFile(credPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: quotaServer.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(rows))
	}
	if rows[0].IsWarning {
		t.Errorf("Expected data row, got warning: %s", rows[0].WarningMsg)
	}
	if refreshCalls != 1 {
		t.Errorf("Expected 1 refresh call, got %d", refreshCalls)
	}
	if quotaCalls != 2 {
		t.Errorf("Expected 2 quota calls (401 + retry), got %d", quotaCalls)
	}

	updated, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("failed to read updated credentials: %v", err)
	}
	var updatedCred geminiCredFile
	if err := json.Unmarshal(updated, &updatedCred); err != nil {
		t.Fatalf("failed to parse updated credentials: %v", err)
	}
	if updatedCred.Token.AccessToken != "new-token" {
		t.Errorf("updated access_token = %q, want %q", updatedCred.Token.AccessToken, "new-token")
	}
	if updatedCred.Token.Expiry == "" {
		t.Error("expected token.expiry to be set in credentials")
	}
}

func TestGeminiProvider_FilePatternFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Valid file (matches gemini-*.json pattern)
	validCred := map[string]any{
		"token":      map[string]string{"access_token": "token"},
		"project_id": "proj",
	}
	validData, _ := json.Marshal(validCred)
	if err := os.WriteFile(filepath.Join(credDir, "gemini-user@test.com-proj.json"), validData, 0600); err != nil {
		t.Fatal(err)
	}

	// Invalid files that should be skipped
	if err := os.WriteFile(filepath.Join(credDir, "noHyphen.json"), validData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "something.txt"), validData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "codex-user@test.com.json"), []byte(`{
		"access_token": "token"
	}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "claude-user@test.com.json"), validData, 0600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiQuotaResponse{
			Buckets: []geminiQuotaBucket{
				{
					ModelID:           "gemini-2.5-pro",
					TokenType:         "REQUESTS",
					RemainingFraction: 0.5,
					ResetTime:         "2025-10-22T16:01:15Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := &GeminiProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	// Should have 1 data row from valid file only.
	// Invalid files are silently skipped:
	// - noHyphen.json: doesn't match gemini-*.json
	// - something.txt: doesn't end with .json
	// - codex-*.json: doesn't match gemini-*.json
	if len(rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(rows))
	}

	if rows[0].IsWarning {
		t.Errorf("Expected data row, got warning: %s", rows[0].WarningMsg)
	}
	if rows[0].Provider != "Gemini (user@test.com)" {
		t.Errorf("Provider = %q, want %q", rows[0].Provider, "Gemini (user@test.com)")
	}
}

func TestGeminiProvider_EmailExtraction(t *testing.T) {
	tests := []struct {
		filename  string
		projectID string
		wantEmail string
	}{
		{"gemini-user@example.com-gen-lang-client-0353902167.json", "gen-lang-client-0353902167", "user@example.com"},
		{"gemini-alice.bob@test.org-proj-123.json", "proj-123", "alice.bob@test.org"},
		{"gemini-name-with-dash@domain.com-myproject.json", "myproject", "name-with-dash@domain.com"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := geminiQuotaResponse{
					Buckets: []geminiQuotaBucket{
						{
							ModelID:           "gemini-2.5-pro",
							RemainingFraction: 0.5,
							ResetTime:         "2025-10-22T16:01:15Z",
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			tmpDir := t.TempDir()
			credDir := filepath.Join(tmpDir, ".cli-proxy-api")
			if err := os.MkdirAll(credDir, 0755); err != nil {
				t.Fatal(err)
			}

			cred := map[string]any{
				"token":      map[string]string{"access_token": "token"},
				"project_id": tt.projectID,
			}
			data, _ := json.Marshal(cred)
			if err := os.WriteFile(filepath.Join(credDir, tt.filename), data, 0600); err != nil {
				t.Fatal(err)
			}

			provider := &GeminiProvider{
				homeDir: tmpDir,
				baseURL: server.URL,
				client:  &http.Client{Timeout: 5 * time.Second},
			}

			rows, err := provider.FetchUsage(context.Background())
			if err != nil {
				t.Fatalf("FetchUsage() error = %v", err)
			}

			if len(rows) == 0 {
				t.Fatal("Expected at least 1 row")
			}

			wantProvider := "Gemini (" + tt.wantEmail + ")"
			if rows[0].Provider != wantProvider {
				t.Errorf("Provider = %q, want %q", rows[0].Provider, wantProvider)
			}
		})
	}
}

func TestNewGeminiProvider(t *testing.T) {
	p, err := NewGeminiProvider()
	if err != nil {
		t.Fatalf("NewGeminiProvider() error = %v", err)
	}

	if p.baseURL != geminiDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", p.baseURL, geminiDefaultBaseURL)
	}
	if p.client == nil {
		t.Error("client should not be nil")
	}
	if p.client.Timeout != geminiHTTPTimeout {
		t.Errorf("client.Timeout = %v, want %v", p.client.Timeout, geminiHTTPTimeout)
	}
	if p.homeDir == "" {
		t.Error("homeDir should not be empty")
	}
}

func TestGeminiLoadNativeCredentials_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	geminiDir := filepath.Join(tmpDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cred := `{"access_token": "ya29.native-token", "refresh_token": "1//refresh", "expiry_date": 1767454704529}`
	if err := os.WriteFile(filepath.Join(geminiDir, "oauth_creds.json"), []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &GeminiProvider{homeDir: tmpDir}
	accounts := p.loadNativeCredentials()
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}

	acc := accounts[0]
	if acc.Email != "native" {
		t.Errorf("Email = %q, want %q", acc.Email, "native")
	}
	if acc.Token != "ya29.native-token" {
		t.Errorf("Token = %q, want %q", acc.Token, "ya29.native-token")
	}
	if acc.RefreshToken != "1//refresh" {
		t.Errorf("RefreshToken = %q, want %q", acc.RefreshToken, "1//refresh")
	}
	if acc.ProjectID != "" {
		t.Errorf("ProjectID = %q, want empty", acc.ProjectID)
	}
	if !acc.IsNative {
		t.Error("IsNative should be true")
	}
}

func TestGeminiLoadNativeCredentials_ExpiryDate(t *testing.T) {
	tmpDir := t.TempDir()
	geminiDir := filepath.Join(tmpDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// expiry_date: 1767454704529 ms = 2026-01-03 08:18:24.529 UTC
	cred := `{"access_token": "ya29.token", "expiry_date": 1767454704529}`
	if err := os.WriteFile(filepath.Join(geminiDir, "oauth_creds.json"), []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &GeminiProvider{homeDir: tmpDir}
	accounts := p.loadNativeCredentials()
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}

	expected := time.UnixMilli(1767454704529)
	if !accounts[0].TokenExpiry.Equal(expected) {
		t.Errorf("TokenExpiry = %v, want %v", accounts[0].TokenExpiry, expected)
	}
}

func TestGeminiLoadNativeCredentials_MissingExpiry(t *testing.T) {
	tmpDir := t.TempDir()
	geminiDir := filepath.Join(tmpDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cred := `{"access_token": "ya29.token"}`
	if err := os.WriteFile(filepath.Join(geminiDir, "oauth_creds.json"), []byte(cred), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &GeminiProvider{homeDir: tmpDir}
	accounts := p.loadNativeCredentials()
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}

	if !accounts[0].TokenExpiry.IsZero() {
		t.Errorf("TokenExpiry = %v, want zero time", accounts[0].TokenExpiry)
	}
}

func TestGeminiLoadNativeCredentials_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	p := &GeminiProvider{homeDir: tmpDir}
	accounts := p.loadNativeCredentials()
	if accounts != nil {
		t.Errorf("expected nil accounts for missing file, got %v", accounts)
	}
}

func TestGeminiLoadNativeCredentials_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	geminiDir := filepath.Join(tmpDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write invalid JSON
	if err := os.WriteFile(filepath.Join(geminiDir, "oauth_creds.json"), []byte("{invalid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &GeminiProvider{homeDir: tmpDir}
	accounts := p.loadNativeCredentials()
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account with LoadErr, got %d accounts", len(accounts))
	}

	acc := accounts[0]
	if acc.LoadErr == "" {
		t.Error("LoadErr should be set for malformed JSON")
	}
	if !strings.Contains(acc.LoadErr, "failed to parse") {
		t.Errorf("LoadErr should mention parse failure, got: %v", acc.LoadErr)
	}
	if acc.Email != "native" {
		t.Errorf("Email = %q, want %q", acc.Email, "native")
	}
	if !acc.IsNative {
		t.Error("IsNative should be true")
	}
}

func TestGeminiLoadCredentials_UsesGlobalSource(t *testing.T) {
	tests := []struct {
		name           string
		setupProxy     bool
		setupNative    bool
		expectNative   bool
		expectAccounts int
	}{
		{
			name:           "proxy creds exist - use proxy",
			setupProxy:     true,
			setupNative:    true,
			expectNative:   false,
			expectAccounts: 1,
		},
		{
			name:           "only native creds - use native",
			setupProxy:     false,
			setupNative:    true,
			expectNative:   true,
			expectAccounts: 1,
		},
		{
			name:           "no creds - returns native empty",
			setupProxy:     false,
			setupNative:    false,
			expectNative:   true,
			expectAccounts: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if tt.setupProxy {
				proxyDir := filepath.Join(tmpDir, ".cli-proxy-api")
				if err := os.MkdirAll(proxyDir, 0o755); err != nil {
					t.Fatal(err)
				}
				// filename format: gemini-{email}-{project_id}.json
				proxyCred := `{"token":{"access_token":"proxy-token","client_id":"id","refresh_token":"rt"},"project_id":"proj-123"}`
				if err := os.WriteFile(filepath.Join(proxyDir, "gemini-test@example.com-proj-123.json"), []byte(proxyCred), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			if tt.setupNative {
				geminiDir := filepath.Join(tmpDir, ".gemini")
				if err := os.MkdirAll(geminiDir, 0o755); err != nil {
					t.Fatal(err)
				}
				nativeCred := `{"access_token":"native-token"}`
				if err := os.WriteFile(filepath.Join(geminiDir, "oauth_creds.json"), []byte(nativeCred), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			p := &GeminiProvider{homeDir: tmpDir}
			accounts, _, _ := p.loadCredentials()

			if len(accounts) != tt.expectAccounts {
				t.Errorf("expected %d accounts, got %d", tt.expectAccounts, len(accounts))
				return
			}

			if tt.expectAccounts > 0 {
				if accounts[0].IsNative != tt.expectNative {
					t.Errorf("IsNative = %v, want %v", accounts[0].IsNative, tt.expectNative)
				}
			}
		})
	}
}

func TestGeminiRefreshAccessToken_NativeSkip(t *testing.T) {
	p := &GeminiProvider{
		homeDir: t.TempDir(),
		client:  &http.Client{},
	}

	account := GeminiAccount{
		Email:        "native",
		Token:        "ya29.expired",
		RefreshToken: "1//refresh",
		IsNative:     true,
	}

	_, err := p.refreshAccessToken(context.Background(), account)
	if err == nil {
		t.Fatal("expected error for native account refresh")
	}
	if !strings.Contains(err.Error(), "Re-authenticate with gemini") {
		t.Errorf("error = %q, want message about re-authentication", err.Error())
	}
}
