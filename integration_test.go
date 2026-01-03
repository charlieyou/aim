//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyou/aim/internal/output"
	"github.com/cyou/aim/internal/providers"
)

func TestClaudeIntegration(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot determine home directory, skipping")
	}

	credPath := filepath.Join(home, ".claude", ".credentials.json")
	if _, err := os.Stat(credPath); os.IsNotExist(err) {
		t.Skip("Claude credentials not found at ~/.claude/.credentials.json, skipping")
	}

	provider, err := providers.NewClaudeProvider()
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := provider.FetchUsage(ctx)
	if err != nil {
		t.Fatalf("FetchUsage failed: %v", err)
	}

	// Check for warnings in rows (indicates credential or API issues)
	for _, row := range rows {
		if row.IsWarning {
			t.Fatalf("Got warning instead of usage data: %s", row.WarningMsg)
		}
	}

	// Should have at least one row (API may return different windows)
	if len(rows) == 0 {
		t.Fatal("Expected at least one row")
	}

	// Log actual values for verification
	for _, row := range rows {
		t.Logf("Claude %s: %.1f%% (resets %v)", row.Label, row.UsagePercent, row.ResetTime)
	}
}

func TestCodexIntegration(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot determine home directory, skipping")
	}

	pattern := filepath.Join(home, ".cli-proxy-api", "codex-*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		t.Skip("No Codex credential files found matching ~/.cli-proxy-api/codex-*.json, skipping")
	}

	provider, err := providers.NewCodexProvider()
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := provider.FetchUsage(ctx)
	if err != nil {
		t.Fatalf("FetchUsage failed: %v", err)
	}

	if len(rows) == 0 {
		t.Fatal("Expected at least one row")
	}

	// Check for warnings (might indicate invalid credentials)
	var warnings, usageRows []providers.UsageRow
	for _, row := range rows {
		if row.IsWarning {
			warnings = append(warnings, row)
		} else {
			usageRows = append(usageRows, row)
		}
	}

	if len(usageRows) == 0 && len(warnings) > 0 {
		t.Fatalf("Got only warnings: %s", warnings[0].WarningMsg)
	}

	// Log actual values for verification
	for _, row := range usageRows {
		t.Logf("%s %s: %.1f%% (resets %v)", row.Provider, row.Label, row.UsagePercent, row.ResetTime)
	}
	for _, row := range warnings {
		t.Logf("Warning: %s - %s", row.Provider, row.WarningMsg)
	}
}

func TestGeminiIntegration(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot determine home directory, skipping")
	}

	// Check for Gemini credential files
	// Gemini files have format: email-projectid.json with project_id field in content
	credDir := filepath.Join(home, ".cli-proxy-api")
	if _, err := os.Stat(credDir); os.IsNotExist(err) {
		t.Skip("~/.cli-proxy-api directory not found, skipping")
	}

	entries, err := os.ReadDir(credDir)
	if err != nil {
		t.Skip("Cannot read ~/.cli-proxy-api directory, skipping")
	}

	hasGeminiCreds := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip non-JSON files and other providers
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.HasPrefix(name, "codex-") || strings.HasPrefix(name, "claude-") {
			continue
		}
		// Check for hyphen pattern (gemini creds have format: email-projectid.json)
		baseName := strings.TrimSuffix(name, ".json")
		if !strings.Contains(baseName, "-") {
			continue
		}
		// Verify it has Gemini-specific fields (project_id and token.access_token)
		filePath := filepath.Join(credDir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		var cred struct {
			Token struct {
				AccessToken string `json:"access_token"`
			} `json:"token"`
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal(data, &cred); err != nil {
			continue
		}
		if cred.Token.AccessToken != "" && cred.ProjectID != "" {
			hasGeminiCreds = true
			break
		}
	}

	if !hasGeminiCreds {
		t.Skip("No valid Gemini credential files found in ~/.cli-proxy-api, skipping")
	}

	provider, err := providers.NewGeminiProvider()
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := provider.FetchUsage(ctx)
	if err != nil {
		t.Fatalf("FetchUsage failed: %v", err)
	}

	if len(rows) == 0 {
		t.Fatal("Expected at least one row")
	}

	// Check for warnings (might indicate invalid/expired credentials)
	var warnings, usageRows []providers.UsageRow
	for _, row := range rows {
		if row.IsWarning {
			warnings = append(warnings, row)
		} else {
			usageRows = append(usageRows, row)
		}
	}

	// If we only got warnings (e.g., expired tokens), skip rather than fail
	// This makes the test safe for CI and machines with stale credentials
	if len(usageRows) == 0 && len(warnings) > 0 {
		t.Skipf("Gemini credentials exist but returned warnings (likely expired): %s", warnings[0].WarningMsg)
	}

	// Log actual values for verification
	for _, row := range usageRows {
		t.Logf("%s %s: %.1f%% (resets %v)", row.Provider, row.Label, row.UsagePercent, row.ResetTime)
	}
	for _, row := range warnings {
		t.Logf("Warning: %s - %s", row.Provider, row.WarningMsg)
	}
}

func TestFullRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create all providers
	claude, err := providers.NewClaudeProvider()
	if err != nil {
		t.Fatalf("Failed to create Claude provider: %v", err)
	}
	codex, err := providers.NewCodexProvider()
	if err != nil {
		t.Fatalf("Failed to create Codex provider: %v", err)
	}
	gemini, err := providers.NewGeminiProvider()
	if err != nil {
		t.Fatalf("Failed to create Gemini provider: %v", err)
	}

	allProviders := []providers.Provider{claude, codex, gemini}

	var allRows []providers.UsageRow
	for _, p := range allProviders {
		rows, err := p.FetchUsage(ctx)
		if err != nil {
			allRows = append(allRows, providers.UsageRow{
				Provider:   p.Name(),
				IsWarning:  true,
				WarningMsg: err.Error(),
			})
			continue
		}
		allRows = append(allRows, rows...)
	}

	if len(allRows) == 0 {
		t.Fatal("Expected at least one row from all providers")
	}

	// Capture stdout
	var buf bytes.Buffer
	output.RenderTable(allRows, &buf)

	tableOutput := buf.String()
	t.Logf("Table output:\n%s", tableOutput)

	// Verify table format contains expected structure
	// Headers are uppercase in the rendered table
	if !strings.Contains(tableOutput, "PROVIDER") {
		t.Error("Table output missing 'PROVIDER' header")
	}
	if !strings.Contains(tableOutput, "WINDOW") {
		t.Error("Table output missing 'WINDOW' header")
	}
	if !strings.Contains(tableOutput, "USAGE") {
		t.Error("Table output missing 'USAGE' header")
	}
	if !strings.Contains(tableOutput, "RESETS AT") {
		t.Error("Table output missing 'RESETS AT' header")
	}

	// Verify we got data or warnings (not empty)
	lines := strings.Split(strings.TrimSpace(tableOutput), "\n")
	if len(lines) < 3 { // Header + separator + at least one data row
		t.Errorf("Expected at least 3 lines in table output, got %d", len(lines))
	}
}
