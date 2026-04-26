// BL197 — chat-channel autonomous PRD parser tests.
// `autonomous <verb>` and the `prd` alias both produce CmdAutonomous
// with the verb-and-args left in cmd.Text for the dispatcher to
// parse. Mirror of the `peers` command pattern.

package router

import (
	"testing"
)

func TestParse_AutonomousVerbs(t *testing.T) {
	cases := []struct {
		in       string
		wantText string
	}{
		{"autonomous", ""},
		{"autonomous status", "status"},
		{"autonomous list", "list"},
		{"autonomous get prd_a3f9", "get prd_a3f9"},
		{"autonomous decompose prd_a3f9", "decompose prd_a3f9"},
		{"autonomous run prd_a3f9", "run prd_a3f9"},
		{"autonomous cancel prd_a3f9", "cancel prd_a3f9"},
		{"autonomous learnings", "learnings"},
		{"autonomous create Add a search-icon toggle to the sessions list", "create Add a search-icon toggle to the sessions list"},
	}
	for _, tc := range cases {
		got := Parse(tc.in)
		if got.Type != CmdAutonomous {
			t.Errorf("%q: type = %v want CmdAutonomous", tc.in, got.Type)
		}
		if got.Text != tc.wantText {
			t.Errorf("%q: text = %q want %q", tc.in, got.Text, tc.wantText)
		}
	}
}

// `prd` is the shorter alias accepted equivalently.
func TestParse_PRDAlias(t *testing.T) {
	cases := []struct {
		in       string
		wantText string
	}{
		{"prd", ""},
		{"prd status", "status"},
		{"prd run prd_a3f9", "run prd_a3f9"},
	}
	for _, tc := range cases {
		got := Parse(tc.in)
		if got.Type != CmdAutonomous {
			t.Errorf("%q: type = %v want CmdAutonomous", tc.in, got.Type)
		}
		if got.Text != tc.wantText {
			t.Errorf("%q: text = %q want %q", tc.in, got.Text, tc.wantText)
		}
	}
}
