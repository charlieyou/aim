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

// hasClaudeCredentials checks if Claude credentials exist
func hasClaudeCredentials() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	pattern := filepath.Join(home, ".cli-proxy-api", "claude-*.json")
	matches, err := filepath.Glob(pattern)
	return err == nil && len(matches) > 0
}

// hasCodexCredentials checks if Codex credentials exist
func hasCodexCredentials() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	pattern := filepath.Join(home, ".cli-proxy-api", "codex-*.json")
	matches, err := filepath.Glob(pattern)
	return err == nil && len(matches) > 0
}

// hasGeminiCredentials checks if Gemini credentials exist
func hasGeminiCredentials() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	credDir := filepath.Join(home, ".cli-proxy-api")
	entries, err := os.ReadDir(credDir)
	if err != nil {
		return false
	}

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
			Email     string `json:"email"`
			Type      string `json:"type"`
		}
		if err := json.Unmarshal(data, &cred); err != nil {
			continue
		}
		// Skip if type is set and not "gemini"
		if cred.Type != "" && cred.Type != "gemini" {
			continue
		}
		if cred.Token.AccessToken == "" || cred.ProjectID == "" {
			continue
		}
		// Valid credential if filename matches pattern OR has email field
		baseName := strings.TrimSuffix(name, ".json")
		suffix := "-" + cred.ProjectID
		if strings.HasSuffix(baseName, suffix) || cred.Email != "" {
			return true
		}
	}
	return false
}

func TestClaudeIntegration(t *testing.T) {
	if !hasClaudeCredentials() {
		t.Skip("No Claude credential files found matching ~/.cli-proxy-api/claude-*.json, skipping")
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

	// Should have at least one row (API may return different windows)
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
		t.Skipf("Claude credentials exist but returned warnings (likely expired): %s", warnings[0].WarningMsg)
	}

	// Log actual values for verification
	for _, row := range usageRows {
		t.Logf("Claude %s: %.1f%% (resets %v)", row.Label, row.UsagePercent, row.ResetTime)
	}
	for _, row := range warnings {
		t.Logf("Warning: Claude - %s", row.WarningMsg)
	}
}

func TestCodexIntegration(t *testing.T) {
	if !hasCodexCredentials() {
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
		t.Skipf("Codex credentials exist but returned warnings (likely expired): %s", warnings[0].WarningMsg)
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
	if !hasGeminiCredentials() {
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
	// Check that at least one provider has credentials
	// Skip if no providers are configured (consistent with per-provider test behavior)
	hasClaude := hasClaudeCredentials()
	hasCodex := hasCodexCredentials()
	hasGemini := hasGeminiCredentials()

	if !hasClaude && !hasCodex && !hasGemini {
		t.Skip("No provider credentials found, skipping full run test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create providers only for those with credentials
	var allProviders []providers.Provider

	if hasClaude {
		claude, err := providers.NewClaudeProvider()
		if err != nil {
			t.Logf("Warning: Claude credentials exist but provider creation failed: %v", err)
		} else {
			allProviders = append(allProviders, claude)
		}
	}

	if hasCodex {
		codex, err := providers.NewCodexProvider()
		if err != nil {
			t.Logf("Warning: Codex credentials exist but provider creation failed: %v", err)
		} else {
			allProviders = append(allProviders, codex)
		}
	}

	if hasGemini {
		gemini, err := providers.NewGeminiProvider()
		if err != nil {
			t.Logf("Warning: Gemini credentials exist but provider creation failed: %v", err)
		} else {
			allProviders = append(allProviders, gemini)
		}
	}

	if len(allProviders) == 0 {
		t.Skip("All provider creations failed despite credentials existing, skipping")
	}

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
	output.RenderTable(allRows, &buf, false)

	tableOutput := buf.String()
	t.Logf("Table output:\n%s", tableOutput)

	// Verify table format contains expected structure
	if !strings.Contains(tableOutput, "Provider") {
		t.Error("Table output missing 'Provider' header")
	}
	if !strings.Contains(tableOutput, "Window") {
		t.Error("Table output missing 'Window' header")
	}
	if !strings.Contains(tableOutput, "Usage") {
		t.Error("Table output missing 'Usage' header")
	}
	if !strings.Contains(tableOutput, "Resets At") {
		t.Error("Table output missing 'Resets At' header")
	}

	// Verify we got data or warnings (not empty)
	lines := strings.Split(strings.TrimSpace(tableOutput), "\n")
	if len(lines) < 2 { // Header + at least one data row
		t.Errorf("Expected at least 2 lines in table output, got %d", len(lines))
	}
}
