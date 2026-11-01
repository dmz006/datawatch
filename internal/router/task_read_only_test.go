package router

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTakeReadOnlyToken(t *testing.T) {
	cases := []struct {
		in       []string
		wantRest []string
		wantRO   string // "", "true", "false"
		wantErr  bool
	}{
		{[]string{"read_only=true", "new", "spec"}, []string{"new", "spec"}, "true", false},
		{[]string{"read-only=false"}, []string{}, "false", false},
		{[]string{"new", "read_only=true"}, []string{"new", "read_only=true"}, "", false},
		{[]string{"read_only=maybe", "x"}, nil, "", true},
		{nil, nil, "", false},
	}
	for _, c := range cases {
		rest, ro, err := takeReadOnlyToken(c.in)
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
		if got != c.wantRO || len(rest) != len(c.wantRest) {
			t.Errorf("%v: rest=%v ro=%q, want rest=%v ro=%q", c.in, rest, got, c.wantRest, c.wantRO)
		}
	}
}

func runAutonomousTaskCommand(t *testing.T, msg string) capturedCreateRequest {
	t.Helper()
	cap := &capturedCreateRequest{}
	port, closeFn := fakeDaemonCreate(t, cap)
	defer closeFn()
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetWebPort(port)
	r.HandleTestMessage(msg)
	return *cap
}

func TestCommEditTaskSendsReadOnly(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous edit-task p1 t1 read_only=true")
	if cap.method != http.MethodPost || cap.path != "/api/autonomous/prds/p1/edit_task" {
		t.Fatalf("request = %s %s", cap.method, cap.path)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(cap.body), &m); err != nil {
		t.Fatalf("body: %v (%q)", err, cap.body)
	}
	if m["task_id"] != "t1" || m["read_only"] != true {
		t.Fatalf("body = %v, want task_id t1 and read_only true", m)
	}
	if _, has := m["new_spec"]; has {
		t.Fatalf("read_only-only edit sent a new_spec: %v", m)
	}
}

func TestCommEditTaskSpecAndReadOnlyTogether(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous edit-task p1 t1 read_only=false run the whole suite")
	var m map[string]any
	_ = json.Unmarshal([]byte(cap.body), &m)
	if m["read_only"] != false || m["new_spec"] != "run the whole suite" {
		t.Fatalf("body = %v", m)
	}
}

func TestCommEditTaskSpecOnlyOmitsReadOnly(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous edit-task p1 t1 reworded spec text")
	var m map[string]any
	_ = json.Unmarshal([]byte(cap.body), &m)
	if _, has := m["read_only"]; has {
		t.Fatalf("spec-only edit sent read_only: %v", m)
	}
	if m["new_spec"] != "reworded spec text" {
		t.Fatalf("body = %v", m)
	}
}

func TestCommAddTaskSendsReadOnly(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous add-task p1 s1 read_only=true Run the gate")
	if cap.path != "/api/autonomous/prds/p1/add_task" {
		t.Fatalf("path = %q", cap.path)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(cap.body), &m)
	if m["title"] != "Run the gate" || m["read_only"] != true {
		t.Fatalf("body = %v", m)
	}
}
