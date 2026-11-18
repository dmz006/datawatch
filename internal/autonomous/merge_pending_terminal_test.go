package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// The merge-pending pass over Automata in the archived, needs_review, blocked
// and failed states. These share newMergePendingHarness: one Automaton
// ("alpha") on mergeTestBranch with a scripted git, here moved out of
// completed into the state under test.

func mergeTerminalPrefix(state PRDStatus) string {
	return "warn|Automaton alpha merge pending (" + string(state) + ", branch " + mergeTestBranch + ")|"
}

// setIncludeTerminal sets autonomous.automation.merge_pending_include_terminal
// (nil = unset, the default).
func setIncludeTerminal(th *triageHarness, v *bool) {
	cfg := th.h.m.Config()
	cfg.Automation.MergePendingIncludeTerminal = v
	th.h.m.SetConfig(cfg)
}

// moveTo puts alpha into a covered state other than completed, keeping its
// work branch.
func moveTo(t *testing.T, th *triageHarness, state PRDStatus) {
	t.Helper()
	switch state {
	case PRDFailed:
		th.failPRD(t, 0, "tests failed", "worker exited")
	case PRDBlocked:
		th.blockedPRD(t, 0, "a guardrail returned block")
	default:
		th.setStatus(t, 0, state)
	}
	p, _ := th.h.m.store.GetPRD(th.h.prds[0].ID)
	if p.Git.Branch != mergeTestBranch {
		t.Fatalf("branch = %q after the move, want %q", p.Git.Branch, mergeTestBranch)
	}
}

func TestMergePendingTerminal_EveryStateAlertsOnceNamingStateAndBranch(t *testing.T) {
	for _, state := range []PRDStatus{PRDArchived, PRDNeedsReview, PRDBlocked, PRDFailed} {
		t.Run(string(state), func(t *testing.T) {
			th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
			moveTo(t, th, state)
			prefix := mergeTerminalPrefix(state)

			th.h.c.triagePass()
			th.assertAlerts(t, 0, prefix)
			th.advance(5 * time.Minute)
			th.h.c.triagePass()
			th.assertAlerts(t, 0, prefix)
			th.advance(5 * time.Minute)
			th.h.c.triagePass()
			th.assertAlerts(t, 1, prefix)

			body := th.alertEntry(t, prefix)
			containsAll(t, body, "Automaton \"alpha\"", "is "+string(state), mergeTestBranch, "2 commit(s)", "main")
			if strings.Contains(body, "PRD") || strings.Contains(prefix, "PRD") {
				t.Fatalf("operator text says PRD: %s", body)
			}

			for i := 0; i < 4; i++ {
				th.advance(10 * time.Minute)
				th.h.c.triagePass()
			}
			th.assertAlerts(t, 1, prefix)
			if n := git.count("rev-list"); n != 1 {
				t.Fatalf("rev-list ran %d times, want 1 (tips never moved)", n)
			}
			if rec := th.record(t, 0); rec != nil && rec.Status == TriageStatusMergePending {
				t.Fatalf("a %s Automaton got a stored merge_pending record: %+v", state, rec)
			}
		})
	}
}

func TestMergePendingTerminal_FailedAndBlockedTriageRecordUntouched(t *testing.T) {
	for _, state := range []PRDStatus{PRDFailed, PRDBlocked} {
		t.Run(string(state), func(t *testing.T) {
			th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
			th.h.c.markReady()
			moveTo(t, th, state)

			th.h.c.triagePass()
			rec := th.record(t, 0)
			if rec == nil || rec.Status != string(state) {
				t.Fatalf("record = %+v, want the %s triage record", rec, state)
			}
			first := rec.FirstSeenAt

			th.advance(10 * time.Minute)
			th.h.c.triagePass()
			th.assertAlerts(t, 1, mergeTerminalPrefix(state))
			th.assertAlerts(t, 1, "warn|Triage: Automaton")
			rec = th.record(t, 0)
			if rec == nil || rec.Status != string(state) || !rec.FirstSeenAt.Equal(first) || rec.AlertedAt == nil {
				t.Fatalf("record after both alerts = %+v, want the %s record kept with its first-seen time", rec, state)
			}

			// the branch lands: the merge episode ends, the triage record stays
			git.setMerged("branch-1", "base-2")
			th.advance(time.Minute)
			th.h.c.triagePass()
			if rec := th.record(t, 0); rec == nil || rec.Status != string(state) {
				t.Fatalf("record after the merge = %+v, want the %s record kept", rec, state)
			}
			if _, ok := th.h.c.mergeEpisodeFor(th.h.prds[0].ID); ok {
				t.Fatal("episode still open after the branch merged")
			}
		})
	}
}

func TestMergePendingTerminal_NewEpisodeAlertsAgain(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	moveTo(t, th, PRDArchived)
	prefix := mergeTerminalPrefix(PRDArchived)

	th.h.c.triagePass()
	th.advance(10 * time.Minute)
	th.h.c.triagePass()
	th.assertAlerts(t, 1, prefix)

	git.setMerged("branch-1", "base-2")
	th.h.c.triagePass()

	git.mu.Lock()
	git.baseTip = "base-3"
	git.mu.Unlock()
	git.setPending("branch-2")
	th.h.c.triagePass()
	th.assertAlerts(t, 1, prefix)
	th.advance(10 * time.Minute)
	th.h.c.triagePass()
	th.assertAlerts(t, 2, prefix)
}

func TestMergePendingTerminal_EmptyAndNeedsLookEndTheEpisode(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(g *scriptedMergeGit)
	}{
		{"empty", func(g *scriptedMergeGit) { g.ahead, g.cherry, g.branchLog = 0, "", "" }},
		// the two tests disagree: subjects on main, a patch is not
		{"needs_look", func(g *scriptedMergeGit) {
			g.ahead, g.cherry, g.branchLog, g.baseLog = 1, "+ aaa\n", "Add the first part\n", "Add the first part\n"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
			moveTo(t, th, PRDFailed)
			th.h.c.triagePass()
			if _, ok := th.h.c.mergeEpisodeFor(th.h.prds[0].ID); !ok {
				t.Fatal("no episode while pending")
			}
			git.mu.Lock()
			git.branchTip = "branch-9"
			tc.set(git)
			git.mu.Unlock()
			for i := 0; i < 4; i++ {
				th.advance(10 * time.Minute)
				th.h.c.triagePass()
			}
			if _, ok := th.h.c.mergeEpisodeFor(th.h.prds[0].ID); ok {
				t.Fatalf("episode still open with the branch %s", tc.name)
			}
			th.assertNoAlerts(t, "warn|Automaton alpha merge pending")
		})
	}
}

func TestMergePendingTerminal_ThresholdZeroNoAlert(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeOn, 0)
	moveTo(t, th, PRDNeedsReview)
	for i := 0; i < 4; i++ {
		th.h.c.triagePass()
		th.advance(time.Hour)
	}
	th.assertNoAlerts(t, "warn|Automaton alpha merge pending")
}

func TestMergePendingTerminal_IncludeTerminalOffKeepsCompletedOnly(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	off := false
	setIncludeTerminal(th, &off)
	moveTo(t, th, PRDFailed)
	for i := 0; i < 3; i++ {
		th.h.c.triagePass()
		th.advance(10 * time.Minute)
	}
	if n := git.total(); n != 0 {
		t.Fatalf("include_terminal off ran %d git commands for a failed Automaton, want none", n)
	}
	th.assertNoAlerts(t, "warn|Automaton alpha merge pending")

	// shadow mode follows the same switch
	cfg := th.h.m.Config()
	cfg.Automation.MergePendingMode = config.MergePendingModeShadow
	th.h.m.SetConfig(cfg)
	th.h.c.triagePass()
	if got := th.h.c.MergeShadowDecisions(0); len(got) != 0 {
		t.Fatalf("shadow decisions = %+v, want none with include_terminal off", got)
	}
}

func TestMergePendingTerminal_ArchivedKeepsEpisodeFromCompleted(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	th.h.c.triagePass()
	th.advance(10 * time.Minute)
	th.h.c.triagePass()
	th.assertAlerts(t, 1, mergeAlertPrefix)
	rec := th.record(t, 0)
	if rec == nil || rec.AlertedAt == nil {
		t.Fatalf("record = %+v, want an alerted merge_pending record", rec)
	}
	first := rec.FirstSeenAt

	moveTo(t, th, PRDArchived)
	for i := 0; i < 3; i++ {
		th.advance(10 * time.Minute)
		th.h.c.triagePass()
	}
	th.assertAlerts(t, 0, mergeTerminalPrefix(PRDArchived))
	ep, ok := th.h.c.mergeEpisodeFor(th.h.prds[0].ID)
	if !ok || !ep.since.Equal(first) || ep.alertedAt == nil {
		t.Fatalf("episode = %+v (ok %v), want it carried over from the completed record", ep, ok)
	}
	if rec := th.record(t, 0); rec != nil {
		t.Fatalf("record = %+v, want the merge_pending record cleared once archived", rec)
	}
}

func TestMergePendingTerminal_PruneDropsEpisodeWhenUncovered(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	moveTo(t, th, PRDFailed)
	th.h.c.triagePass()
	if _, ok := th.h.c.mergeEpisodeFor(th.h.prds[0].ID); !ok {
		t.Fatal("no episode while pending")
	}
	th.setStatus(t, 0, PRDRunning)
	th.h.c.triagePass()
	if _, ok := th.h.c.mergeEpisodeFor(th.h.prds[0].ID); ok {
		t.Fatal("episode kept for an Automaton the pass no longer covers")
	}
}

func TestMergePendingTerminal_ShadowRecordsStateAndFoundFalse(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeShadow, 10)
	th.h.c.markReady()
	moveTo(t, th, PRDFailed)

	th.h.c.triagePass()
	th.advance(10 * time.Minute)
	th.h.c.triagePass()
	got := th.h.c.MergeShadowDecisions(0)
	if len(got) != 2 || !got[0].WouldAlert || got[0].AutomatonState != string(PRDFailed) {
		t.Fatalf("shadow decisions = %+v, want a would_alert decision for the failed Automaton", got)
	}
	if rec := th.record(t, 0); rec == nil || rec.Status != string(PRDFailed) {
		t.Fatalf("record = %+v, want the failed triage record kept in shadow mode", rec)
	}
	th.assertNoAlerts(t, "warn|Automaton alpha merge pending")

	// the episode ends with a branch that needs a look: the would-alert
	// decision was a false one
	git.mu.Lock()
	git.branchTip, git.ahead, git.cherry = "branch-9", 1, "+ aaa\n"
	git.branchLog, git.baseLog = "Add the first part\n", "Add the first part\n"
	git.mu.Unlock()
	th.advance(time.Minute)
	th.h.c.triagePass()
	got = th.h.c.MergeShadowDecisions(0)
	if len(got) != 3 || got[0].State != MergeStateNeedsLook {
		t.Fatalf("shadow decisions = %+v, want a newest needs_look decision", got)
	}
	if !got[1].FoundFalse || got[2].FoundFalse || got[0].FoundFalse {
		t.Fatalf("found_false = %v,%v,%v; want only the would_alert decision marked", got[0].FoundFalse, got[1].FoundFalse, got[2].FoundFalse)
	}
	total, found := th.h.c.shadowRecords().counts()
	if total != 3 || found != 1 {
		t.Fatalf("counts = %d total, %d false; want 3 and 1", total, found)
	}

	// a later episode that ends merged marks nothing
	git.setPending("branch-10")
	th.h.c.triagePass()
	th.advance(10 * time.Minute)
	th.h.c.triagePass()
	git.setMerged("branch-10", "base-2")
	th.h.c.triagePass()
	total, found = th.h.c.shadowRecords().counts()
	if total != 6 || found != 1 {
		t.Fatalf("counts after a merged episode = %d total, %d false; want 6 and 1", total, found)
	}
}

func TestMergePendingTerminal_ShadowCountsSurviveRestart(t *testing.T) {
	th, git := newMergePendingHarness(t, config.MergePendingModeShadow, 10)
	moveTo(t, th, PRDArchived)
	th.h.c.triagePass()
	th.advance(10 * time.Minute)
	th.h.c.triagePass()
	git.mu.Lock()
	git.branchTip, git.ahead, git.cherry, git.branchLog = "branch-9", 0, "", ""
	git.mu.Unlock()
	th.h.c.triagePass() // ends empty: one false decision, flushed

	back := newMergeShadowRecords(th.h.m.store.dir)
	total, found := back.counts()
	if total != 3 || found != 1 {
		t.Fatalf("reloaded counts = %d total, %d false; want 3 and 1", total, found)
	}
	list := back.list(0)
	if len(list) != 3 || !list[1].FoundFalse || list[0].AutomatonState != string(PRDArchived) {
		t.Fatalf("reloaded decisions = %+v", list)
	}
}

func TestMergeShadowRecords_LegacyListLoads(t *testing.T) {
	dir := t.TempDir()
	legacy := `[{"at":"2026-01-02T00:00:00Z","automaton_id":"a1","name":"alpha","branch":"automaton/a1","state":"pending","commits_ahead":2,"would_alert":true,"repeat_count":1,"last_seen_at":"2026-01-02T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(dir, mergeShadowRecordsFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newMergeShadowRecords(dir)
	if total, found := r.counts(); total != 1 || found != 0 {
		t.Fatalf("legacy counts = %d, %d; want 1 and 0", total, found)
	}
	// ending the episode empty marks the legacy decision and writes the new
	// layout
	r.observe(MergeShadowDecision{AutomatonID: "a1", Branch: "automaton/a1", State: MergeStateEmpty}, triageBase)
	r.flush()
	back := newMergeShadowRecords(dir)
	if total, found := back.counts(); total != 2 || found != 1 {
		t.Fatalf("counts after rewrite = %d, %d; want 2 and 1", total, found)
	}
	data, err := os.ReadFile(filepath.Join(dir, mergeShadowRecordsFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"decisions_total":2`, `"decisions_false":1`, `"found_false":true`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("file lacks %s: %s", key, data)
		}
	}
}

func TestMergeStatus_TerminalEntryAndShadowCounts(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	th.h.m.mu.Lock()
	th.h.m.coordinator = th.h.c
	th.h.m.mu.Unlock()
	moveTo(t, th, PRDNeedsReview)

	th.h.c.triagePass()
	th.advance(10 * time.Minute)
	th.h.c.triagePass()

	ms := th.h.m.MergeStatus()
	if len(ms.Automata) != 1 {
		t.Fatalf("automata = %+v, want the needs_review Automaton", ms.Automata)
	}
	e := ms.Automata[0]
	if e.AutomatonState != string(PRDNeedsReview) || e.State != MergeStatePending || e.PendingSince == nil || e.AlertedAt == nil {
		t.Fatalf("entry = %+v, want needs_review, pending, with pending_since and alerted_at", e)
	}
	if !e.PendingSince.Equal(triageBase) {
		t.Fatalf("pending_since = %v, want %v", e.PendingSince, triageBase)
	}
	if ms.ShadowDecisionsTotal != 0 || ms.ShadowDecisionsFalse != 0 {
		t.Fatalf("counts = %d, %d; want 0 with no shadow run", ms.ShadowDecisionsTotal, ms.ShadowDecisionsFalse)
	}
	text := RenderMergeState(ms)
	if !strings.Contains(text, "Automaton is needs_review") {
		t.Fatalf("status text lacks the Automaton state:\n%s", text)
	}

	// shadow counts are reported
	cfg := th.h.m.Config()
	cfg.Automation.MergePendingMode = config.MergePendingModeShadow
	th.h.m.SetConfig(cfg)
	th.h.c.triagePass()
	ms = th.h.m.MergeStatus()
	if ms.ShadowDecisionsTotal != 1 || len(ms.ShadowDecisions) != 1 || ms.ShadowDecisions[0].AutomatonState != string(PRDNeedsReview) {
		t.Fatalf("shadow status = total %d, decisions %+v", ms.ShadowDecisionsTotal, ms.ShadowDecisions)
	}
	if !strings.Contains(RenderMergeState(ms), "Shadow decisions made: 1, later found false: 0.") {
		t.Fatalf("status text lacks the shadow counts:\n%s", RenderMergeState(ms))
	}
}
