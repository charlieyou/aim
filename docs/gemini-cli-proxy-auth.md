# Gemini CLI Proxy Authentication

Research on how Gemini CLI proxies handle OAuth authentication and GCP project requirements.

## Proxies Compared

| Feature | [gemini-cli-proxy](https://github.com/ubaltaci/gemini-cli-proxy) | [Cli-Proxy-API](https://github.com/router-for-me/Cli-Proxy-API) |
|---------|------------------|---------------|
| Project required upfront | ❌ No | ✅ Yes (prompts during `--login`) |
| Auto-provisions project | ✅ Yes (free tier) | ❌ No |
| Credential source | `~/.gemini/oauth_creds.json` | Own login flow + project selection |

## Common Endpoint

Both proxies use the **same backend API**:

```
https://cloudcode-pa.googleapis.com/v1internal
```

Endpoints:
- `:generateContent` - non-streaming
- `:streamGenerateContent?alt=sse` - streaming
- `:countTokens` - token counting

## Authentication Flow

### OAuth Credentials

Both use personal Google OAuth with these scopes:
- `https://www.googleapis.com/auth/cloud-platform`
- `https://www.googleapis.com/auth/userinfo.email`
- `https://www.googleapis.com/auth/userinfo.profile`

OAuth Client ID (public, same as Gemini CLI):
```
681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com
```

Credentials cached at: `~/.gemini/oauth_creds.json`

### Project Auto-Provisioning

gemini-cli-proxy auto-provisions via:

```typescript
// 1. Try to load existing project
await callEndpoint("loadCodeAssist", {
    cloudaicompanionProject: "default-project",
});

// 2. If none exists, provision free tier
await callEndpoint("onboardUser", {
    tierId: "free-tier",
    cloudaicompanionProject: "default-project",
});
```

## Key Finding: Project Selection Doesn't Matter

The `cloudcode-pa.googleapis.com` API can auto-provision a "default-project" for personal OAuth users. This means:

1. **Cli-Proxy-API's project selection during `--login` is unnecessary friction**
2. You can pick any project - the API works regardless
3. The project gets stored but isn't strictly required for the API to function

## Recommendations

1. **Use gemini-cli-proxy** for simplest setup - no project prompt, just OAuth
2. **Or with Cli-Proxy-API**: just pick any available project during `--login`
3. **Or modify Cli-Proxy-API** to skip project selection and pass "default-project"

## Other Endpoints (for reference)

Cli-Proxy-API also supports:

| Provider | Endpoint | Use Case |
|----------|----------|----------|
| Official Gemini API | `generativelanguage.googleapis.com/v1beta` | Direct API key access |
| Vertex AI | `{location}-aiplatform.googleapis.com/v1` | Enterprise/service account |

## Settings for Gemini CLI

To use personal OAuth for telemetry (instead of project-scoped ADC):

```json
// ~/.gemini/settings.json
{
  "telemetry": {
    "enabled": true,
    "target": "gcp",
    "useCliAuth": true
  }
}
```

Or:
```bash
export GEMINI_TELEMETRY_USE_CLI_AUTH=true
```
