package output

import (
	"fmt"
	"io"
	"math"

	"github.com/cyou/aim/internal/providers"
	"github.com/olekukonko/tablewriter"
)

const (
	barWidth   = 6
	fullBlock  = '█' // U+2588
	emptyBlock = '░' // U+2591
)

// generateBar creates an ASCII progress bar (6 chars wide).
// e.g., 24% -> "█░░░░░", 50% -> "███░░░", 100% -> "██████"
func generateBar(percent float64) string {
	// Clamp percent to 0-100
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	// Calculate filled blocks, rounding to nearest
	filled := int(math.Round(percent / 100.0 * float64(barWidth)))

	bar := make([]rune, barWidth)
	for i := 0; i < barWidth; i++ {
		if i < filled {
			bar[i] = fullBlock
		} else {
			bar[i] = emptyBlock
		}
	}
	return string(bar)
}

// RenderTable renders usage rows as an ASCII table.
func RenderTable(rows []providers.UsageRow, w io.Writer) {
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
	table.SetHeader([]string{"Provider", "Window", "Usage", "Resets At"})

	// Add rows
	for _, row := range rows {
		if row.IsWarning {
			// Warning row: provider in first column, message in second column
			// (remaining columns left empty as tablewriter doesn't support colspan)
			table.Append([]string{row.Provider, "⚠ " + row.WarningMsg, "", ""})
		} else {
			// Normal row: all columns populated
			usageStr := fmt.Sprintf("%s %d%%", generateBar(row.UsagePercent), int(math.Round(row.UsagePercent)))
			resetStr := FormatResetTime(row.ResetTime)
			table.Append([]string{row.Provider, row.Label, usageStr, resetStr})
		}
	}

	table.Render()
}
