package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
)

// setMergeBaseRepo points the Manager at repo as its worktree base repo, the
// repository the merge lock is keyed by.
func setMergeBaseRepo(t *testing.T, m *Manager, repo string) {
	t.Helper()
	g := m.Config()
	g.WorktreeBaseRepo = repo
	m.SetConfig(g)
}

func TestMergeStatusNoLockHasNoFieldKeyOrTextLine(t *testing.T) {
	th := newTriageHarness(t, 1, 10)
	m := th.h.m
	repo := t.TempDir()
	setMergeBaseRepo(t, m, repo)

	ms := m.MergeStatus()
	if ms.MergeLock != nil {
		t.Fatalf("no lock is held, want a nil MergeLock, got %+v", ms.MergeLock)
	}
	raw, err := json.Marshal(ms)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "merge_lock") {
		t.Fatalf("no lock is held, the JSON must not carry the merge_lock key: %s", raw)
	}
	if text := RenderMergeState(ms); strings.Contains(text, "Merge lock") {
		t.Fatalf("no lock is held, the text must not mention a lock: %q", text)
	}
}

func TestMergeStatusEmptyLockIsNotShown(t *testing.T) {
	th := newTriageHarness(t, 1, 10)
	m := th.h.m
	repo := t.TempDir()
	setMergeBaseRepo(t, m, repo)
	locks, err := m.MergeLocks()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locks.Acquire(repo, MergeLockHolder{}); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if ms := m.MergeStatus(); ms.MergeLock != nil {
		t.Fatalf("a lock that names no run or Automaton holds nothing, got %+v", ms.MergeLock)
	}
}

func TestMergeStatusNoBaseRepoHasNoLock(t *testing.T) {
	th := newTriageHarness(t, 1, 10)
	m := th.h.m
	setMergeBaseRepo(t, m, "")
	if ms := m.MergeStatus(); ms.MergeLock != nil {
		t.Fatalf("no base repo means no lock to show, got %+v", ms.MergeLock)
	}
}

func TestMergeStatusHeldLockShowsHolderAndTextLine(t *testing.T) {
	th := newTriageHarness(t, 1, 10)
	m := th.h.m
	repo := t.TempDir()
	setMergeBaseRepo(t, m, repo)
	locks, err := m.MergeLocks()
	if err != nil {
		t.Fatal(err)
	}
	held, err := locks.Acquire(repo, MergeLockHolder{
		RunID:       "run-lock-1",
		AutomatonID: "auto-lock-1",
		Branches:    []string{"feature/a", "feature/b"},
	})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	ms := m.MergeStatus()
	if ms.MergeLock == nil {
		t.Fatal("a held lock must be shown as MergeLock")
	}
	got := ms.MergeLock
	if got.Repo != repo || got.RunID != "run-lock-1" || got.AutomatonID != "auto-lock-1" {
		t.Fatalf("holder not reported: %+v", got)
	}
	if len(got.Branches) != 2 || got.Branches[0] != "feature/a" || got.Branches[1] != "feature/b" {
		t.Fatalf("branches not reported: %v", got.Branches)
	}
	if !got.Since.Equal(held.Since) {
		t.Fatalf("since: want %s, got %s", held.Since, got.Since)
	}

	text := RenderMergeState(ms)
	for _, want := range []string{"Merge lock held", "run-lock-1", "auto-lock-1", "feature/a, feature/b", mergeTimeText(&held.Since)} {
		if !strings.Contains(text, want) {
			t.Fatalf("text line missing %q:\n%s", want, text)
		}
	}
}

func TestMergeStatusTextLineWithoutBranches(t *testing.T) {
	text := RenderMergeState(MergeStatus{
		Mode:      "off",
		MergeLock: &MergeLockStatus{RunID: "r9", AutomatonID: "a9"},
	})
	if !strings.Contains(text, "no branches") || !strings.Contains(text, "run r9") {
		t.Fatalf("text line for a lock with no branches: %q", text)
	}
}

func TestParseMergeStatusRoundTripsMergeLock(t *testing.T) {
	th := newTriageHarness(t, 1, 10)
	m := th.h.m
	repo := t.TempDir()
	setMergeBaseRepo(t, m, repo)
	locks, err := m.MergeLocks()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locks.Acquire(repo, MergeLockHolder{RunID: "run-rt", AutomatonID: "auto-rt", Branches: []string{"b1"}}); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	want := m.MergeStatus()
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseMergeStatus(raw)
	if err != nil {
		t.Fatalf("ParseMergeStatus: %v", err)
	}
	if back.MergeLock == nil || want.MergeLock == nil {
		t.Fatalf("MergeLock lost in the round trip: %+v", back.MergeLock)
	}
	if back.MergeLock.RunID != "run-rt" || back.MergeLock.AutomatonID != "auto-rt" ||
		len(back.MergeLock.Branches) != 1 || back.MergeLock.Branches[0] != "b1" ||
		!back.MergeLock.Since.Equal(want.MergeLock.Since) || back.MergeLock.Repo != repo {
		t.Fatalf("round trip changed the lock: got %+v, want %+v", back.MergeLock, want.MergeLock)
	}
}
