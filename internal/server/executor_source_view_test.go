package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// TestSessionAutomatonExecutorSource covers the session status block
// carrying the level that supplied the task's execution backend.
func TestSessionAutomatonExecutorSource(t *testing.T) {
	prd := automatonStatusPRD("spec")
	prd.Story[0].Tasks[0].ExecutorSource = "automation"
	api := &automatonStatusAPI{prd: prd}
	s := automatonStatusServer(t, api, &session.Session{
		ID: "aa", FullID: "h-aa", PRDID: "prd1", TaskID: "t1",
	})
	a, ok := getSessionStatus(t, s, "h-aa")["automaton"].(map[string]any)
	if !ok {
		t.Fatal("automaton block missing")
	}
	if a["executor_source"] != "automation" {
		t.Errorf("executor_source=%v want automation", a["executor_source"])
	}

	s2 := automatonStatusServer(t, api, &session.Session{
		ID: "bb", FullID: "h-bb", PRDID: "prd1", TaskID: "t2",
	})
	a2, ok := getSessionStatus(t, s2, "h-bb")["automaton"].(map[string]any)
	if !ok {
		t.Fatal("automaton block missing for task without executor_source")
	}
	if _, has := a2["executor_source"]; has {
		t.Errorf("executor_source present for task with none: %v", a2["executor_source"])
	}
}
