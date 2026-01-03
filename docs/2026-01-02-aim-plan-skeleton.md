# Implementation Plan: aim CLI Tool

## Context & Goals
- **Spec**: [docs/2026-01-02-aim-spec.md](./2026-01-02-aim-spec.md)
- Create a Go CLI tool that queries Claude and Codex quota APIs
- Display usage percentages and reset times in a unified ASCII table
- Read-only operation with graceful error handling

## Scope & Non-Goals
- **In Scope**
  - Single Go binary (`aim`)
  - Claude OAuth quota API integration (5-hour + 7-day windows)
  - Codex quota API integration (multiple accounts supported)
  - ASCII table output with percentage bars
  - Warning rows for missing credentials or API failures
  - Relative time format (<24h) and absolute time format (>=24h)

- **Out of Scope (Non-Goals)**
  - Token refresh (user must refresh externally)
  - JSON/CSV export formats
  - Configuration file support
  - Gemini integration (no proactive quota API)
  - Model-specific quotas (seven_day_sonnet, seven_day_opus)
  - Codex code_review_rate_limit
  - `--watch` flag for live updates
  - Credential path overrides via env vars or flags

## Assumptions & Constraints

### Implementation Constraints
- [TBD: Project structure - flat vs packages]
- [TBD: HTTP client approach - stdlib vs library]
- [TBD: Table rendering approach - manual vs library like tablewriter]

### Testing Constraints
- [TBD: Testing strategy - unit tests, integration tests, manual testing only]
- [TBD: Mocking approach for HTTP calls]

## Prerequisites
- [ ] Go installed (version [TBD: minimum Go version])
- [ ] Access to credential file locations for testing
- [ ] [TBD: Any other prerequisites]

## High-Level Approach
[TBD: Overall implementation strategy - e.g., layered approach with providers, output, and main orchestration]

## Detailed Plan

### Task 1: Project Setup
- **Goal**: Initialize Go module and project structure
- **Covers**: Build acceptance criterion (single static binary)
- **Depends on**: None
- **Changes**:
  - New: `go.mod`
  - New: `main.go`
  - New: [TBD: package structure]
- **Verification**: `go build` produces a single binary
- **Rollback**: Delete project files

### Task 2: Credential Discovery
- **Goal**: Read Claude and Codex credentials from filesystem
- **Covers**: Credential discovery acceptance criteria
- **Depends on**: Task 1
- **Changes**:
  - New: [TBD: credential reader location]
- **Verification**:
  - [TBD: How to verify credential reading works]
- **Rollback**: Remove credential reading code

### Task 3: Claude API Integration
- **Goal**: Query Claude OAuth usage endpoint and parse response
- **Covers**: Claude quota display, time formatting for ISO 8601
- **Depends on**: Task 2
- **Changes**:
  - New: [TBD: Claude provider location]
- **Verification**: [TBD]
- **Rollback**: Remove Claude provider code

### Task 4: Codex API Integration
- **Goal**: Query Codex usage endpoint for all discovered accounts
- **Covers**: Codex quota display, time formatting for Unix timestamps
- **Depends on**: Task 2
- **Changes**:
  - New: [TBD: Codex provider location]
- **Verification**: [TBD]
- **Rollback**: Remove Codex provider code

### Task 5: ASCII Table Output
- **Goal**: Render quota data as ASCII table with percentage bars
- **Covers**: UX example output format, warning row format
- **Depends on**: Tasks 3, 4
- **Changes**:
  - New: [TBD: Table rendering location]
- **Verification**: Output matches spec example format
- **Rollback**: Remove table rendering code

### Task 6: Error Handling & Warning Rows
- **Goal**: Handle all error scenarios gracefully with warning rows
- **Covers**: Error scenarios (missing creds, expired tokens, API failures, malformed files)
- **Depends on**: Tasks 2-5
- **Changes**:
  - [TBD: Error handling locations]
- **Verification**:
  - Tool never exits non-zero for credential/API failures
  - Warning rows display correctly
- **Rollback**: Revert error handling changes

### Task 7: Time Formatting
- **Goal**: Implement relative (<24h) and absolute (>=24h) time display
- **Covers**: Time formatting acceptance criteria
- **Depends on**: Tasks 3, 4
- **Changes**:
  - [TBD: Time formatting location]
- **Verification**:
  - Times <24h show "in Xh Ym" format
  - Times >=24h show "Mon D HH:MM TZ" format
- **Rollback**: Revert time formatting code

### Task 8: Main Orchestration
- **Goal**: Wire everything together in main.go
- **Covers**: Full UX flow
- **Depends on**: Tasks 2-7
- **Changes**:
  - `main.go` updates
- **Verification**: Full end-to-end run produces expected output
- **Rollback**: Revert main.go changes

## Risks, Edge Cases & Breaking Changes

From spec Edge Cases:
- Missing credentials file: Show warning row with path checked
- Expired/invalid token: Show warning row with API error
- API call fails (network, rate limit): Show warning row, continue with other providers
- Malformed credential file: Show warning with parse error, continue with others
- All providers fail: Display table with all warning rows, exit 0

Additional risks:
- [TBD: Any additional risks identified during planning]

## Testing & Validation
- [TBD: Unit test coverage expectations]
- [TBD: Integration test approach]
- [TBD: Manual validation checklist]

### Acceptance Criteria Coverage
| Spec AC | Covered By |
|---------|------------|
| Home directory resolution via `os.UserHomeDir()` | Task 2 |
| Claude credentials from `~/.claude/.credentials.json` | Task 2 |
| Codex credentials via glob `~/.cli-proxy-api/codex-*.json` | Task 2 |
| Claude 5-hour + 7-day rows | Task 3 |
| Codex 5-hour + 7-day per account | Task 4 |
| Warning rows for missing/failed providers | Task 6 |
| Relative time format (<24h) | Task 7 |
| Absolute time format (>=24h) | Task 7 |
| Single static Go binary | Task 1 |
| Never exit non-zero for warnings | Task 6 |
| No inference endpoints called | Tasks 3, 4 |
| Credentials never modified | Task 2 |

## Rollback Strategy (Plan-Level)
- Each task is independently rollback-able by reverting its files
- No external state is modified (read-only tool)
- No database migrations or infrastructure changes

## Open Questions
- [TBD: Any questions from interview phase]
