# Implementation Plan: Direct CLI Credentials Fallback

## Context & Goals
- **Spec**: `docs/2026-01-03-direct-cli-credentials-fallback-spec.md`
- Enable AIM to automatically discover credentials from native CLI directories (`~/.claude/`, `~/.codex/`, `~/.gemini/`) when no matching credentials exist in `~/.cli-proxy-api/`
- Native CLI credentials are read-only (no refresh attempts)
- Preserve existing `.cli-proxy-api` behavior entirely

## Scope & Non-Goals
- **In Scope**
  - Add `IsNative` field to `claudeAuth`, `CodexAccount`, `GeminiAccount` structs
  - Implement native credential parsing for each provider (different JSON structures)
  - Fallback discovery logic when `.cli-proxy-api` has no matches
  - Skip refresh for native credentials; show re-auth message on 401/403
  - Unit tests for new parsing and fallback logic
- **Out of Scope (Non-Goals)**
  - Windows support
  - Multi-account selection for native credentials (each native file is single-account)
  - Writing/modifying native CLI credential files
  - Using native credentials as auth fallback when `.cli-proxy-api` creds fail refresh

## Assumptions & Constraints
- Native CLI credential file structures are as documented in the spec (confirmed Jan 2026)
- All three native CLI directories use single-credential files
- `~/.claude/.credentials.json` uses nested `claudeAiOauth` structure
- `~/.codex/auth.json` uses `tokens` structure with JWT access token
- `~/.gemini/oauth_creds.json` uses flat structure with `expiry_date` in milliseconds

### Implementation Constraints
- Must preserve existing `.cli-proxy-api` credential loading and refresh behavior
- Native credentials are loaded read-only; no file writes to native directories
- [TBD: Should we add debug logging for native credential discovery?]

### Testing Constraints
- [TBD: Coverage requirements for new parsing functions?]
- [TBD: Integration test requirements?]

## Prerequisites
- [ ] None identified; this is self-contained within the existing codebase

## High-Level Approach

The implementation follows a consistent pattern across all three providers:

1. **Add `IsNative` field** to each account struct
2. **Implement `loadNativeCredentials()`** function for each provider with provider-specific parsing
3. **Modify `loadCredentials()`** to call native fallback when `.cli-proxy-api` glob returns empty
4. **Modify refresh logic** to skip refresh and return re-auth error when `IsNative=true`

Each provider has unique native file structure:
- **Claude**: Nested `claudeAiOauth` object with `expiresAt` in milliseconds
- **Codex**: `tokens` object with JWT-encoded `access_token` (decode for expiry)
- **Gemini**: Flat structure with `expiry_date` in milliseconds; empty `project_id` for API calls

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
            +-- Parse native JSON structure
            +-- Return account with IsNative=true
```

### Data Model

**claudeAuth** (modify):
```go
type claudeAuth struct {
    // ... existing fields ...
    IsNative bool  // New: true when loaded from ~/.claude/
}
```

**CodexAccount** (modify):
```go
type CodexAccount struct {
    // ... existing fields ...
    IsNative bool  // New: true when loaded from ~/.codex/
}
```

**GeminiAccount** (modify):
```go
type GeminiAccount struct {
    // ... existing fields ...
    IsNative bool  // New: true when loaded from ~/.gemini/
}
```

### API/Interface Design

No public API changes. Internal changes:

| Provider | New Function | Purpose |
|----------|--------------|---------|
| Claude | `loadNativeCredentials()` | Parse `~/.claude/.credentials.json` |
| Codex | `loadNativeCredentials()` | Parse `~/.codex/auth.json` with JWT decode |
| Gemini | `parseNativeCredFile()` | Parse `~/.gemini/oauth_creds.json` |

### File Impact Summary

| Path | Status | Change |
|------|--------|--------|
| `internal/providers/claude.go` | Exists | Add `IsNative` field, `loadNativeCredentials()`, modify refresh logic |
| `internal/providers/codex.go` | Exists | Add `IsNative` field, `loadNativeCredentials()`, JWT expiry parsing, modify refresh logic |
| `internal/providers/gemini.go` | Exists | Add `IsNative` field, `parseNativeCredFile()`, modify refresh logic |
| `internal/providers/claude_test.go` | Exists | Add tests for native credential parsing |
| `internal/providers/codex_test.go` | Exists | Add tests for native credential parsing and JWT decode |
| `internal/providers/gemini_test.go` | Exists | Add tests for native credential parsing |

## Risks, Edge Cases & Breaking Changes

- **Risk**: Native CLI credential JSON structure may change in future tool versions
  - Mitigation: Silent skip on parse failure; feature degrades gracefully
- **Risk**: Gemini API may reject empty `project_id`
  - Mitigation: Show warning row with error; don't fail the entire provider
- **Edge case**: Native file exists but is unreadable (permissions)
  - Handling: Silent skip (per spec)
- **Edge case**: Native file contains invalid JSON
  - Handling: Warning row (per spec)
- **Edge case**: Missing expiry fields
  - Handling: Treat as valid, attempt API call, 401 triggers re-auth message
- **Edge case**: Codex JWT decode failure
  - Handling: Set zero expiry, attempt API call
- **Backwards compatibility**: None; new functionality only triggers when `.cli-proxy-api` has no credentials

## Testing & Validation Strategy

### Unit Tests (Required)
- [TBD: List specific test cases for each provider]

### Integration Tests
- [TBD: Any integration test requirements?]

### Manual Validation
- Remove `~/.cli-proxy-api` credentials and verify native credentials are discovered
- Test with both locations populated; verify `.cli-proxy-api` takes precedence
- Test expired native credentials to verify re-auth message

### Acceptance Criteria Coverage

| Spec AC | Approach |
|---------|----------|
| Load from `~/.claude/.credentials.json` when no proxy creds | `loadNativeCredentials()` fallback in `loadCredentials()` |
| Load from `~/.codex/auth.json` when no proxy creds | `loadNativeCredentials()` fallback in `loadCredentials()` |
| Load from `~/.gemini/oauth_creds.json` when no proxy creds | `parseNativeCredFile()` fallback in `loadCredentials()` |
| Prefer `.cli-proxy-api` when both exist | Fallback only triggers on empty glob |
| `IsNative` field on all account structs | Add bool field to `claudeAuth`, `CodexAccount`, `GeminiAccount` |
| Skip refresh for `IsNative=true` | Check `IsNative` before refresh attempt |
| Show re-auth message on 401/403 for native | Return specific error message |
| Display names use "(native)" suffix | Set Email/DisplayName appropriately |
| Codex JWT parsing uses base64url | Use `base64.RawURLEncoding` with padding fallback |

## Rollback Strategy

- No database migrations or persistent state changes
- Rollback: revert code changes; behavior returns to proxy-only credential loading
- No feature flags needed for this change

## Open Questions

- [TBD: Should debug logging be added for native credential discovery attempts?]
- [TBD: Test coverage percentage requirements?]
- [TBD: Should we add any new integration tests, or are unit tests sufficient?]

## Next Steps
After this plan is approved, run `/create-tasks` to generate:
- `--beads` -> Beads issues with dependencies for multi-agent execution
- (default) -> TODO.md checklist for simpler tracking
