# Implementation Plan: Direct CLI Credentials Fallback

## Context & Goals
- **Spec**: `docs/2026-01-03-direct-cli-credentials-fallback-spec.md`
- Enable AIM to automatically discover credentials from native CLI directories (`~/.claude/`, `~/.codex/`, `~/.gemini/`) when no matching credentials exist in `~/.cli-proxy-api/`
- Native CLI credentials are read-only (no refresh attempts); users must re-authenticate with the native CLI tool when tokens expire
- Improves onboarding for users who already use native CLI tools by removing manual credential copying

## Scope & Non-Goals
- **In Scope**
  - Add `IsNative bool` field to `claudeAuth`, `CodexAccount`, `GeminiAccount` structs
  - Implement native credential loading for each provider (different JSON structures)
  - Fallback discovery logic when `.cli-proxy-api` has no matches
  - Skip refresh for native credentials; show re-auth message on 401/403
  - Unit tests for new parsing and fallback logic
  - Debug logging for native credential discovery using existing `debugf()` pattern
- **Out of Scope (Non-Goals)**
  - Windows support (Unix/macOS only for this iteration)
  - Multi-account selection for native credentials (each native file is single-account)
  - Writing/modifying native CLI credential files
  - Using native credentials as auth fallback when `.cli-proxy-api` creds fail refresh
  - Integration/E2E tests (per user decision: unit tests only)
  - Bi-directional sync between native and proxy credential locations

## Assumptions & Constraints
- Native CLI credential file structures are as documented in the spec (confirmed Jan 2026)
- All three native CLI directories use single-credential files
- `~/.claude/.credentials.json` uses nested `claudeAiOauth` structure with `expiresAt` in milliseconds
- `~/.codex/auth.json` uses `tokens` structure with JWT access token (decode for `exp` claim)
- `~/.gemini/oauth_creds.json` uses flat structure with `expiry_date` in milliseconds
- Users may have credentials in both locations; proxy credentials always take precedence

### Implementation Constraints
- **Read-Only**: Must not modify files in native CLI directories
- **Structure**: Modify existing `internal/providers/{provider}.go` files; do not create new packages
- **Reuse**: Use existing `decodeJWTClaims()` function in `codex.go` for Codex JWT expiry extraction
- **Logging**: Use existing `debugf("Provider", format, args...)` pattern for native credential discovery
- **Error Handling**: Silent skip for missing/unreadable files; warning row for parseable but invalid files

### Testing Constraints
- **Unit Tests Only**: Verify parsing logic and fallback precedence using unit tests in `*_test.go`
- **Coverage**: Test parsing functions for each native schema, precedence behavior, and refresh skip logic
- **Mocking**: Tests should focus on parsing functions; use temporary files if needed (matching existing test patterns)

## Prerequisites
- [ ] Existing `decodeJWTClaims()` function in `internal/providers/codex.go` available for reuse
- [ ] Existing `debugf()` function in `internal/providers/debug.go` available for logging

## High-Level Approach

The implementation follows a consistent pattern across all three providers:

1. **Add `IsNative` field** to each account struct to track credential source
2. **Implement native loader function** for each provider with provider-specific JSON parsing
3. **Modify `loadCredentials()`** to call native fallback when `.cli-proxy-api` glob returns empty
4. **Guard refresh logic** to skip refresh and return re-auth error when `IsNative=true`
5. **Add unit tests** for native parsing, precedence behavior, and refresh skip logic

Each provider has unique native file structure:
- **Claude**: Nested `claudeAiOauth` object with `expiresAt` in milliseconds epoch
- **Codex**: `tokens` object with JWT-encoded `access_token` (decode payload for `exp` claim)
- **Gemini**: Flat structure with `expiry_date` in milliseconds epoch; empty `project_id` for API calls

## Technical Design

### Architecture

```
loadCredentials()
    |
    +-- Glob ~/.cli-proxy-api/{provider}-*.json
    |
    +-- If matches found: parse proxy creds (existing behavior)
    |
    +-- If NO matches: call loadNativeCredentials()
            |
            +-- Check if native file exists
            +-- Parse native JSON structure (provider-specific)
            +-- Set IsNative=true
            +-- Return account with "(native)" display name
```

### Data Model Changes

**claudeAuth** (modify `internal/providers/claude.go`):
```go
type claudeAuth struct {
    // ... existing fields ...
    IsNative bool  // New: true when loaded from ~/.claude/
}
```

**CodexAccount** (modify `internal/providers/codex.go`):
```go
type CodexAccount struct {
    // ... existing fields ...
    IsNative bool  // New: true when loaded from ~/.codex/
}
```

**GeminiAccount** (modify `internal/providers/gemini.go`):
```go
type GeminiAccount struct {
    // ... existing fields ...
    IsNative bool  // New: true when loaded from ~/.gemini/
}
```

### Native Credential Schemas

| Provider | Path | Required Field | Expiry Field |
|----------|------|----------------|--------------|
| Claude | `~/.claude/.credentials.json` | `claudeAiOauth.accessToken` | `claudeAiOauth.expiresAt` (ms epoch) |
| Codex | `~/.codex/auth.json` | `tokens.access_token` | JWT `exp` claim (seconds epoch) |
| Gemini | `~/.gemini/oauth_creds.json` | `access_token` | `expiry_date` (ms epoch) |

### File Impact Summary

| Path | Status | Change |
|------|--------|--------|
| `internal/providers/claude.go` | Exists | Add `IsNative` field, `loadNativeCredentials()`, modify refresh logic |
| `internal/providers/codex.go` | Exists | Add `IsNative` field, `loadNativeCredentials()`, modify refresh logic |
| `internal/providers/gemini.go` | Exists | Add `IsNative` field, `loadNativeCredentials()`, modify refresh logic |
| `internal/providers/claude_test.go` | Exists | Add tests for native credential parsing and refresh skip |
| `internal/providers/codex_test.go` | Exists | Add tests for native credential parsing, JWT decode, and refresh skip |
| `internal/providers/gemini_test.go` | Exists | Add tests for native credential parsing and refresh skip |

## Detailed Plan

### Task 1: Claude Native Fallback
- **Goal**: Enable fallback to `~/.claude/.credentials.json` when no proxy credentials found
- **Covers**: AC #1 (Claude fallback), AC #4 (Precedence), AC #5 (IsNative field), AC #6 (No refresh), AC #7 (Display name)
- **Depends on**: Prerequisites
- **Changes**:
  - `internal/providers/claude.go`:
    - Add `IsNative bool` field to `claudeAuth` struct
    - Implement `loadNativeCredentials() ([]claudeAuth, error)`:
      - Read `~/.claude/.credentials.json`
      - Parse nested `claudeAiOauth` structure
      - Extract `accessToken` (required), `expiresAt` (optional, milliseconds epoch)
      - Return struct with `IsNative: true`, `Email: "Claude (native)"`
      - Silent return (nil, nil) if file missing/unreadable
      - Return warning-style account if JSON invalid or `accessToken` missing
    - Update `loadCredentials()`:
      - If `filepath.Glob` returns empty AND no error: call `loadNativeCredentials()`
      - Add debug log: `debugf("Claude", "no proxy credentials found, checking native CLI")`
    - Update `refreshAccessToken()`:
      - Check `IsNative` at start; if true, return error: "Token expired. Re-authenticate with claude to refresh."
- **Verification**:
  - Add unit tests in `internal/providers/claude_test.go`:
    - Test `loadNativeCredentials` with valid JSON content
    - Test `loadNativeCredentials` with missing `accessToken` returns warning
    - Test `loadNativeCredentials` with missing file returns empty slice
    - Test precedence: proxy credentials used when both exist
    - Test refresh returns re-auth error when `IsNative=true`
- **Rollback**: Revert changes to `claude.go` and `claude_test.go`

### Task 2: Codex Native Fallback
- **Goal**: Enable fallback to `~/.codex/auth.json` when no proxy credentials found
- **Covers**: AC #2 (Codex fallback), AC #4 (Precedence), AC #5 (IsNative field), AC #6 (No refresh), AC #9 (JWT parsing)
- **Depends on**: Prerequisites
- **Changes**:
  - `internal/providers/codex.go`:
    - Add `IsNative bool` field to `CodexAccount` struct
    - Implement `loadNativeCredentials() ([]CodexAccount, error)`:
      - Read `~/.codex/auth.json`
      - Parse `tokens` structure: `access_token` (required), `account_id`, `refresh_token`
      - Use existing `decodeJWTClaims()` on `access_token` to extract `exp` claim for `ExpiresAt`
      - If JWT decode fails or `exp` missing: set `ExpiresAt` to zero value (token will be attempted)
      - Parse `last_refresh` field if present for display
      - Return struct with `IsNative: true`, `DisplayName: "Codex (native)"`
      - Silent return (nil, nil) if file missing/unreadable
      - Return warning-style account if JSON invalid or `access_token` missing
    - Update `loadCredentials()`:
      - If `filepath.Glob` returns empty: call `loadNativeCredentials()`
      - Add debug log: `debugf("Codex", "no proxy credentials found, checking native CLI")`
    - Update `refreshAccessToken()`:
      - Check `IsNative` at start; if true, return error: "Token expired. Re-authenticate with codex to refresh."
- **Verification**:
  - Add unit tests in `internal/providers/codex_test.go`:
    - Test `loadNativeCredentials` with valid JSON content
    - Test JWT `exp` claim extraction populates `ExpiresAt`
    - Test malformed JWT sets zero `ExpiresAt` (no crash)
    - Test missing `access_token` returns warning
    - Test precedence: proxy credentials used when both exist
    - Test refresh returns re-auth error when `IsNative=true`
- **Rollback**: Revert changes to `codex.go` and `codex_test.go`

### Task 3: Gemini Native Fallback
- **Goal**: Enable fallback to `~/.gemini/oauth_creds.json` when no proxy credentials found
- **Covers**: AC #3 (Gemini fallback), AC #4 (Precedence), AC #5 (IsNative field), AC #6 (No refresh), AC #8 (Missing expiry)
- **Depends on**: Prerequisites
- **Changes**:
  - `internal/providers/gemini.go`:
    - Add `IsNative bool` field to `GeminiAccount` struct
    - Implement `loadNativeCredentials() ([]GeminiAccount, error)`:
      - Read `~/.gemini/oauth_creds.json`
      - Parse flat structure: `access_token` (required), `expiry_date` (optional, milliseconds epoch)
      - Set `ProjectID: ""` (empty string for API calls)
      - Set `Email: "Gemini (native)"`, `IsNative: true`
      - Set `RefreshToken: ""` (do not use even if present)
      - Set `ClientID: ""`, `ClientSecret: ""` (not needed)
      - Set `TokenURI: geminiTokenURI` (default)
      - Silent return (nil, nil) if file missing/unreadable
      - Return warning-style account if JSON invalid or `access_token` missing
    - Update `loadCredentials()`:
      - If `filepath.Glob` returns empty: call `loadNativeCredentials()` before returning
      - Add debug log: `debugf("Gemini", "no proxy credentials found, checking native CLI")`
    - Update `refreshAccessToken()`:
      - Check `IsNative` at start; if true, return error: "Token expired. Re-authenticate with gemini to refresh."
    - Update `doQuotaRequest()` to handle empty `ProjectID` (already sends `{"project":""}`)
- **Verification**:
  - Add unit tests in `internal/providers/gemini_test.go`:
    - Test `loadNativeCredentials` with valid JSON content
    - Test `expiry_date` (int64 milliseconds) maps to `time.Time`
    - Test missing `expiry_date` sets zero expiry
    - Test missing `access_token` returns warning
    - Test precedence: proxy credentials used when both exist
    - Test refresh returns re-auth error when `IsNative=true`
- **Rollback**: Revert changes to `gemini.go` and `gemini_test.go`

### Task 4: Update Warning Messages for Native Credentials
- **Goal**: Ensure 401/403 errors for native credentials show the correct re-auth message
- **Covers**: AC #7 (Re-auth message on 401/403)
- **Depends on**: Tasks 1, 2, 3
- **Changes**:
  - `internal/providers/claude.go`:
    - In `fetchAccountUsage()`, when 401/403 received AND `IsNative=true`: return specific error "Token expired. Re-authenticate with claude to refresh."
    - Skip the existing refresh attempt for native accounts
  - `internal/providers/codex.go`:
    - In `fetchAccountUsage()`, when 401 received AND `IsNative=true`: return specific error "Token expired. Re-authenticate with codex to refresh."
    - Skip the existing refresh attempt for native accounts
  - `internal/providers/gemini.go`:
    - In `fetchAccountUsage()`, when 401 received AND `IsNative=true`: return specific error "Token expired. Re-authenticate with gemini to refresh."
    - Skip the existing refresh attempt for native accounts
- **Verification**:
  - Manual test: Use expired native credentials and verify the custom error message appears
  - Unit test: Mock 401 response for native account and verify correct error message
- **Rollback**: Revert warning message changes in all three provider files

## Risks, Edge Cases & Breaking Changes

### Edge Cases & Failure Modes

| Edge Case | Handling | Test Coverage |
|-----------|----------|---------------|
| Native file missing | Silent skip, return empty slice | Unit test |
| Native file unreadable (permissions) | Silent skip, return empty slice | Unit test |
| Native file invalid JSON | Warning row (matches proxy behavior) | Unit test |
| Native file missing required field | Warning row | Unit test |
| Codex JWT malformed | Set zero expiry, attempt API call | Unit test |
| Codex JWT missing `exp` claim | Set zero expiry, attempt API call | Unit test |
| Missing expiry fields | Treat as valid, attempt API call, 401 triggers re-auth | Unit test |
| Gemini API rejects empty project_id | Show warning row with error, continue | Manual test |
| Both proxy and native creds exist | Proxy takes precedence | Unit test |
| Native token returns 401/403 | Show re-auth message, no refresh attempt | Unit test |

### Breaking Changes & Compatibility
- **Potential Breaking Changes**: None. This is purely additive behavior.
- **Backwards Compatibility**:
  - Existing `.cli-proxy-api` credential loading unchanged
  - Existing refresh behavior unchanged for proxy credentials
  - Native fallback only triggers when proxy glob returns empty
- **Rollout Strategy**: Direct inclusion in next release (no feature flags needed)

## Testing & Validation

### Unit Tests (Required)

**Claude (`claude_test.go`)**:
- `TestLoadNativeCredentials_ValidFile` - parse valid `~/.claude/.credentials.json`
- `TestLoadNativeCredentials_MissingFile` - silent skip
- `TestLoadNativeCredentials_InvalidJSON` - warning account
- `TestLoadNativeCredentials_MissingAccessToken` - warning account
- `TestLoadCredentials_Precedence` - proxy over native
- `TestRefreshAccessToken_NativeSkip` - returns re-auth error

**Codex (`codex_test.go`)**:
- `TestLoadNativeCredentials_ValidFile` - parse valid `~/.codex/auth.json`
- `TestLoadNativeCredentials_JWTExpiry` - extract `exp` claim
- `TestLoadNativeCredentials_MalformedJWT` - zero expiry, no crash
- `TestLoadNativeCredentials_MissingFile` - silent skip
- `TestLoadCredentials_Precedence` - proxy over native
- `TestRefreshAccessToken_NativeSkip` - returns re-auth error

**Gemini (`gemini_test.go`)**:
- `TestLoadNativeCredentials_ValidFile` - parse valid `~/.gemini/oauth_creds.json`
- `TestLoadNativeCredentials_ExpiryDate` - milliseconds to time.Time
- `TestLoadNativeCredentials_MissingExpiry` - zero expiry
- `TestLoadNativeCredentials_MissingFile` - silent skip
- `TestLoadCredentials_Precedence` - proxy over native
- `TestRefreshAccessToken_NativeSkip` - returns re-auth error

### Manual Validation
- Remove `~/.cli-proxy-api` credentials and verify native credentials are discovered
- Test with both locations populated; verify `.cli-proxy-api` takes precedence
- Test expired native credentials to verify re-auth message (no file modification)
- Verify Gemini native credentials work with empty project_id (or show appropriate error)
- Test with missing expiry fields to confirm tokens are still attempted

### Acceptance Criteria Coverage

| Spec AC | Covered By |
|---------|------------|
| AC #1: Load from `~/.claude/.credentials.json` when no proxy creds | Task 1 (`loadNativeCredentials()`) |
| AC #2: Load from `~/.codex/auth.json` when no proxy creds | Task 2 (`loadNativeCredentials()`) |
| AC #3: Load from `~/.gemini/oauth_creds.json` when no proxy creds | Task 3 (`loadNativeCredentials()`) |
| AC #4: Prefer `.cli-proxy-api` when both exist | Tasks 1-3 (fallback only on empty glob) |
| AC #5: `IsNative` field on all account structs | Tasks 1-3 (struct changes) |
| AC #6: Skip refresh for `IsNative=true` | Tasks 1-3 (refresh guard) |
| AC #7: Show re-auth message on 401/403 for native | Task 4 (error handling) |
| AC #8: Missing expiry = treat as valid | Tasks 1-3 (zero expiry handling) |
| AC #9: Codex JWT uses base64url (RFC 4648) | Task 2 (reuse `decodeJWTClaims()`) |
| Display names use "(native)" suffix | Tasks 1-3 (Email/DisplayName) |

## Rollback Strategy (Plan-Level)

- **No database migrations or persistent state changes**
- **Rollback procedure**: Revert code changes to `internal/providers/*.go` and `internal/providers/*_test.go`
- **Verification**: After rollback, AIM returns to proxy-only credential loading behavior
- **Data cleanup**: None required (read-only feature)
- **Feature flags**: Not needed; change is additive and low-risk

## Open Questions

None. The spec and user decisions provide sufficient clarity for implementation:
- Debug logging: Yes, using existing `debugf()` pattern (confirmed)
- Tests: Unit tests only (confirmed)
- JWT helper: Reuse existing `decodeJWTClaims()` (confirmed)
- Codex expiry: Decode JWT `exp` claim; if decode fails, set zero expiry and let 401 trigger re-auth (spec section 3)
