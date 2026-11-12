// autonomous get on the comm channel appends one counts-first line per task
// with a non-empty skipped_snapshot_paths ("left unstaged: <task> N
// (reason=count, ...)") to the raw JSON reply; the detailed path list stays
// in the JSON.

package router

import (
	"net/http"
	"strings"
	"testing"
)

const skippedSnapshotCommPRD = `{"id":"prd1","stories":[{"tasks":[
  {"id":"t1","skipped_snapshot_paths":[
     {"path":"a.log","reason":"ignored"},
     {"path":"b.tmp","reason":"size"},
     {"path":"c.tmp","reason":"size"}
  ]},
  {"id":"t2"}
]}]}`

func TestAutonomousGetShowsSkippedSnapshotLines(t *testing.T) {
	var gotPath string
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(skippedSnapshotCommPRD))
	})
	r := setupPeerRouter(t, port)

	got := strings.Join(r.HandleTestMessage("autonomous get prd1"), "\n")

	if gotPath != "/api/autonomous/prds/prd1" {
		t.Fatalf("daemon path = %q, want /api/autonomous/prds/prd1", gotPath)
	}
	// The raw JSON keeps the detailed path list.
	if !strings.Contains(got, `"skipped_snapshot_paths"`) || !strings.Contains(got, `"a.log"`) {
		t.Errorf("reply lacks the skipped_snapshot_paths detail: %s", got)
	}
	// The compact counts-first line shows for the task that skipped files.
	if !strings.Contains(got, "left unstaged: t1 3 (ignored=1, size=2)") {
		t.Errorf("missing the counts-first line: %s", got)
	}
	// ...and not for the task that skipped nothing.
	if strings.Contains(got, "left unstaged: t2") {
		t.Errorf("t2 skipped nothing, no line expected: %s", got)
	}
}

func TestAutonomousGetNoSkippedPathsNoLine(t *testing.T) {
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"prd1","stories":[{"tasks":[{"id":"t1"}]}]}`))
	})
	r := setupPeerRouter(t, port)

	got := strings.Join(r.HandleTestMessage("autonomous show prd1"), "\n")
	if strings.Contains(got, "left unstaged:") {
		t.Errorf("no task skipped anything, no line expected: %s", got)
	}
}
