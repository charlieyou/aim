package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charlieyou/aim/internal/providers"
)

func TestGenerateBar(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		want    string
	}{
		{
			name:    "0%",
			percent: 0,
			want:    "░░░░░░",
		},
		{
			name:    "100%",
			percent: 100,
			want:    "██████",
		},
		{
			name:    "50%",
			percent: 50,
			want:    "███░░░",
		},
		{
			name:    "16.67% rounds to 1 block",
			percent: 16.67,
			want:    "█░░░░░",
		},
		{
			name:    "33.33% rounds to 2 blocks",
			percent: 33.33,
			want:    "██░░░░",
		},
		{
			name:    "24% rounds to 1 block",
			percent: 24,
			want:    "█░░░░░",
		},
		{
			name:    "25% rounds to 2 blocks",
			percent: 25,
			want:    "██░░░░",
		},
		{
			name:    "negative clamps to 0",
			percent: -10,
			want:    "░░░░░░",
		},
		{
			name:    "over 100 clamps to 100",
			percent: 150,
			want:    "██████",
		},
		{
			name:    "83.33% rounds to 5 blocks",
			percent: 83.33,
			want:    "█████░",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateBar(defaultBarWidth, tt.percent)
			if got != tt.want {
				t.Errorf("generateBar(%v) = %q, want %q", tt.percent, got, tt.want)
			}
		})
	}
}

func TestRenderTable_NormalRows(t *testing.T) {
	now := time.Now()
	rows := []providers.UsageRow{
		{
			Provider:     "Claude",
			Label:        "5-hour",
			UsagePercent: 24,
			ResetTime:    now.Add(2*time.Hour + 15*time.Minute),
		},
		{
			Provider:     "Claude",
			Label:        "7-day",
			UsagePercent: 36,
			ResetTime:    now.Add(7 * 24 * time.Hour),
		},
	}

	var buf bytes.Buffer
	RenderTable(rows, &buf, false)
	output := buf.String()

	// Verify headers are present
	if !strings.Contains(output, "Provider") {
		t.Error("Output missing 'Provider' header")
	}
	if !strings.Contains(output, "Window") {
		t.Error("Output missing 'Window' header")
	}
	if !strings.Contains(output, "Usage") {
		t.Error("Output missing 'Usage' header")
	}
	if !strings.Contains(output, "Resets At") {
		t.Error("Output missing 'Resets At' header")
	}

	// Verify content
	if !strings.Contains(output, "Claude") {
		t.Error("Output missing 'Claude' provider")
	}
	if !strings.Contains(output, "5-hour") {
		t.Error("Output missing '5-hour' label")
	}
	if !strings.Contains(output, "7-day") {
		t.Error("Output missing '7-day' label")
	}

	// Verify usage bars are present
	if !strings.Contains(output, "24%") {
		t.Error("Output missing '24%' usage")
	}
	if !strings.Contains(output, "36%") {
		t.Error("Output missing '36%' usage")
	}

	// Verify relative time format for first row (may be 2h 14m or 2h 15m due to timing)
	if !strings.Contains(output, "in 2h 1") {
		t.Error("Output missing relative time starting with 'in 2h 1'")
	}
}

func TestRenderTable_DebugColumn(t *testing.T) {
	now := time.Now()
	rows := []providers.UsageRow{
		{
			Provider:     "Codex (user@example.com)",
			Label:        "5-hour",
			UsagePercent: 12,
			ResetTime:    now.Add(1 * time.Hour),
			DebugInfo:    "acct:abc123 plan:pro token:deadbeef",
		},
	}

	var buf bytes.Buffer
	RenderTable(rows, &buf, true)
	output := buf.String()

	if !strings.Contains(output, "Debug") {
		t.Error("Output missing 'Debug' header")
	}
	if !strings.Contains(output, "acct:abc123") {
		t.Error("Output missing debug info")
	}
}

func TestRenderTable_WarningRow(t *testing.T) {
	rows := []providers.UsageRow{
		{
			Provider:   "Claude",
			IsWarning:  true,
			WarningMsg: "No credential files found matching ~/.cli-proxy-api/claude-*.json",
		},
	}

	var buf bytes.Buffer
	RenderTable(rows, &buf, false)
	output := buf.String()

	// Verify warning indicator
	if !strings.Contains(output, "⚠") {
		t.Error("Output missing warning indicator '⚠'")
	}

	// Verify warning message
	if !strings.Contains(output, "credential files found matching") {
		t.Error("Output missing warning message")
	}

	// Verify provider name is present
	if !strings.Contains(output, "Claude") {
		t.Error("Output missing provider name in warning row")
	}
}

func TestRenderTable_MixedRows(t *testing.T) {
	now := time.Now()
	rows := []providers.UsageRow{
		{
			Provider:     "Claude",
			Label:        "5-hour",
			UsagePercent: 50,
			ResetTime:    now.Add(1 * time.Hour),
		},
		{
			Provider:   "Codex",
			IsWarning:  true,
			WarningMsg: "API token expired",
		},
		{
			Provider:     "Gemini (user@example.com)",
			Label:        "gemini-2.5-pro",
			UsagePercent: 75,
			ResetTime:    now.Add(30 * time.Minute),
		},
	}

	var buf bytes.Buffer
	RenderTable(rows, &buf, false)
	output := buf.String()

	// Verify all providers are present
	if !strings.Contains(output, "Claude") {
		t.Error("Output missing 'Claude' provider")
	}
	if !strings.Contains(output, "Codex") {
		t.Error("Output missing 'Codex' provider")
	}
	if !strings.Contains(output, "Gemini") {
		t.Error("Output missing 'Gemini' provider")
	}

	// Verify warning
	if !strings.Contains(output, "⚠") {
		t.Error("Output missing warning indicator")
	}
	if !strings.Contains(output, "API token expired") {
		t.Error("Output missing warning message")
	}

	// Verify normal row content
	if !strings.Contains(output, "50%") {
		t.Error("Output missing '50%' usage")
	}
	if !strings.Contains(output, "75%") {
		t.Error("Output missing '75%' usage")
	}
}

func TestRenderTable_EmptyRows(t *testing.T) {
	var rows []providers.UsageRow

	var buf bytes.Buffer
	RenderTable(rows, &buf, false)
	output := buf.String()

	// Empty table should still have headers
	if !strings.Contains(output, "Provider") {
		t.Error("Empty table missing 'Provider' header")
	}
	if !strings.Contains(output, "Window") {
		t.Error("Empty table missing 'Window' header")
	}
	if !strings.Contains(output, "Usage") {
		t.Error("Empty table missing 'Usage' header")
	}
	if !strings.Contains(output, "Resets At") {
		t.Error("Empty table missing 'Resets At' header")
	}
}

func TestRenderTable_UsageBarFormatting(t *testing.T) {
	now := time.Now()
	rows := []providers.UsageRow{
		{
			Provider:     "Test",
			Label:        "test",
			UsagePercent: 0,
			ResetTime:    now.Add(1 * time.Hour),
		},
	}

	var buf bytes.Buffer
	RenderTable(rows, &buf, false)
	output := buf.String()

	// Verify bar characters are present (empty bar for 0%)
	if !strings.Contains(output, "░") {
		t.Error("Output missing empty block character")
	}
	if !strings.Contains(output, "0%") {
		t.Error("Output missing '0%' usage")
	}
}

func TestRenderTable_FullUsage(t *testing.T) {
	now := time.Now()
	rows := []providers.UsageRow{
		{
			Provider:     "Test",
			Label:        "test",
			UsagePercent: 100,
			ResetTime:    now.Add(1 * time.Hour),
		},
	}

	var buf bytes.Buffer
	RenderTable(rows, &buf, false)
	output := buf.String()

	// Verify full bar for 100%
	if !strings.Contains(output, "██████") {
		t.Error("Output missing full bar for 100%")
	}
	if !strings.Contains(output, "100%") {
		t.Error("Output missing '100%' usage")
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "string shorter than maxLen",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "string equal to maxLen",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "string longer than maxLen includes ellipsis in limit",
			input:  "hello world this is a long string",
			maxLen: 10,
			want:   "hello w...",
		},
		{
			name:   "truncated result length equals maxLen",
			input:  "abcdefghijklmnopqrstuvwxyz",
			maxLen: 10,
			want:   "abcdefg...",
		},
		{
			name:   "maxLen of 3 returns first 3 chars without ellipsis",
			input:  "hello",
			maxLen: 3,
			want:   "hel",
		},
		{
			name:   "maxLen of 4 returns 1 char plus ellipsis",
			input:  "hello",
			maxLen: 4,
			want:   "h...",
		},
		{
			name:   "unicode runes are counted correctly",
			input:  "日本語テスト文字列です",
			maxLen: 7,
			want:   "日本語テ...",
		},
		{
			name:   "maxWarningLen truncation stays within limit",
			input:  strings.Repeat("a", 150),
			maxLen: 120,
			want:   strings.Repeat("a", 117) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateRunes(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
			// Verify result length never exceeds maxLen
			if len([]rune(got)) > tt.maxLen {
				t.Errorf("truncateRunes(%q, %d) result length %d exceeds maxLen %d",
					tt.input, tt.maxLen, len([]rune(got)), tt.maxLen)
			}
		})
	}
}
