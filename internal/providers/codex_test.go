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

func TestCodexProvider_Name(t *testing.T) {
	provider := &CodexProvider{}
	if got := provider.Name(); got != "Codex" {
		t.Errorf("Name() = %q, want %q", got, "Codex")
	}
}

func TestCodexProvider_EmailExtraction(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"codex-user@example.com.json", "user@example.com"},
		{"codex-alice@domain.org.json", "alice@domain.org"},
		{"codex-bob+tag@test.io.json", "bob+tag@test.io"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := extractEmailFromFilename(tt.filename)
			if got != tt.want {
				t.Errorf("extractEmailFromFilename(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestCodexProvider_FetchUsage_SingleAccount(t *testing.T) {
	// Create mock server
	resetTime := time.Now().Add(1 * time.Hour).Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/backend-api/wham/usage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("unexpected Authorization: %s", auth)
		}
		if accept := r.Header.Get("Accept"); accept != "application/json" {
			t.Errorf("unexpected Accept: %s", accept)
		}
		if ua := r.Header.Get("User-Agent"); ua != "ai-meter/0.1.0" {
			t.Errorf("unexpected User-Agent: %s", ua)
		}

		resp := codexAPIResponse{
			PlanType: "pro",
		}
		resp.RateLimit.PrimaryWindow.UsedPercent = 25.5
		resp.RateLimit.PrimaryWindow.ResetAt = resetTime
		resp.RateLimit.SecondaryWindow.UsedPercent = 10.0
		resp.RateLimit.SecondaryWindow.ResetAt = resetTime + 86400

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create temp directory with credential file
	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credFile := filepath.Join(credDir, "codex-user@example.com.json")
	credData := `{"access_token": "test-token", "refresh_token": "refresh"}`
	if err := os.WriteFile(credFile, []byte(credData), 0600); err != nil {
		t.Fatal(err)
	}

	provider := &CodexProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("FetchUsage() returned %d rows, want 2", len(rows))
	}

	// Check 5-hour row
	if rows[0].Provider != "Codex (user@example.com)" {
		t.Errorf("rows[0].Provider = %q, want %q", rows[0].Provider, "Codex (user@example.com)")
	}
	if rows[0].Label != "5-hour" {
		t.Errorf("rows[0].Label = %q, want %q", rows[0].Label, "5-hour")
	}
	if rows[0].UsagePercent != 25.5 {
		t.Errorf("rows[0].UsagePercent = %f, want %f", rows[0].UsagePercent, 25.5)
	}
	if rows[0].IsWarning {
		t.Errorf("rows[0].IsWarning = true, want false")
	}

	// Check 7-day row
	if rows[1].Provider != "Codex (user@example.com)" {
		t.Errorf("rows[1].Provider = %q, want %q", rows[1].Provider, "Codex (user@example.com)")
	}
	if rows[1].Label != "7-day" {
		t.Errorf("rows[1].Label = %q, want %q", rows[1].Label, "7-day")
	}
	if rows[1].UsagePercent != 10.0 {
		t.Errorf("rows[1].UsagePercent = %f, want %f", rows[1].UsagePercent, 10.0)
	}
}

func TestCodexProvider_FetchUsage_MultipleAccounts(t *testing.T) {
	// Define expected usage percentages per account
	accountData := map[string]struct {
		primary   float64
		secondary float64
	}{
		"token-alice@example.com": {primary: 30.0, secondary: 15.0},
		"token-bob@example.com":   {primary: 60.0, secondary: 25.0},
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// Extract token from Authorization header to return account-specific data
		auth := r.Header.Get("Authorization")
		token := ""
		if len(auth) > 7 && auth[:7] == "Bearer " {
			token = auth[7:]
		}

		data, ok := accountData[token]
		if !ok {
			t.Errorf("unexpected token: %s", token)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		resp := codexAPIResponse{
			PlanType: "pro",
		}
		resp.RateLimit.PrimaryWindow.UsedPercent = data.primary
		resp.RateLimit.PrimaryWindow.ResetAt = time.Now().Unix()
		resp.RateLimit.SecondaryWindow.UsedPercent = data.secondary
		resp.RateLimit.SecondaryWindow.ResetAt = time.Now().Unix()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create two credential files
	for _, email := range []string{"alice@example.com", "bob@example.com"} {
		credFile := filepath.Join(credDir, "codex-"+email+".json")
		credData := `{"access_token": "token-` + email + `", "refresh_token": "refresh"}`
		if err := os.WriteFile(credFile, []byte(credData), 0600); err != nil {
			t.Fatal(err)
		}
	}

	provider := &CodexProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 4 {
		t.Fatalf("FetchUsage() returned %d rows, want 4 (2 per account)", len(rows))
	}

	if callCount != 2 {
		t.Errorf("API called %d times, want 2", callCount)
	}

	// Verify each account's data is correctly associated
	// Build a map of provider -> rows for easier verification
	rowsByProvider := make(map[string][]UsageRow)
	for _, row := range rows {
		rowsByProvider[row.Provider] = append(rowsByProvider[row.Provider], row)
	}

	// Verify Alice's data
	aliceRows, ok := rowsByProvider["Codex (alice@example.com)"]
	if !ok {
		t.Fatal("missing rows for alice@example.com")
	}
	if len(aliceRows) != 2 {
		t.Fatalf("alice has %d rows, want 2", len(aliceRows))
	}
	aliceExpected := map[string]float64{"5-hour": 30.0, "7-day": 15.0}
	for _, row := range aliceRows {
		want, ok := aliceExpected[row.Label]
		if !ok {
			t.Errorf("alice has unexpected label %q", row.Label)
			continue
		}
		if row.UsagePercent != want {
			t.Errorf("alice %s UsagePercent = %f, want %f", row.Label, row.UsagePercent, want)
		}
		delete(aliceExpected, row.Label)
	}
	for label := range aliceExpected {
		t.Errorf("alice missing expected label %q", label)
	}

	// Verify Bob's data
	bobRows, ok := rowsByProvider["Codex (bob@example.com)"]
	if !ok {
		t.Fatal("missing rows for bob@example.com")
	}
	if len(bobRows) != 2 {
		t.Fatalf("bob has %d rows, want 2", len(bobRows))
	}
	bobExpected := map[string]float64{"5-hour": 60.0, "7-day": 25.0}
	for _, row := range bobRows {
		want, ok := bobExpected[row.Label]
		if !ok {
			t.Errorf("bob has unexpected label %q", row.Label)
			continue
		}
		if row.UsagePercent != want {
			t.Errorf("bob %s UsagePercent = %f, want %f", row.Label, row.UsagePercent, want)
		}
		delete(bobExpected, row.Label)
	}
	for label := range bobExpected {
		t.Errorf("bob missing expected label %q", label)
	}
}

func TestCodexProvider_FetchUsage_NoCreds(t *testing.T) {
	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	provider := &CodexProvider{
		homeDir: tmpDir,
		baseURL: "http://unused",
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("FetchUsage() returned %d rows, want 1 warning", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("expected warning row")
	}
	if rows[0].Provider != "Codex" {
		t.Errorf("rows[0].Provider = %q, want %q", rows[0].Provider, "Codex")
	}
}

func TestCodexProvider_FetchUsage_MalformedFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := codexAPIResponse{PlanType: "pro"}
		resp.RateLimit.PrimaryWindow.UsedPercent = 20
		resp.RateLimit.PrimaryWindow.ResetAt = time.Now().Unix()
		resp.RateLimit.SecondaryWindow.UsedPercent = 5
		resp.RateLimit.SecondaryWindow.ResetAt = time.Now().Unix()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create one good file
	goodFile := filepath.Join(credDir, "codex-good@example.com.json")
	if err := os.WriteFile(goodFile, []byte(`{"access_token": "valid"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Create one malformed file
	badFile := filepath.Join(credDir, "codex-bad@example.com.json")
	if err := os.WriteFile(badFile, []byte(`{not valid json`), 0600); err != nil {
		t.Fatal(err)
	}

	provider := &CodexProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	// Should have 2 rows from good account + 1 warning from bad account
	if len(rows) != 3 {
		t.Fatalf("FetchUsage() returned %d rows, want 3", len(rows))
	}

	// Find warning row and verify it contains the specific JSON parse error
	var warningFound bool
	for _, row := range rows {
		if row.IsWarning {
			warningFound = true
			if row.Provider != "Codex (bad@example.com)" {
				t.Errorf("warning row Provider = %q, want %q", row.Provider, "Codex (bad@example.com)")
			}
			// Verify the specific error is included, not just generic "failed to load credentials"
			if !strings.Contains(row.WarningMsg, "failed to parse JSON") {
				t.Errorf("warning message should contain specific JSON parse error, got: %q", row.WarningMsg)
			}
		}
	}
	if !warningFound {
		t.Error("expected to find a warning row for malformed file")
	}
}

func TestCodexProvider_FetchUsage_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credFile := filepath.Join(credDir, "codex-user@example.com.json")
	if err := os.WriteFile(credFile, []byte(`{"access_token": "token"}`), 0600); err != nil {
		t.Fatal(err)
	}

	provider := &CodexProvider{
		homeDir: tmpDir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("FetchUsage() returned %d rows, want 1 warning", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("expected warning row for API error")
	}
	if rows[0].Provider != "Codex (user@example.com)" {
		t.Errorf("rows[0].Provider = %q, want %q", rows[0].Provider, "Codex (user@example.com)")
	}
}

func TestCodexProvider_FetchUsage_RefreshesTokenOn401(t *testing.T) {
	refreshCalls := 0
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls++
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		if values.Get("refresh_token") != "refresh-token" {
			t.Errorf("expected refresh_token refresh-token, got %q", values.Get("refresh_token"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-token"}`))
	}))
	defer refreshServer.Close()

	usageCalls := 0
	usageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usageCalls++
		if r.Header.Get("Authorization") != "Bearer new-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		resp := codexAPIResponse{PlanType: "pro"}
		resp.RateLimit.PrimaryWindow.UsedPercent = 12.0
		resp.RateLimit.PrimaryWindow.ResetAt = time.Now().Unix()
		resp.RateLimit.SecondaryWindow.UsedPercent = 34.0
		resp.RateLimit.SecondaryWindow.ResetAt = time.Now().Unix()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer usageServer.Close()

	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	credFile := filepath.Join(credDir, "codex-user@example.com.json")
	credData := `{"access_token": "old-token", "refresh_token": "refresh-token"}`
	if err := os.WriteFile(credFile, []byte(credData), 0600); err != nil {
		t.Fatal(err)
	}

	provider := &CodexProvider{
		homeDir:    tmpDir,
		baseURL:    usageServer.URL,
		refreshURL: refreshServer.URL,
		client:     &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
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
}

func TestCodexProvider_FetchUsage_MissingAccessToken(t *testing.T) {
	tmpDir := t.TempDir()
	credDir := filepath.Join(tmpDir, ".cli-proxy-api")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create file with missing access_token
	credFile := filepath.Join(credDir, "codex-user@example.com.json")
	if err := os.WriteFile(credFile, []byte(`{"refresh_token": "refresh"}`), 0600); err != nil {
		t.Fatal(err)
	}

	provider := &CodexProvider{
		homeDir: tmpDir,
		baseURL: "http://unused",
		client:  &http.Client{Timeout: 5 * time.Second},
	}

	rows, err := provider.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage() error = %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("FetchUsage() returned %d rows, want 1 warning", len(rows))
	}

	if !rows[0].IsWarning {
		t.Error("expected warning row for missing access_token")
	}
	// Verify the specific error is included
	if !strings.Contains(rows[0].WarningMsg, "missing access_token") {
		t.Errorf("warning message should contain specific error, got: %q", rows[0].WarningMsg)
	}
}

func TestNewCodexProvider(t *testing.T) {
	provider, err := NewCodexProvider()
	if err != nil {
		t.Fatalf("NewCodexProvider() error = %v", err)
	}

	if provider.baseURL != "https://chatgpt.com" {
		t.Errorf("baseURL = %q, want %q", provider.baseURL, "https://chatgpt.com")
	}

	if provider.client == nil {
		t.Error("client is nil")
	}

	if provider.client.Timeout != 30*time.Second {
		t.Errorf("client.Timeout = %v, want %v", provider.client.Timeout, 30*time.Second)
	}

	if provider.homeDir == "" {
		t.Error("homeDir is empty")
	}
}
