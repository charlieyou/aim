# aim - AI Usage Meter

A CLI tool that displays usage quotas for multiple AI providers in a unified ASCII table.

## Features

- **Multi-provider support**: Claude (Anthropic), Codex (OpenAI), and Gemini (Google)
- **Multiple accounts**: Automatically discovers all Codex and Gemini accounts
- **Unified view**: See all quotas in one table with usage bars and reset times
- **Graceful degradation**: Missing credentials or API failures show warnings without blocking other providers

## Installation

```bash
go install github.com/cyou/aim@latest
```

Or build from source (static binary with no runtime dependencies):

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o aim .
```

## Usage

```bash
aim
```

Example output:

```
┌───────────────────────────────┬─────────┬─────────────┬─────────────────────┐
│ Provider                      │ Window  │ Usage       │ Resets At           │
├───────────────────────────────┼─────────┼─────────────┼─────────────────────┤
│ Claude                        │ 5-hour  │ ████░░ 24%  │ in 2h 15m           │
│ Claude                        │ 7-day   │ ██████░ 36% │ Jan 8 07:00 PST     │
│ Codex (user@ex.com)           │ 5-hour  │ ░░░░░░ 3%   │ in 4h 24m           │
│ Codex (user@ex.com)           │ 7-day   │ █░░░░░ 9%   │ Jan 8 12:30 PST     │
│ Gemini (user@ex.com/my-proj)  │ 5-hour  │ ██░░░░ 12%  │ in 3h 45m           │
│ Gemini (user@ex.com/my-proj)  │ 7-day   │ ███░░░ 18%  │ Jan 9 09:00 PST     │
└───────────────────────────────┴─────────┴─────────────┴─────────────────────┘
```

## Credential Locations

| Provider | Path |
|----------|------|
| Claude   | `~/.claude/.credentials.json` |
| Codex    | `~/.cli-proxy-api/codex-{email}.json` |
| Gemini   | `~/.cli-proxy-api/{email}-{project_id}.json` |

The tool reads credentials from these locations automatically. It never modifies credential files.

## Time Display

- **< 24 hours**: Relative format (e.g., `in 2h 15m`)
- **≥ 24 hours**: Absolute timestamp in local time (e.g., `Jan 8 07:00 PST`)

## API Documentation

See [QUOTA_APIS.md](QUOTA_APIS.md) for detailed API reference for each provider.

## License

MIT
