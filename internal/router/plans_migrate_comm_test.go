package router

import (
	"strings"
	"testing"
)

func TestPlanMigrateText(t *testing.T) {
	preview := `{"note":"n","result":{"applied":false,"would_create":[{"title":"A"}],"would_link":[{"title":"A"}],
"skipped_existing":[],"conflicts":[{"title":"B  b","reason":"title taken","held_by":"record p1 \"B\"","would_change":"create the plan \"B\""}]}}`
	got := planMigrateText(preview)
	for _, want := range []string{"Preview: would create 1, would link 1, already present 0, history without an Automaton 0, needs review 0, conflicts 1",
		"conflict: B b — title taken; held by: record p1 \"B\"; would have changed: create the plan \"B\"", "autonomous plan-migrate apply"} {
		if !strings.Contains(got, want) {
			t.Errorf("preview text missing %q:\n%s", want, got)
		}
	}
	done := planMigrateText(`{"result":{"applied":true,"would_create":[{"title":"A"}]}}`)
	if !strings.Contains(done, "Migration: created 1") || strings.Contains(done, "plan-migrate apply") {
		t.Errorf("applied text = %s", done)
	}
	if !strings.Contains(planMigrateText("not json"), "cannot read") {
		t.Error("bad JSON must say so")
	}
}
