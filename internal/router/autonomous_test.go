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
		{"autonomous approve prd_a3f9", "approve prd_a3f9"},
		{"autonomous plan-review prd_a3f9", "plan-review prd_a3f9"},
		{"autonomous plan-review prd_a3f9 re-check story 2", "plan-review prd_a3f9 re-check story 2"},
		{"autonomous config-set plan_review_mode=block", "config-set plan_review_mode=block"},
		{"autonomous config-set plan_review_backend=anthropic", "config-set plan_review_backend=anthropic"},
		{"autonomous config-set plan_review_model=claude-opus-4-8", "config-set plan_review_model=claude-opus-4-8"},
		{"autonomous config-set smoke_verification_backend=anthropic", "config-set smoke_verification_backend=anthropic"},
		{"autonomous config-set smoke_verification_model=claude-haiku-4-5", "config-set smoke_verification_model=claude-haiku-4-5"},
		{"autonomous config-set plan_review_backend=anthropic plan_review_model=opus plan_review_mode=warn smoke_verification_backend=openai smoke_verification_model=gpt-5.5", "config-set plan_review_backend=anthropic plan_review_model=opus plan_review_mode=warn smoke_verification_backend=openai smoke_verification_model=gpt-5.5"},
		{"autonomous learnings", "learnings"},
		{"autonomous create Add a search-icon toggle to the sessions list", "create Add a search-icon toggle to the sessions list"},
		// BL427 (phase 3) — lifecycle-event subscription verbs.
		{"autonomous subscribe session_started,prd.blocked prd=prd_a3f9", "subscribe session_started,prd.blocked prd=prd_a3f9"},
		{"autonomous subscribe task=task_7 prd_updated", "subscribe task=task_7 prd_updated"},
		{"autonomous unsubscribe sub_a3f9", "unsubscribe sub_a3f9"},
		{"autonomous subscriptions", "subscriptions"},
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
