package providers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	codexDefaultBaseURL  = "https://chatgpt.com"
	codexRefreshURL      = "https://auth.openai.com/oauth/token"
	codexDefaultClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// CodexAccount holds credentials for a single Codex account
type CodexAccount struct {
	Email          string
	AccountID      string
	SourceName     string
	DisplayName    string
	Token          string
	IDToken        string
	RefreshToken   string
	ClientID       string
	Scopes         []string
	LastRefresh    time.Time
	ExpiresAt      time.Time
	CredentialPath string
	LoadErr        string // Error message from loading credentials, if any
	IsNative       bool   // true when loaded from ~/.codex/ instead of proxy
}

// codexCredentials represents the JSON structure of credential files
type codexCredentials struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	Email        string `json:"email"`
	AccountID    string `json:"account_id"`
	LastRefresh  string `json:"last_refresh"`
	Expired      string `json:"expired"`
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
		warningMsg := "No credential files found matching ~/.cli-proxy-api/codex-*.json"
		if DetectCredentialSource(c.homeDir) == SourceNative {
			warningMsg = "No credentials found in ~/.codex/auth.json"
		}
		return []UsageRow{{
			Provider:   "Codex",
			IsWarning:  true,
			WarningMsg: warningMsg,
		}}, nil
	}

	var rows []UsageRow
	for _, account := range accounts {
		accountRows, err := c.fetchAccountUsage(ctx, account)
		if err != nil {
			rows = append(rows, UsageRow{
				Provider:   codexProviderName(account),
				IsWarning:  true,
				WarningMsg: err.Error(),
				DebugInfo:  codexAccountDebug(account, ""),
			})
			continue
		}
		rows = append(rows, accountRows...)
	}

	return rows, nil
}

// loadCredentials discovers and loads all Codex credential files
func (c *CodexProvider) loadCredentials() ([]CodexAccount, error) {
	source := DetectCredentialSource(c.homeDir)
	if source == SourceNative {
		return c.loadNativeCredentials()
	}

	pattern := filepath.Join(c.homeDir, ".cli-proxy-api", "codex-*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob credentials: %w", err)
	}

	sort.Strings(matches)

	accounts := make([]CodexAccount, 0, len(matches))
	for _, path := range matches {
		account, err := c.loadCredentialFile(path)
		if err != nil {
			// Return a partial account with email and error so we can report specific details
			sourceName := extractEmailFromFilename(filepath.Base(path))
			account = CodexAccount{
				Email:      sourceName,
				SourceName: sourceName,
				Token:      "", // Empty token signals a load error
				LoadErr:    err.Error(),
			}
		}
		accounts = append(accounts, account)
	}

	applyCodexDisplayNames(accounts)

	return accounts, nil
}

// codexNativeCredentials represents the JSON structure of ~/.codex/auth.json
type codexNativeCredentials struct {
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

// loadNativeCredentials loads credentials from ~/.codex/auth.json
func (c *CodexProvider) loadNativeCredentials() ([]CodexAccount, error) {
	path := filepath.Join(c.homeDir, ".codex", "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		// Silent return if file missing/unreadable
		return nil, nil
	}

	var creds codexNativeCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		// Return account with LoadErr so caller can surface the parse error
		return []CodexAccount{{
			CredentialPath: path,
			IsNative:       true,
			DisplayName:    "native",
			LoadErr:        fmt.Sprintf("failed to parse %s: %v", path, err),
		}}, nil
	}

	if creds.Tokens.AccessToken == "" {
		// Return account with LoadErr so caller can surface the issue
		return []CodexAccount{{
			CredentialPath: path,
			IsNative:       true,
			DisplayName:    "native",
			LoadErr:        fmt.Sprintf("no access token found in %s", path),
		}}, nil
	}

	account := CodexAccount{
		AccountID:      creds.Tokens.AccountID,
		Token:          creds.Tokens.AccessToken,
		RefreshToken:   creds.Tokens.RefreshToken,
		CredentialPath: path,
		IsNative:       true,
		DisplayName:    "native",
	}

	// Extract expiry from JWT
	claims, err := decodeJWTClaims(creds.Tokens.AccessToken)
	if err == nil {
		if exp, ok := claims["exp"].(float64); ok {
			account.ExpiresAt = time.Unix(int64(exp), 0)
		}
	}
	// If JWT decode fails or exp missing, ExpiresAt stays at zero value

	// Parse last_refresh if present
	if creds.LastRefresh != "" {
		if t, err := time.Parse(time.RFC3339Nano, creds.LastRefresh); err == nil {
			account.LastRefresh = t
		}
	}

	// Extract client ID and scopes from token
	account.ClientID, account.Scopes = extractFromToken(creds.Tokens.AccessToken)

	return []CodexAccount{account}, nil
}

func applyCodexDisplayNames(accounts []CodexAccount) {
	emailCounts := make(map[string]int)
	emailHasAltSource := make(map[string]bool)
	for _, account := range accounts {
		if account.Email == "" {
			continue
		}
		key := strings.ToLower(account.Email)
		emailCounts[key]++
		if account.SourceName != "" && !strings.EqualFold(account.SourceName, account.Email) {
			emailHasAltSource[key] = true
		}
	}

	for i := range accounts {
		label := accounts[i].Email
		if label == "" {
			label = accounts[i].SourceName
		}
		if label == "" {
			label = "unknown"
		}

		key := strings.ToLower(accounts[i].Email)
		if key != "" && emailCounts[key] > 1 {
			if accounts[i].SourceName != "" && !strings.EqualFold(accounts[i].SourceName, accounts[i].Email) {
				label = accounts[i].SourceName
			} else if !emailHasAltSource[key] && accounts[i].AccountID != "" {
				label = fmt.Sprintf("%s#%s", accounts[i].Email, shortID(accounts[i].AccountID))
			}
		}

		accounts[i].DisplayName = label
	}
}

// loadCredentialFile loads a single credential file
func (c *CodexProvider) loadCredentialFile(path string) (CodexAccount, error) {
	filename := filepath.Base(path)
	sourceName := extractEmailFromFilename(filename)

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

	email := creds.Email
	if email == "" {
		email = sourceName
	}
	lastRefresh, _ := parseCodexTime(creds.LastRefresh)
	expiresAt, _ := parseCodexTime(creds.Expired)

	clientID, scopes := extractCodexAuthDetails(creds.AccessToken, creds.IDToken)

	return CodexAccount{
		Email:          email,
		AccountID:      creds.AccountID,
		SourceName:     sourceName,
		Token:          creds.AccessToken,
		IDToken:        creds.IDToken,
		RefreshToken:   creds.RefreshToken,
		ClientID:       clientID,
		Scopes:         scopes,
		LastRefresh:    lastRefresh,
		ExpiresAt:      expiresAt,
		CredentialPath: path,
	}, nil
}

// extractEmailFromFilename extracts email from filename like "codex-user@example.com.json"
func extractEmailFromFilename(filename string) string {
	// Remove "codex-" prefix and ".json" suffix
	name := strings.TrimPrefix(filename, "codex-")
	name = strings.TrimSuffix(name, ".json")
	return name
}

func parseCodexTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts, nil
	}
	return time.Parse(time.RFC3339, value)
}

func shortID(value string) string {
	if len(value) <= 6 {
		return value
	}
	return value[:6]
}

func codexTokenFingerprint(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:4])
}

func codexAccountDebug(account CodexAccount, planType string) string {
	parts := make([]string, 0, 3)
	if account.AccountID != "" {
		parts = append(parts, "acct:"+shortID(account.AccountID))
	}
	if planType != "" {
		parts = append(parts, "plan:"+planType)
	}
	if fp := codexTokenFingerprint(account.Token); fp != "" {
		parts = append(parts, "token:"+fp)
	}
	return strings.Join(parts, " ")
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
			debugf("Codex", "attempting token refresh after status=%d for %s", statusErr.StatusCode, codexProviderName(account))
			refreshed, refreshErr := c.refreshAccessToken(ctx, account)
			if refreshErr != nil {
				debugf("Codex", "token refresh failed for %s: %v", codexProviderName(account), refreshErr)
				return nil, refreshErr
			}
			debugf("Codex", "token refresh succeeded, retrying usage API for %s", codexProviderName(account))
			account.Token = refreshed
			apiResp, err = c.fetchUsageWithToken(ctx, refreshed)
		}
	}
	if err != nil {
		return nil, err
	}

	providerName := codexProviderName(account)
	debugInfo := codexAccountDebug(account, apiResp.PlanType)

	return []UsageRow{
		{
			Provider:     providerName,
			Label:        "5-hour",
			UsagePercent: apiResp.RateLimit.PrimaryWindow.UsedPercent,
			ResetTime:    time.Unix(apiResp.RateLimit.PrimaryWindow.ResetAt, 0),
			DebugInfo:    debugInfo,
		},
		{
			Provider:     providerName,
			Label:        "7-day",
			UsagePercent: apiResp.RateLimit.SecondaryWindow.UsedPercent,
			ResetTime:    time.Unix(apiResp.RateLimit.SecondaryWindow.ResetAt, 0),
			DebugInfo:    debugInfo,
		},
	}, nil
}

func codexProviderName(account CodexAccount) string {
	label := account.DisplayName
	if label == "" {
		if account.Email != "" {
			label = account.Email
		} else if account.SourceName != "" {
			label = account.SourceName
		}
	}
	if label == "" {
		return "Codex"
	}
	return fmt.Sprintf("Codex (%s)", label)
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
		debugf("Codex", "usage API non-200 status=%d body=%q", resp.StatusCode, debugBody(body))
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
	if account.IsNative {
		return "", fmt.Errorf("token expired. Re-authenticate with codex to refresh")
	}

	if account.RefreshToken == "" {
		return "", fmt.Errorf("refresh token not available")
	}

	refreshURL := c.refreshURL
	if refreshURL == "" {
		refreshURL = codexRefreshURL
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", account.RefreshToken)
	clientID := account.ClientID
	if clientID == "" {
		clientID = codexDefaultClientID
	}
	form.Set("client_id", clientID)
	if len(account.Scopes) > 0 {
		form.Set("scope", strings.Join(account.Scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		debugf("Codex", "token refresh request failed: %v", err)
		return "", fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		debugf("Codex", "failed to read token refresh response: %v", err)
		return "", fmt.Errorf("failed to read token refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		debugf("Codex", "token refresh non-200 status=%d body=%q", resp.StatusCode, debugBody(body))
		return "", APIStatusError{
			StatusCode: resp.StatusCode,
			Body:       TruncateBody(body, 200),
		}
	}

	var refreshResp struct {
		Raw map[string]any
	}
	if err := json.Unmarshal(body, &refreshResp.Raw); err != nil {
		debugf("Codex", "failed to parse token refresh response: %v body=%q", err, debugBody(body))
		return "", fmt.Errorf("failed to parse token refresh response: %w", err)
	}
	accessToken := stringFromMap(refreshResp.Raw, "access_token", "accessToken", "token")
	if accessToken == "" {
		debugf("Codex", "token refresh response missing access_token")
		return "", fmt.Errorf("token refresh failed: empty access_token")
	}

	refreshToken := stringFromMap(refreshResp.Raw, "refresh_token", "refreshToken")
	idToken := stringFromMap(refreshResp.Raw, "id_token", "idToken")
	expiresIn := int64FromMap(refreshResp.Raw, "expires_in", "expiresIn")

	if account.CredentialPath == "" {
		return "", fmt.Errorf("credential path not available for refresh")
	}
	if err := updateCodexCredentialFile(account.CredentialPath, accessToken, refreshToken, idToken, expiresIn); err != nil {
		return "", err
	}

	return accessToken, nil
}

func updateCodexCredentialFile(path, accessToken, refreshToken, idToken string, expiresIn int64) error {
	now := time.Now()
	return updateJSONCredentials(path, func(raw map[string]any) error {
		raw["access_token"] = accessToken
		if refreshToken != "" {
			raw["refresh_token"] = refreshToken
		}
		if idToken != "" {
			raw["id_token"] = idToken
		}
		raw["last_refresh"] = formatCredentialTime(now)
		if expiresIn > 0 {
			raw["expired"] = formatCredentialTime(now.Add(time.Duration(expiresIn) * time.Second))
		}
		return nil
	})
}

func stringFromMap(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if val, ok := raw[key]; ok {
			if str, ok := val.(string); ok && str != "" {
				return str
			}
		}
	}
	return ""
}

func int64FromMap(raw map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if val, ok := raw[key]; ok {
			switch typed := val.(type) {
			case float64:
				return int64(typed)
			case int64:
				return typed
			case int:
				return int64(typed)
			case string:
				if parsed, err := strconv.ParseInt(typed, 10, 64); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func extractCodexAuthDetails(accessToken, idToken string) (string, []string) {
	// Prefer id_token for client_id since it contains the OAuth client_id,
	// not the API audience like access tokens do
	clientID, scopes := extractFromToken(idToken)

	// Fall back to access token only for explicit client_id claim.
	// Do NOT use aud from access tokens - it contains the API audience URL,
	// not the OAuth client_id, which would cause refresh to fail.
	if clientID == "" {
		clientID = extractExplicitClientID(accessToken)
	}

	// Scopes can come from either token - use whichever has them
	if len(scopes) == 0 {
		_, scopes = extractFromToken(accessToken)
	}

	if clientID == "" {
		clientID = codexDefaultClientID
	}
	return clientID, scopes
}

// extractExplicitClientID extracts only the explicit client_id claim from a token,
// without falling back to aud (which may contain API audience URLs).
func extractExplicitClientID(token string) string {
	claims, err := decodeJWTClaims(token)
	if err != nil {
		return ""
	}
	if v, ok := claims["client_id"].(string); ok && v != "" {
		return v
	}
	return ""
}

func extractFromToken(token string) (string, []string) {
	claims, err := decodeJWTClaims(token)
	if err != nil {
		return "", nil
	}
	return extractClientID(claims), extractScopes(claims)
}

func decodeJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid token")
	}
	payload := parts[1]
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		padding := strings.Repeat("=", (4-len(payload)%4)%4)
		decoded, err = base64.URLEncoding.DecodeString(payload + padding)
		if err != nil {
			return nil, err
		}
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, err
	}

	return claims, nil
}

func extractClientID(claims map[string]any) string {
	if v, ok := claims["client_id"].(string); ok && v != "" {
		return v
	}
	if aud, ok := claims["aud"]; ok {
		switch t := aud.(type) {
		case string:
			return t
		case []any:
			for _, item := range t {
				if s, ok := item.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func extractScopes(claims map[string]any) []string {
	if v, ok := claims["scp"]; ok {
		return normalizeScopes(v)
	}
	if v, ok := claims["scope"]; ok {
		return normalizeScopes(v)
	}
	return nil
}

func normalizeScopes(value any) []string {
	switch v := value.(type) {
	case []any:
		var scopes []string
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				scopes = append(scopes, s)
			}
		}
		return scopes
	case []string:
		var scopes []string
		for _, item := range v {
			if item != "" {
				scopes = append(scopes, item)
			}
		}
		return scopes
	case string:
		return strings.Fields(v)
	default:
		return nil
	}
}
