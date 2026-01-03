package main

import (
	"context"
	"flag"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charlieyou/aim/internal/output"
	"github.com/charlieyou/aim/internal/providers"
)

func main() {
	debug := flag.Bool("debug", false, "Show debug metadata for usage rows")
	showGeminiOld := flag.Bool("gemini-old", false, "Show Gemini 2.x models (gemini-2*)")
	flag.Parse()
	providers.SetDebug(*debug)

	// Detect and display credential source
	homeDir, err := os.UserHomeDir()
	if err == nil {
		credSource := providers.DetectCredentialSource(homeDir)
		output.PrintCredentialSource(os.Stdout, credSource.DisplayName())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var allRows []providers.UsageRow
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Create providers, handle constructor errors
	providerFactories := []struct {
		name    string
		factory func() (providers.Provider, error)
	}{
		{"Claude", func() (providers.Provider, error) { return providers.NewClaudeProvider() }},
		{"Codex", func() (providers.Provider, error) { return providers.NewCodexProvider() }},
		{"Gemini", func() (providers.Provider, error) { return providers.NewGeminiProvider() }},
	}

	for _, pf := range providerFactories {
		provider, err := pf.factory()
		if err != nil {
			// Constructor failed - add warning row
			mu.Lock()
			allRows = append(allRows, providers.UsageRow{
				Provider:   pf.name,
				IsWarning:  true,
				WarningMsg: err.Error(),
			})
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(p providers.Provider, name string) {
			defer wg.Done()
			rows, err := p.FetchUsage(ctx)
			mu.Lock()
			if err != nil {
				// FetchUsage failed - add warning row
				allRows = append(allRows, providers.UsageRow{
					Provider:   name,
					IsWarning:  true,
					WarningMsg: err.Error(),
				})
			} else {
				allRows = append(allRows, rows...)
			}
			mu.Unlock()
		}(provider, pf.name)
	}

	wg.Wait()

	allRows = filterRows(allRows, *showGeminiOld)

	// Sort rows
	sortRows(allRows)

	allRows = formatGeminiRows(allRows)
	allRows = groupProviderRows(allRows)

	output.RenderTable(allRows, os.Stdout, *debug)
}

// sortRows sorts usage rows by:
// 1. Provider order: Claude, Codex, Gemini (based on prefix)
// 2. Warnings last within each provider group
// 3. Full provider name (for multi-account providers like Codex)
// 4. Alphabetical by Label within each provider
func sortRows(rows []providers.UsageRow) {
	providerOrder := map[string]int{
		"Claude": 0,
		"Codex":  1,
		"Gemini": 2,
	}

	// providerPrefixes defines the canonical prefixes in deterministic order.
	// Longer prefixes are checked first to handle potential overlaps correctly.
	providerPrefixes := []string{"Claude", "Codex", "Gemini"}

	// getProviderPrefix extracts the base provider name from Provider field
	// e.g., "Codex (user@example.com)" -> "Codex"
	getProviderPrefix := func(provider string) string {
		for _, prefix := range providerPrefixes {
			if len(provider) >= len(prefix) && provider[:len(prefix)] == prefix {
				return prefix
			}
		}
		return provider
	}

	// getOrder returns the sort order for a provider prefix.
	// Unknown providers get a high value to sort after known ones.
	getOrder := func(prefix string) int {
		if order, ok := providerOrder[prefix]; ok {
			return order
		}
		return len(providerOrder) // Unknown providers sort last
	}

	sort.SliceStable(rows, func(i, j int) bool {
		// Get provider prefixes
		prefixI := getProviderPrefix(rows[i].Provider)
		prefixJ := getProviderPrefix(rows[j].Provider)

		// Primary: Provider order
		orderI := getOrder(prefixI)
		orderJ := getOrder(prefixJ)
		if orderI != orderJ {
			return orderI < orderJ
		}

		// Secondary: Warnings last within each provider group
		if rows[i].IsWarning != rows[j].IsWarning {
			return !rows[i].IsWarning // non-warnings come first
		}

		// Tertiary: Full provider name (for multi-account providers like Codex)
		if rows[i].Provider != rows[j].Provider {
			return rows[i].Provider < rows[j].Provider
		}

		// Quaternary: Alphabetical by Label
		return rows[i].Label < rows[j].Label
	})
}

func filterRows(rows []providers.UsageRow, showGeminiOld bool) []providers.UsageRow {
	if showGeminiOld {
		return rows
	}

	filtered := make([]providers.UsageRow, 0, len(rows))
	for _, row := range rows {
		if row.IsWarning {
			filtered = append(filtered, row)
			continue
		}
		if strings.HasPrefix(row.Provider, "Gemini") && isGemini2xModel(row.Label) {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func isGemini2xModel(label string) bool {
	return strings.HasPrefix(strings.ToLower(label), "gemini-2")
}

func formatGeminiRows(rows []providers.UsageRow) []providers.UsageRow {
	const (
		geminiPrefix = "Gemini"
		windowLabel  = "24-hour"
		modelIndent  = "  "
	)

	formatted := make([]providers.UsageRow, 0, len(rows))
	seenHeader := make(map[string]bool)

	for _, row := range rows {
		if !strings.HasPrefix(row.Provider, geminiPrefix) {
			formatted = append(formatted, row)
			continue
		}

		// Account-level warnings or generic Gemini warnings remain unchanged.
		if row.IsWarning && row.Label == "" {
			formatted = append(formatted, row)
			continue
		}

		// Model-level rows are grouped under the account header.
		if row.Label != "" {
			if !seenHeader[row.Provider] {
				formatted = append(formatted, providers.UsageRow{
					Provider: row.Provider,
					IsGroup:  true,
				})
				seenHeader[row.Provider] = true
			}

			row.Provider = modelIndent + row.Label
			row.Label = windowLabel
			formatted = append(formatted, row)
			continue
		}

		formatted = append(formatted, row)
	}

	return formatted
}

func groupProviderRows(rows []providers.UsageRow) []providers.UsageRow {
	groupedProviders := make(map[string]bool)
	for _, row := range rows {
		if row.IsGroup {
			groupedProviders[row.Provider] = true
		}
	}

	counts := make(map[string]int)
	for _, row := range rows {
		if row.IsGroup || strings.HasPrefix(row.Provider, "  ") || groupedProviders[row.Provider] {
			continue
		}
		counts[row.Provider]++
	}

	formatted := make([]providers.UsageRow, 0, len(rows))
	seenHeader := make(map[string]bool)

	for _, row := range rows {
		if row.IsGroup || strings.HasPrefix(row.Provider, "  ") {
			formatted = append(formatted, row)
			continue
		}

		if groupedProviders[row.Provider] {
			row.Provider = ""
			formatted = append(formatted, row)
			continue
		}

		if counts[row.Provider] <= 1 {
			formatted = append(formatted, row)
			continue
		}

		if !seenHeader[row.Provider] {
			formatted = append(formatted, providers.UsageRow{
				Provider: row.Provider,
				IsGroup:  true,
			})
			seenHeader[row.Provider] = true
		}

		row.Provider = ""
		formatted = append(formatted, row)
	}

	return formatted
}
