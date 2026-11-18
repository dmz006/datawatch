package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// A finished Automaton whose worktree holds only the completion marker has no
// work to hold: no question, no hold, and it completes.
func TestUncommittedRollupMarkerOnlyCompletes(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold,
		map[string]string{completionMarkerFile: "??"})

	got := r.run(t)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s; want completed", got.Status)
	}
	if got.UncommittedWork != nil {
		t.Errorf("a marker-only worktree recorded a hold: %+v", got.UncommittedWork)
	}
	if qs := r.store.List(QuestionFilter{}); len(qs) != 0 {
		t.Errorf("questions = %d; want 0: %+v", len(qs), qs)
	}
	if n := len(r.decisions(uncommittedHeldDecisionKind)); n != 0 {
		t.Errorf("held decisions = %d; want 0", n)
	}
	if r.git.statusCount() == 0 {
		t.Error("the worktree was never read")
	}
}

// A marker next to a real untracked file: only the real file is held, the
// question lists it, does not list the marker as a file, and says the marker
// was skipped.
func TestUncommittedRollupMarkerWithRealFileHoldsOnlyRealFile(t *testing.T) {
	const real = "notes/result.txt"
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold,
		map[string]string{completionMarkerFile: "??", real: "??"})

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	if got.UncommittedWork == nil || got.UncommittedWork.Kind != UncommittedHoldKindWorktree {
		t.Fatalf("hold = %+v; want a worktree hold", got.UncommittedWork)
	}
	if files := got.UncommittedWork.Files; len(files) != 1 || files[0] != real {
		t.Errorf("hold files = %v; want [%s]", files, real)
	}

	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %d; want exactly 1", len(qs))
	}
	text := qs[0].Text
	if !strings.Contains(text, real) {
		t.Errorf("question does not list %q:\n%s", real, text)
	}
	for _, line := range strings.Split(text, "\n") {
		entry := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-*• "))
		if entry == completionMarkerFile || strings.HasPrefix(entry, completionMarkerFile+" ") {
			t.Errorf("question lists the marker as a file line: %q\n%s", line, text)
		}
	}
	if !strings.Contains(text, "Skipped the Automaton marker file "+completionMarkerFile) {
		t.Errorf("question does not say the Automaton marker file was skipped:\n%s", text)
	}
}

// The rig's worktree is a real temporary directory, so the finish path's
// marker removal is visible on disk: the marker goes, a real file stays.
func TestUncommittedRollupMarkerRemovedFromDiskRealFileKept(t *testing.T) {
	const real = "notes/result.txt"
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold,
		map[string]string{completionMarkerFile: "??", real: "??"})

	dir := r.prd.ProjectDir
	if dir == "" {
		t.Fatal("the rig has no worktree directory")
	}
	markerPath := filepath.Join(dir, completionMarkerFile)
	realPath := filepath.Join(dir, filepath.FromSlash(real))
	if err := os.MkdirAll(filepath.Dir(realPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte("done\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	if err := os.WriteFile(realPath, []byte("result\n"), 0o644); err != nil {
		t.Fatalf("write real file: %v", err)
	}

	r.run(t)

	if _, err := os.Lstat(markerPath); !os.IsNotExist(err) {
		t.Errorf("the marker file is still on disk (err = %v)", err)
	}
	if _, err := os.Stat(realPath); err != nil {
		t.Errorf("the real file was not kept: %v", err)
	}
}
