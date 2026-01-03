package output

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/charlieyou/aim/internal/providers"
	"golang.org/x/term"
)

const (
	defaultBarWidth = 6
	maxBarWidth     = 24
	fullBlock       = '█' // U+2588
	emptyBlock      = '░' // U+2591
	maxWarningLen   = 120
	ansiReset       = "\x1b[0m"
	ansiBold        = "\x1b[1m"
	ansiDim         = "\x1b[90m"
	ansiRed         = "\x1b[31m"
	ansiYellow      = "\x1b[33m"
	ansiGreen       = "\x1b[32m"
	ansiCyan        = "\x1b[36m"
	ansiUnderline   = "\x1b[4m"
)

// generateBar creates an ASCII progress bar of the requested width.
// e.g., width=6, 24% -> "█░░░░░", 50% -> "███░░░", 100% -> "██████"
func generateBar(width int, percent float64) string {
	if width <= 0 {
		width = defaultBarWidth
	}
	// Clamp percent to 0-100
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	// Calculate filled blocks, rounding to nearest
	filled := int(math.Round(percent / 100.0 * float64(width)))

	bar := make([]rune, width)
	for i := 0; i < width; i++ {
		if i < filled {
			bar[i] = fullBlock
		} else {
			bar[i] = emptyBlock
		}
	}
	return string(bar)
}

func sanitizeWarning(msg string) string {
	cleaned := strings.Join(strings.Fields(msg), " ")
	if cleaned == "" {
		return cleaned
	}
	return truncateRunes(cleaned, maxWarningLen)
}

func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	// Truncate to maxLen-3 to leave room for "..." ellipsis
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

func stringWidth(value string) int {
	return len([]rune(value))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func colorize(enabled bool, value string, codes ...string) string {
	if !enabled || value == "" || len(codes) == 0 {
		return value
	}
	return strings.Join(codes, "") + value + ansiReset
}

func usageColor(percent float64) string {
	switch {
	case percent >= 80:
		return strings.Join([]string{ansiRed, ansiBold}, "")
	case percent >= 50:
		return ansiYellow
	default:
		return ansiGreen
	}
}

func stripANSI(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	inEscape := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if inEscape {
			if ch == 'm' {
				inEscape = false
			}
			continue
		}
		if ch == 0x1b && i+1 < len(value) && value[i+1] == '[' {
			inEscape = true
			i++
			continue
		}
		builder.WriteByte(ch)
	}
	return builder.String()
}

func visibleWidth(value string) int {
	return len([]rune(stripANSI(value)))
}

func isBlankRow(row []string) bool {
	if len(row) == 0 {
		return true
	}
	for _, cell := range row {
		if strings.TrimSpace(stripANSI(cell)) != "" {
			return false
		}
	}
	return true
}

func padRight(value string, width int) string {
	padding := width - visibleWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func splitProvider(provider string) (string, string) {
	start := strings.Index(provider, " (")
	if start == -1 || !strings.HasSuffix(provider, ")") {
		return provider, ""
	}
	name := strings.TrimSpace(provider[:start])
	detail := strings.TrimSuffix(provider[start+2:], ")")
	if name == "" || detail == "" {
		return provider, ""
	}
	return name, detail
}

func formatProviderHeader(provider string, useColor bool) string {
	name, detail := splitProvider(provider)
	if detail == "" {
		return colorize(useColor, name, ansiBold)
	}
	name = colorize(useColor, name, ansiBold)
	detail = colorize(useColor, "("+detail+")", ansiDim)
	return name + " " + detail
}

func terminalWidth(w io.Writer) (int, bool) {
	file, ok := w.(*os.File)
	if !ok {
		return 0, false
	}
	fd := int(file.Fd())
	if !term.IsTerminal(fd) {
		return 0, false
	}
	width, _, err := term.GetSize(fd)
	if err != nil || width <= 0 {
		return 0, false
	}
	return width, true
}

func computeBarWidth(rows []providers.UsageRow, debug bool, termWidth int, now time.Time) int {
	if termWidth <= 0 {
		return defaultBarWidth
	}

	providerWidth := stringWidth("Provider")
	windowWidth := stringWidth("Window")
	usageHeaderWidth := stringWidth("Usage")
	resetWidth := stringWidth("Resets At")
	debugWidth := 0
	if debug {
		debugWidth = stringWidth("Debug")
	}

	percentWidth := 0
	hasUsage := false

	for _, row := range rows {
		providerWidth = maxInt(providerWidth, stringWidth(row.Provider))

		if row.IsGroup {
			continue
		}

		if row.IsWarning {
			warning := "⚠ " + sanitizeWarning(row.WarningMsg)
			windowWidth = maxInt(windowWidth, stringWidth(warning))
			if debug {
				debugWidth = maxInt(debugWidth, stringWidth(row.DebugInfo))
			}
			continue
		}

		windowWidth = maxInt(windowWidth, stringWidth(row.Label))
		resetStr := formatResetTimeFrom(row.ResetTime, now)
		resetWidth = maxInt(resetWidth, stringWidth(resetStr))
		if debug {
			debugWidth = maxInt(debugWidth, stringWidth(row.DebugInfo))
		}

		percentStr := fmt.Sprintf("%d%%", int(math.Round(row.UsagePercent)))
		percentWidth = maxInt(percentWidth, stringWidth(percentStr))
		hasUsage = true
	}

	if !hasUsage {
		return defaultBarWidth
	}

	columns := 4
	if debug {
		columns++
	}
	gapWidth := 2

	fixedContent := providerWidth + windowWidth + resetWidth
	if debug {
		fixedContent += debugWidth
	}

	available := termWidth - fixedContent - gapWidth*(columns-1)
	minUsageContent := maxInt(usageHeaderWidth, percentWidth+1+defaultBarWidth)
	if available <= minUsageContent {
		return defaultBarWidth
	}

	barWidth := available - (percentWidth + 1)
	if barWidth < defaultBarWidth {
		return defaultBarWidth
	}
	if barWidth > maxBarWidth {
		return maxBarWidth
	}

	return barWidth
}

// PrintCredentialSource prints dimmed header showing credential source
func PrintCredentialSource(w io.Writer, source string) {
	useColor := isColorEnabled(w)
	if useColor {
		fmt.Fprintf(w, "%sCredentials: %s%s\n\n", ansiDim, source, ansiReset)
	} else {
		fmt.Fprintf(w, "Credentials: %s\n\n", source)
	}
}

// RenderTable renders usage rows as an ASCII table.
func RenderTable(rows []providers.UsageRow, w io.Writer, debug bool) {
	now := time.Now()
	barWidth := defaultBarWidth
	if termWidth, ok := terminalWidth(w); ok {
		barWidth = computeBarWidth(rows, debug, termWidth, now)
	}
	useColor := isColorEnabled(w)

	headers := []string{"Provider", "Window", "Usage", "Resets At"}
	if debug {
		headers = append(headers, "Debug")
	}

	styledHeaders := make([]string, len(headers))
	for i, header := range headers {
		styledHeaders[i] = colorize(useColor, header, ansiDim, ansiUnderline)
	}

	colCount := len(headers)
	rendered := make([][]string, 0, len(rows)+1)
	rendered = append(rendered, styledHeaders)

	for _, row := range rows {
		cells := make([]string, 0, colCount)

		if row.IsGroup {
			provider := formatProviderHeader(row.Provider, useColor)
			cells = append(cells, provider, "", "", "")
			if debug {
				cells = append(cells, "")
			}
			rendered = append(rendered, cells)
			continue
		}

		if row.IsWarning {
			warning := sanitizeWarning(row.WarningMsg)
			warnText := "⚠ " + warning
			warnText = colorize(useColor, warnText, ansiBold, ansiYellow)
			provider := row.Provider
			if strings.HasPrefix(provider, "  ") {
				provider = colorize(useColor, provider, ansiDim)
			}
			cells = append(cells, provider, warnText, "", "")
			if debug {
				cells = append(cells, row.DebugInfo)
			}
			rendered = append(rendered, cells)
			continue
		}

		usageStr := fmt.Sprintf("%s %d%%", generateBar(barWidth, row.UsagePercent), int(math.Round(row.UsagePercent)))
		usageStr = colorize(useColor, usageStr, usageColor(row.UsagePercent))
		resetStr := formatResetTimeFrom(row.ResetTime, now)
		if !row.ResetTime.IsZero() {
			diff := row.ResetTime.Sub(now)
			if diff > 0 && diff < 4*time.Hour {
				resetStr = colorize(useColor, resetStr, ansiCyan)
			} else if diff >= 4*time.Hour {
				resetStr = colorize(useColor, resetStr, ansiDim)
			}
		}
		provider := row.Provider
		if strings.HasPrefix(provider, "  ") {
			provider = colorize(useColor, provider, ansiDim)
		}
		cells = append(cells, provider, row.Label, usageStr, resetStr)
		if debug {
			cells = append(cells, row.DebugInfo)
		}
		rendered = append(rendered, cells)
	}

	colWidths := make([]int, colCount)
	for _, row := range rendered {
		for i, cell := range row {
			colWidths[i] = maxInt(colWidths[i], visibleWidth(cell))
		}
	}

	for _, row := range rendered {
		var builder strings.Builder
		if isBlankRow(row) {
			fmt.Fprintln(w)
			continue
		}
		for i, cell := range row {
			if i > 0 {
				builder.WriteString("  ")
			}
			builder.WriteString(padRight(cell, colWidths[i]))
		}
		fmt.Fprintln(w, builder.String())
	}
}
