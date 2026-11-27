package main

import (
	"testing"
)

func TestAcceptanceCLIBody(t *testing.T) {
	t.Setenv("DATAWATCH_SESSION_ID", "")
	if b := acceptanceCLIBody("r1", ""); b["via"] != "cli" || b["via_session"] != nil || b["words"] != nil {
		t.Fatalf("%v", b)
	}
	t.Setenv("DATAWATCH_SESSION_ID", "w1")
	if b := acceptanceCLIBody("r1", "said"); b["via_session"] != "w1" || b["words"] != "said" {
		t.Fatalf("%v", b)
	}
}

func TestAcceptanceCLIVerbs(t *testing.T) {
	got := map[string]bool{}
	for _, c := range newAcceptanceCmd().Commands() {
		got[c.Name()] = true
	}
	for _, verb := range []string{"list", "show", "edit", "freeze", "confirm", "accept", "report", "catalog"} {
		if !got[verb] {
			t.Errorf("missing %s", verb)
		}
	}
}
