package main

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const mergeShadowTurnOnBody = `{"projects":[],"turn_on":{"project":"demo","merges":[{"automaton_id":"a1"}],"lines":["Turn-on report for demo. Merging is off.","Matched hand merges in a row: 1 of 5 (not met)."]}}`

func TestMergeShadowCLIReport(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergeShadowTurnOnBody)
	out, err := runLaneFillCmd(t, newAutonomousMergeShadowCmd(), []string{"report"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/merge/shadow" || !strings.Contains(out, "Turn-on report for demo. Merging is off.\nMatched hand merges in a row: 1 of 5 (not met).\n") {
		t.Errorf("path %s output %q", d.path, out)
	}
	// With shadowed hand merges and no earlier comparison, status says so.
	out, err = runLaneFillCmd(t, newAutonomousMergeShadowCmd(), []string{"status"})
	if err != nil || !strings.Contains(out, "1 of 5") || strings.Contains(out, "no merge shadow comparison exists yet") {
		t.Errorf("status %q %v", out, err)
	}
	newLaneFillCLIDaemon(t, http.StatusOK, `{"projects":[]}`)
	out, err = runLaneFillCmd(t, newAutonomousMergeShadowCmd(), []string{"report"})
	if err != nil || !strings.Contains(out, "no turn-on report") {
		t.Errorf("no report: %q %v", out, err)
	}
}

func TestShippedRecordCLIHandChecks(t *testing.T) {
	a := &shippedRecordArgs{id: "a1", commit: "abc1234", source: "merge_step", handChecks: []string{"lint=pass,build=pass", "go-test=fail"}}
	body, err := a.body()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"lint": "pass", "build": "pass", "go-test": "fail"}
	if got := body["hand_checks"]; !reflect.DeepEqual(got, want) {
		t.Errorf("hand_checks = %v", got)
	}
	a.handChecks = []string{"lint=maybe"}
	if _, err := a.body(); err == nil {
		t.Error("an unknown result was sent")
	}
	a.handChecks = nil
	if body, _ := a.body(); body["hand_checks"] != nil {
		t.Errorf("hand_checks sent with none given: %v", body)
	}
	cmd := newAutonomousShippedCmd()
	for _, sub := range cmd.Commands() {
		if sub.Name() == "record" && sub.Flags().Lookup("hand-check") == nil {
			t.Error("record has no --hand-check flag")
		}
	}
}
