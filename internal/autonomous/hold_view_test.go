package autonomous

import (
	"strings"
	"testing"
	"time"
)

// holdViewNow is the fixed reading of the clock the held group tests use.
var holdViewNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func holdViewTime(d time.Duration) *time.Time {
	t := holdViewNow.Add(d)
	return &t
}

// holdViewPRD is a held Automaton with the given look-again time and hold age.
func holdViewPRD(id string, until *time.Time, heldAgo time.Duration) *PRD {
	return &PRD{
		ID:     id,
		Title:  "Automaton " + id,
		Status: PRDHeld,
		Hold: &HoldState{
			Reason: "wait for the release " + id,
			HeldAt: holdViewNow.Add(-heldAgo),
			HeldBy: "operator",
			From:   PRDApproved,
			Until:  until,
		},
	}
}

func holdViewIDs(g HeldGroup) []string {
	ids := make([]string, 0, len(g.Held))
	for _, v := range g.Held {
		ids = append(ids, v.ID)
	}
	return ids
}

func TestHoldGroupViewOrdersExpiredFirst(t *testing.T) {
	prds := []*PRD{
		holdViewPRD("dated-late", holdViewTime(72*time.Hour), time.Hour),
		holdViewPRD("undated-old", nil, 48*time.Hour),
		holdViewPRD("dated-soon", holdViewTime(2*time.Hour), time.Hour),
		holdViewPRD("expired-late", holdViewTime(-time.Hour), time.Hour),
		holdViewPRD("undated-new", nil, time.Hour),
		holdViewPRD("expired-early", holdViewTime(-5*time.Hour), time.Hour),
		{ID: "running-one", Status: PRDRunning},
	}
	g := BuildHeldGroup(prds, holdViewNow)
	want := []string{"expired-early", "expired-late", "dated-soon", "dated-late", "undated-old", "undated-new"}
	got := holdViewIDs(g)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if g.Total != 6 || g.Expired != 2 {
		t.Fatalf("total=%d expired=%d, want 6 and 2", g.Total, g.Expired)
	}

	// The same rules read from the store through the API, with a fake clock.
	m := swNewManager(t)
	m.now = func() time.Time { return holdViewNow }
	for _, p := range prds {
		if err := m.store.SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}
	api := NewAPI(m)
	if got := holdViewIDs(api.HeldAutomata()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("API order = %v, want %v", got, want)
	}

	// No held Automaton: the lists are empty, never nil.
	empty := NewAPI(swNewManager(t)).HeldAutomata()
	if empty.Held == nil || empty.Total != 0 || empty.Expired != 0 {
		t.Fatalf("empty group = %+v, want non-nil empty list and zero counts", empty)
	}
}

func TestHoldGroupViewNextLookText(t *testing.T) {
	dated := NewHeldView(holdViewPRD("dated", holdViewTime(36*time.Hour), time.Hour), holdViewNow)
	if dated.NextLookText != "looked at again on 2026-11-16" || dated.Expired {
		t.Fatalf("dated: text=%q expired=%v", dated.NextLookText, dated.Expired)
	}
	if dated.NextLook != holdViewNow.Add(36*time.Hour).Format(time.RFC3339) {
		t.Fatalf("dated NextLook = %q", dated.NextLook)
	}

	undated := NewHeldView(holdViewPRD("undated", nil, time.Hour), holdViewNow)
	if undated.NextLookText != "no date set" || undated.NextLook != "" || undated.Expired {
		t.Fatalf("undated: text=%q next=%q expired=%v", undated.NextLookText, undated.NextLook, undated.Expired)
	}

	expired := NewHeldView(holdViewPRD("expired", holdViewTime(-time.Minute), time.Hour), holdViewNow)
	if expired.NextLookText != "hold expired, ready to review" || !expired.Expired {
		t.Fatalf("expired: text=%q expired=%v", expired.NextLookText, expired.Expired)
	}
	if expired.NextLook == "" {
		t.Fatal("an expired hold still names the date it was to be looked at")
	}

	// The expiry mark alone also reads as expired, even before the clock
	// passes the look-again time.
	marked := holdViewPRD("marked", holdViewTime(time.Hour), time.Hour)
	marked.Hold.ExpiredAt = holdViewTime(-time.Second)
	if v := NewHeldView(marked, holdViewNow); !v.Expired || v.NextLookText != "hold expired, ready to review" {
		t.Fatalf("marked: %+v", v)
	}

	// The record fields carry through.
	p := holdViewPRD("fields", nil, time.Hour)
	p.Project = "datawatch"
	p.Hold.Condition = "until after the release"
	v := NewHeldView(p, holdViewNow)
	if v.ID != "fields" || v.Title != "Automaton fields" || v.Project != "datawatch" ||
		v.Reason != "wait for the release fields" || v.Condition != "until after the release" ||
		v.HeldBy != "operator" || v.From != string(PRDApproved) || v.HeldAt == nil {
		t.Fatalf("fields: %+v", v)
	}
}

func TestHoldSupervisorWatchShowsHeldGroupNotBlocked(t *testing.T) {
	// A held Automaton that still carries the triage record of the failure it
	// was held from, a task held for review and a finished merge-pending set
	// of tasks: every check that would have raised a finding stays quiet.
	held := workTestPRD("held-1",
		&TriageRecord{PRDID: "held-1", Status: "merge_pending", TaskID: "t-fail"},
		workTestTask("held-1", "t-fail", TaskFailed),
		workTestTask("held-1", "t-review", TaskReviewNeeded))
	held.Status = PRDHeld
	held.Hold = &HoldState{Reason: "wait for the release", HeldAt: holdViewNow, From: PRDBlocked}

	control := workTestPRD("blocked-1",
		&TriageRecord{PRDID: "blocked-1", Status: "blocked", TaskID: "t-fail"},
		workTestTask("blocked-1", "t-fail", TaskFailed),
		workTestTask("blocked-1", "t-review", TaskReviewNeeded))
	control.Status = PRDBlocked

	w := NewSupervisorWatchWork(&fakeSupervisorWatchPRDLister{prds: []*PRD{held, control}}, nil, nil)
	var raised []SupervisorWatchFinding
	raised = append(raised, w.CheckTaskFailed()...)
	raised = append(raised, w.CheckTaskReviewNeeded()...)
	raised = append(raised, w.CheckAutomatonNotMerged()...)
	for _, f := range raised {
		if f.Subject == "held-1" {
			t.Fatalf("a held Automaton raised %s", f.Kind)
		}
	}
	if len(raised) != 2 {
		t.Fatalf("the blocked control should raise a task-failed and a task-review finding, got %d: %+v", len(raised), raised)
	}

	// The read view counts the blocked and waiting-for-review Automata
	// without the held one, and lists the held one in its own group.
	m := swNewManager(t)
	m.now = func() time.Time { return holdViewNow }
	for _, p := range []*PRD{
		held,
		control,
		{ID: "review-1", Title: "Automaton review-1", Status: PRDNeedsReview},
		holdViewPRD("held-2", holdViewTime(-time.Hour), time.Hour),
	} {
		if err := m.store.SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}
	got := m.SupervisorWatchStatus()
	if got.BlockedAutomata != 1 || got.WaitingForReviewAutomata != 1 {
		t.Fatalf("blocked=%d waiting for review=%d, want 1 and 1 (held excluded)", got.BlockedAutomata, got.WaitingForReviewAutomata)
	}
	if got.Held.Total != 2 || got.Held.Expired != 1 || len(got.Held.Held) != 2 {
		t.Fatalf("held group = %+v, want 2 held, 1 expired", got.Held)
	}
	if got.Held.Held[0].ID != "held-2" {
		t.Fatalf("the expired Automaton should come first, got %v", holdViewIDs(got.Held))
	}
	if len(got.Findings) != 0 {
		t.Fatalf("a held Automaton raised a stored finding: %+v", got.Findings)
	}

	var out strings.Builder
	RenderSupervisorWatch(&out, &got)
	text := out.String()
	for _, want := range []string{
		"Automata: 1 blocked, 1 waiting for review",
		"Held by the operator: 2 Automata (1 expired)",
		"wait for the release held-2",
		"hold expired, ready to review",
		"wait for the release — no date set",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("watch text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "PRD") {
		t.Errorf("watch text says PRD:\n%s", text)
	}
}

func TestHoldTriageShowsHeldGroup(t *testing.T) {
	h := newTriageHarness(t, 3, 10)
	h.failPRD(t, 0, "boom-one", "operator ended it")
	h.failPRD(t, 1, "boom-two", "operator ended it")
	h.h.c.triagePass()

	held := h.h.prds[1].ID
	h.h.m.now = func() time.Time { return holdViewNow }
	until := holdViewNow.Add(48 * time.Hour)
	if _, err := h.h.m.Hold(held, HoldRequest{Reason: "wait for the release", Until: &until}, "operator"); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	tl := TriageList{
		Records:      h.h.m.ListTriageRecords(),
		AlertMinutes: h.h.m.TriageAlertMinutes(),
		Held:         NewAPI(h.h.m).HeldAutomata(),
	}
	if len(tl.Records) != 1 || tl.Records[0].PRDID == held {
		t.Fatalf("the failed records must not include the held Automaton: %+v", tl.Records)
	}
	if tl.Held.Total != 1 || tl.Held.Held[0].ID != held || tl.Held.Held[0].Reason != "wait for the release" {
		t.Fatalf("held group = %+v", tl.Held)
	}

	text := RenderTriageText(tl)
	for _, want := range []string{
		"Triage: 1 Automaton waiting for the operator",
		"Held by the operator: 1 Automaton",
		"wait for the release",
		"looked at again on 2026-11-16",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("triage text lacks %q:\n%s", want, text)
		}
	}

	// With nothing failed or blocked the held group still shows.
	only := RenderTriageText(TriageList{Records: []*TriageRecord{}, Held: tl.Held})
	if !strings.Contains(only, "No failed or blocked Automata to triage.") || !strings.Contains(only, "Held by the operator: 1 Automaton") {
		t.Errorf("held-only triage text:\n%s", only)
	}

	// The body survives a round trip, and an old body with no held key
	// reads as an empty group.
	raw := `{"records":[],"alert_minutes":30}`
	parsed, err := ParseTriageList([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Held.Held == nil || parsed.Held.Total != 0 {
		t.Fatalf("a body with no held key must read as an empty group, got %+v", parsed.Held)
	}
}

func TestHoldAttentionListExcludesHeld(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	attentionFixture(t, st)
	save := func(p *PRD) {
		t.Helper()
		if err := st.SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}
	save(&PRD{ID: "p-held", Status: PRDHeld, Hold: &HoldState{Reason: "wait for the release", From: PRDBlocked}})
	// A held Automaton with a task held for review is still not attention.
	save(&PRD{ID: "p-held-review", Status: PRDHeld, Hold: &HoldState{Reason: "wait", From: PRDRunning},
		Story: []Story{{ID: "s1", Tasks: []Task{{ID: "t1", Status: TaskReviewNeeded}}}}})

	api := NewAPI(m)
	for name, statuses := range map[string][]string{
		"default":          ResolveAttentionStatuses(nil),
		"held is listed":   {"blocked", "needs_review", "paused", "held"},
		"only held listed": {"held"},
	} {
		for _, id := range attentionIDs(t, api.AttentionAutomata(statuses)) {
			if id == "p-held" || id == "p-held-review" {
				t.Errorf("%s: the attention list returned held Automaton %s", name, id)
			}
		}
	}
	if got := strings.Join(DefaultAttentionStatuses, ","); got != "blocked,needs_review,paused" {
		t.Fatalf("default attention statuses = %s", got)
	}
	// The other Automata still show, so the exclusion is not a blanket drop.
	want := []string{"p-blocked", "p-paused", "p-running-review"}
	if got := attentionIDs(t, api.AttentionAutomata(DefaultAttentionStatuses)); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("attention items = %v, want %v", got, want)
	}
}
