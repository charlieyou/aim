package output

import (
	"fmt"
	"time"
)

// FormatResetTime formats a reset time for display.
// - If <24h from now: "in Xh Ym" (relative)
// - If >=24h from now: "Mon D HH:MM TZ" (absolute in local time)
// - If zero time: "-"
// - If in the past: "expired"
func FormatResetTime(t time.Time) string {
	return formatResetTimeFrom(t, time.Now())
}

// formatResetTimeFrom is the internal implementation that accepts "now" for testability.
func formatResetTimeFrom(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}

	diff := t.Sub(now)

	if diff <= 0 {
		return "expired"
	}

	if diff < 24*time.Hour {
		hours := int(diff.Hours())
		minutes := int(diff.Minutes()) % 60
		if hours > 0 {
			return fmt.Sprintf("in %dh %dm", hours, minutes)
		}
		return fmt.Sprintf("in %dm", minutes)
	}

	// Use local timezone
	local := t.Local()
	return local.Format("Jan 2 15:04 MST")
}
