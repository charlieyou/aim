package providers

import (
	"context"
	"time"
)

// UsageRow represents a single row in the output table
type UsageRow struct {
	Provider     string    // e.g., "Claude", "Codex (user@example.com)", "Gemini (user@example.com)"
	Label        string    // e.g., "5-hour", "7-day", "gemini-2.5-pro"
	UsagePercent float64   // 0-100
	ResetTime    time.Time // When quota resets
	IsWarning    bool      // If true, this is a warning row
	WarningMsg   string    // Warning message (only if IsWarning)
}

// Provider defines the interface all quota providers must implement
type Provider interface {
	Name() string
	FetchUsage(ctx context.Context) ([]UsageRow, error)
}

// ProviderError wraps errors with provider context
type ProviderError struct {
	Provider string
	Message  string
	Err      error
}

func (e ProviderError) Error() string {
	switch {
	case e.Message != "" && e.Err != nil:
		return e.Provider + ": " + e.Message + ": " + e.Err.Error()
	case e.Message != "":
		return e.Provider + ": " + e.Message
	case e.Err != nil:
		return e.Provider + ": " + e.Err.Error()
	default:
		return e.Provider + ": unknown error"
	}
}

// Unwrap returns the underlying error, enabling errors.Is and errors.As
func (e ProviderError) Unwrap() error {
	return e.Err
}

// TruncateBody truncates a byte slice to maxLen, appending "..." if truncated.
// This prevents large API error responses from breaking table rendering.
// The truncation happens on the byte slice before string conversion to avoid
// unnecessary allocations for large response bodies.
func TruncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
