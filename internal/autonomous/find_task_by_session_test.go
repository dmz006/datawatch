// Finding the task of a session by either id form: the files-touched hook passes
// the full id the lister reports, while the task stores only the short id.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestFindTaskBySessionID
package autonomous

import "testing"

func TestFindTaskBySessionIDAcceptsFullID(t *testing.T) {
	m, prd, _ := adoptReconcileSetup(t, "a1b2")
	task := prd.Story[0].Tasks[0]

	gotPRD, gotTask := m.FindTaskBySessionID("datawatch-box-a1b2")
	if gotPRD != prd.ID || gotTask != task.ID {
		t.Fatalf("full id: got (%q, %q), want (%q, %q)", gotPRD, gotTask, prd.ID, task.ID)
	}
	if _, gotTask := m.FindTaskBySessionID("a1b2"); gotTask != task.ID {
		t.Fatalf("short id: got task %q, want %q", gotTask, task.ID)
	}
	if p, tk := m.FindTaskBySessionID("datawatch-box-zz99"); p != "" || tk != "" {
		t.Fatalf("unrelated id matched (%q, %q)", p, tk)
	}
}
