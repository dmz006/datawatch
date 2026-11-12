package main

import "testing"

func TestAutonomousPRDResumeCmd_Shape(t *testing.T) {
	cmd := newAutonomousPRDResumeCmd()
	if cmd.Use != "prd-resume <id>" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.Flags().Lookup("note") == nil {
		t.Error("missing --note flag")
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("expected error without an id")
	}
	if err := cmd.Args(cmd, []string{"prd_a"}); err != nil {
		t.Errorf("one id should be accepted: %v", err)
	}
}

func TestAutonomousPRDResumeCmd_RegisteredUnderAutonomous(t *testing.T) {
	root := newAutonomousCmd()
	for _, c := range root.Commands() {
		if c.Name() == "prd-resume" {
			return
		}
	}
	t.Error("prd-resume not registered under autonomous")
}
