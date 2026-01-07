//go:build integration

package providers

import (
	"context"
	"testing"
	"time"
)

func TestClaudeRefreshIntegration(t *testing.T) {
	provider, err := NewClaudeProvider()
	if err != nil {
		t.Skipf("Failed to create provider: %v", err)
	}

	creds, err := provider.loadCredentials()
	if err != nil {
		t.Skipf("Claude credentials not available: %v", err)
	}

	var account *claudeAuth
	for i := range creds {
		if creds[i].RefreshToken != "" {
			account = &creds[i]
			break
		}
	}
	if account == nil {
		t.Skip("Claude refresh token not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	token, err := provider.refreshAccessToken(ctx, *account)
	if err != nil {
		t.Fatalf("Claude refresh failed: %v", err)
	}
	if token == "" {
		t.Fatal("Claude refresh returned empty access token")
	}
}

func TestCodexRefreshIntegration(t *testing.T) {
	provider, err := NewCodexProvider()
	if err != nil {
		t.Skipf("Failed to create provider: %v", err)
	}

	accounts, err := provider.loadCredentials()
	if err != nil {
		t.Skipf("Codex credentials not available: %v", err)
	}

	var account *CodexAccount
	for i := range accounts {
		if accounts[i].RefreshToken != "" && accounts[i].Token != "" {
			account = &accounts[i]
			break
		}
	}
	if account == nil {
		t.Skip("No Codex account with refresh token available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	token, err := provider.refreshAccessToken(ctx, *account)
	if err != nil {
		t.Fatalf("Codex refresh failed: %v", err)
	}
	if token == "" {
		t.Fatal("Codex refresh returned empty access token")
	}
}

func TestGeminiRefreshIntegration(t *testing.T) {
	provider, err := NewGeminiProvider()
	if err != nil {
		t.Skipf("Failed to create provider: %v", err)
	}

	accounts, _, _ := provider.loadCredentials()

	var account *GeminiAccount
	for i := range accounts {
		if accounts[i].RefreshToken != "" && accounts[i].ClientID != "" {
			account = &accounts[i]
			break
		}
	}
	if account == nil {
		t.Skip("No Gemini account with refresh token available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	token, err := provider.refreshAccessToken(ctx, *account)
	if err != nil {
		t.Fatalf("Gemini refresh failed: %v", err)
	}
	if token == "" {
		t.Fatal("Gemini refresh returned empty access token")
	}
}
