package stationserver

import (
	"testing"
)

func TestParsePlainLines_ConcatenatedItems(t *testing.T) {
	sp := map[string]string{
		"tandoor": "p-tandoor",
		"wok":     "p-wok",
		"pass":    "p-pass",
	}

	// Scenario: all items pasted on a single line without newlines
	input := "2x Paneer Tikka (less spicy) – Tandoor – Rs 240 4x Butter Naan – Tandoor – Rs 60 1x Veg Hakka Noodles (no onion) – Wok – Rs 180 2x Masala Chai – Pass – Rs 40"

	items := parsePlainLines(input, sp)

	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d: %+v", len(items), items)
	}

	tests := []struct {
		qty     int
		name    string
		notes   string
		station string
		price   int // paise
	}{
		{2, "Paneer Tikka", "less spicy", "Tandoor", 24000},
		{4, "Butter Naan", "", "Tandoor", 6000},
		{1, "Veg Hakka Noodles", "no onion", "Wok", 18000},
		{2, "Masala Chai", "", "Pass", 4000},
	}

	for i, tt := range tests {
		it := items[i]
		if it.Quantity != tt.qty {
			t.Errorf("item[%d] quantity: got %d, want %d", i, it.Quantity, tt.qty)
		}
		if it.Name != tt.name {
			t.Errorf("item[%d] name: got %q, want %q", i, it.Name, tt.name)
		}
		if it.Notes != tt.notes {
			t.Errorf("item[%d] notes: got %q, want %q", i, it.Notes, tt.notes)
		}
		if it.Station != tt.station {
			t.Errorf("item[%d] station: got %q, want %q", i, it.Station, tt.station)
		}
		if it.UnitPrice != tt.price {
			t.Errorf("item[%d] price: got %d, want %d", i, it.UnitPrice, tt.price)
		}
	}
}

func TestParsePlainLines_NormalNewlines(t *testing.T) {
	sp := map[string]string{
		"tandoor": "p-tandoor",
		"pass":    "p-pass",
	}

	// Normal case: items separated by newlines should still work
	input := "2x Paneer Tikka (less spicy) – Tandoor – Rs 240\n4x Butter Naan – Tandoor – Rs 60\n2x Masala Chai – Pass – Rs 40"

	items := parsePlainLines(input, sp)

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d: %+v", len(items), items)
	}
	if items[0].Name != "Paneer Tikka" {
		t.Errorf("item[0] name: got %q, want %q", items[0].Name, "Paneer Tikka")
	}
	if items[0].UnitPrice != 24000 {
		t.Errorf("item[0] price: got %d, want %d", items[0].UnitPrice, 24000)
	}
}

func TestSplitConcatenatedItems(t *testing.T) {
	input := "2x Paneer Tikka – Tandoor – Rs 240 4x Butter Naan – Tandoor – Rs 60"
	result := splitConcatenatedItems(input)

	lines := 0
	for _, line := range splitLines(result) {
		if line != "" {
			lines++
		}
	}
	if lines < 2 {
		t.Errorf("expected at least 2 non-empty lines, got %d from: %q", lines, result)
	}
}

func splitLines(s string) []string {
	var lines []string
	for _, l := range splitByNewline(s) {
		l = trimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func splitByNewline(s string) []string {
	return splitStringBy(s, '\n')
}

func splitStringBy(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}

