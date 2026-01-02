# AI Provider OAuth Quota APIs

Documentation for fetching usage/quota limits from OAuth-authenticated AI providers.

---

## Claude (Anthropic)

### Endpoint
```
GET https://api.anthropic.com/api/oauth/usage
```

### Headers
```
Authorization: Bearer {accessToken}
Accept: application/json
anthropic-beta: oauth-2025-04-20
```

### Response Format
```json
{
  "five_hour": {
    "utilization": 24.0,
    "resets_at": "2026-01-02T19:59:59.600956+00:00"
  },
  "seven_day": {
    "utilization": 36.0,
    "resets_at": "2026-01-08T06:59:59.600974+00:00"
  },
  "seven_day_sonnet": {
    "utilization": 1.0,
    "resets_at": "2026-01-08T14:59:59.600981+00:00"
  },
  "seven_day_opus": null,
  "seven_day_oauth_apps": null,
  "iguana_necktie": null,
  "extra_usage": {
    "is_enabled": false,
    "monthly_limit": null,
    "used_credits": null,
    "utilization": null
  }
}
```

### Fields
| Field | Description |
|-------|-------------|
| `five_hour.utilization` | Percentage used in 5-hour window (0-100) |
| `five_hour.resets_at` | ISO 8601 timestamp when window resets |
| `seven_day.utilization` | Percentage used in 7-day window (0-100) |
| `seven_day_sonnet.utilization` | Model-specific quota for Sonnet |
| `seven_day_opus.utilization` | Model-specific quota for Opus |
| `extra_usage.is_enabled` | Whether extra usage billing is enabled |

### Credential Location
```
~/.claude/.credentials.json
```
```json
{
  "claudeAiOauth": {
    "accessToken": "...",
    "refreshToken": "...",
    "expiresAt": 1767396165210,
    "subscriptionType": "max",
    "rateLimitTier": "default_claude_max_20x"
  }
}
```

---

## Codex (OpenAI)

### Endpoint
```
GET https://chatgpt.com/backend-api/wham/usage
```

### Headers
```
Authorization: Bearer {accessToken}
Accept: application/json
```

### Response Format
```json
{
  "plan_type": "pro",
  "rate_limit": {
    "allowed": true,
    "limit_reached": false,
    "primary_window": {
      "used_percent": 3,
      "limit_window_seconds": 18000,
      "reset_after_seconds": 15858,
      "reset_at": 1767385852
    },
    "secondary_window": {
      "used_percent": 9,
      "limit_window_seconds": 604800,
      "reset_after_seconds": 489450,
      "reset_at": 1767859444
    }
  },
  "code_review_rate_limit": {
    "allowed": true,
    "limit_reached": false,
    "primary_window": {
      "used_percent": 0,
      "limit_window_seconds": 604800,
      "reset_after_seconds": 604800,
      "reset_at": 1767974794
    },
    "secondary_window": null
  },
  "credits": {
    "has_credits": false,
    "unlimited": false,
    "balance": "0",
    "approx_local_messages": [0, 0],
    "approx_cloud_messages": [0, 0]
  }
}
```

### Fields
| Field | Description |
|-------|-------------|
| `plan_type` | Subscription tier (`"pro"`, `"plus"`, etc.) |
| `rate_limit.limit_reached` | Whether quota is exhausted |
| `rate_limit.primary_window.used_percent` | Percentage used in 5-hour window |
| `rate_limit.primary_window.limit_window_seconds` | Window duration (18000 = 5 hours) |
| `rate_limit.primary_window.reset_at` | Unix timestamp when window resets |
| `rate_limit.secondary_window.used_percent` | Percentage used in 7-day window |
| `rate_limit.secondary_window.limit_window_seconds` | Window duration (604800 = 7 days) |
| `credits.balance` | Remaining credits (string) |

### Credential Location
```
~/.cli-proxy-api/codex-{email}.json
```
```json
{
  "id_token": "...",
  "access_token": "...",
  "refresh_token": "rt_GV..."
}
```

### Token Refresh
```
POST https://token.oaifree.com/api/auth/refresh
Content-Type: application/x-www-form-urlencoded

refresh_token={refreshToken}
```

---

## Gemini (Google)

Gemini does **not** have a proactive quota endpoint. Quota information is only available from 429 error responses.

### Generation Endpoint
```
POST https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse
POST https://cloudcode-pa.googleapis.com/v1internal:generateContent
```

### Headers
```
Authorization: Bearer {accessToken}
Content-Type: application/json
User-Agent: google-api-nodejs-client/9.15.1
X-Goog-Api-Client: gl-node/22.17.0
Client-Metadata: ideType=IDE_UNSPECIFIED,platform=PLATFORM_UNSPECIFIED,pluginType=GEMINI
```

### 429 Error Response Format
```json
{
  "error": {
    "code": 429,
    "message": "You have exhausted your capacity on this model. Your quota will reset after 0s.",
    "details": [
      {
        "@type": "type.googleapis.com/google.rpc.ErrorInfo",
        "reason": "RATE_LIMIT_EXCEEDED",
        "domain": "cloudcode-pa.googleapis.com",
        "metadata": {
          "uiMessage": "true",
          "model": "gemini-2.5-pro",
          "quotaResetDelay": "539.477544ms",
          "quotaResetTimeStamp": "2025-10-20T19:14:08Z"
        }
      },
      {
        "@type": "type.googleapis.com/google.rpc.RetryInfo",
        "retryDelay": "0.539477544s"
      }
    ]
  }
}
```

### Fields
| Field | Description |
|-------|-------------|
| `error.details[].retryDelay` | Time to wait before retry (e.g., `"0.847655010s"`) |
| `error.details[].metadata.quotaResetDelay` | Alternative format (e.g., `"373.801628ms"`) |
| `error.details[].metadata.quotaResetTimeStamp` | ISO timestamp when quota resets |
| `error.details[].reason` | `"RATE_LIMIT_EXCEEDED"` (retryable) or `"QUOTA_EXHAUSTED"` (terminal) |
| `error.message` | May contain: `"Your quota will reset after 600s."` |

### Subscription Info Endpoint
```
POST https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist
```
```json
{
  "metadata": { "ideType": "IDE_UNSPECIFIED" }
}
```
Response:
```json
{
  "allowedTiers": [
    {
      "id": "standard-tier",
      "name": "Gemini Code Assist",
      "description": "Unlimited coding assistant with the most powerful Gemini models",
      "isDefault": true
    }
  ]
}
```

### Credential Location
```
~/.gemini/oauth_creds.json
```
```json
{
  "access_token": "ya29...",
  "refresh_token": "1//05...",
  "expiry_date": 1767370763595
}
```

Or CLI Proxy API format:
```
~/.cli-proxy-api/{email}-{project_id}.json
```
```json
{
  "token": {
    "access_token": "ya29...",
    "client_id": "681255809395-....apps.googleusercontent.com",
    "client_secret": "GOCSPX-...",
    "refresh_token": "1//05..."
  },
  "project_id": "gen-lang-client-0353902167"
}
```

### Token Refresh
```
POST https://oauth2.googleapis.com/token
Content-Type: application/x-www-form-urlencoded

client_id={clientId}&client_secret={clientSecret}&refresh_token={refreshToken}&grant_type=refresh_token
```

---

## Summary Table

| Provider | Quota Endpoint | Method | Auth Header |
|----------|---------------|--------|-------------|
| Claude | `api.anthropic.com/api/oauth/usage` | GET | `Bearer {token}` + `anthropic-beta: oauth-2025-04-20` |
| Codex | `chatgpt.com/backend-api/wham/usage` | GET | `Bearer {token}` |
| Gemini | N/A (from 429 errors only) | - | `Bearer {token}` |

| Provider | Windows | Reset Info Format |
|----------|---------|-------------------|
| Claude | 5-hour, 7-day, per-model | ISO 8601 timestamp |
| Codex | 5-hour (18000s), 7-day (604800s) | Unix timestamp |
| Gemini | Variable | Seconds string or ISO timestamp |
