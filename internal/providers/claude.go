package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	claudeDefaultBaseURL = "https://api.anthropic.com"
	claudeAPIPath        = "/api/oauth/usage"
	claudeAnthropicBeta  = "oauth-2025-04-20"
	claudeTimeout        = 30 * time.Second
	claudeTokenURL       = "https://console.anthropic.com/v1/oauth/token"
	claudeClientID       = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)

var claudeDefaultScopes = []string{
	"user:profile",
	"user:inference",
	"user:sessions:claude_code",
}

// ClaudeProvider implements the Provider interface for Claude (Anthropic)
type ClaudeProvider struct {
	homeDir  string
	baseURL  string
	tokenURL string
	client   *http.Client
}

// claudeCredentials represents the ~/.cli-proxy-api/claude-*.json structure.
type claudeCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Email        string `json:"email"`
	Type         string `json:"type"`
	Expired      string `json:"expired"`
	LastRefresh  string `json:"last_refresh"`
}

type claudeAuth struct {
	AccessToken    string
	RefreshToken   string
	ExpiresAt      time.Time
	Scopes         []string
	Email          string
	SourceName     string
	CredentialPath string
	LoadErr        string
	IsNative       bool
}

// claudeUsageResponse represents the API response
type claudeUsageResponse struct {
	FiveHour *claudeWindow `json:"five_hour"`
	SevenDay *claudeWindow `json:"seven_day"`
}

type claudeRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
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
		homeDir:  homeDir,
		baseURL:  claudeDefaultBaseURL,
		tokenURL: claudeTokenURL,
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
	accounts, err := c.loadCredentials()
	if err != nil {
		return []UsageRow{{
			Provider:   c.Name(),
			IsWarning:  true,
			WarningMsg: claudeWarningMessage(err),
		}}, nil
	}

	if len(accounts) == 0 {
		var credPath string
		if DetectCredentialSource(c.homeDir) == SourceNative {
			credPath = filepath.Join(c.homeDir, ".claude", ".credentials.json")
		} else {
			credPath = filepath.Join(c.homeDir, ".cli-proxy-api", "claude-*.json")
		}
		return []UsageRow{{
			Provider:   c.Name(),
			IsWarning:  true,
			WarningMsg: fmt.Sprintf("No credential files found matching %s", credPath),
		}}, nil
	}

	var rows []UsageRow
	for _, account := range accounts {
		accountRows, err := c.fetchAccountUsage(ctx, account)
		if err != nil {
			rows = append(rows, UsageRow{
				Provider:   claudeProviderName(account),
				IsWarning:  true,
				WarningMsg: claudeWarningMessage(err),
			})
			continue
		}
		rows = append(rows, accountRows...)
	}

	return rows, nil
}

// loadCredentials loads the access token from the credentials file
func (c *ClaudeProvider) loadCredentials() ([]claudeAuth, error) {
	source := DetectCredentialSource(c.homeDir)
	if source == SourceNative {
		return c.loadNativeCredentials()
	}

	pattern := filepath.Join(c.homeDir, ".cli-proxy-api", "claude-*.json")

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob credentials: %w", err)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("No credential files found matching %s", pattern)
	}

	sort.Strings(matches)

	accounts := make([]claudeAuth, 0, len(matches))
	for _, credsPath := range matches {
		account, err := c.loadCredentialFile(credsPath)
		if err != nil {
			sourceName := extractClaudeEmailFromFilename(filepath.Base(credsPath))
			accounts = append(accounts, claudeAuth{
				Email:          sourceName,
				SourceName:     sourceName,
				CredentialPath: credsPath,
				LoadErr:        err.Error(),
			})
			continue
		}
		accounts = append(accounts, account)
	}

	return accounts, nil
}

func (c *ClaudeProvider) loadCredentialFile(credsPath string) (claudeAuth, error) {
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return claudeAuth{}, fmt.Errorf("failed to read credentials file %s: %w", credsPath, err)
	}

	var creds claudeCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return claudeAuth{}, fmt.Errorf("failed to parse credentials file %s: %w", credsPath, err)
	}

	if creds.Type != "" && creds.Type != "claude" {
		return claudeAuth{}, fmt.Errorf("unexpected credential type %q in %s", creds.Type, credsPath)
	}

	if creds.AccessToken == "" {
		return claudeAuth{}, fmt.Errorf("no access token found in credentials")
	}

	sourceName := extractClaudeEmailFromFilename(filepath.Base(credsPath))
	email := creds.Email
	if email == "" {
		email = sourceName
	}

	return claudeAuth{
		AccessToken:    creds.AccessToken,
		RefreshToken:   creds.RefreshToken,
		Email:          email,
		SourceName:     sourceName,
		CredentialPath: credsPath,
	}, nil
}

// nativeCredentials represents the ~/.claude/.credentials.json structure.
type nativeCredentials struct {
	ClaudeAIOAuth *nativeOAuth `json:"claudeAiOauth"`
}

type nativeOAuth struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
}

// loadNativeCredentials loads credentials from ~/.claude/.credentials.json.
func (c *ClaudeProvider) loadNativeCredentials() ([]claudeAuth, error) {
	credsPath := filepath.Join(c.homeDir, ".claude", ".credentials.json")

	data, err := os.ReadFile(credsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // silent skip if file missing
		}
		return nil, nil // silent skip if unreadable
	}

	var creds nativeCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return []claudeAuth{{
			Email:          "native",
			SourceName:     "native",
			CredentialPath: credsPath,
			IsNative:       true,
			LoadErr:        fmt.Sprintf("failed to parse credentials: %v", err),
		}}, nil
	}

	if creds.ClaudeAIOAuth == nil || creds.ClaudeAIOAuth.AccessToken == "" {
		return []claudeAuth{{
			Email:          "native",
			SourceName:     "native",
			CredentialPath: credsPath,
			IsNative:       true,
			LoadErr:        "no access token found in credentials",
		}}, nil
	}

	auth := claudeAuth{
		AccessToken:    creds.ClaudeAIOAuth.AccessToken,
		RefreshToken:   creds.ClaudeAIOAuth.RefreshToken,
		Email:          "native",
		SourceName:     "native",
		CredentialPath: credsPath,
		IsNative:       true,
	}

	if creds.ClaudeAIOAuth.ExpiresAt > 0 {
		auth.ExpiresAt = time.UnixMilli(creds.ClaudeAIOAuth.ExpiresAt)
	}

	return []claudeAuth{auth}, nil
}

func (c *ClaudeProvider) fetchAccountUsage(ctx context.Context, account claudeAuth) ([]UsageRow, error) {
	if account.AccessToken == "" {
		if account.LoadErr != "" {
			return nil, fmt.Errorf("failed to load credentials: %s", account.LoadErr)
		}
		return nil, fmt.Errorf("no access token found in credentials")
	}

	resp, err := c.fetchUsageFromAPI(ctx, account.AccessToken)
	if err != nil {
		var statusErr APIStatusError
		if errors.As(err, &statusErr) {
			claudeDebugf("usage API status=%d body=%q", statusErr.StatusCode, redactTokens(statusErr.Body))
		}
		if errors.As(err, &statusErr) && account.RefreshToken != "" &&
			(statusErr.StatusCode == http.StatusUnauthorized || statusErr.StatusCode == http.StatusForbidden) {
			claudeDebugf("attempting token refresh after status=%d", statusErr.StatusCode)
			refreshedToken, refreshErr := c.refreshAccessToken(ctx, account)
			if refreshErr == nil {
				claudeDebugf("token refresh succeeded, retrying usage API")
				resp, err = c.fetchUsageFromAPI(ctx, refreshedToken)
			} else {
				claudeDebugf("token refresh failed: %v", refreshErr)
				err = refreshErr
			}
		}
	}
	if err != nil {
		return nil, err
	}

	providerName := claudeProviderName(account)
	return c.parseUsageResponse(resp, providerName), nil
}

func (c *ClaudeProvider) refreshAccessToken(ctx context.Context, creds claudeAuth) (string, error) {
	if creds.IsNative {
		return "", fmt.Errorf("token expired. Re-authenticate with claude to refresh")
	}
	if creds.RefreshToken == "" {
		return "", fmt.Errorf("refresh token not available")
	}

	scopes := creds.Scopes
	if len(scopes) == 0 {
		scopes = claudeDefaultScopes
	}

	payload := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": creds.RefreshToken,
		"client_id":     claudeClientID,
	}
	if len(scopes) > 0 {
		payload["scope"] = strings.Join(scopes, " ")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to encode refresh request: %w", err)
	}

	tokenURL := c.tokenURL
	if tokenURL == "" {
		tokenURL = claudeTokenURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ai-meter/0.1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		claudeDebugf("token refresh request failed: %v", err)
		return "", fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		claudeDebugf("failed to read token refresh response: %v", err)
		return "", fmt.Errorf("failed to read token refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		claudeDebugf("token refresh non-200 status=%d body=%q", resp.StatusCode, debugBody(respBody))
		return "", APIStatusError{
			StatusCode: resp.StatusCode,
			Body:       TruncateBody(respBody, 200),
		}
	}

	var refreshResp claudeRefreshResponse
	if err := json.Unmarshal(respBody, &refreshResp); err != nil {
		claudeDebugf("failed to parse token refresh response: %v body=%q", err, debugBody(respBody))
		return "", fmt.Errorf("failed to parse token refresh response: %w", err)
	}
	if refreshResp.AccessToken == "" {
		claudeDebugf("token refresh response missing access_token")
		return "", fmt.Errorf("token refresh failed: empty access_token")
	}

	if creds.CredentialPath == "" {
		return "", fmt.Errorf("credential path not available for refresh")
	}
	if err := updateClaudeCredentialFile(creds.CredentialPath, refreshResp); err != nil {
		return "", err
	}

	return refreshResp.AccessToken, nil
}

func updateClaudeCredentialFile(path string, refreshResp claudeRefreshResponse) error {
	now := time.Now()
	return updateJSONCredentials(path, func(raw map[string]any) error {
		raw["access_token"] = refreshResp.AccessToken
		if refreshResp.RefreshToken != "" {
			raw["refresh_token"] = refreshResp.RefreshToken
		}
		raw["last_refresh"] = formatCredentialTime(now)
		if refreshResp.ExpiresIn > 0 {
			raw["expired"] = formatCredentialTime(now.Add(time.Duration(refreshResp.ExpiresIn) * time.Second))
		}
		return nil
	})
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
	req.Header.Set("User-Agent", "ai-meter/0.1.0")
	req.Header.Set("anthropic-beta", claudeAnthropicBeta)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		claudeDebugf("usage API non-200 status=%d body=%q", resp.StatusCode, debugBody(body))
		return nil, APIStatusError{
			StatusCode: resp.StatusCode,
			Body:       TruncateBody(body, 200),
		}
	}

	var usageResp claudeUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usageResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	return &usageResp, nil
}

// parseUsageResponse converts the API response to UsageRows
func (c *ClaudeProvider) parseUsageResponse(resp *claudeUsageResponse, providerName string) []UsageRow {
	var rows []UsageRow

	if resp.FiveHour != nil {
		resetTime, err := parseClaudeResetTime(resp.FiveHour.ResetsAt)
		if err != nil {
			rows = append(rows, UsageRow{
				Provider:   providerName,
				Label:      "5-hour",
				IsWarning:  true,
				WarningMsg: fmt.Sprintf("Parse error: invalid reset time format: %v", err),
			})
		} else {
			rows = append(rows, UsageRow{
				Provider:     providerName,
				Label:        "5-hour",
				UsagePercent: resp.FiveHour.Utilization,
				ResetTime:    resetTime,
			})
		}
	}

	if resp.SevenDay != nil {
		resetTime, err := parseClaudeResetTime(resp.SevenDay.ResetsAt)
		if err != nil {
			rows = append(rows, UsageRow{
				Provider:   providerName,
				Label:      "7-day",
				IsWarning:  true,
				WarningMsg: fmt.Sprintf("Parse error: invalid reset time format: %v", err),
			})
		} else {
			rows = append(rows, UsageRow{
				Provider:     providerName,
				Label:        "7-day",
				UsagePercent: resp.SevenDay.Utilization,
				ResetTime:    resetTime,
			})
		}
	}

	return rows
}

func parseClaudeResetTime(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, raw)
}

func claudeWarningMessage(err error) string {
	if err == nil {
		return ""
	}

	if isTimeoutError(err) {
		return "request timed out"
	}

	var statusErr APIStatusError
	if errors.As(err, &statusErr) {
		lowerBody := strings.ToLower(statusErr.Body)
		if strings.Contains(lowerBody, "revok") || strings.Contains(lowerBody, "invalid_grant") {
			return "authentication failed (token revoked)"
		}
		switch statusErr.StatusCode {
		case http.StatusUnauthorized:
			return "authentication failed (token may be expired)"
		case http.StatusForbidden:
			return "authentication failed (permission denied)"
		}
	}

	return err.Error()
}

func claudeProviderName(creds claudeAuth) string {
	label := strings.TrimSpace(creds.Email)
	if label == "" {
		label = strings.TrimSpace(creds.SourceName)
	}
	if label == "" {
		return "Claude"
	}
	return fmt.Sprintf("Claude (%s)", label)
}

func extractClaudeEmailFromFilename(filename string) string {
	name := strings.TrimPrefix(filename, "claude-")
	return strings.TrimSuffix(name, ".json")
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func claudeDebugf(format string, args ...any) {
	debugf("Claude", format, args...)
}
