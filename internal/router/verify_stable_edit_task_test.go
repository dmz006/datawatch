package router

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestVerifyStable_TakeEditTaskTokens(t *testing.T) {
	cases := []struct {
		name       string
		in         []string
		wantRest   []string
		wantScript string
		wantReason string
		wantErr    bool
	}{
		{"script then spec", []string{"verification_script=scripts/v.sh", "tighten", "it"}, []string{"tighten", "it"}, "scripts/v.sh", "", false},
		{"single word reason", []string{"verification_script_reason=docs-only", "x"}, []string{"x"}, "", "docs-only", false},
		{"quoted reason with spaces", []string{"verification_script_reason=\"docs", "only", "change\"", "new", "spec"}, []string{"new", "spec"}, "", "docs only change", false},
		{"quoted single word", []string{`verification_script_reason="docs"`, "x"}, []string{"x"}, "", "docs", false},
		{"both in any order", []string{"verification_script_reason=none", "read_only=true", "verification_script=a/b.sh"}, nil, "a/b.sh", "none", false},
		{"unterminated quote", []string{`verification_script_reason="docs`, "only"}, nil, "", "", true},
		{"unknown key ends the run", []string{"other=1", "verification_script=a.sh"}, []string{"other=1", "verification_script=a.sh"}, "", "", false},
	}
	for _, c := range cases {
		rest, tok, err := takeEditTaskTokens(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v", c.name, err)
			continue
		}
		if c.wantErr {
			continue
		}
		if len(rest) != len(c.wantRest) || (len(rest) > 0 && !reflect.DeepEqual(rest, c.wantRest)) {
			t.Errorf("%s: rest = %v, want %v", c.name, rest, c.wantRest)
		}
		if tok.verificationScript != c.wantScript || tok.verificationScriptReason != c.wantReason {
			t.Errorf("%s: script=%q reason=%q, want %q / %q", c.name, tok.verificationScript, tok.verificationScriptReason, c.wantScript, c.wantReason)
		}
	}
	// add-task's parser does not take the script keys: they stay in the text.
	rest, _, _, err := takeTaskTokens([]string{"verification_script=a.sh", "x"})
	if err != nil || len(rest) != 2 {
		t.Fatalf("takeTaskTokens rest=%v err=%v, want the script token left alone", rest, err)
	}
}

func TestVerifyStable_CommEditTaskParsesBothOptions(t *testing.T) {
	cap := runAutonomousTaskCommand(t, `autonomous edit-task p1 gate verification_script=.datawatch/verify/gate.sh verification_script_reason="gate is report only" read_only=true`)
	if cap.path != "/api/autonomous/prds/p1/edit_task" {
		t.Fatalf("path = %q", cap.path)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(cap.body), &m); err != nil {
		t.Fatalf("body: %v (%q)", err, cap.body)
	}
	if m["task_id"] != "gate" || m["verification_script"] != ".datawatch/verify/gate.sh" ||
		m["verification_script_reason"] != "gate is report only" || m["read_only"] != true {
		t.Fatalf("body = %v", m)
	}
	if _, has := m["new_spec"]; has {
		t.Fatalf("option-only edit sent a new_spec: %v", m)
	}
}

func TestVerifyStable_CommEditTaskReasonAloneAndSpecAfterOptions(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous edit-task p1 t1 verification_script_reason=docs-only")
	var m map[string]any
	if err := json.Unmarshal([]byte(cap.body), &m); err != nil {
		t.Fatalf("body: %v (%q)", err, cap.body)
	}
	if m["verification_script_reason"] != "docs-only" {
		t.Fatalf("body = %v", m)
	}
	if _, has := m["verification_script"]; has {
		t.Fatalf("reason-only edit sent a script: %v", m)
	}
	cap = runAutonomousTaskCommand(t, "autonomous edit-task p1 t1 verification_script=scripts/v.sh reword the spec")
	m = nil
	if err := json.Unmarshal([]byte(cap.body), &m); err != nil {
		t.Fatalf("body: %v (%q)", err, cap.body)
	}
	if m["new_spec"] != "reword the spec" || m["verification_script"] != "scripts/v.sh" {
		t.Fatalf("body = %v", m)
	}
	if _, has := m["verification_script_reason"]; has {
		t.Fatalf("script-only edit sent a reason: %v", m)
	}
}

func TestVerifyStable_CommHelpAndUsageNameBothOptions(t *testing.T) {
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	for _, msg := range []string{"autonomous", "autonomous edit-task p1"} {
		reply := strings.Join(r.HandleTestMessage(msg), "\n")
		for _, want := range []string{"verification_script=<path>", "verification_script_reason=<text"} {
			if !strings.Contains(reply, want) {
				t.Errorf("reply to %q does not name %q: %s", msg, want, reply)
			}
		}
	}
}
