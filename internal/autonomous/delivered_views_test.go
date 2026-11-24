package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// amgHandPort lands the work of the amgBranch branch on main by hand: the same
// content in file a (so the content matches) plus another file in the same
// commit (so the patch-id differs and git cherry cannot see it). It returns
// the hand-port commit.
func amgHandPort(t *testing.T, base string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(base, "a"), []byte("alpha\nwork\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "extra.txt"), []byte("extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	amgGit(t, base, "add", "-A")
	amgGit(t, base, "commit", "-q", "-m", "Hand port of the guard work")
	return amgGit(t, base, "rev-parse", "HEAD")
}

// amgHandPortOtherContent lands different work under another name: the commit
// can be named by a shipped record, but no file content matches the branch.
func amgHandPortOtherContent(t *testing.T, base string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(base, "ported.txt"), []byte("ported differently\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	amgGit(t, base, "add", "-A")
	amgGit(t, base, "commit", "-q", "-m", "Hand port under another name")
	return amgGit(t, base, "rev-parse", "HEAD")
}

func amgShip(t *testing.T, m *Manager, p *PRD, commit string) {
	t.Helper()
	m.store.WithLock(func() {
		p.MergedInRelease = "9.183.0"
		p.MergeCommit = commit
	})
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
}

func TestMergeStateView_HandPortNotFinishedNotMerged(t *testing.T) {
	t.Run("control: nothing on main", func(t *testing.T) {
		base := amgBaseRepo(t)
		amgBranchAhead(t, base)
		m, p := amgRig(t, base, PRDArchived)
		if got := m.AutomatonMergeState(p); got != MergeStateFinishedNotMerged {
			t.Fatalf("got %q, want %q", got, MergeStateFinishedNotMerged)
		}
	})
	t.Run("shipped record commit on main", func(t *testing.T) {
		base := amgBaseRepo(t)
		amgBranchAhead(t, base)
		port := amgHandPortOtherContent(t, base)
		m, p := amgRig(t, base, PRDArchived)
		amgShip(t, m, p, port)
		if got := m.AutomatonMergeState(p); got != "" {
			t.Fatalf("got %q, want empty for a hand-ported record", got)
		}
	})
	t.Run("shipped record commit not on main", func(t *testing.T) {
		base := amgBaseRepo(t)
		amgBranchAhead(t, base)
		m, p := amgRig(t, base, PRDArchived)
		amgShip(t, m, p, amgGit(t, base, "rev-parse", amgBranch)) // on the branch only
		if got := m.AutomatonMergeState(p); got != MergeStateFinishedNotMerged {
			t.Fatalf("got %q, want %q", got, MergeStateFinishedNotMerged)
		}
	})
	t.Run("same content on main", func(t *testing.T) {
		base := amgBaseRepo(t)
		amgBranchAhead(t, base)
		amgHandPort(t, base)
		m, p := amgRig(t, base, PRDCompleted)
		if got := m.AutomatonMergeState(p); got != "" {
			t.Fatalf("got %q, want empty for same content on main", got)
		}
	})
}

// The index-backed list views carry only id, status and branch; the reader
// must look the shipped record up itself.
func TestMergeStateViewOf_UsesStoredShippedRecord(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	port := amgHandPortOtherContent(t, base)
	m, p := amgRig(t, base, PRDArchived)
	amgShip(t, m, p, port)
	if got := m.AutomatonMergeStateOf(p.ID, p.Status, p.Git.Branch, ""); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestArchivedUnmerged_HandPortNotListed(t *testing.T) {
	list := func(m *Manager) []ArchivedUnmergedEntry { return m.ArchivedUnmerged(context.Background()) }
	t.Run("control: nothing on main", func(t *testing.T) {
		base := amgBaseRepo(t)
		amgBranchAhead(t, base)
		m, _ := amgRig(t, base, PRDArchived)
		if got := list(m); len(got) != 1 || got[0].State != ArchivedUnmergedAhead {
			t.Fatalf("want one ahead entry, got %+v", got)
		}
	})
	t.Run("shipped record commit on main", func(t *testing.T) {
		base := amgBaseRepo(t)
		amgBranchAhead(t, base)
		port := amgHandPortOtherContent(t, base)
		m, p := amgRig(t, base, PRDArchived)
		amgShip(t, m, p, port)
		if got := list(m); len(got) != 0 {
			t.Fatalf("hand-ported record listed: %+v", got)
		}
	})
	t.Run("shipped record commit not on main", func(t *testing.T) {
		base := amgBaseRepo(t)
		amgBranchAhead(t, base)
		m, p := amgRig(t, base, PRDArchived)
		amgShip(t, m, p, amgGit(t, base, "rev-parse", amgBranch))
		if got := list(m); len(got) != 1 {
			t.Fatalf("want one entry, got %+v", got)
		}
	})
	t.Run("same content on main", func(t *testing.T) {
		base := amgBaseRepo(t)
		amgBranchAhead(t, base)
		amgHandPort(t, base)
		m, _ := amgRig(t, base, PRDArchived)
		if got := list(m); len(got) != 0 {
			t.Fatalf("same-content record listed: %+v", got)
		}
	})
}

// ---- landing decision tip --------------------------------------------------

// The decision records the branch tip it was made at.
func TestDecideLanding_RecordsBranchTip(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDArchived)
	tip := amgGit(t, base, "rev-parse", amgBranch)
	out, err := m.DropLanding(p.ID, "operator", "abandoned")
	if err != nil {
		t.Fatal(err)
	}
	if out.Landing == nil || out.Landing.Tip != tip {
		t.Fatalf("landing tip = %+v, want %s", out.Landing, tip)
	}
}

// A decision whose tip cannot be read still records (never blocks), with no tip.
func TestDecideLanding_NoRepoRecordsNoTip(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.store.CreatePRD("x", "/p", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Git.Branch = "automaton/x"
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	out, err := m.KeepLanding(p.ID, "operator", "on purpose")
	if err != nil {
		t.Fatal(err)
	}
	if out.Landing == nil || out.Landing.State != LandingKept || out.Landing.Tip != "" {
		t.Fatalf("landing = %+v", out.Landing)
	}
}

// A kept or dropped decision hides the branch only while the tip is the one
// the decision was made at. New commits re-list it.
func TestUnlandedList_DecisionStopsHidingWhenTipMoves(t *testing.T) {
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDArchived)
	ctx := context.Background()
	if _, err := m.DropLanding(p.ID, "operator", "abandoned"); err != nil {
		t.Fatal(err)
	}
	if got, err := m.UnlandedWork(ctx); err != nil || len(got) != 0 {
		t.Fatalf("after the decision: got %+v err %v, want empty", got, err)
	}
	// New commit on the branch after the decision.
	amgGit(t, base, "checkout", "-q", amgBranch)
	if err := os.WriteFile(filepath.Join(base, "later.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	amgGit(t, base, "add", "-A")
	amgGit(t, base, "commit", "-q", "-m", "work after the decision")
	amgGit(t, base, "checkout", "-q", "main")
	got, err := m.UnlandedWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AutomatonID != p.ID {
		t.Fatalf("want the branch re-listed, got %+v", got)
	}
	if !strings.Contains(got[0].Reason, "new commits after the decision") {
		t.Errorf("reason = %q, want it to say new commits after the decision", got[0].Reason)
	}
	if got[0].Recommendation == UnlandedLanded {
		t.Errorf("recommendation = landed for undelivered new work")
	}
}

// The supervisor watch raises the same work as the archived-unmerged finder,
// so it follows the same delivery proof.
func TestSupervisorWatchArchivedUnmerged_HandPortNotRaised(t *testing.T) {
	now := time.Now()
	base := amgBaseRepo(t)
	amgBranchAhead(t, base)
	m, p := amgRig(t, base, PRDArchived)
	if got := m.supervisorWatchArchivedUnmerged(now); len(got) != 1 {
		t.Fatalf("control: want 1 finding, got %+v", got)
	}
	port := amgHandPortOtherContent(t, base)
	amgShip(t, m, p, port)
	later := now.Add(supervisorWatchArchivedCacheTTL + time.Second)
	if got := m.supervisorWatchArchivedUnmerged(later); len(got) != 0 {
		t.Fatalf("hand-ported record raised: %+v", got)
	}
}
