// BL216 — session state transition tests for completion pattern detection.
// Tests that completion patterns are properly detected even when preceded by ANSI escape codes.

package session

import (
	"strings"
	"testing"
)

func TestCompletionPatternDetectionWithANSI(t *testing.T) {
	tests := []struct {
		name            string
		line            string
		shouldMatch     bool
		expectedStripped string
	}{
		{
			name:            "plain completion marker",
			line:            "DATAWATCH_COMPLETE: test",
			shouldMatch:     true,
			expectedStripped: "DATAWATCH_COMPLETE: test",
		},
		{
			name:            "completion marker with leading ANSI color",
			line:            "\x1b[32mDATAWATCH_COMPLETE: test\x1b[0m",
			shouldMatch:     true,
			expectedStripped: "DATAWATCH_COMPLETE: test",
		},
		// Note: OSC escape sequences (like window title) may not be fully stripped
		// if they lack proper terminators in the output. The main fix handles
		// CSI sequences which are more common in terminal output.
		{
			name:            "completion marker with CSI escape codes",
			line:            "\x1b[2J\x1b[HDATAWATCH_COMPLETE: test",
			shouldMatch:     true,
			expectedStripped: "DATAWATCH_COMPLETE: test",
		},
		{
			name:            "completion marker with mixed ANSI codes",
			line:            "\x1b[38;5;153m❯\x1b[39m DATAWATCH_COMPLETE: test",
			shouldMatch:     false,
			expectedStripped: "❯ DATAWATCH_COMPLETE: test",
		},
		{
			name:            "no completion marker",
			line:            "some other output",
			shouldMatch:     false,
			expectedStripped: "some other output",
		},
		{
			name:            "completion marker with whitespace",
			line:            "  \t  DATAWATCH_COMPLETE: test",
			shouldMatch:     true,
			expectedStripped: "DATAWATCH_COMPLETE: test",
		},
		{
			name:            "embedded completion marker should not match at start",
			line:            "echo 'DATAWATCH_COMPLETE: test'",
			shouldMatch:     false,
			expectedStripped: "echo 'DATAWATCH_COMPLETE: test'",
		},
		{
			// Real-world bytes captured from claude-code session output.log when claude exits.
			// Includes CSI sequences, OSC sequences with BEL terminators, and DEC private
			// sequences (ESC 7 / ESC 8 = save/restore cursor) that all need stripping.
			name:            "claude-code exit sequence (real-world)",
			line:            "\x1b[7C\x1b[8A\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[7D\x1b[8B\x1b[>4m\x1b[<u\x1b[?1004l\x1b[?2031l\x1b[?2004l\x1b[?25h\x1b7\x1b[r\x1b8\x1b]9;4;0;\x07\x1b]0;\x07DATAWATCH_COMPLETE: claude done\r",
			shouldMatch:     true,
			expectedStripped: "DATAWATCH_COMPLETE: claude done",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Test StripANSI behavior
			stripped := StripANSI(tc.line)
			trimmed := strings.TrimSpace(stripped)
			if trimmed != tc.expectedStripped {
				t.Logf("stripped: %q", trimmed)
				if trimmed != tc.expectedStripped {
					t.Errorf("StripANSI + TrimSpace(%q) = %q, want %q", tc.line, trimmed, tc.expectedStripped)
				}
			}

			// Test pattern matching behavior
			patterns := []string{"DATAWATCH_COMPLETE:"}
			matches := false
			for _, pat := range patterns {
				if strings.HasPrefix(trimmed, pat) {
					matches = true
					break
				}
			}

			if matches != tc.shouldMatch {
				t.Errorf("pattern match for %q: got %v, want %v", tc.line, matches, tc.shouldMatch)
			}
		})
	}
}

func TestInputNeededPatternDetectionWithANSI(t *testing.T) {
	tests := []struct {
		name            string
		line            string
		shouldMatch     bool
		expectedStripped string
	}{
		{
			name:            "plain input needed marker",
			line:            "DATAWATCH_NEEDS_INPUT: some question",
			shouldMatch:     true,
			expectedStripped: "DATAWATCH_NEEDS_INPUT: some question",
		},
		{
			name:            "input marker with leading ANSI",
			line:            "\x1b[33mDATAWATCH_NEEDS_INPUT: user input needed\x1b[0m",
			shouldMatch:     true,
			expectedStripped: "DATAWATCH_NEEDS_INPUT: user input needed",
		},
		// Note: OSC escape sequences may not be fully stripped if they lack
		// proper terminators. Focus on CSI sequences which are more common.
		{
			name:            "no input needed marker",
			line:            "normal output",
			shouldMatch:     false,
			expectedStripped: "normal output",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stripped := StripANSI(tc.line)
			if stripped != tc.expectedStripped {
				t.Errorf("StripANSI(%q) = %q, want %q", tc.line, stripped, tc.expectedStripped)
			}

			patterns := []string{"DATAWATCH_NEEDS_INPUT:"}
			matches := false
			for _, pat := range patterns {
				if strings.Contains(stripped, pat) {
					matches = true
					break
				}
			}

			if matches != tc.shouldMatch {
				t.Errorf("pattern match for %q: got %v, want %v", tc.line, matches, tc.shouldMatch)
			}
		})
	}
}
