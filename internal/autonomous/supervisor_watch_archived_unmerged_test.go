// Tests for the archived_unmerged supervisor watch finding. Git, the clock and
// the record store are fakes; nothing shells out. Run with:
//
//	env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestSupervisorWatchArchivedUnmerged
package autonomous

import (
	"strings"
	"testing"
	"time"
)

func swauRig(t *testing.T, git *slbFakeGit, clock *time.Time) *Manager {
	t.Helper()
	m := swNewManager(t)
	cfg := m.Config()
	cfg.WorktreeBaseRepo = "base-repo"
	m.SetConfig(cfg)
	m.archivedUnmergedGit = git
	m.now = func() time.Time { return *clock }
	return m
}

func swauArchive(t *testing.T, m *Manager, title, branch string) *PRD {
	t.Helper()
	p, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Title = title
	p.Status = PRDArchived
	p.Git.Branch = branch
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSupervisorWatchArchivedUnmergedRaised(t *testing.T) {
	clock := time.Now()
	git := &slbFakeGit{refOut: "refs/heads/work\n", ahead: "3\n"}
	m := swauRig(t, git, &clock)
	p := swauArchive(t, m, "Ship the thing", "work")
	got := m.supervisorWatchArchivedUnmerged(clock)
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	f := got[0]
	if f.Kind != SupervisorWatchArchivedUnmerged || f.Subject != p.ID {
		t.Fatalf("bad identity: %+v", f)
	}
	for _, want := range []string{"Ship the thing", "work", "3", "carry-forward " + p.ID} {
		if !strings.Contains(f.Text, want) {
			t.Errorf("text %q missing %q", f.Text, want)
		}
	}
}

func TestSupervisorWatchArchivedUnmergedResolvedAfterMerge(t *testing.T) {
	clock := time.Now()
	git := &slbFakeGit{refOut: "refs/heads/work\n", ahead: "2\n"}
	m := swauRig(t, git, &clock)
	p := swauArchive(t, m, "T", "work")
	if len(m.supervisorWatchArchivedUnmerged(clock)) != 1 {
		t.Fatal("expected a finding")
	}
	git.ahead = "0\n" // merged
	clock = clock.Add(supervisorWatchArchivedCacheTTL + time.Second)
	if got := m.supervisorWatchArchivedUnmerged(clock); len(got) != 0 {
		t.Fatalf("merged branch must yield none: %+v", got)
	}
	git.refOut = "" // branch disappeared
	git.ahead = "2\n"
	clock = clock.Add(supervisorWatchArchivedCacheTTL + time.Second)
	if got := m.supervisorWatchArchivedUnmerged(clock); len(got) != 0 {
		t.Fatalf("missing branch must yield none: %+v (%s)", got, p.ID)
	}
}

func TestSupervisorWatchArchivedUnmergedNoneForMergedOrBranchless(t *testing.T) {
	clock := time.Now()
	git := &slbFakeGit{refOut: "refs/heads/work\n", ahead: "0\n"}
	m := swauRig(t, git, &clock)
	swauArchive(t, m, "merged", "work")
	swauArchive(t, m, "branchless", "")
	if got := m.supervisorWatchArchivedUnmerged(clock); len(got) != 0 {
		t.Fatalf("want none, got %+v", got)
	}
}

func TestSupervisorWatchArchivedUnmergedCache(t *testing.T) {
	clock := time.Now()
	git := &slbFakeGit{refOut: "refs/heads/work\n", ahead: "1\n"}
	m := swauRig(t, git, &clock)
	swauArchive(t, m, "T", "work")
	m.supervisorWatchArchivedUnmerged(clock)
	git.ahead = "0\n"
	clock = clock.Add(time.Second) // within the cache lifetime: reused
	if got := m.supervisorWatchArchivedUnmerged(clock); len(got) != 1 {
		t.Fatalf("cached result should be reused, got %d", len(got))
	}
}

func TestSupervisorWatchArchivedUnmergedCapKeepsOpenFinding(t *testing.T) {
	clock := time.Now()
	git := &slbFakeGit{refOut: "refs/heads/work\n", ahead: "1\n"}
	m := swauRig(t, git, &clock)
	n := supervisorWatchArchivedMaxChecks + 2
	for i := 0; i < n; i++ {
		swauArchive(t, m, "T", "work")
	}
	got := m.supervisorWatchArchivedUnmerged(clock)
	if len(got) != supervisorWatchArchivedMaxChecks {
		t.Fatalf("want %d findings under the cap, got %d", supervisorWatchArchivedMaxChecks, len(got))
	}
	for _, f := range got {
		if _, err := m.SaveSupervisorWatchFinding(&SupervisorWatchFinding{Kind: f.Kind, Subject: f.Subject, Text: f.Text, FirstSeenAt: clock, LastSeenAt: clock}); err != nil {
			t.Fatal(err)
		}
	}
	// Next pass inside the cache lifetime: cached ones are free, the rest check.
	clock = clock.Add(time.Second)
	if got := m.supervisorWatchArchivedUnmerged(clock); len(got) != n {
		t.Fatalf("want %d findings after second pass, got %d", n, len(got))
	}
}

func TestSupervisorWatchArchivedUnmergedKindAndText(t *testing.T) {
	if string(SupervisorWatchArchivedUnmerged) != "archived_unmerged" || !ValidSupervisorWatchKind(SupervisorWatchArchivedUnmerged) {
		t.Fatal("kind not registered")
	}
	if txt, err := SupervisorWatchText(SupervisorWatchArchivedUnmerged, "x1"); err != nil || txt == "" {
		t.Fatalf("no text: %v", err)
	}
}
