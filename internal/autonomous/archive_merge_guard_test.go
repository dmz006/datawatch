// Merged-branch guard tests: Manager.Archive refuses a completed record
// whose work branch has not landed (typed error naming the branch and the
// commit count), archives it once the work is on base — merged, rebase-
// equivalent, or already dropped — keeps it on git failure, and lets the
// operator archive either way; the close step keeps an unmerged completed
// record, and the derived merge_state view surfaces the state without
// persisting it.
package autonomous

import (
	"errors"
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

// amgStatus reads the record's stored status, failing the test when the
// record is gone entirely (which would hide a missing record as archived).
func amgStatus(t *testing.T, m *Manager, id string) PRDStatus {
	t.Helper()
	p, ok := m.store.GetPRD(id)
	if !ok {
		t.Fatalf("record %s no longer in the store", id)
	}
	return p.Status
}

func TestArchiveUnmergedRefusedForAutomatedActor(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDCompleted)

	out, err := m.Archive(p.ID, "supervisor-loop")
	if err == nil {
		t.Fatalf("archived an unmerged completed record; out=%v", out)
	}
	if out != nil {
		t.Errorf("refusal returned a record: %v", out)
	}
	if !errors.Is(err, ErrArchiveUnmerged) {
		t.Fatalf("err = %v, want ErrArchiveUnmerged", err)
	}
	var ue *ArchiveUnmergedError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v, want *ArchiveUnmergedError", err)
	}
	if ue.Branch != amgBranch {
		t.Errorf("Branch = %q, want %q", ue.Branch, amgBranch)
	}
	if ue.Commits < 1 {
		t.Errorf("Commits = %d, want at least the one unmerged commit", ue.Commits)
	}
	if !strings.Contains(err.Error(), amgBranch) || !strings.Contains(err.Error(), "commit") {
		t.Errorf("refusal message %q does not name the branch and the commit count", err)
	}
	if st := amgStatus(t, m, p.ID); st != PRDCompleted {
		t.Errorf("status = %q, want still completed", st)
	}
}

func TestArchiveUnmergedAllowedAfterMergeLands(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDCompleted)

	// The merge step lands the branch on main.
	amgGit(t, base, "merge", "-q", amgBranch)
	if out, err := m.Archive(p.ID, "supervisor-loop"); err != nil {
		t.Fatalf("archive after the merge landed: %v (out=%v)", err, out)
	}
	if st := amgStatus(t, m, p.ID); st != PRDArchived {
		t.Errorf("status = %q, want archived", st)
	}
}

func TestArchiveUnmergedAllowedAfterRebaseEquivalentLanding(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	// The identical patch lands on main from a different history — the
	// rebase/squash case: different shas, the same change.
	amgAppend(t, base, "work\n")
	m, p := amgRig(t, base, PRDCompleted)

	if out, err := m.Archive(p.ID, "supervisor-loop"); err != nil {
		t.Fatalf("archive after rebase-equivalent landing: %v (out=%v)", err, out)
	}
	if st := amgStatus(t, m, p.ID); st != PRDArchived {
		t.Errorf("status = %q, want archived", st)
	}
}

func TestArchiveUnmergedAllowedWhenBranchDropped(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDCompleted)

	// The branch was deleted out from under the record: no work left to
	// wait for, so the record may go.
	amgGit(t, base, "branch", "-D", amgBranch)
	if out, err := m.Archive(p.ID, "supervisor-loop"); err != nil {
		t.Fatalf("archive with the branch dropped: %v (out=%v)", err, out)
	}
	if st := amgStatus(t, m, p.ID); st != PRDArchived {
		t.Errorf("status = %q, want archived", st)
	}
}

func TestArchiveUnmergedGitErrorKeeps(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)

	baseResolve := map[string]scriptStep{
		joinArgs("remote"): {out: "origin\n"},
		joinArgs("symbolic-ref", "refs/remotes/origin/HEAD"): {err: errors.New("no symbolic ref")},
		joinArgs("symbolic-ref", "--short", "HEAD"):          {err: errors.New("no symbolic ref")},
	}
	withBase := func(m map[string]scriptStep) map[string]scriptStep {
		for k, v := range baseResolve {
			m[k] = v
		}
		return m
	}
	cases := map[string]func() (*memberProofMergeGit, map[string]scriptStep){
		"branch ref query fails": func() (*memberProofMergeGit, map[string]scriptStep) {
			s := withBase(map[string]scriptStep{
				joinArgs("for-each-ref", "--format=%(refname)", "refs/heads/"+amgBranch): {err: errors.New("boom")},
			})
			return newMemberProofMergeGit(t, s), s
		},
		"compare fails": func() (*memberProofMergeGit, map[string]scriptStep) {
			s := withBase(map[string]scriptStep{
				joinArgs("for-each-ref", "--format=%(refname)", "refs/heads/"+amgBranch): {out: "refs/heads/" + amgBranch + "\n"},
				joinArgs("rev-list", "--count", "main.."+amgBranch):                      {err: errors.New("boom")},
			})
			return newMemberProofMergeGit(t, s), s
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			m2, p2 := amgRig(t, base, PRDCompleted)
			git, _ := mk()
			m2.SetArchiveGuardGit(git)
			_, err := m2.Archive(p2.ID, "supervisor-loop")
			if err == nil {
				t.Fatalf("archived with an undetermined merge state")
			}
			if !errors.Is(err, ErrArchiveMergeUnknown) {
				t.Errorf("err = %v, want ErrArchiveMergeUnknown", err)
			}
			if st := amgStatus(t, m2, p2.ID); st != PRDCompleted {
				t.Errorf("status = %q, want still completed", st)
			}
		})
	}
}

func TestArchiveUnmergedEmptyBaseRepoKeeps(t *testing.T) {
	m, p := amgRig(t, "", PRDCompleted)
	if _, err := m.Archive(p.ID, "supervisor-loop"); err == nil {
		t.Fatalf("archived with no base repository to check against")
	}
	if st := amgStatus(t, m, p.ID); st != PRDCompleted {
		t.Errorf("status = %q, want still completed", st)
	}
}

func TestArchiveUnmergedNoBranchStillArchives(t *testing.T) {
	base := amgBaseRepo(t)
	m, p := amgRig(t, base, PRDCompleted)
	p, _ = m.store.GetPRD(p.ID)
	p.Git.Branch = ""
	m.store.SavePRD(p)
	if out, err := m.Archive(p.ID, "supervisor-loop"); err != nil {
		t.Fatalf("record with no branch: %v (out=%v)", err, out)
	}
}

func TestArchiveUnmergedCancelledStillArchives(t *testing.T) {
	// The guard scopes to completed records; a cancelled record with
	// unmerged branch work is archived on request as before (the close
	// step's branch check is what keeps it through supervisor passes).
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDCancelled)
	if out, err := m.Archive(p.ID, "supervisor-loop"); err != nil {
		t.Fatalf("cancelled record: %v (out=%v)", err, out)
	}
	if st := amgStatus(t, m, p.ID); st != PRDArchived {
		t.Errorf("status = %q, want archived", st)
	}
}

func TestArchiveUnmergedOperatorStillArchives(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDCompleted)
	if out, err := m.Archive(p.ID, "operator"); err != nil {
		t.Fatalf("operator archive of unmerged work: %v (out=%v)", err, out)
	}
	if st := amgStatus(t, m, p.ID); st != PRDArchived {
		t.Errorf("status = %q, want archived", st)
	}
}

// amgCloseRig builds the supervisor-loop close-step rig over an on-disk
// base repository, with record 0 in the given terminal status carrying the
// work branch.
func amgCloseRig(t *testing.T, base string, status PRDStatus) (*coordHarness, *SupervisorLoopSteps) {
	t.Helper()
	h, steps, _ := newStepsRig(t, SupervisorLoopHooks{})
	cfg := h.m.Config()
	cfg.WorktreeBaseRepo = base
	h.m.SetConfig(cfg)
	p, ok := h.m.store.GetPRD(h.prds[0].ID)
	if !ok {
		t.Fatal("prd missing")
	}
	p.Status = status
	p.Git.Branch = amgBranch
	if err := h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	slSetClock(t, steps, slBase)
	return h, steps
}

func TestCloseStepKeepsUnmergedCompleted(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	h, steps := amgCloseRig(t, base, PRDCompleted)
	slbRun(t, steps)
	if st := slStatus(t, h, 0); st != PRDCompleted {
		t.Errorf("completed record with unmerged branch = %s, want kept", st)
	}
}

func TestCloseStepArchivesCompletedOnceMerged(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	amgGit(t, base, "merge", "-q", amgBranch)
	h, steps := amgCloseRig(t, base, PRDCompleted)
	slbRun(t, steps)
	if st := slStatus(t, h, 0); st != PRDArchived {
		t.Errorf("completed record after merge = %s, want archived", st)
	}
}

func TestCloseStepKeepsUnmergedCompletedOnGitError(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	h, steps := amgCloseRig(t, base, PRDCompleted)
	// Force the shared check to fail: branch ref query errors.
	g, _ := func() (*memberProofMergeGit, map[string]scriptStep) {
		s := map[string]scriptStep{
			joinArgs("remote"): {out: "origin\n"},
			joinArgs("symbolic-ref", "refs/remotes/origin/HEAD"):                     {err: errors.New("no symbolic ref")},
			joinArgs("symbolic-ref", "--short", "HEAD"):                              {err: errors.New("no symbolic ref")},
			joinArgs("for-each-ref", "--format=%(refname)", "refs/heads/"+amgBranch): {err: errors.New("boom")},
		}
		return newMemberProofMergeGit(t, s), s
	}()
	h.m.SetArchiveGuardGit(g)
	slbRun(t, steps)
	if st := slStatus(t, h, 0); st != PRDCompleted {
		t.Errorf("completed record that failed its merge check = %s, want kept", st)
	}
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
