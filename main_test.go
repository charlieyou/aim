package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cyou/aim/internal/providers"
)

func TestFilterRows_ShowOld(t *testing.T) {
	rows := []providers.UsageRow{
		{Provider: "Gemini (a)", Label: "gemini-2.5-pro"},
		{Provider: "Codex", Label: "5-hour"},
	}

	filtered := filterRows(rows, true)
	if !reflect.DeepEqual(filtered, rows) {
		t.Fatalf("expected rows unchanged when showing old models")
	}
}

func TestFilterRows_HideGemini2xByDefault(t *testing.T) {
	rows := []providers.UsageRow{
		{Provider: "Gemini (a)", Label: "gemini-2.5-pro"},
		{Provider: "Gemini (a)", Label: "gemini-3.0-pro"},
		{Provider: "Gemini", IsWarning: true, WarningMsg: "warning"},
		{Provider: "Codex", Label: "5-hour"},
		{Provider: "Gemini (a)", Label: "Gemini-2.0-flash"},
	}

	filtered := filterRows(rows, false)
	if len(filtered) != 3 {
		t.Fatalf("expected 3 rows after filtering, got %d", len(filtered))
	}
	for _, row := range filtered {
		if strings.HasPrefix(strings.ToLower(row.Label), "gemini-2") {
			t.Fatalf("unexpected gemini-2x model remaining: %q", row.Label)
		}
	}
}

func TestFormatGeminiRows_GroupsModelsUnderAccount(t *testing.T) {
	rows := []providers.UsageRow{
		{Provider: "Gemini (a)", Label: "gemini-3.0-pro", UsagePercent: 10},
		{Provider: "Gemini (a)", Label: "gemini-3.0-flash", UsagePercent: 20},
		{Provider: "Codex", Label: "5-hour"},
	}

	formatted := formatGeminiRows(rows)
	if len(formatted) != 4 {
		t.Fatalf("expected 4 rows after formatting, got %d", len(formatted))
	}

	if !formatted[0].IsGroup || formatted[0].Provider != "Gemini (a)" {
		t.Fatalf("expected group header for Gemini account, got %+v", formatted[0])
	}

	if formatted[1].Provider != "  gemini-3.0-pro" || formatted[1].Label != "24-hour" {
		t.Fatalf("expected indented model row, got %+v", formatted[1])
	}

	if formatted[2].Provider != "  gemini-3.0-flash" || formatted[2].Label != "24-hour" {
		t.Fatalf("expected indented model row, got %+v", formatted[2])
	}

	if formatted[3].Provider != "Codex" || formatted[3].Label != "5-hour" {
		t.Fatalf("expected non-gemini row unchanged, got %+v", formatted[3])
	}
}

func TestFormatGeminiRows_IndentsModelWarnings(t *testing.T) {
	rows := []providers.UsageRow{
		{Provider: "Gemini (a)", Label: "gemini-3.0-pro", IsWarning: true, WarningMsg: "parse error"},
	}

	formatted := formatGeminiRows(rows)
	if len(formatted) != 2 {
		t.Fatalf("expected 2 rows after formatting, got %d", len(formatted))
	}

	if !formatted[0].IsGroup || formatted[0].Provider != "Gemini (a)" {
		t.Fatalf("expected group header for Gemini account, got %+v", formatted[0])
	}

	if formatted[1].Provider != "  gemini-3.0-pro" {
		t.Fatalf("expected indented warning row, got %+v", formatted[1])
	}
}

func TestGroupProviderRows_AddsHeaderAndBlanksProvider(t *testing.T) {
	rows := []providers.UsageRow{
		{Provider: "Claude (a)", Label: "5-hour"},
		{Provider: "Claude (a)", Label: "7-day"},
		{Provider: "Codex", Label: "5-hour"},
	}

	grouped := groupProviderRows(rows)
	if len(grouped) != 4 {
		t.Fatalf("expected 4 rows after grouping, got %d", len(grouped))
	}

	if !grouped[0].IsGroup || grouped[0].Provider != "Claude (a)" {
		t.Fatalf("expected group header for Claude, got %+v", grouped[0])
	}

	if grouped[1].Provider != "" || grouped[2].Provider != "" {
		t.Fatalf("expected grouped rows to blank provider, got %+v %+v", grouped[1], grouped[2])
	}

	if grouped[3].Provider != "Codex" {
		t.Fatalf("expected non-grouped provider unchanged, got %+v", grouped[3])
	}
}
