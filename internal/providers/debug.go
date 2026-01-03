package providers

import (
	"log"
	"regexp"
	"sync/atomic"
)

var (
	debugEnabled atomic.Bool
	tokenRedact  = regexp.MustCompile(`(?i)("(?:access_token|refresh_token|id_token)"\s*:\s*")[^"]*(")`)
)

// SetDebug toggles debug output for provider internals.
func SetDebug(enabled bool) {
	debugEnabled.Store(enabled)
}

func debugf(provider string, format string, args ...any) {
	if !debugEnabled.Load() {
		return
	}
	log.Printf(provider+": "+format, args...)
}

func debugBody(body []byte) string {
	return redactTokens(TruncateBody(body, 200))
}

func redactTokens(value string) string {
	if value == "" {
		return value
	}
	return tokenRedact.ReplaceAllString(value, `$1<redacted>$2`)
}
