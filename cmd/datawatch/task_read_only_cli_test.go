package main

import (
	"encoding/json"
	"testing"
)

func decodeBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return m
}

func TestPRDEditTaskCLIBodyReadOnlyOnlyWhenFlagGiven(t *testing.T) {
	cmd := newAutonomousPRDEditTaskCmd()
	if _, err := prdEditTaskCLIBody(cmd, "t1", "", false, ""); err == nil {
		t.Fatal("expected an error when neither --spec nor --read-only is given")
	}
	if err := cmd.Flags().Set("read-only", "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	raw, err := prdEditTaskCLIBody(cmd, "t1", "", true, "")
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	m := decodeBody(t, raw)
	if m["read_only"] != true || m["task_id"] != "t1" {
		t.Fatalf("body = %v", m)
	}
	if _, has := m["new_spec"]; has {
		t.Fatalf("read-only-only edit sent new_spec: %v", m)
	}
}

func TestPRDEditTaskCLIBodyClearAndSpecOnly(t *testing.T) {
	clear := newAutonomousPRDEditTaskCmd()
	_ = clear.Flags().Set("read-only", "false")
	raw, err := prdEditTaskCLIBody(clear, "t1", "new spec", false, "")
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	m := decodeBody(t, raw)
	if v, ok := m["read_only"].(bool); !ok || v || m["new_spec"] != "new spec" {
		t.Fatalf("explicit --read-only=false body = %v", m)
	}

	specOnly := newAutonomousPRDEditTaskCmd()
	raw, err = prdEditTaskCLIBody(specOnly, "t1", "new spec", false, "")
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	if _, has := decodeBody(t, raw)["read_only"]; has {
		t.Fatal("spec-only edit sent read_only")
	}
}

func TestPRDEditTaskCLIBodyReportPathOnlyWhenGiven(t *testing.T) {
	cmd := newAutonomousPRDEditTaskCmd()
	_ = cmd.Flags().Set("read-only", "true")
	raw, err := prdEditTaskCLIBody(cmd, "t1", "", true, ".datawatch/reports/gate.txt")
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	m := decodeBody(t, raw)
	if m["read_only"] != true || m["report_path"] != ".datawatch/reports/gate.txt" {
		t.Fatalf("body = %v", m)
	}
	if _, has := m["new_spec"]; has {
		t.Fatalf("sent new_spec: %v", m)
	}

	notGiven := newAutonomousPRDEditTaskCmd()
	_ = notGiven.Flags().Set("read-only", "true")
	raw, err = prdEditTaskCLIBody(notGiven, "t1", "", true, "")
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	if _, has := decodeBody(t, raw)["report_path"]; has {
		t.Fatal("report_path sent although --report-path was not given")
	}
}

func TestPRDEditTaskCLIBodyReportPathAloneCountsAsEdit(t *testing.T) {
	cmd := newAutonomousPRDEditTaskCmd()
	raw, err := prdEditTaskCLIBody(cmd, "t1", "", false, ".datawatch/reports/gate.txt")
	if err != nil {
		t.Fatalf("a report-path-only edit must not be refused: %v", err)
	}
	m := decodeBody(t, raw)
	if m["report_path"] != ".datawatch/reports/gate.txt" {
		t.Fatalf("body = %v", m)
	}
	if _, has := m["read_only"]; has {
		t.Fatalf("read_only sent although --read-only was not given: %v", m)
	}
}

func TestPRDAddTaskCLIBodyReportPathOnlyWhenGiven(t *testing.T) {
	cmd := newAutonomousPRDAddTaskCmd()
	raw, err := prdAddTaskCLIBody(cmd, "s1", "Run the gate", "", "", true, ".datawatch/reports/gate.txt")
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	m := decodeBody(t, raw)
	if m["read_only"] != true || m["report_path"] != ".datawatch/reports/gate.txt" {
		t.Fatalf("body = %v", m)
	}

	notGiven := newAutonomousPRDAddTaskCmd()
	raw, err = prdAddTaskCLIBody(notGiven, "s1", "Run the gate", "", "", false, "")
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	for _, k := range []string{"read_only", "report_path"} {
		if _, has := decodeBody(t, raw)[k]; has {
			t.Fatalf("%s sent although the flag was not given: %v", k, m)
		}
	}
}

func TestPRDTaskCommandsDeclareReadOnlyFlag(t *testing.T) {
	if newAutonomousPRDEditTaskCmd().Flags().Lookup("read-only") == nil {
		t.Error("prd-edit-task has no --read-only flag")
	}
	if newAutonomousPRDAddTaskCmd().Flags().Lookup("read-only") == nil {
		t.Error("prd-add-task has no --read-only flag")
	}
}

func TestPRDTaskCommandsDeclareReportPathFlag(t *testing.T) {
	if newAutonomousPRDEditTaskCmd().Flags().Lookup("report-path") == nil {
		t.Error("prd-edit-task has no --report-path flag")
	}
	if newAutonomousPRDAddTaskCmd().Flags().Lookup("report-path") == nil {
		t.Error("prd-add-task has no --report-path flag")
	}
}
