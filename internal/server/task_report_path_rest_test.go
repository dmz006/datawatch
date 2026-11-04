package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestRESTEditTaskSetsReportPath(t *testing.T) {
	s, id := newWriteCasesTestServer(t, autonomous.PRDNeedsReview)
	body := `{"task_id":"t1","read_only":true,"report_path":".datawatch/reports/gate.txt"}`
	if rec := postTaskWrite(t, s, id, "edit_task", body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	tk := storedTask(t, s, id, "t1")
	if !tk.ReadOnly || tk.ReportPath != ".datawatch/reports/gate.txt" {
		t.Fatalf("read_only=%v report_path=%q, want both set", tk.ReadOnly, tk.ReportPath)
	}
}

func TestRESTEditTaskReportPathRequiresReadOnly(t *testing.T) {
	s, id := newWriteCasesTestServer(t, autonomous.PRDNeedsReview)
	rec := postTaskWrite(t, s, id, "edit_task", `{"task_id":"t1","report_path":".datawatch/reports/gate.txt"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (task is not read-only)", rec.Code)
	}
	if tk := storedTask(t, s, id, "t1"); tk.ReportPath != "" {
		t.Fatalf("rejected edit still set report_path %q", tk.ReportPath)
	}
	// A read_only=false in the same call is not enough either.
	if rec := postTaskWrite(t, s, id, "edit_task", `{"task_id":"t1","read_only":false,"report_path":".datawatch/reports/gate.txt"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRESTEditTaskReportPathInvalidRefused(t *testing.T) {
	s, id := newWriteCasesTestServer(t, autonomous.PRDNeedsReview)
	for _, p := range []string{"../escape.txt", "/etc/passwd", "a/b/../c.txt"} {
		rec := postTaskWrite(t, s, id, "edit_task", `{"task_id":"t1","report_path":"`+p+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %q: status = %d, want 400", p, rec.Code)
		}
	}
	if tk := storedTask(t, s, id, "t1"); tk.ReportPath != "" {
		t.Fatalf("rejected edits still set report_path %q", tk.ReportPath)
	}
}

func TestRESTAddTaskSetsReportPath(t *testing.T) {
	s, id := newWriteCasesTestServer(t, autonomous.PRDNeedsReview)
	body := `{"story_id":"s1","title":"Run the gate","read_only":true,"report_path":".datawatch/reports/gate.txt"}`
	if rec := postTaskWrite(t, s, id, "add_task", body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	v, ok := s.autonomousMgr.GetPRD(id)
	if !ok || v == nil {
		t.Fatal("GetPRD: not found")
	}
	found := false
	for _, tk := range v.(*autonomous.PRD).Story[0].Tasks {
		if tk.Title == "Run the gate" {
			found = tk.ReadOnly && tk.ReportPath == ".datawatch/reports/gate.txt"
		}
	}
	if !found {
		t.Fatal("added task is not read-only with the report path")
	}

	// A report path without read_only is refused and adds nothing.
	rec := postTaskWrite(t, s, id, "add_task", `{"story_id":"s1","title":"Not read-only","report_path":".datawatch/reports/gate.txt"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	v, _ = s.autonomousMgr.GetPRD(id)
	for _, tk := range v.(*autonomous.PRD).Story[0].Tasks {
		if tk.Title == "Not read-only" {
			t.Fatal("a rejected add_task still created the task")
		}
	}
}

func TestOpenAPIDocumentsTaskReportPath(t *testing.T) {
	for _, p := range []string{"web/openapi.yaml"} {
		raw, err := webFS.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		s := string(raw)
		for _, want := range []string{`report_path:`} {
			if !strings.Contains(s, want) {
				t.Errorf("%s lacks %q", p, want)
			}
		}
	}
}
