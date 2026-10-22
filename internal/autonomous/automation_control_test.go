package autonomous

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// cancelStarter is a fakeStarter that also records the sessions it was asked to
// end, and can refuse one.
type cancelStarter struct {
	*fakeStarter
	cmu       sync.Mutex
	cancelled []string
	cancelErr error
}

func (s *cancelStarter) Cancel(id string) error {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	s.cancelled = append(s.cancelled, id)
	return s.cancelErr
}

func (s *cancelStarter) ended() []string {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	return append([]string(nil), s.cancelled...)
}

// newControlHarness is a coordinator harness whose Manager knows its
// coordinator (as after Coordinator.Start) and whose starter can end sessions.
func newControlHarness(t *testing.T, nPRDs int) (*coordHarness, *cancelStarter) {
	t.Helper()
	h := newCoordHarness(t, nPRDs)
	st := &cancelStarter{fakeStarter: h.fake}
	h.c.starter = st
	h.m.coordinator = h.c
	return h, st
}

// cm is a member over harness PRD i with the given outcome edges.
func (h *coordHarness) cm(i int, onComplete, onBlocked, onFailed string) AutomationMember {
	return AutomationMember{
		ID: "m" + string(rune('0'+i)), PRDID: h.prds[i].ID,
		OnComplete: onComplete, OnBlocked: onBlocked, OnFailed: onFailed,
	}
}

// graph creates a draft Automation over members, entry m0, and forces it active
// with m0 running (the single-cursor state after ActivateAutomation).
func (h *coordHarness) graph(members ...AutomationMember) *Automation {
	h.t.Helper()
	a, err := h.m.CreateAutomation(AutomationInput{Name: "ctl", EntryMemberID: "m0", Members: members}, "operator")
	if err != nil {
		h.t.Fatal(err)
	}
	h.setStatus(0, PRDRunning)
	return h.force(a)
}

// diamond is m0 -> m1 (complete) / m2 (blocked) / m3 (failed), m1 -> m2.
func (h *coordHarness) diamond() *Automation {
	return h.graph(
		h.cm(0, "m1", "m2", "m3"),
		h.cm(1, "m2", "", ""),
		h.cm(2, "", "", ""),
		h.cm(3, "", "", ""),
	)
}

func (h *coordHarness) auditDetail(action string) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.audits) - 1; i >= 0; i-- {
		if h.audits[i] == action {
			return h.auditD[i]
		}
	}
	return nil
}

func wantErr(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want an error containing %q, got nil", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("error %q does not contain %q", err.Error(), p)
		}
	}
}

func TestAutomationControlPause(t *testing.T) {
	t.Run("active to paused", func(t *testing.T) {
		h, st := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		got, err := h.m.PauseAutomation(a.ID, "alice", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != AutomationPaused || got.Version != a.Version+1 {
			t.Fatalf("status %s version %d", got.Status, got.Version)
		}
		if m := automationMember(got, "m0"); m.Status != MemberRunning {
			t.Fatalf("in-flight member was touched: %s", m.Status)
		}
		if len(st.ended()) != 0 {
			t.Fatalf("pause ended sessions: %v", st.ended())
		}
		d := got.Decisions[len(got.Decisions)-1]
		if d.Kind != "automation_pause" || d.Actor != "alice" {
			t.Fatalf("decision %+v", d)
		}
		det := h.auditDetail("automation_pause")
		if det == nil || det["actor"] != "alice" || det["version"] != got.Version {
			t.Fatalf("audit %v", det)
		}
	})
	t.Run("second pause rejected", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		p, _ := h.m.PauseAutomation(a.ID, "", a.Version)
		_, err := h.m.PauseAutomation(a.ID, "", p.Version)
		wantErr(t, err, "409: ", "paused")
	})
	t.Run("draft rejected", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		a := h.makeAuto([][3]string{{"m1", "", ""}}, 2)
		_, err := h.m.PauseAutomation(a.ID, "", a.Version)
		wantErr(t, err, "409: ", "activate it first")
	})
	t.Run("unknown", func(t *testing.T) {
		h, _ := newControlHarness(t, 1)
		_, err := h.m.PauseAutomation("nope", "", 1)
		wantErr(t, err, "404: ")
	})
}

func TestAutomationControlResume(t *testing.T) {
	t.Run("paused to active", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		p, _ := h.m.PauseAutomation(a.ID, "", a.Version)
		got, err := h.m.ResumeAutomation(a.ID, "bob", p.Version)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != AutomationActive || got.Version != p.Version+1 {
			t.Fatalf("status %s version %d", got.Status, got.Version)
		}
		if m := automationMember(got, "m0"); m.Status != MemberRunning {
			t.Fatalf("running member restarted or changed: %s", m.Status)
		}
		if runs, resumes := h.fake.counts(); len(runs)+len(resumes) != 0 {
			t.Fatalf("resume restarted members: %v %v", runs, resumes)
		}
		if h.auditDetail("automation_resume") == nil {
			t.Fatal("no resume audit")
		}
	})
	t.Run("active rejected", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		_, err := h.m.ResumeAutomation(a.ID, "", a.Version)
		wantErr(t, err, "409: ", "active")
	})
	t.Run("continues from the cursor", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		if _, err := h.m.PauseAutomation(a.ID, "", a.Version); err != nil {
			t.Fatal(err)
		}
		// The running member ends while paused: the cursor moves and nothing starts.
		h.setStatus(0, PRDCompleted)
		h.event(0, PRDCompleted)
		mid := h.get(a)
		if st := automationMember(mid, "m1").Status; mid.CurrentMemberID != "m1" || (st != MemberPending && st != "") {
			t.Fatalf("cursor %s m1 %q", mid.CurrentMemberID, st)
		}
		if runs, _ := h.fake.counts(); len(runs) != 0 {
			t.Fatalf("paused automation started %v", runs)
		}
		got, err := h.m.ResumeAutomation(a.ID, "", mid.Version)
		if err != nil {
			t.Fatal(err)
		}
		if m := automationMember(got, "m1"); m.Status != MemberRunning {
			t.Fatalf("m1 %s", m.Status)
		}
		if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
			t.Fatalf("runs %v", runs)
		}
	})
}

func TestAutomationControlCancel(t *testing.T) {
	chain := func(h *coordHarness) *Automation {
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "m2", "", ""), h.cm(2, "", "", ""))
		h.setStatus(0, PRDRunning)
		return a
	}

	t.Run("choice missing", func(t *testing.T) {
		h, st := newControlHarness(t, 3)
		a := chain(h)
		_, err := h.m.CancelAutomation(a.ID, "", false, false, a.Version)
		wantErr(t, err, "cancel_in_flight", "cancel_in_flight=true", "cancel_in_flight=false", "operator must pick")
		if got := h.get(a); got.Status != AutomationActive || got.Version != a.Version {
			t.Fatalf("state changed: %s v%d", got.Status, got.Version)
		}
		if len(st.ended()) != 0 {
			t.Fatalf("ended %v", st.ended())
		}
	})

	for _, tc := range []struct {
		name         string
		inFlight     bool
		wantM0       MemberStatus
		wantEnded    int
		wantCursorM0 bool
	}{
		{"cancel_in_flight=true", true, MemberCancelled, 1, false},
		{"cancel_in_flight=false", false, MemberRunning, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, st := newControlHarness(t, 3)
			a := chain(h)
			got, err := h.m.CancelAutomation(a.ID, "carol", tc.inFlight, true, a.Version)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != AutomationCancelled || got.FinishedAt == nil || got.Version != a.Version+1 {
				t.Fatalf("status %s finished %v version %d", got.Status, got.FinishedAt, got.Version)
			}
			if m := automationMember(got, "m0"); m.Status != tc.wantM0 {
				t.Fatalf("in-flight member %s, want %s", m.Status, tc.wantM0)
			}
			for _, id := range []string{"m1", "m2"} {
				if m := automationMember(got, id); m.Status != MemberCancelled {
					t.Fatalf("%s %s, want cancelled", id, m.Status)
				}
			}
			ended := st.ended()
			if len(ended) != tc.wantEnded || (tc.wantEnded == 1 && ended[0] != h.prds[0].ID) {
				t.Fatalf("ended %v", ended)
			}
			if (got.CurrentMemberID == "m0") != tc.wantCursorM0 {
				t.Fatalf("cursor %q", got.CurrentMemberID)
			}
			det := h.auditDetail("automation_cancel")
			if det == nil || det["cancel_in_flight"] != tc.inFlight || det["actor"] != "carol" || det["version"] != got.Version {
				t.Fatalf("audit %v", det)
			}
			if d := got.Decisions[len(got.Decisions)-1]; d.Kind != "automation_cancel" {
				t.Fatalf("decision %+v", d)
			}
			// The member PRDs are released: a cancelled Automation owns nothing.
			if p, _ := h.m.store.GetPRD(h.prds[1].ID); p.AutomationID != "" {
				t.Fatalf("prd still owned by %s", p.AutomationID)
			}
		})
	}

	t.Run("end failure keeps it cancelled", func(t *testing.T) {
		h, st := newControlHarness(t, 3)
		st.cancelErr = errors.New("boom")
		a := chain(h)
		got, err := h.m.CancelAutomation(a.ID, "", true, true, a.Version)
		wantErr(t, err, "boom", h.prds[0].ID)
		if got == nil || got.Status != AutomationCancelled {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("finished member session is not ended", func(t *testing.T) {
		h, st := newControlHarness(t, 3)
		a := chain(h)
		h.setStatus(0, PRDCompleted)
		if _, err := h.m.CancelAutomation(a.ID, "", true, true, a.Version); err != nil {
			t.Fatal(err)
		}
		if len(st.ended()) != 0 {
			t.Fatalf("ended %v", st.ended())
		}
	})
	t.Run("draft and terminal rejected", func(t *testing.T) {
		hd, _ := newControlHarness(t, 2)
		d := hd.makeAuto([][3]string{{"m1", "", ""}}, 2)
		_, err := hd.m.CancelAutomation(d.ID, "", true, true, d.Version)
		wantErr(t, err, "activate it first")
		h, _ := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		c, err := h.m.CancelAutomation(a.ID, "", true, true, a.Version)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.m.CancelAutomation(a.ID, "", true, true, c.Version)
		wantErr(t, err, "409: ", "cancelled")
	})
}

func TestAutomationControlSkipEdge(t *testing.T) {
	for _, tc := range []struct {
		name       string
		edge       string
		wantEdge   string
		wantCursor string
		wantStatus AutomationStatus
		wantErr    string
	}{
		{"empty defaults to on_complete", "", "on_complete", "m1", AutomationActive, ""},
		{"on_complete", "on_complete", "on_complete", "m1", AutomationActive, ""},
		{"on_blocked", "on_blocked", "on_blocked", "m2", AutomationActive, ""},
		{"on_failed", "on_failed", "on_failed", "m3", AutomationActive, ""},
		{"bad edge", "sideways", "", "m0", AutomationActive, "sideways"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newControlHarness(t, 4)
			a := h.diamond()
			got, err := h.m.SkipMember(a.ID, "", tc.edge, "dave", a.Version)
			if tc.wantErr != "" {
				wantErr(t, err, tc.wantErr, "on_complete")
				if cur := h.get(a); cur.Version != a.Version || cur.CurrentMemberID != "m0" {
					t.Fatalf("state changed on a rejected edge")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			m0 := automationMember(got, "m0")
			if m0.Status != MemberSkipped || m0.EdgeTaken != tc.wantEdge || m0.FinishedAt == nil {
				t.Fatalf("m0 %s edge %q", m0.Status, m0.EdgeTaken)
			}
			if got.CurrentMemberID != tc.wantCursor {
				t.Fatalf("cursor %q, want %q", got.CurrentMemberID, tc.wantCursor)
			}
			if next := automationMember(got, tc.wantCursor); next.Status != MemberRunning {
				t.Fatalf("next member %s", next.Status)
			}
			if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[int(tc.wantCursor[1]-'0')].ID {
				t.Fatalf("runs %v", runs)
			}
			det := h.auditDetail("automation_skip_member")
			if det == nil || det["member"] != "m0" || det["edge"] != tc.wantEdge || det["actor"] != "dave" || det["version"] != got.Version {
				t.Fatalf("audit %v", det)
			}
			if !hasCoordDecision(got.Decisions, "automation_skip_member") {
				t.Fatal("no skip decision")
			}
		})
	}

	t.Run("skip on a path that ends completes the automation", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		s1, err := h.m.SkipMember(a.ID, "m0", "", "", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		s2, err := h.m.SkipMember(a.ID, "", "on_complete", "", s1.Version)
		if err != nil {
			t.Fatal(err)
		}
		if s2.Status != AutomationCompleted {
			t.Fatalf("status %s", s2.Status)
		}
	})
	t.Run("naming a member that is not the cursor is refused", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		_, err := h.m.SkipMember(a.ID, "m1", "", "", a.Version)
		wantErr(t, err, "m1", "m0")
	})
}

func TestAutomationControlRemove(t *testing.T) {
	for _, tc := range []struct {
		name    string
		member  string
		wantErr []string
	}{
		{"outgoing edges", "m1", []string{"409: ", "outgoing edges"}},
		{"entry with edges", "m0", []string{"409: ", "outgoing edges"}},
		{"clean member", "m2", nil},
		{"clean member behind an edge", "m3", nil},
		{"unknown member", "zz", []string{"404: "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newControlHarness(t, 4)
			a := h.diamond()
			got, err := h.m.RemoveMember(a.ID, tc.member, "erin", a.Version)
			if tc.wantErr != nil {
				wantErr(t, err, tc.wantErr...)
				if cur := h.get(a); cur.Version != a.Version || len(cur.Members) != 4 {
					t.Fatal("state changed on a rejected remove")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if automationMember(got, tc.member) != nil || len(got.Members) != 3 || got.Version != a.Version+1 {
				t.Fatalf("members %d version %d", len(got.Members), got.Version)
			}
			for _, m := range got.Members {
				if m.OnComplete == tc.member || m.OnBlocked == tc.member || m.OnFailed == tc.member {
					t.Fatalf("member %s still names the removed member", m.ID)
				}
			}
			idx := int(tc.member[1] - '0')
			if p, _ := h.m.store.GetPRD(h.prds[idx].ID); p.AutomationID != "" {
				t.Fatalf("removed member prd still owned by %s", p.AutomationID)
			}
			det := h.auditDetail("automation_remove_member")
			if det == nil || det["member"] != tc.member || det["actor"] != "erin" || det["version"] != got.Version {
				t.Fatalf("audit %v", det)
			}
		})
	}
	t.Run("entry member", func(t *testing.T) {
		h, _ := newControlHarness(t, 1)
		a := h.graph(h.cm(0, "", "", ""))
		_, err := h.m.RemoveMember(a.ID, "m0", "", a.Version)
		wantErr(t, err, "409: ", "entry member")
	})
	t.Run("in-flight member", func(t *testing.T) {
		h, _ := newControlHarness(t, 2)
		a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
		s, err := h.m.SkipMember(a.ID, "", "", "", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.m.RemoveMember(a.ID, "m1", "", s.Version)
		wantErr(t, err, "409: ", "in flight")
	})
}

func TestAutomationControlReorder(t *testing.T) {
	t.Run("started member rejected", func(t *testing.T) {
		h, _ := newControlHarness(t, 4)
		a := h.diamond()
		_, err := h.m.ReorderEdges(a.ID, "m0", "m3", "m2", "m1", "", a.Version)
		wantErr(t, err, "409: ", "in flight")
		if cur := h.get(a); cur.Version != a.Version {
			t.Fatal("state changed on a rejected reorder")
		}
	})
	t.Run("not-started member rewired", func(t *testing.T) {
		h, _ := newControlHarness(t, 4)
		a := h.diamond()
		got, err := h.m.ReorderEdges(a.ID, "m1", "m3", "m2", "", "frank", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		m1 := automationMember(got, "m1")
		if m1.OnComplete != "m3" || m1.OnBlocked != "m2" || m1.OnFailed != "" || got.Version != a.Version+1 {
			t.Fatalf("m1 %+v version %d", m1, got.Version)
		}
		det := h.auditDetail("automation_reorder_edges")
		if det == nil || det["member"] != "m1" || det["actor"] != "frank" || det["version"] != got.Version {
			t.Fatalf("audit %v", det)
		}
		if !hasCoordDecision(got.Decisions, "automation_reorder_edges") {
			t.Fatal("no reorder decision")
		}
	})
	t.Run("invalid graph rejected", func(t *testing.T) {
		h, _ := newControlHarness(t, 4)
		a := h.diamond()
		_, err := h.m.ReorderEdges(a.ID, "m1", "m1", "", "", "", a.Version)
		wantErr(t, err, "m1")
		_, err = h.m.ReorderEdges(a.ID, "m1", "nope", "", "", "", a.Version)
		wantErr(t, err, "nope")
		if cur := h.get(a); cur.Version != a.Version {
			t.Fatal("state changed on a rejected reorder")
		}
	})
	t.Run("edge to a started member rejected", func(t *testing.T) {
		h, _ := newControlHarness(t, 4)
		a := h.diamond()
		_, err := h.m.ReorderEdges(a.ID, "m1", "m0", "", "", "", a.Version)
		wantErr(t, err, "m0")
	})
}

func TestAutomationControlVersionConflict(t *testing.T) {
	stale := func(a *Automation) int64 { return a.Version + 5 }
	for _, tc := range []struct {
		name  string
		setup func(h *coordHarness) *Automation
		call  func(h *coordHarness, a *Automation) error
	}{
		{"pause", (*coordHarness).diamond, func(h *coordHarness, a *Automation) error {
			_, err := h.m.PauseAutomation(a.ID, "", stale(a))
			return err
		}},
		{"resume", func(h *coordHarness) *Automation {
			a := h.diamond()
			p, err := h.m.PauseAutomation(a.ID, "", a.Version)
			if err != nil {
				h.t.Fatal(err)
			}
			return p
		}, func(h *coordHarness, a *Automation) error {
			_, err := h.m.ResumeAutomation(a.ID, "", stale(a))
			return err
		}},
		{"cancel", (*coordHarness).diamond, func(h *coordHarness, a *Automation) error {
			_, err := h.m.CancelAutomation(a.ID, "", true, true, stale(a))
			return err
		}},
		{"skip", (*coordHarness).diamond, func(h *coordHarness, a *Automation) error {
			_, err := h.m.SkipMember(a.ID, "", "", "", stale(a))
			return err
		}},
		{"remove", (*coordHarness).diamond, func(h *coordHarness, a *Automation) error {
			_, err := h.m.RemoveMember(a.ID, "m2", "", stale(a))
			return err
		}},
		{"reorder", (*coordHarness).diamond, func(h *coordHarness, a *Automation) error {
			_, err := h.m.ReorderEdges(a.ID, "m1", "m3", "", "", "", stale(a))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, st := newControlHarness(t, 4)
			a := tc.setup(h)
			err := tc.call(h, a)
			wantErr(t, err, "409: ")
			if !strings.Contains(err.Error(), "expected version") || !strings.Contains(err.Error(), "current version") {
				t.Fatalf("error does not name both versions: %v", err)
			}
			cur := h.get(a)
			if cur.Version != a.Version || cur.Status != a.Status || len(cur.Decisions) != len(a.Decisions) {
				t.Fatalf("state changed: v%d %s %d decisions", cur.Version, cur.Status, len(cur.Decisions))
			}
			if runs, _ := h.fake.counts(); len(runs) != 0 {
				t.Fatalf("started %v", runs)
			}
			if len(st.ended()) != 0 {
				t.Fatalf("ended %v", st.ended())
			}
		})
	}
}

func TestAutomationControlBranchNaming(t *testing.T) {
	setup := func(t *testing.T) (*coordHarness, *Automation) {
		h, _ := newControlHarness(t, 4)
		st := newBranchStarter()
		a := h.build(h.forkMembers("", ""), st)
		a = h.activateEntry(a)
		h.startFork(a)
		got := h.get(a)
		if len(got.ActiveCursors) != 2 {
			t.Fatalf("cursors %v", got.ActiveCursors)
		}
		return h, got
	}

	for _, tc := range []struct {
		name   string
		member string
	}{
		{"unnamed", ""},
		{"not a cursor", "e0"},
		{"unknown", "zz"},
	} {
		t.Run("reject "+tc.name, func(t *testing.T) {
			h, a := setup(t)
			_, err := h.m.SkipMember(a.ID, tc.member, "", "", a.Version)
			wantErr(t, err, "b1", "b2", "branch")
			if cur := h.get(a); cur.Version != a.Version {
				t.Fatal("state changed on a rejected skip")
			}
		})
	}

	t.Run("named branch skipped", func(t *testing.T) {
		h, a := setup(t)
		got, err := h.m.SkipMember(a.ID, "b1", "", "", a.Version)
		if err != nil {
			t.Fatal(err)
		}
		b1 := automationMember(got, "b1")
		if b1.Status != MemberSkipped || b1.EdgeTaken != "on_complete" {
			t.Fatalf("b1 %s edge %q", b1.Status, b1.EdgeTaken)
		}
		if automationMember(got, "b2").Status != MemberRunning {
			t.Fatal("the sibling branch was touched")
		}
		if len(got.ActiveCursors) != 1 || got.ActiveCursors[0] != "b2" {
			t.Fatalf("cursors %v", got.ActiveCursors)
		}
		// One branch is left, so it needs no name.
		again, err := h.m.SkipMember(a.ID, "", "on_failed", "", got.Version)
		if err != nil {
			t.Fatal(err)
		}
		if automationMember(again, "b2").Status != MemberSkipped {
			t.Fatal("b2 not skipped")
		}
	})
}
