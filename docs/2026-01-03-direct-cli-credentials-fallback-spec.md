# Direct CLI Credentials Fallback

**Tier:** S
**Owner:** TBD
**Target ship:** TBD

## 1. Outcome & Scope

**Problem / context**
Users with credentials only in native CLI directories (`~/.claude/`, `~/.codex/`, `~/.gemini/`) cannot use AIM without manually copying credentials to `~/.cli-proxy-api/`. This creates unnecessary friction for users who authenticate through native CLI tools but do not use cli-proxy-api.

**Change summary**
Each provider (Claude, Codex, Gemini) will implement automatic fallback to discover credentials in their respective native CLI directories when no matching credentials are found in `~/.cli-proxy-api/`. Credentials loaded from native CLI directories are treated as read-only and will not be refreshed.

**Scope boundary**
- Only affects credential discovery and loading logic within each provider
- Does not change API endpoints, response parsing, output formatting, or existing refresh logic for `~/.cli-proxy-api` credentials
- Unix/Linux and macOS only; Windows support is out of scope for this iteration

## 2. User Experience & Flows

**UX impact**
- User-visible: Yes
- When `~/.cli-proxy-api/` has no matching credentials, AIM automatically tries native CLI directories
- If native CLI credentials are expired or return 401/403, user sees: "Token expired. Re-authenticate with [claude/codex/gemini] to refresh."
- No new CLI flags or configuration required
- Existing behavior unchanged when `.cli-proxy-api` credentials are present

**Fallback behavior:**
- Fallback only triggers when NO credentials are found in `.cli-proxy-api` for that provider
- If `.cli-proxy-api` credentials exist but are expired/invalid, existing refresh logic applies; native CLI is NOT used as fallback in this case
- This keeps the mental model simple: native CLI is a discovery fallback, not an auth fallback

## 3. Requirements + Verification

### Native Credential Schemas and Required Fields

**Claude (`~/.claude/.credentials.json`):**
```json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-oat01-...",
    "refreshToken": "sk-ant-ort01-...",
    "expiresAt": 1767479369930,
    "scopes": ["user:inference", "user:profile", "user:sessions:claude_code"],
    "subscriptionType": "max",
    "rateLimitTier": "default_claude_max_20x"
  }
}
```
- **Required fields:** `claudeAiOauth.accessToken` (if missing, skip file silently)
- **Optional fields:** `claudeAiOauth.expiresAt` (if missing, treat token as valid and attempt API call; 401 will trigger re-auth message)
- Display name: "Claude (native)"
- Requires new `loadNativeCredentials()` function (structure differs from proxy's flat `access_token`)

**Codex (`~/.codex/auth.json`):**
```json
{
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "eyJ...",
    "access_token": "eyJ...",
    "refresh_token": "rt_...",
    "account_id": "79b30623-af95-47e9-b629-3e5053c55c6a"
  },
  "last_refresh": "2025-12-28T21:55:29.182552327Z"
}
```
- **Required fields:** `tokens.access_token` (if missing, skip file silently)
- **Optional fields:** `tokens.account_id`, `last_refresh`
- Display name: "Codex (native)"
- Expiry detection during credential loading:
  1. Split JWT on `.` to get header.payload.signature
  2. Decode payload using **base64url** (RFC 4648 with `-_` alphabet, no padding required)
  3. Parse JSON and extract `exp` claim (Unix seconds)
  4. If any step fails (malformed JWT, missing `exp`): set expiry to `time.Time{}` (zero value); token will be attempted and 401 triggers re-auth message
- Populate `CodexAccount.LastRefresh` from `last_refresh` field if present (for display purposes)

**Gemini (`~/.gemini/oauth_creds.json`):**
```json
{
  "access_token": "ya29...",
  "refresh_token": "1//...",
  "scope": "https://www.googleapis.com/auth/...",
  "token_type": "Bearer",
  "id_token": "eyJ...",
  "expiry_date": 1767454704529
}
```
- **Required fields:** `access_token` (if missing, skip file silently)
- **Optional fields:** `expiry_date`, `refresh_token`, `scope` (if `expiry_date` missing, treat as valid and attempt API call)
- Display name: "Gemini (native)"
- **No project_id required**: For API request, send `{"project":""}` (empty string). If API rejects empty project, show warning row with error and continue.

### Implementation Notes

**IsNative field across all providers:**

Add `IsNative bool` field to all three account structs:
- `claudeAuth` in `claude.go`
- `CodexAccount` in `codex.go`
- `GeminiAccount` in `gemini.go`

Set `IsNative = true` when loading from native CLI directories. The refresh logic checks this flag:
- If `IsNative == true` AND 401/403 received: return error "Token expired. Re-authenticate with [tool] to refresh." (do NOT attempt refresh)
- If `IsNative == false`: existing refresh behavior applies

**Provider-specific parsing functions:**

Each provider needs a new native credential parsing function (separate from proxy parsing):
- Claude: `loadNativeCredentials()` - parses nested `claudeAiOauth` structure
- Codex: `loadNativeCredentials()` - parses `tokens` structure with JWT expiry extraction
- Gemini: `parseNativeCredFile()` - skips `project_id` validation, uses empty string for ProjectID

**Gemini field initialization for native credentials:**

When creating `GeminiAccount` from native file, set:
- `Email`: "Gemini (native)"
- `Token`: value from `access_token`
- `RefreshToken`: "" (do not use even if present, since `IsNative=true`)
- `ClientID`: "" (not needed for API calls)
- `ClientSecret`: "" (not needed for API calls)
- `TokenURI`: `geminiTokenURI` constant (default)
- `TokenExpiry`: parsed from `expiry_date` if present, else zero value
- `ProjectID`: "" (empty string)
- `IsNative`: true

**Error handling for native credential files:**

Native fallback should be **silent** (no warning rows) when:
- File does not exist
- File is unreadable (permissions)

Native fallback should show **warning row** when:
- File exists and is readable but contains invalid JSON
- File exists but required fields are missing

This matches the existing proxy behavior where parse failures generate warnings.

### Acceptance Criteria

- When `~/.cli-proxy-api/claude-*.json` has no matches, load from `~/.claude/.credentials.json`
- When `~/.cli-proxy-api/codex-*.json` has no matches, load from `~/.codex/auth.json`
- When `~/.cli-proxy-api/gemini-*.json` has no matches, load from `~/.gemini/oauth_creds.json`
- If credentials exist in both locations, prefer `~/.cli-proxy-api` (existing behavior preserved)
- `IsNative` field added to `claudeAuth`, `CodexAccount`, and `GeminiAccount` structs
- Native credentials are marked with `IsNative=true` when loaded
- Do NOT attempt token refresh for accounts where `IsNative=true`, even if a refresh token is present
- When native CLI credentials return 401/403, display: "Token expired. Re-authenticate with [claude/codex/gemini] to refresh."
- Existing `.cli-proxy-api` credential refresh continues to work as before
- Display names for native credentials use fixed labels: "Claude (native)", "Codex (native)", "Gemini (native)"
- Missing expiry fields: treat token as valid and attempt API call (401 triggers re-auth message)
- Codex JWT parsing uses base64url decoding (RFC 4648)

## 4. Instrumentation & Release Checks

**Validation after release**
- Remove `~/.cli-proxy-api` credentials and verify AIM loads credentials from native CLI directories
- Ensure credentials in both locations results in `~/.cli-proxy-api` being used
- Use expired native CLI credentials and verify the custom error message appears with no file modifications to the native directory
- Verify existing `.cli-proxy-api` credentials still work with token refresh
- Verify Gemini native credentials work with empty project_id (or show appropriate error if API rejects)
- Test with missing expiry fields to confirm tokens are still attempted

**Known risks**
- Limited to users with native CLI tools installed
- Native CLI credential JSON structure may vary across tool versions (field names confirmed as of Jan 2026)
- Gemini API may require project_id in some configurations (will show warning row if rejected)

**Decisions**
- No source indication in output beyond the "(native)" suffix in display name
- Error handling: silent skip for missing/unreadable files; warning row for parseable but invalid files
- Windows paths out of scope; feature is Unix/macOS only for this iteration
- All three native files are single-account; no multi-account selection logic needed
- Fallback is discovery-only: if `.cli-proxy-api` creds exist but fail refresh, we do NOT fall back to native
- Missing expiry: treat as valid, attempt API call, let 401 trigger re-auth message
- Codex JWT decode failures: set zero expiry, attempt API call, let 401 trigger re-auth message
