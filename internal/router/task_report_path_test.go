package router

import (
	"encoding/json"
	"strings"
	"testing"
)

const routerTestReportPath = ".datawatch/reports/gate.md"

func TestTakeTaskTokensParsesReportPath(t *testing.T) {
	cases := []struct {
		in       []string
		wantRest []string
		wantRO   string // "", "true", "false"
		wantRP   string
		wantErr  bool
	}{
		{
			[]string{"read_only=true", "report_path=" + routerTestReportPath, "new", "spec"},
			[]string{"new", "spec"}, "true", routerTestReportPath, false,
		},
		{
			[]string{"report_path=" + routerTestReportPath, "read_only=false", "x"},
			[]string{"x"}, "false", routerTestReportPath, false,
		},
		{
			[]string{"read-only=true", "x"},
			[]string{"x"}, "true", "", false,
		},
		{[]string{"read_only=maybe"}, nil, "", "", true},
		{nil, nil, "", "", false},
	}
	for _, c := range cases {
		rest, ro, rp, err := takeTaskTokens(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%v: err = %v", c.in, err)
			continue
		}
		if c.wantErr {
			continue
		}
		got := ""
		if ro != nil {
			got = map[bool]string{true: "true", false: "false"}[*ro]
		}
		if got != c.wantRO || rp != c.wantRP || len(rest) != len(c.wantRest) {
			t.Errorf("%v: rest=%v ro=%q rp=%q, want rest=%v ro=%q rp=%q", c.in, rest, got, rp, c.wantRest, c.wantRO, c.wantRP)
		}
	}
}

// takeReadOnlyToken is a thin wrapper over takeTaskTokens, so a
// report_path token is consumed too and only the positional words remain.
func TestTakeReadOnlyTokenConsumesReportPathToken(t *testing.T) {
	rest, ro, err := takeReadOnlyToken([]string{"read_only=true", "report_path=" + routerTestReportPath, "new", "spec"})
	if err != nil || ro == nil || !*ro || len(rest) != 2 {
		t.Fatalf("rest=%v ro=%v err=%v, want ro=true rest=[new spec]", rest, ro, err)
	}
}

func TestCommEditTaskSendsReportPath(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous edit-task p1 t1 read_only=true report_path="+routerTestReportPath)
	var m map[string]any
	if err := json.Unmarshal([]byte(cap.body), &m); err != nil {
		t.Fatalf("body: %v (%q)", err, cap.body)
	}
	if m["task_id"] != "t1" || m["read_only"] != true || m["report_path"] != routerTestReportPath {
		t.Fatalf("body = %v, want task_id t1, read_only true and the report_path", m)
	}
	if _, has := m["new_spec"]; has {
		t.Fatalf("flag-only edit sent a new_spec: %v", m)
	}
}

func TestCommEditTaskReportPathBeforeReadOnly(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous edit-task p1 t1 report_path="+routerTestReportPath+" read_only=true")
	var m map[string]any
	_ = json.Unmarshal([]byte(cap.body), &m)
	if m["read_only"] != true || m["report_path"] != routerTestReportPath {
		t.Fatalf("body = %v", m)
	}
}

func TestCommEditTaskSpecPlusTokens(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous edit-task p1 t1 report_path="+routerTestReportPath+" tighten the acceptance bar")
	var m map[string]any
	_ = json.Unmarshal([]byte(cap.body), &m)
	if m["new_spec"] != "tighten the acceptance bar" || m["report_path"] != routerTestReportPath {
		t.Fatalf("body = %v", m)
	}
	if _, has := m["read_only"]; has {
		t.Fatalf("no read_only token was given but the body has one: %v", m)
	}
}

func TestCommAddTaskSendsReportPath(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous add-task p1 s1 read_only=true report_path="+routerTestReportPath+" Run the gate")
	if cap.path != "/api/autonomous/prds/p1/add_task" {
		t.Fatalf("path = %q", cap.path)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(cap.body), &m)
	if m["title"] != "Run the gate" || m["read_only"] != true || m["report_path"] != routerTestReportPath {
		t.Fatalf("body = %v, want the title, read_only true and the report_path", m)
	}
}

func TestCommAddTaskReportPathTokenOnlyNoTitleFails(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous add-task p1 s1 read_only=true report_path="+routerTestReportPath)
	if cap.path != "" && strings.Contains(cap.path, "add_task") {
		t.Fatalf("a title-less add was POSTed: %s %s", cap.method, cap.path)
	}
}
