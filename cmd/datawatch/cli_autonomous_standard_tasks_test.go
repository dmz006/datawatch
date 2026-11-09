package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// standardTasksFixturePath is the shared expected list every surface reads.
const standardTasksFixturePath = "../../testdata/standard-tasks/expected.json"

func TestStandardTaskCLIOutput(t *testing.T) {
	body, err := os.ReadFile(standardTasksFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Tasks []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Kind    string `json:"kind"`
			Enabled bool   `json:"enabled"`
			Mode    string `json:"mode"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(body, &want); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := renderStandardTasksTable(&buf, body); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != len(want.Tasks)+1 {
		t.Fatalf("want header + %d rows, got %d lines:\n%s", len(want.Tasks), len(lines), buf.String())
	}
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != "ID TITLE KIND ENABLED MODE" {
		t.Errorf("header = %q", lines[0])
	}
	// The table prints id, title, kind, enabled and mode; each row must carry them all.
	for i, w := range want.Tasks {
		line := lines[i+1]
		for _, field := range []string{w.ID, w.Title, w.Kind, fmt.Sprintf("%t", w.Enabled), w.Mode} {
			if !strings.Contains(line, field) {
				t.Errorf("row %d = %q, missing %q", i, line, field)
			}
		}
		if !strings.HasPrefix(line, w.ID+" ") {
			t.Errorf("row %d = %q, want id %q first", i, line, w.ID)
		}
	}

	// --json prints the response as-is; its field names must match the fixture.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(obj["tasks"], &rows); err != nil || len(rows) == 0 {
		t.Fatalf("tasks: %v", err)
	}
	for _, k := range []string{"id", "title", "kind", "enabled", "mode"} {
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
