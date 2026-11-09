package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestStandardTaskCLIOutput(t *testing.T) {
	body, err := os.ReadFile("testdata/standard_tasks.json")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := renderStandardTasksTable(&buf, body); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d lines:\n%s", len(lines), buf.String())
	}
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != "ID TITLE KIND ENABLED MODE" {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "changelog_entry") || !strings.Contains(lines[1], "true") {
		t.Errorf("row 1 = %q", lines[1])
	}
	if !strings.Contains(lines[2], "final_gate") || !strings.Contains(lines[2], "false") {
		t.Errorf("row 2 = %q", lines[2])
	}

	// --json prints the response as-is; its field names must match the fixture.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"enabled", "tasks"} {
		if _, ok := obj[k]; !ok {
			t.Errorf("missing top-level field %q", k)
		}
	}
	var rows []map[string]any
	if err := json.Unmarshal(obj["tasks"], &rows); err != nil || len(rows) == 0 {
		t.Fatalf("tasks: %v", err)
	}
	for _, k := range []string{"id", "title", "kind", "summary", "offered_when", "enabled", "project_override", "mode"} {
		if _, ok := rows[0][k]; !ok {
			t.Errorf("row missing field %q", k)
		}
	}

	cmd := newAutonomousStandardTasksCmd()
	if cmd.Flags().Lookup("json") == nil {
		t.Error("missing --json flag")
	}
	if !strings.Contains(cmd.Short, "standard tasks the decomposer adds to each story") {
		t.Errorf("help text = %q", cmd.Short)
	}
}
