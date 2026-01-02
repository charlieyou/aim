package output

import (
	"testing"
	"time"
)

// fixedNow is a fixed point in time for deterministic tests
var fixedNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

func TestFormatResetTime_Relative(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "1 hour from now",
			duration: 1 * time.Hour,
			want:     "in 1h 0m",
		},
		{
			name:     "2 hours 15 minutes from now",
			duration: 2*time.Hour + 15*time.Minute,
			want:     "in 2h 15m",
		},
		{
			name:     "12 hours from now",
			duration: 12 * time.Hour,
			want:     "in 12h 0m",
		},
		{
			name:     "23 hours 59 minutes from now",
			duration: 23*time.Hour + 59*time.Minute,
			want:     "in 23h 59m",
		},
		{
			name:     "30 minutes from now",
			duration: 30 * time.Minute,
			want:     "in 30m",
		},
		{
			name:     "5 minutes from now",
			duration: 5 * time.Minute,
			want:     "in 5m",
		},
		{
			name:     "30 seconds from now",
			duration: 30 * time.Second,
			want:     "in <1m",
		},
		{
			name:     "1 second from now",
			duration: 1 * time.Second,
			want:     "in <1m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTime := fixedNow.Add(tt.duration)
			got := formatResetTimeFrom(resetTime, fixedNow)
			if got != tt.want {
				t.Errorf("formatResetTimeFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatResetTime_Absolute(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
	}{
		{
			name:     "25 hours from now",
			duration: 25 * time.Hour,
		},
		{
			name:     "48 hours from now",
			duration: 48 * time.Hour,
		},
		{
			name:     "7 days from now",
			duration: 7 * 24 * time.Hour,
		},
		{
			name:     "exactly 24 hours (boundary)",
			duration: 24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTime := fixedNow.Add(tt.duration)
			got := formatResetTimeFrom(resetTime, fixedNow)

			// For absolute times, verify format matches "Mon D HH:MM TZ"
			expected := resetTime.Local().Format("Jan 2 15:04 MST")
			if got != expected {
				t.Errorf("formatResetTimeFrom() = %q, want %q", got, expected)
			}
		})
	}
}

func TestFormatResetTime_Zero(t *testing.T) {
	got := formatResetTimeFrom(time.Time{}, fixedNow)
	want := "-"
	if got != want {
		t.Errorf("formatResetTimeFrom(zero) = %q, want %q", got, want)
	}
}

func TestFormatResetTime_Past(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "1 hour ago",
			duration: -1 * time.Hour,
			want:     "expired",
		},
		{
			name:     "1 second ago",
			duration: -1 * time.Second,
			want:     "expired",
		},
		{
			name:     "1 day ago",
			duration: -24 * time.Hour,
			want:     "expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTime := fixedNow.Add(tt.duration)
			got := formatResetTimeFrom(resetTime, fixedNow)
			if got != tt.want {
				t.Errorf("formatResetTimeFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatResetTime_PublicAPI(t *testing.T) {
	// Test the public API to ensure it works with real time.Now()
	// These tests are more lenient since time.Now() can drift slightly

	t.Run("zero time returns dash", func(t *testing.T) {
		got := FormatResetTime(time.Time{})
		if got != "-" {
			t.Errorf("FormatResetTime(zero) = %q, want %q", got, "-")
		}
	})

	t.Run("past time returns expired", func(t *testing.T) {
		got := FormatResetTime(time.Now().Add(-1 * time.Hour))
		if got != "expired" {
			t.Errorf("FormatResetTime(past) = %q, want %q", got, "expired")
		}
	})

	t.Run("future time returns non-empty string", func(t *testing.T) {
		got := FormatResetTime(time.Now().Add(2 * time.Hour))
		if got == "" || got == "-" || got == "expired" {
			t.Errorf("FormatResetTime(future) = %q, expected a relative time", got)
		}
	})
}
