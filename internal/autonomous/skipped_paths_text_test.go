// SkippedPathsCounts and RenderSkippedSnapshotLines: the counts-first line
// the CLI and comm channels print for a task's skipped_snapshot_paths,
// counting per reason (stagingGuardCounts wording) and never naming paths
// on the text line.

package autonomous

import (
	"strings"
	"testing"
)

func TestSkippedPathsCounts(t *testing.T) {
	cases := []struct {
		name  string
		input []SnapshotSkippedPath
		want  string
	}{
		{name: "empty", input: nil, want: ""},
		{
			name:  "single",
			input: []SnapshotSkippedPath{{Path: "a.log", Reason: "ignored"}},
			want:  "ignored=1",
		},
		{
			name: "counts aggregate and sort, paths never appear",
			input: []SnapshotSkippedPath{
				{Path: "b.tmp", Reason: "size"},
				{Path: "a.log", Reason: "ignored"},
				{Path: "c.tmp", Reason: "size"},
			},
			want: "ignored=1, size=2",
		},
		{
			name: "multiple reasons sorted",
			input: []SnapshotSkippedPath{
				{Path: "x", Reason: "never-stage"},
				{Path: "y", Reason: "ignored"},
				{Path: "z", Reason: "outside-plan"},
			},
			want: "ignored=1, never-stage=1, outside-plan=1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SkippedPathsCounts(tc.input)
			if got != tc.want {
				t.Errorf("SkippedPathsCounts = %q, want %q", got, tc.want)
			}
			for _, p := range tc.input {
				if p.Path != "" && strings.Contains(got, p.Path) {
					t.Errorf("line names the path %q: %q", p.Path, got)
				}
			}
		})
	}
}

func TestRenderSkippedSnapshotLines(t *testing.T) {
	t.Run("full view one task with skips", func(t *testing.T) {
		body := `{"stories":[{"tasks":[
			{"id":"t1","skipped_snapshot_paths":[{"path":"a.log","reason":"ignored"},{"path":"b.tmp","reason":"size"}]},
			{"id":"t2"}
		]}]}`
		lines := RenderSkippedSnapshotLines([]byte(body))
		if len(lines) != 1 {
			t.Fatalf("lines = %v, want exactly one", lines)
		}
		if lines[0] != "left unstaged: t1 2 (ignored=1, size=1)" {
			t.Fatalf("line = %q", lines[0])
		}
		if strings.Contains(lines[0], "a.log") || strings.Contains(lines[0], "b.tmp") {
			t.Fatalf("line must stay counts-only, got %q", lines[0])
		}
	})

	t.Run("tasks view flat list", func(t *testing.T) {
		body := `{"tasks":[{"id":"a","skipped_snapshot_paths":[{"path":"p","reason":"size"},{"path":"q","reason":"size"},{"path":"r","reason":"never-stage"}]}]}`
		lines := RenderSkippedSnapshotLines([]byte(body))
		if len(lines) != 1 || lines[0] != "left unstaged: a 3 (never-stage=1, size=2)" {
			t.Fatalf("lines = %v", lines)
		}
	})

	t.Run("no skips gives no lines", func(t *testing.T) {
		if got := RenderSkippedSnapshotLines([]byte(`{"stories":[{"tasks":[{"id":"t1"}]}]}`)); got != nil {
			t.Fatalf("lines = %v, want nil", got)
		}
		if got := RenderSkippedSnapshotLines([]byte(`{}`)); got != nil {
			t.Fatalf("lines = %v, want nil", got)
		}
	})

	t.Run("invalid json gives no lines", func(t *testing.T) {
		if got := RenderSkippedSnapshotLines([]byte("not json")); got != nil {
			t.Fatalf("lines = %v, want nil", got)
		}
	})
}
