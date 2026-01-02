package main

import (
	"context"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/cyou/aim/internal/output"
	"github.com/cyou/aim/internal/providers"
)

func main() {
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

	// Sort rows
	sortRows(allRows)

	output.RenderTable(allRows, os.Stdout)
}

// sortRows sorts usage rows by:
// 1. Provider order: Claude, Codex, Gemini (based on prefix)
// 2. Alphabetical by Label within each provider
// 3. Warnings last within each group
func sortRows(rows []providers.UsageRow) {
	providerOrder := map[string]int{
		"Claude": 0,
		"Codex":  1,
		"Gemini": 2,
	}

	// getProviderPrefix extracts the base provider name from Provider field
	// e.g., "Codex (user@example.com)" -> "Codex"
	getProviderPrefix := func(provider string) string {
		for prefix := range providerOrder {
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

		// Secondary: Full provider name (for multi-account providers like Codex)
		if rows[i].Provider != rows[j].Provider {
			return rows[i].Provider < rows[j].Provider
		}

		// Tertiary: Warnings last within each provider
		if rows[i].IsWarning != rows[j].IsWarning {
			return !rows[i].IsWarning // non-warnings come first
		}

		// Quaternary: Alphabetical by Label
		return rows[i].Label < rows[j].Label
	})
}
