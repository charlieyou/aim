package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	geminiDefaultBaseURL = "https://cloudcode-pa.googleapis.com"
	geminiEndpoint       = "/v1internal:retrieveUserQuota"
	geminiHTTPTimeout    = 30 * time.Second
	geminiTokenURI       = "https://oauth2.googleapis.com/token"
	geminiTokenSkew      = time.Minute
)

// GeminiAccount holds credentials for a single Gemini account
type GeminiAccount struct {
	Email        string
	Token        string
	RefreshToken string
	ClientID     string
	ClientSecret string
	TokenURI     string
	TokenExpiry  time.Time
	ProjectID    string
}

// GeminiProvider fetches usage data from Gemini (Google) quota API
type GeminiProvider struct {
	homeDir string
	baseURL string
	client  *http.Client
}

// geminiCredFile represents the structure of ~/.cli-proxy-api/*-*.json files
type geminiCredFile struct {
	Token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		TokenURI     string `json:"token_uri"`
		Expiry       string `json:"expiry"`
	} `json:"token"`
	ProjectID string `json:"project_id"`
	Email     string `json:"email"`
	Type      string `json:"type"`
}

// geminiQuotaResponse represents the API response
type geminiQuotaResponse struct {
	Buckets []geminiQuotaBucket `json:"buckets"`
}

// geminiQuotaBucket represents a single quota bucket
type geminiQuotaBucket struct {
	ModelID           string  `json:"modelId"`
	TokenType         string  `json:"tokenType"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
}

type geminiRefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

var errNotGeminiCred = errors.New("not gemini credential")

// NewGeminiProvider creates a new GeminiProvider with default settings
func NewGeminiProvider() (*GeminiProvider, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	return &GeminiProvider{
		homeDir: homeDir,
		baseURL: geminiDefaultBaseURL,
		client: &http.Client{
			Timeout: geminiHTTPTimeout,
		},
	}, nil
}

// Name returns the provider name
func (g *GeminiProvider) Name() string {
	return "Gemini"
}

// FetchUsage fetches usage data from all discovered Gemini accounts
func (g *GeminiProvider) FetchUsage(ctx context.Context) ([]UsageRow, error) {
	accounts, warnings := g.loadCredentials()

	var rows []UsageRow

	// Add warning rows for credential loading issues
	for _, w := range warnings {
		rows = append(rows, UsageRow{
			Provider:   "Gemini",
			IsWarning:  true,
			WarningMsg: w,
		})
	}

	if len(accounts) == 0 {
		rows = append(rows, UsageRow{
			Provider:   "Gemini",
			IsWarning:  true,
			WarningMsg: "No valid credential files found in ~/.cli-proxy-api/",
		})
		return rows, nil
	}

	// Fetch usage for each account
	for _, account := range accounts {
		accountRows, err := g.fetchAccountUsage(ctx, account)
		if err != nil {
			rows = append(rows, UsageRow{
				Provider:   fmt.Sprintf("Gemini (%s)", account.Email),
				IsWarning:  true,
				WarningMsg: fmt.Sprintf("API error: %v", err),
			})
			continue
		}
		rows = append(rows, accountRows...)
	}

	return rows, nil
}

// loadCredentials discovers and loads credentials from ~/.cli-proxy-api/*-*.json files
func (g *GeminiProvider) loadCredentials() ([]GeminiAccount, []string) {
	var accounts []GeminiAccount
	var warnings []string

	credDir := filepath.Join(g.homeDir, ".cli-proxy-api")

	// Check if directory exists
	if _, err := os.Stat(credDir); os.IsNotExist(err) {
		return accounts, warnings
	}

	// Find all files matching *-*.json pattern
	entries, err := os.ReadDir(credDir)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("Failed to read ~/.cli-proxy-api/: %v", err))
		return accounts, warnings
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Must end with .json and contain a hyphen (for *-*.json pattern)
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		// Skip files from other providers (e.g., codex-*, claude-*)
		if strings.HasPrefix(name, "codex-") || strings.HasPrefix(name, "claude-") {
			continue
		}

		// Check for the pattern: must have at least one hyphen before .json
		baseName := strings.TrimSuffix(name, ".json")
		lastHyphenIdx := strings.LastIndex(baseName, "-")
		if lastHyphenIdx < 0 {
			continue
		}

		// Read and parse the file
		filePath := filepath.Join(credDir, name)
		account, err := g.parseCredFile(filePath, baseName)
		if err != nil {
			if errors.Is(err, errNotGeminiCred) {
				continue
			}
			// Report warning for files that look like Gemini credentials but fail to parse
			warnings = append(warnings, fmt.Sprintf("Failed to parse %s: %v", name, err))
			continue
		}

		accounts = append(accounts, *account)
	}

	return accounts, warnings
}

// parseCredFile reads and validates a credential file
func (g *GeminiProvider) parseCredFile(filePath, baseName string) (*GeminiAccount, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var cred geminiCredFile
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	if cred.Type != "" && cred.Type != "gemini" {
		return nil, errNotGeminiCred
	}

	// Validate required fields
	if cred.Token.AccessToken == "" {
		return nil, fmt.Errorf("missing token.access_token")
	}
	if cred.ProjectID == "" {
		return nil, fmt.Errorf("missing project_id")
	}

	// Extract email from filename by stripping the project_id suffix
	// Filename format: {email}-{project_id}.json
	// Example: user@example.com-gen-lang-client-0353902167.json
	email := ""
	suffix := "-" + cred.ProjectID
	if strings.HasSuffix(baseName, suffix) {
		email = strings.TrimSuffix(baseName, suffix)
	} else if cred.Email != "" {
		email = cred.Email
	} else {
		return nil, fmt.Errorf("filename does not match project_id in content")
	}

	tokenURI := cred.Token.TokenURI
	if tokenURI == "" {
		tokenURI = geminiTokenURI
	}

	var expiry time.Time
	if cred.Token.Expiry != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, cred.Token.Expiry); err == nil {
			expiry = parsed
		}
	}

	return &GeminiAccount{
		Email:        email,
		Token:        cred.Token.AccessToken,
		RefreshToken: cred.Token.RefreshToken,
		ClientID:     cred.Token.ClientID,
		ClientSecret: cred.Token.ClientSecret,
		TokenURI:     tokenURI,
		TokenExpiry:  expiry,
		ProjectID:    cred.ProjectID,
	}, nil
}

// fetchAccountUsage makes the API call for a single account
func (g *GeminiProvider) fetchAccountUsage(ctx context.Context, account GeminiAccount) ([]UsageRow, error) {
	token, err := g.accessTokenForAccount(ctx, account)
	if err != nil {
		return nil, err
	}

	body, status, err := g.doQuotaRequest(ctx, account, token)
	if err != nil {
		return nil, err
	}

	if status == http.StatusUnauthorized && account.RefreshToken != "" {
		token, err = g.refreshAccessToken(ctx, account)
		if err != nil {
			return nil, err
		}
		body, status, err = g.doQuotaRequest(ctx, account, token)
		if err != nil {
			return nil, err
		}
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", status, TruncateBody(body, 200))
	}

	// Parse response
	var quotaResp geminiQuotaResponse
	if err := json.Unmarshal(body, &quotaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for empty buckets
	if len(quotaResp.Buckets) == 0 {
		return []UsageRow{{
			Provider:   fmt.Sprintf("Gemini (%s)", account.Email),
			IsWarning:  true,
			WarningMsg: "Empty buckets array in response",
		}}, nil
	}

	// Convert buckets to usage rows
	var rows []UsageRow
	for _, bucket := range quotaResp.Buckets {
		row, err := g.bucketToRow(account.Email, bucket)
		if err != nil {
			rows = append(rows, UsageRow{
				Provider:   fmt.Sprintf("Gemini (%s)", account.Email),
				Label:      bucket.ModelID,
				IsWarning:  true,
				WarningMsg: fmt.Sprintf("Parse error: %v", err),
			})
			continue
		}
		rows = append(rows, row)
	}

	return rows, nil
}

func (g *GeminiProvider) accessTokenForAccount(ctx context.Context, account GeminiAccount) (string, error) {
	if account.Token == "" {
		return "", fmt.Errorf("missing access token")
	}
	if !account.TokenExpiry.IsZero() && time.Now().After(account.TokenExpiry.Add(-geminiTokenSkew)) {
		if account.RefreshToken == "" {
			return "", fmt.Errorf("access token expired and no refresh_token available")
		}
		return g.refreshAccessToken(ctx, account)
	}
	return account.Token, nil
}

func (g *GeminiProvider) refreshAccessToken(ctx context.Context, account GeminiAccount) (string, error) {
	if account.RefreshToken == "" || account.ClientID == "" {
		return "", fmt.Errorf("refresh token not available")
	}

	tokenURI := account.TokenURI
	if tokenURI == "" {
		tokenURI = geminiTokenURI
	}

	form := url.Values{}
	form.Set("client_id", account.ClientID)
	form.Set("refresh_token", account.RefreshToken)
	form.Set("grant_type", "refresh_token")
	if account.ClientSecret != "" {
		form.Set("client_secret", account.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token refresh failed: status %d: %s", resp.StatusCode, TruncateBody(body, 200))
	}

	var refreshResp geminiRefreshResponse
	if err := json.Unmarshal(body, &refreshResp); err != nil {
		return "", fmt.Errorf("failed to parse token refresh response: %w", err)
	}
	if refreshResp.AccessToken == "" {
		return "", fmt.Errorf("token refresh failed: empty access_token")
	}

	return refreshResp.AccessToken, nil
}

func (g *GeminiProvider) doQuotaRequest(ctx context.Context, account GeminiAccount, token string) ([]byte, int, error) {
	url := g.baseURL + geminiEndpoint
	reqBody := fmt.Sprintf(`{"project":"%s"}`, account.ProjectID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(reqBody))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ai-meter/0.1.0")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}

	return body, resp.StatusCode, nil
}

// bucketToRow converts a quota bucket to a UsageRow
func (g *GeminiProvider) bucketToRow(email string, bucket geminiQuotaBucket) (UsageRow, error) {
	// Parse reset time (ISO 8601) - use RFC3339Nano to accept fractional seconds
	resetTime, err := time.Parse(time.RFC3339Nano, bucket.ResetTime)
	if err != nil {
		return UsageRow{}, fmt.Errorf("invalid reset time format: %w", err)
	}

	// Calculate used percent from remaining fraction
	// remainingFraction 0.75 means 75% remaining = 25% used
	remainingFraction := bucket.RemainingFraction

	// Clamp to [0, 1] range defensively
	if remainingFraction > 1.0 {
		remainingFraction = 1.0
	}
	if remainingFraction < 0.0 {
		remainingFraction = 0.0
	}

	usedPercent := (1.0 - remainingFraction) * 100.0

	return UsageRow{
		Provider:     fmt.Sprintf("Gemini (%s)", email),
		Label:        bucket.ModelID,
		UsagePercent: usedPercent,
		ResetTime:    resetTime,
		IsWarning:    false,
	}, nil
}
