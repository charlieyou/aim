package output

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/cyou/aim/internal/providers"
	"github.com/mattn/go-runewidth"
	"github.com/olekukonko/tablewriter"
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
	ansiDim         = "\x1b[2m"
	ansiRed         = "\x1b[31m"
	ansiYellow      = "\x1b[33m"
	ansiGreen       = "\x1b[32m"
	ansiCyan        = "\x1b[36m"
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
	return runewidth.StringWidth(value)
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
		return ansiRed
	case percent >= 50:
		return ansiYellow
	default:
		return ansiGreen
	}
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
	separators := columns + 1
	padding := columns * 2

	fixedContent := providerWidth + windowWidth + resetWidth
	if debug {
		fixedContent += debugWidth
	}

	available := termWidth - separators - padding - fixedContent
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

// RenderTable renders usage rows as an ASCII table.
func RenderTable(rows []providers.UsageRow, w io.Writer, debug bool) {
	now := time.Now()
	barWidth := defaultBarWidth
	if termWidth, ok := terminalWidth(w); ok {
		barWidth = computeBarWidth(rows, debug, termWidth, now)
	}
	useColor := isColorEnabled(w)

	table := tablewriter.NewWriter(w)

	// Configure table style for Unicode box-drawing
	table.SetBorder(true)
	table.SetCenterSeparator("┼")
	table.SetColumnSeparator("│")
	table.SetRowSeparator("─")
	table.SetHeaderLine(true)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetAutoWrapText(false)

	// Custom borders for Unicode
	table.SetTablePadding(" ")
	table.SetBorders(tablewriter.Border{
		Left:   true,
		Right:  true,
		Top:    true,
		Bottom: true,
	})

	// Set headers
	headers := []string{"Provider", "Window", "Usage", "Resets At"}
	if debug {
		headers = append(headers, "Debug")
	}
	table.SetHeader(headers)

	// Add rows
	for _, row := range rows {
		if row.IsGroup {
			provider := colorize(useColor, row.Provider, ansiBold, ansiCyan)
			cells := []string{provider, "", "", ""}
			if debug {
				cells = append(cells, "")
			}
			table.Append(cells)
			continue
		}
		if row.IsWarning {
			// Warning row: provider in first column, message in second column
			// (remaining columns left empty as tablewriter doesn't support colspan)
			warning := sanitizeWarning(row.WarningMsg)
			warnText := "⚠ " + warning
			warnText = colorize(useColor, warnText, ansiBold, ansiYellow)
			cells := []string{row.Provider, warnText, "", ""}
			if debug {
				cells = append(cells, row.DebugInfo)
			}
			table.Append(cells)
		} else {
			// Normal row: all columns populated
			usageStr := fmt.Sprintf("%s %d%%", generateBar(barWidth, row.UsagePercent), int(math.Round(row.UsagePercent)))
			usageStr = colorize(useColor, usageStr, usageColor(row.UsagePercent))
			resetStr := formatResetTimeFrom(row.ResetTime, now)
			provider := row.Provider
			if strings.HasPrefix(provider, "  ") {
				provider = colorize(useColor, provider, ansiDim)
			}
			cells := []string{provider, row.Label, usageStr, resetStr}
			if debug {
				cells = append(cells, row.DebugInfo)
			}
			table.Append(cells)
		}
	}

	table.Render()
}
