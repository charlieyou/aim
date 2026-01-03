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
	codexDefaultBaseURL = "https://chatgpt.com"
	codexRefreshURL     = "https://token.oaifree.com/api/auth/refresh"
)

// CodexAccount holds credentials for a single Codex account
type CodexAccount struct {
	Email        string
	Token        string
	RefreshToken string
	LoadErr      string // Error message from loading credentials, if any
}

// codexCredentials represents the JSON structure of credential files
type codexCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// codexAPIResponse represents the API response structure
type codexAPIResponse struct {
	PlanType  string `json:"plan_type"`
	RateLimit struct {
		PrimaryWindow struct {
			UsedPercent float64 `json:"used_percent"`
			ResetAt     int64   `json:"reset_at"`
		} `json:"primary_window"`
		SecondaryWindow struct {
			UsedPercent float64 `json:"used_percent"`
			ResetAt     int64   `json:"reset_at"`
		} `json:"secondary_window"`
	} `json:"rate_limit"`
}

// CodexProvider implements the Provider interface for OpenAI Codex
type CodexProvider struct {
	homeDir    string
	baseURL    string
	refreshURL string
	client     *http.Client
}

// NewCodexProvider creates a new CodexProvider with default settings
func NewCodexProvider() (*CodexProvider, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	return &CodexProvider{
		homeDir:    homeDir,
		baseURL:    codexDefaultBaseURL,
		refreshURL: codexRefreshURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Name returns the provider name
func (c *CodexProvider) Name() string {
	return "Codex"
}

// FetchUsage fetches usage data from all configured Codex accounts
func (c *CodexProvider) FetchUsage(ctx context.Context) ([]UsageRow, error) {
	accounts, err := c.loadCredentials()
	if err != nil {
		return nil, err
	}

	if len(accounts) == 0 {
		return []UsageRow{{
			Provider:   "Codex",
			IsWarning:  true,
			WarningMsg: "No credential files found matching ~/.cli-proxy-api/codex-*.json",
		}}, nil
	}

	var rows []UsageRow
	for _, account := range accounts {
		accountRows, err := c.fetchAccountUsage(ctx, account)
		if err != nil {
			rows = append(rows, UsageRow{
				Provider:   fmt.Sprintf("Codex (%s)", account.Email),
				IsWarning:  true,
				WarningMsg: err.Error(),
			})
			continue
		}
		rows = append(rows, accountRows...)
	}

	return rows, nil
}

// loadCredentials discovers and loads all Codex credential files
func (c *CodexProvider) loadCredentials() ([]CodexAccount, error) {
	pattern := filepath.Join(c.homeDir, ".cli-proxy-api", "codex-*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob credentials: %w", err)
	}

	var accounts []CodexAccount
	for _, path := range matches {
		account, err := c.loadCredentialFile(path)
		if err != nil {
			// Return a partial account with email and error so we can report specific details
			email := extractEmailFromFilename(filepath.Base(path))
			accounts = append(accounts, CodexAccount{
				Email:   email,
				Token:   "", // Empty token signals a load error
				LoadErr: err.Error(),
			})
			continue
		}
		accounts = append(accounts, account)
	}

	return accounts, nil
}

// loadCredentialFile loads a single credential file
func (c *CodexProvider) loadCredentialFile(path string) (CodexAccount, error) {
	filename := filepath.Base(path)
	email := extractEmailFromFilename(filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return CodexAccount{}, fmt.Errorf("failed to read file: %w", err)
	}

	var creds codexCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return CodexAccount{}, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if creds.AccessToken == "" {
		return CodexAccount{}, fmt.Errorf("missing access_token")
	}

	return CodexAccount{
		Email:        email,
		Token:        creds.AccessToken,
		RefreshToken: creds.RefreshToken,
	}, nil
}

// extractEmailFromFilename extracts email from filename like "codex-user@example.com.json"
func extractEmailFromFilename(filename string) string {
	// Remove "codex-" prefix and ".json" suffix
	name := strings.TrimPrefix(filename, "codex-")
	name = strings.TrimSuffix(name, ".json")
	return name
}

// fetchAccountUsage fetches usage for a single account
func (c *CodexProvider) fetchAccountUsage(ctx context.Context, account CodexAccount) ([]UsageRow, error) {
	if account.Token == "" {
		if account.LoadErr != "" {
			return nil, fmt.Errorf("failed to load credentials: %s", account.LoadErr)
		}
		return nil, fmt.Errorf("failed to load credentials")
	}

	apiResp, err := c.fetchUsageWithToken(ctx, account.Token)
	if err != nil {
		var statusErr APIStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusUnauthorized && account.RefreshToken != "" {
			refreshed, refreshErr := c.refreshAccessToken(ctx, account)
			if refreshErr != nil {
				return nil, refreshErr
			}
			apiResp, err = c.fetchUsageWithToken(ctx, refreshed)
		}
	}
	if err != nil {
		return nil, err
	}

	providerName := fmt.Sprintf("Codex (%s)", account.Email)

	return []UsageRow{
		{
			Provider:     providerName,
			Label:        "5-hour",
			UsagePercent: apiResp.RateLimit.PrimaryWindow.UsedPercent,
			ResetTime:    time.Unix(apiResp.RateLimit.PrimaryWindow.ResetAt, 0),
		},
		{
			Provider:     providerName,
			Label:        "7-day",
			UsagePercent: apiResp.RateLimit.SecondaryWindow.UsedPercent,
			ResetTime:    time.Unix(apiResp.RateLimit.SecondaryWindow.ResetAt, 0),
		},
	}, nil
}

func (c *CodexProvider) fetchUsageWithToken(ctx context.Context, token string) (*codexAPIResponse, error) {
	url := c.baseURL + "/backend-api/wham/usage"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ai-meter/0.1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, APIStatusError{
			StatusCode: resp.StatusCode,
			Body:       TruncateBody(body, 200),
		}
	}

	var apiResp codexAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &apiResp, nil
}

func (c *CodexProvider) refreshAccessToken(ctx context.Context, account CodexAccount) (string, error) {
	if account.RefreshToken == "" {
		return "", fmt.Errorf("refresh token not available")
	}

	refreshURL := c.refreshURL
	if refreshURL == "" {
		refreshURL = codexRefreshURL
	}

	form := url.Values{}
	form.Set("refresh_token", account.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", APIStatusError{
			StatusCode: resp.StatusCode,
			Body:       TruncateBody(body, 200),
		}
	}

	var refreshResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &refreshResp); err != nil {
		return "", fmt.Errorf("failed to parse token refresh response: %w", err)
	}
	if refreshResp.AccessToken == "" {
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err == nil {
			if v, ok := raw["access_token"].(string); ok && v != "" {
				refreshResp.AccessToken = v
			} else if v, ok := raw["accessToken"].(string); ok && v != "" {
				refreshResp.AccessToken = v
			} else if v, ok := raw["token"].(string); ok && v != "" {
				refreshResp.AccessToken = v
			}
		}
	}
	if refreshResp.AccessToken == "" {
		return "", fmt.Errorf("token refresh failed: empty access_token")
	}

	return refreshResp.AccessToken, nil
}
