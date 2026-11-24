package main

import "testing"

func TestResetTaskActor(t *testing.T) {
	t.Setenv("DATAWATCH_SESSION_ID", "")
	if got := resetTaskActor(); got != "operator" {
		t.Fatalf("outside a session = %q, want operator", got)
	}
	t.Setenv("DATAWATCH_SESSION_ID", "host-sess-3")
	if got := resetTaskActor(); got != "session:host-sess-3" {
		t.Fatalf("inside a session = %q, want session:host-sess-3", got)
	}
}

func TestPRDResetTaskCmdFlags(t *testing.T) {
	cmd := newAutonomousPRDResetTaskCmd()
	for _, f := range []string{"force", "clean-restart"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Errorf("prd-reset-task lacks --%s", f)
		}
	}
	if err := cmd.Args(cmd, []string{"only-one"}); err == nil {
		t.Errorf("prd-reset-task must take an Automaton id and a task id")
	}
}
