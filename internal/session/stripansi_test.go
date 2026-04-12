package session

import (
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"plain text", "plain text"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"\x1b[1;32mbold green\x1b[0m", "bold green"},
		{"\x1b[38;5;153m❯\x1b[39m", "❯"},
		{"no escapes here", "no escapes here"},
		{"", ""},
		{"\x1b[2J\x1b[H", ""},
		{"line1\x1b[Kline2", "line1line2"},
	}
	for _, tc := range tests {
		result := StripANSI(tc.input)
		if result != tc.expected {
			t.Errorf("StripANSI(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestIsAllSameChar(t *testing.T) {
	if !isAllSameChar("────────") {
		t.Error("expected true for repeated ─")
	}
	if !isAllSameChar("========") {
		t.Error("expected true for repeated =")
	}
	if isAllSameChar("abc") {
		t.Error("expected false for mixed chars")
	}
	if isAllSameChar("") {
		t.Error("expected false for empty")
	}
}

func TestTruncateStr(t *testing.T) {
	if truncateStr("hello", 10) != "hello" {
		t.Error("short string should not be truncated")
	}
	result := truncateStr("hello world this is a long string", 10)
	if len(result) > 13 { // 10 + "..."
		t.Errorf("expected truncated, got %q (len %d)", result, len(result))
	}
}
