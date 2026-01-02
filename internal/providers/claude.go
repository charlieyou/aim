package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	claudeDefaultBaseURL  = "https://api.anthropic.com"
	claudeAPIPath         = "/api/oauth/usage"
	claudeAnthropicBeta   = "oauth-2025-04-20"
	claudeTimeout         = 30 * time.Second
)

// ClaudeProvider implements the Provider interface for Claude (Anthropic)
type ClaudeProvider struct {
	homeDir string
	baseURL string
	client  *http.Client
}

// claudeCredentials represents the credentials file structure
type claudeCredentials struct {
	ClaudeAIOauth struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"` // milliseconds since epoch
	} `json:"claudeAiOauth"`
}

// claudeUsageResponse represents the API response
type claudeUsageResponse struct {
	FiveHour *claudeWindow `json:"five_hour"`
	SevenDay *claudeWindow `json:"seven_day"`
}

// claudeWindow represents a usage window
type claudeWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

// NewClaudeProvider creates a new ClaudeProvider
func NewClaudeProvider() (*ClaudeProvider, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	return &ClaudeProvider{
		homeDir: homeDir,
		baseURL: claudeDefaultBaseURL,
		client: &http.Client{
			Timeout: claudeTimeout,
		},
	}, nil
}

// Name returns the provider name
func (c *ClaudeProvider) Name() string {
	return "Claude"
}

// FetchUsage fetches usage data from the Claude API
func (c *ClaudeProvider) FetchUsage(ctx context.Context) ([]UsageRow, error) {
	token, err := c.loadCredentials()
	if err != nil {
		return []UsageRow{{
			Provider:   c.Name(),
			IsWarning:  true,
			WarningMsg: err.Error(),
		}}, nil
	}

	resp, err := c.fetchUsageFromAPI(ctx, token)
	if err != nil {
		return []UsageRow{{
			Provider:   c.Name(),
			IsWarning:  true,
			WarningMsg: err.Error(),
		}}, nil
	}

	return c.parseUsageResponse(resp), nil
}

// loadCredentials loads the access token from the credentials file
func (c *ClaudeProvider) loadCredentials() (string, error) {
	credsPath := filepath.Join(c.homeDir, ".claude", ".credentials.json")

	data, err := os.ReadFile(credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("credentials file not found: %s", credsPath)
		}
		return "", fmt.Errorf("failed to read credentials: %w", err)
	}

	var creds claudeCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("failed to parse credentials: %w", err)
	}

	if creds.ClaudeAIOauth.AccessToken == "" {
		return "", fmt.Errorf("no access token found in credentials")
	}

	return creds.ClaudeAIOauth.AccessToken, nil
}

// fetchUsageFromAPI makes the HTTP request to the Claude API
func (c *ClaudeProvider) fetchUsageFromAPI(ctx context.Context, token string) (*claudeUsageResponse, error) {
	url := c.baseURL + claudeAPIPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-beta", claudeAnthropicBeta)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var usageResp claudeUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usageResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	return &usageResp, nil
}

// parseUsageResponse converts the API response to UsageRows
func (c *ClaudeProvider) parseUsageResponse(resp *claudeUsageResponse) []UsageRow {
	var rows []UsageRow

	if resp.FiveHour != nil {
		resetTime, err := time.Parse(time.RFC3339, resp.FiveHour.ResetsAt)
		if err != nil {
			rows = append(rows, UsageRow{
				Provider:   c.Name(),
				Label:      "5-hour",
				IsWarning:  true,
				WarningMsg: fmt.Sprintf("Parse error: invalid reset time format: %v", err),
			})
		} else {
			rows = append(rows, UsageRow{
				Provider:     c.Name(),
				Label:        "5-hour",
				UsagePercent: resp.FiveHour.Utilization,
				ResetTime:    resetTime,
			})
		}
	}

	if resp.SevenDay != nil {
		resetTime, err := time.Parse(time.RFC3339, resp.SevenDay.ResetsAt)
		if err != nil {
			rows = append(rows, UsageRow{
				Provider:   c.Name(),
				Label:      "7-day",
				IsWarning:  true,
				WarningMsg: fmt.Sprintf("Parse error: invalid reset time format: %v", err),
			})
		} else {
			rows = append(rows, UsageRow{
				Provider:     c.Name(),
				Label:        "7-day",
				UsagePercent: resp.SevenDay.Utilization,
				ResetTime:    resetTime,
			})
		}
	}

	return rows
}
