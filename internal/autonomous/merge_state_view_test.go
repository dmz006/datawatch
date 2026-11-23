// Tests for the derived merge_state view: a completed or archived record whose
// branch is ahead of the base branch reads finished_not_merged, a merged or
// active record reads empty, and the read never moves the record. Real git on a
// temporary base repository.
package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const amgBranch = "automaton/guard-work"

// amgGit runs one git command in dir and fails the test on error, returning
// the trimmed combined output.
func amgGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// amgBaseRepo creates an on-disk base repository with a main branch and one
// committed file.
func amgBaseRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	amgGit(t, dir, "init", "-q", "-b", "main")
	amgGit(t, dir, "config", "user.email", "amg@test")
	amgGit(t, dir, "config", "user.name", "Amg")
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	amgGit(t, dir, "add", "-A")
	amgGit(t, dir, "commit", "-q", "-m", "base")
	return dir
}

// amgAppend appends line to a and commits it on the checked-out branch.
func amgAppend(t *testing.T, dir, line string) {
	t.Helper()
	p := filepath.Join(dir, "a")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(b, []byte(line)...), 0o644); err != nil {
		t.Fatal(err)
	}
	amgGit(t, dir, "add", "-A")
	amgGit(t, dir, "commit", "-q", "-m", "add work")
}

// amgBranchAhead creates the work branch with one content commit.
func amgBranchAhead(t *testing.T, dir string) {
	t.Helper()
	amgGit(t, dir, "checkout", "-q", "-b", amgBranch)
	amgAppend(t, dir, "work\n")
	amgGit(t, dir, "checkout", "-q", "main")
}

// amgRig builds a Manager whose base repository is base, with one record of
// the given status whose branch is amgBranch.
func amgRig(t *testing.T, base string, status PRDStatus) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.Config()
	cfg.WorktreeBaseRepo = base
	m.SetConfig(cfg)
	p, err := m.store.CreatePRD("amg", "/p", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Status = status
	p.Git.Branch = amgBranch
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return m, p
}

func TestMergeStateViewUnmergedCompleted(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDCompleted)
	if got := m.AutomatonMergeState(p); got != MergeStateFinishedNotMerged {
		t.Errorf("AutomatonMergeState = %q, want %q", got, MergeStateFinishedNotMerged)
	}
	// The record is not mutated by the read.
	p2, _ := m.store.GetPRD(p.ID)
	if p2.Status != PRDCompleted {
		t.Errorf("view read moved the record to %q", p2.Status)
	}
}

func TestMergeStateViewMergedCompleted(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	amgGit(t, base, "merge", "-q", amgBranch)
	m, p := amgRig(t, base, PRDCompleted)
	if got := m.AutomatonMergeState(p); got != "" {
		t.Errorf("AutomatonMergeState = %q, want empty for a merged record", got)
	}
}

func TestMergeStateViewActiveAndCancelled(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDActive)
	p2, _ := m.store.GetPRD(p.ID)
	if got := m.AutomatonMergeState(p2); got != "" {
		t.Errorf("active record: AutomatonMergeState = %q, want empty", got)
	}
	m2, p3 := amgRig(t, base, PRDCancelled)
	if got := m2.AutomatonMergeState(p3); got != "" {
		t.Errorf("cancelled record: AutomatonMergeState = %q, want empty", got)
	}
}

func TestMergeStateViewArchivedStillShows(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDCancelled)
	// Operator archives the record while the work is still out.
	if _, err := m.Archive(p.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	p2, _ := m.store.GetPRD(p.ID)
	if got := m.AutomatonMergeState(p2); got != MergeStateFinishedNotMerged {
		t.Errorf("archived-with-unmerged-work: AutomatonMergeState = %q, want %q", got, MergeStateFinishedNotMerged)
	}
}

func TestMergeStateViewCacheDoesNotStaleAcrossStatus(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDCompleted)
	if got := m.AutomatonMergeState(p); got != MergeStateFinishedNotMerged {
		t.Fatalf("first read = %q, want %q", got, MergeStateFinishedNotMerged)
	}
	// A status change must not serve the cached completed value.
	p2, _ := m.store.GetPRD(p.ID)
	p2.Status = PRDCancelled
	m.store.SavePRD(p2)
	if got := m.AutomatonMergeState(p2); got != "" {
		t.Errorf("cached value leaked across a status change: %q, want empty", got)
	}
}

// The one-time un-archive repair judges "unmerged" with the same git check as
// the archive guard: an archived record (archived by the supervisor loop) whose
// branch is ahead is a candidate, one whose work is on the base is not.
func TestUnarchiveCandidatesUseBranchCheck(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDArchived)
	p.Decisions = append(p.Decisions, Decision{Kind: "archive", Actor: "supervisor_loop"})
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	got := ManagerUnarchiveOps{M: m}.Candidates(t.Context())
	if len(got) != 1 || got[0].ID != p.ID || got[0].Unsafe || got[0].Commits != 1 || got[0].RestoreTo != PRDCompleted {
		t.Fatalf("unmerged archived record: candidates = %+v, want one restorable to completed with 1 commit", got)
	}

	amgGit(t, base, "merge", "-q", amgBranch)
	m2, p2 := amgRig(t, base, PRDArchived)
	p2.Decisions = append(p2.Decisions, Decision{Kind: "archive", Actor: "supervisor_loop"})
	if err := m2.store.SavePRD(p2); err != nil {
		t.Fatal(err)
	}
	if got := (ManagerUnarchiveOps{M: m2}).Candidates(t.Context()); len(got) != 0 {
		t.Fatalf("merged archived record: candidates = %+v, want none", got)
	}
}
