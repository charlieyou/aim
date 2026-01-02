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

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return e.Provider + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Provider + ": " + e.Message
}
