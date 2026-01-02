package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	credFile := filepath.Join(credDir, "user@example.com-gen-lang-client-123.json")
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
	if err := os.WriteFile(filepath.Join(credDir, "alice@test.com-proj-1.json"), credData, 0600); err != nil {
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
	if err := os.WriteFile(filepath.Join(credDir, "alice@test.com-proj-1.json"), data1, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "bob@test.com-proj-2.json"), data2, 0600); err != nil {
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
	if row.WarningMsg != "No valid credential files found in ~/.cli-proxy-api/" {
		t.Errorf("WarningMsg = %q", row.WarningMsg)
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
	if err := os.WriteFile(filepath.Join(credDir, "user@example.com-proj.json"), data, 0600); err != nil {
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
	if !contains(rows[0].WarningMsg, "Failed to parse") || !contains(rows[0].WarningMsg, "missing project_id") {
		t.Errorf("Expected warning about failed parse with missing project_id, got: %s", rows[0].WarningMsg)
	}

	// Check second warning is about no valid credentials
	if !rows[1].IsWarning {
		t.Error("Expected rows[1].IsWarning = true")
	}
	if !contains(rows[1].WarningMsg, "No valid credential files") {
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
			if err := os.WriteFile(filepath.Join(credDir, "user@test.com-proj.json"), data, 0600); err != nil {
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
	if err := os.WriteFile(filepath.Join(credDir, "user@test.com-proj.json"), data, 0600); err != nil {
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
	if !contains(rows[0].WarningMsg, "Empty buckets") {
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
	if err := os.WriteFile(filepath.Join(credDir, "user@test.com-proj.json"), data, 0600); err != nil {
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
	if !contains(rows[0].WarningMsg, "API error") {
		t.Errorf("Expected API error warning, got: %s", rows[0].WarningMsg)
	}
}

func TestGeminiProvider_FilePatternFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Valid file (matches *-*.json pattern)
	validCred := map[string]any{
		"token":      map[string]string{"access_token": "token"},
		"project_id": "proj",
	}
	validData, _ := json.Marshal(validCred)
	if err := os.WriteFile(filepath.Join(credDir, "user@test.com-proj.json"), validData, 0600); err != nil {
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
	// - noHyphen.json: no hyphen in name (doesn't match *-*.json pattern)
	// - something.txt: doesn't end with .json
	// - codex-*.json: explicitly excluded as it's from another provider
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
		{"user@example.com-gen-lang-client-0353902167.json", "gen-lang-client-0353902167", "user@example.com"},
		{"alice.bob@test.org-proj-123.json", "proj-123", "alice.bob@test.org"},
		{"name-with-dash@domain.com-myproject.json", "myproject", "name-with-dash@domain.com"},
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

// contains checks if substr is in s
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
