package autonomous

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStarter records starts and flips the PRD to running like the real API.
type fakeStarter struct {
	mu      sync.Mutex
	m       *Manager
	runs    []string
	resumes []string
	runErr  error
}

func (f *fakeStarter) Run(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, id)
	if f.runErr != nil {
		return f.runErr
	}
	return nil
}

func (f *fakeStarter) Resume(id, actor string) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumes = append(f.resumes, id+":"+actor)
	return nil, nil
}

func (f *fakeStarter) counts() (runs, resumes []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.runs...), append([]string(nil), f.resumes...)
}

type coordHarness struct {
	t      *testing.T
	m      *Manager
	c      *Coordinator
	fake   *fakeStarter
	prds   []*PRD
	mu     sync.Mutex
	alerts []string // "level|title|body"
	audits []string // action
	auditD []map[string]any
}

func newCoordHarness(t *testing.T, nPRDs int) *coordHarness {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h := &coordHarness{t: t, m: m, fake: &fakeStarter{m: m}}
	h.prds = makeAutomationMemberPRDs(t, m, nPRDs)
	names := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	for i, p := range h.prds {
		p.Title = names[i%len(names)]
		if err := m.store.SavePRD(p); err != nil {
			t.Fatal(err)
		}
		h.setStatus(i, PRDApproved)
	}
	h.c = NewCoordinator(m, h.fake)
	h.c.alertFn = func(level, title, body string) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.alerts = append(h.alerts, level+"|"+title+"|"+body)
	}
	h.c.auditFn = func(action string, d map[string]any) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.audits = append(h.audits, action)
		h.auditD = append(h.auditD, d)
	}
	return h
}

func (h *coordHarness) setStatus(i int, s PRDStatus) {
	h.t.Helper()
	p, ok := h.m.store.GetPRD(h.prds[i].ID)
	if !ok {
		h.t.Fatalf("prd %d missing", i)
	}
	p.Status = s
	if err := h.m.store.SavePRD(p); err != nil {
		h.t.Fatal(err)
	}
}

// makeAuto creates an Automation whose members are prds[0..n) with the given
// edges (member IDs m0, m1, ...), then force-activates it at the entry.
func (h *coordHarness) makeAuto(edges [][3]string, n int) *Automation {
	h.t.Helper()
	var members []AutomationMember
	for i := 0; i < n; i++ {
		mem := AutomationMember{ID: "m" + string(rune('0'+i)), PRDID: h.prds[i].ID}
		if i < len(edges) {
			mem.OnComplete, mem.OnBlocked, mem.OnFailed = edges[i][0], edges[i][1], edges[i][2]
		}
		members = append(members, mem)
	}
	a, err := h.m.CreateAutomation(AutomationInput{Name: "chain", Members: members}, "operator")
	if err != nil {
		h.t.Fatal(err)
	}
	return a
}

func (h *coordHarness) force(a *Automation) *Automation {
	h.t.Helper()
	out, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		now := time.Now()
		a.Status = AutomationActive
		a.StartedAt = &now
		a.CurrentMemberID = a.EntryMemberID
		a.Members[0].Status = MemberRunning
		a.Members[0].StartedAt = &now
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *coordHarness) event(i int, s PRDStatus) {
	h.c.onMemberTerminal(LifecycleEvent{Level: LevelPRD, Kind: PRDStatusKind(s), PRDID: h.prds[i].ID, NewStatus: string(s)})
}

func (h *coordHarness) get(a *Automation) *Automation {
	h.t.Helper()
	out, ok := h.m.automations.Get(a.ID)
	if !ok {
		h.t.Fatal("automation gone")
	}
	return out
}

func (h *coordHarness) hasAudit(action string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, a := range h.audits {
		if a == action {
			return true
		}
	}
	return false
}

func (h *coordHarness) alertCount(prefix string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, a := range h.alerts {
		if strings.HasPrefix(a, prefix) {
			n++
		}
	}
	return n
}

func hasCoordDecision(ds []Decision, kind string) bool {
	for _, d := range ds {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

func TestAutomationCoordinator_LinearAdvance(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	runs, _ := h.fake.counts()
	if len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("starter runs = %v, want [%s]", runs, h.prds[1].ID)
	}
	got := h.get(a)
	if got.CurrentMemberID != "m1" || got.Status != AutomationActive || got.IdleSince != nil {
		t.Fatalf("cursor=%q status=%s idle=%v", got.CurrentMemberID, got.Status, got.IdleSince)
	}
	m0, m1 := got.Members[0], got.Members[1]
	if m0.Status != MemberCompleted || m0.EdgeTaken != "on_complete" || m0.Outcome != "completed" || m0.FinishedAt == nil {
		t.Fatalf("finished member not recorded: %+v", m0)
	}
	if m1.Status != MemberRunning || m1.StartedAt == nil {
		t.Fatalf("next member not marked running: %+v", m1)
	}
	if !hasCoordDecision(got.Decisions, "advance") {
		t.Fatalf("no advance decision: %+v", got.Decisions)
	}
	next, _ := h.m.store.GetPRD(h.prds[1].ID)
	if !hasCoordDecision(next.Decisions, "automation_advance") {
		t.Fatalf("member PRD missing automation_advance decision: %+v", next.Decisions)
	}
	if !h.hasAudit("automation_advance") {
		t.Fatalf("audit = %v, want automation_advance", h.audits)
	}
	if got.Version != a.Version+1 {
		t.Fatalf("version %d, want %d (one write)", got.Version, a.Version+1)
	}

	// Finish the last member: no on_complete edge completes the Automation.
	h.setStatus(1, PRDCompleted)
	h.event(1, PRDCompleted)
	done := h.get(a)
	if done.Status != AutomationCompleted || done.FinishedAt == nil || done.CurrentMemberID != "" {
		t.Fatalf("not completed: status=%s finished=%v cursor=%q", done.Status, done.FinishedAt, done.CurrentMemberID)
	}
	if h.alertCount("info|") != 1 {
		t.Fatalf("alerts = %v, want one info", h.alerts)
	}
	if p, _ := h.m.store.GetPRD(h.prds[1].ID); p.AutomationID != "" {
		t.Fatalf("membership not released on completion: %q", p.AutomationID)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 {
		t.Fatalf("starter runs after completion = %v", runs)
	}
}

func TestAutomationCoordinator_EdgeMappingTable(t *testing.T) {
	cases := []struct {
		name       string
		status     PRDStatus
		memRunning bool // member status before the event
		wantAuto   AutomationStatus
		wantCursor string
		wantMember MemberStatus
		wantEdge   string
		wantReason string
		wantRun    string // member id started
		wantNoop   bool
	}{
		{name: "completed", status: PRDCompleted, memRunning: true, wantAuto: AutomationActive, wantCursor: "m1", wantMember: MemberCompleted, wantEdge: "on_complete", wantRun: "m1"},
		{name: "blocked", status: PRDBlocked, memRunning: true, wantAuto: AutomationActive, wantCursor: "m2", wantMember: MemberBlocked, wantEdge: "on_blocked", wantRun: "m2"},
		{name: "failed", status: PRDFailed, memRunning: true, wantAuto: AutomationActive, wantCursor: "m3", wantMember: MemberFailed, wantEdge: "on_failed", wantRun: "m3"},
		{name: "cancelled", status: PRDCancelled, memRunning: true, wantAuto: AutomationBlocked, wantCursor: "m0", wantMember: MemberBlocked, wantReason: "member cancelled outside the Automation"},
		{name: "archived", status: PRDArchived, memRunning: true, wantAuto: AutomationBlocked, wantCursor: "m0", wantMember: MemberBlocked, wantReason: "member cancelled outside the Automation"},
		{name: "rejected", status: PRDRejected, memRunning: true, wantAuto: AutomationBlocked, wantCursor: "m0", wantMember: MemberBlocked, wantReason: "member cancelled outside the Automation"},
		{name: "revert-to-approved-after-start", status: PRDApproved, memRunning: true, wantAuto: AutomationBlocked, wantCursor: "m0", wantMember: MemberBlocked, wantReason: "member run errored"},
		{name: "approved-before-start", status: PRDApproved, memRunning: false, wantNoop: true},
		{name: "needs_review", status: PRDNeedsReview, memRunning: true, wantNoop: true},
		{name: "revisions_asked", status: PRDRevisionsAsked, memRunning: true, wantNoop: true},
		{name: "planning", status: PRDPlanning, memRunning: true, wantNoop: true},
		{name: "draft", status: PRDDraft, memRunning: true, wantNoop: true},
		{name: "paused", status: PRDPaused, memRunning: true, wantNoop: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newCoordHarness(t, 4)
			h.c.SetReady()
			a := h.force(h.makeAuto([][3]string{{"m1", "m2", "m3"}}, 4))
			if !tc.memRunning {
				a, _ = h.m.automations.Update(a.ID, 0, func(a *Automation) error {
					a.Members[0].Status = MemberPending
					return nil
				})
			}
			before := h.get(a)
			h.event(0, tc.status)
			got := h.get(a)
			runs, _ := h.fake.counts()
			if tc.wantNoop {
				if got.Version != before.Version || len(runs) != 0 {
					t.Fatalf("expected no change, version %d->%d runs=%v", before.Version, got.Version, runs)
				}
				return
			}
			if got.Status != tc.wantAuto || got.CurrentMemberID != tc.wantCursor {
				t.Fatalf("status=%s cursor=%s, want %s %s", got.Status, got.CurrentMemberID, tc.wantAuto, tc.wantCursor)
			}
			m0 := got.Members[0]
			if m0.Status != tc.wantMember || m0.EdgeTaken != tc.wantEdge || m0.Outcome != string(tc.status) || m0.FinishedAt == nil {
				t.Fatalf("member 0 = %+v", m0)
			}
			if tc.wantReason != "" {
				last := got.Decisions[len(got.Decisions)-1]
				if last.Kind != "blocked" || last.Note != tc.wantReason {
					t.Fatalf("decision = %+v, want blocked %q", last, tc.wantReason)
				}
				if !h.hasAudit("automation_blocked") || h.alertCount("warn|") != 1 {
					t.Fatalf("audits=%v alerts=%v", h.audits, h.alerts)
				}
			}
			if tc.wantRun == "" {
				if len(runs) != 0 {
					t.Fatalf("unexpected starts: %v", runs)
				}
				return
			}
			idx := int(tc.wantRun[1] - '0')
			if len(runs) != 1 || runs[0] != h.prds[idx].ID {
				t.Fatalf("runs = %v, want [%s]", runs, h.prds[idx].ID)
			}
		})
	}
}

func TestAutomationCoordinator_DoubleAdvanceStartsOnce(t *testing.T) {
	for i := 0; i < 25; i++ {
		h := newCoordHarness(t, 2)
		a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
		h.setStatus(0, PRDCompleted)

		var wg sync.WaitGroup
		start := make(chan struct{})
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				h.event(0, PRDCompleted)
			}()
		}
		close(start)
		wg.Wait()

		if runs, _ := h.fake.counts(); len(runs) != 1 {
			t.Fatalf("iteration %d: starter called %d times, want 1 (%v)", i, len(runs), runs)
		}
		got := h.get(a)
		if got.Version != a.Version+1 || got.CurrentMemberID != "m1" {
			t.Fatalf("iteration %d: version=%d cursor=%s", i, got.Version, got.CurrentMemberID)
		}
		if n := strings.Count(strings.Join(h.audits, ","), "automation_advance"); n != 1 {
			t.Fatalf("iteration %d: %d advance audits", i, n)
		}
	}
}

func TestAutomationCoordinator_PausedAutomationMovesCursorWithoutStarting(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	if _, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.Status = AutomationPaused
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	got := h.get(a)
	if got.Status != AutomationPaused || got.CurrentMemberID != "m1" || got.IdleSince == nil {
		t.Fatalf("status=%s cursor=%s idle=%v", got.Status, got.CurrentMemberID, got.IdleSince)
	}
	if got.Members[1].Status == MemberRunning || got.Members[1].StartedAt != nil {
		t.Fatalf("next member was started: %+v", got.Members[1])
	}
	if runs, resumes := h.fake.counts(); len(runs)+len(resumes) != 0 {
		t.Fatalf("paused Automation started a member: %v %v", runs, resumes)
	}
}

func TestAutomationCoordinator_UnapprovedNextMemberBlocks(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	h.setStatus(1, PRDNeedsReview)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	got := h.get(a)
	if got.Status != AutomationBlocked {
		t.Fatalf("status = %s, want blocked", got.Status)
	}
	last := got.Decisions[len(got.Decisions)-1]
	if want := "next member bravo is needs_review"; last.Note != want {
		t.Fatalf("reason = %q, want %q", last.Note, want)
	}
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("started an unapproved member: %v", runs)
	}
	if h.alertCount("warn|") != 1 || !h.hasAudit("automation_blocked") {
		t.Fatalf("alerts=%v audits=%v", h.alerts, h.audits)
	}
}

func TestAutomationCoordinator_PausedNextMemberResumes(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	h.setStatus(1, PRDPaused)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)

	runs, resumes := h.fake.counts()
	if len(runs) != 0 || len(resumes) != 1 || resumes[0] != h.prds[1].ID+":automation" {
		t.Fatalf("runs=%v resumes=%v", runs, resumes)
	}
	if got := h.get(a); got.Status != AutomationActive || got.Members[1].Status != MemberRunning {
		t.Fatalf("status=%s member=%+v", got.Status, got.Members[1])
	}
}

func TestAutomationCoordinator_EmptyEdge(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		h := newCoordHarness(t, 1)
		a := h.force(h.makeAuto(nil, 1))
		h.setStatus(0, PRDCompleted)
		h.event(0, PRDCompleted)
		if got := h.get(a); got.Status != AutomationCompleted {
			t.Fatalf("status = %s", got.Status)
		}
		if h.alertCount("info|") != 1 || h.alertCount("warn|") != 0 {
			t.Fatalf("alerts = %v", h.alerts)
		}
	})
	t.Run("blocked", func(t *testing.T) {
		h := newCoordHarness(t, 1)
		a := h.force(h.makeAuto(nil, 1))
		h.setStatus(0, PRDBlocked)
		h.event(0, PRDBlocked)
		got := h.get(a)
		if got.Status != AutomationBlocked || got.CurrentMemberID != "m0" {
			t.Fatalf("status=%s cursor=%s", got.Status, got.CurrentMemberID)
		}
		if h.alertCount("warn|") != 1 || h.alertCount("info|") != 0 {
			t.Fatalf("alerts = %v", h.alerts)
		}
	})
}

func TestAutomationCoordinator_IgnoresNonMembersAndNonCursor(t *testing.T) {
	h := newCoordHarness(t, 3)
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2)) // prds[2] is not a member
	before := h.get(a)

	h.setStatus(2, PRDCompleted)
	h.event(2, PRDCompleted) // not a member
	h.setStatus(1, PRDCompleted)
	h.event(1, PRDCompleted) // member, but cursor is m0
	h.c.onMemberTerminal(LifecycleEvent{Level: LevelPRD, PRDID: "does-not-exist", NewStatus: "completed"})

	if got := h.get(a); got.Version != before.Version || got.CurrentMemberID != "m0" {
		t.Fatalf("automation changed: version %d->%d cursor=%s", before.Version, got.Version, got.CurrentMemberID)
	}
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("runs = %v", runs)
	}
}

func TestAutomationCoordinator_FailedDefersUntilBootRecovery(t *testing.T) {
	h := newCoordHarness(t, 2)
	a := h.force(h.makeAuto([][3]string{{"", "", "m1"}}, 2))
	h.setStatus(0, PRDFailed)
	h.event(0, PRDFailed)
	if got := h.get(a); got.CurrentMemberID != "m0" {
		t.Fatalf("advanced before boot recovery finished: cursor=%s", got.CurrentMemberID)
	}
	h.c.SetReady()
	got := h.get(a)
	if got.CurrentMemberID != "m1" || got.Members[0].EdgeTaken != "on_failed" {
		t.Fatalf("deferred failure not replayed: cursor=%s member=%+v", got.CurrentMemberID, got.Members[0])
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 {
		t.Fatalf("runs = %v", runs)
	}
}

func TestAutomationCoordinator_StartFailureBlocks(t *testing.T) {
	h := newCoordHarness(t, 2)
	h.fake.runErr = errors.New("executor busy")
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
	got := h.get(a)
	if got.Status != AutomationBlocked {
		t.Fatalf("status = %s, want blocked", got.Status)
	}
	if !strings.Contains(got.Decisions[len(got.Decisions)-1].Note, "executor busy") {
		t.Fatalf("decisions = %+v", got.Decisions)
	}
	if h.alertCount("warn|") != 1 {
		t.Fatalf("alerts = %v", h.alerts)
	}
}

func TestAutomationCoordinator_ActivateStartsEntryMember(t *testing.T) {
	h := newCoordHarness(t, 2)
	h.c.Start()
	defer h.c.Stop()
	a := h.makeAuto([][3]string{{"m1", "", ""}}, 2)

	got, err := h.m.ActivateAutomation(a.ID, "alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationActive || got.StartedAt == nil || got.CurrentMemberID != "m0" || got.Members[0].Status != MemberRunning {
		t.Fatalf("activated = %+v", got)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[0].ID {
		t.Fatalf("runs = %v", runs)
	}
	if !h.hasAudit("automation_activate") || !hasCoordDecision(got.Decisions, "activate") {
		t.Fatalf("audits=%v decisions=%+v", h.audits, got.Decisions)
	}
	if _, err := h.m.ActivateAutomation(a.ID, "alice", 0); err == nil {
		t.Fatal("activating a non-draft Automation should fail")
	}
}

func TestAutomationCoordinator_ActivateBlocksOnUnapprovedEntry(t *testing.T) {
	h := newCoordHarness(t, 2)
	h.c.Start()
	defer h.c.Stop()
	a := h.makeAuto([][3]string{{"m1", "", ""}}, 2)
	h.setStatus(0, PRDNeedsReview)

	got, err := h.m.ActivateAutomation(a.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AutomationBlocked || got.StartedAt == nil {
		t.Fatalf("status=%s started=%v", got.Status, got.StartedAt)
	}
	if runs, _ := h.fake.counts(); len(runs) != 0 {
		t.Fatalf("runs = %v", runs)
	}
	if h.alertCount("warn|") != 1 || !h.hasAudit("automation_activate") {
		t.Fatalf("alerts=%v audits=%v", h.alerts, h.audits)
	}
}

func TestAutomationCoordinator_ActivateWithoutCoordinator(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ActivateAutomation("nope", "x", 0); err == nil {
		t.Fatal("expected an error without a running coordinator")
	}
}

func TestAutomationCoordinator_NilSafe(t *testing.T) {
	var c *Coordinator
	c.Start()
	c.Stop()
	c.SetReady()
	if NewCoordinator(nil, nil) != nil {
		t.Fatal("NewCoordinator(nil, nil) should be nil")
	}
}

func TestAutomationCoordinator_AdvancesFromLifecycleEvents(t *testing.T) {
	h := newCoordHarness(t, 2)
	h.c.Start()
	defer h.c.Stop()
	a := h.force(h.makeAuto([][3]string{{"m1", "", ""}}, 2))

	h.m.emit(LifecycleEvent{Level: LevelPRD, Kind: PRDStatusKind(PRDRunning), PRDID: h.prds[0].ID, NewStatus: "running"})
	h.setStatus(0, PRDCompleted)
	h.m.emit(LifecycleEvent{Level: LevelPRD, Kind: PRDStatusKind(PRDCompleted), PRDID: h.prds[0].ID, NewStatus: "completed"})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runs, _ := h.fake.counts(); len(runs) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs = %v", runs)
	}
	if got := h.get(a); got.CurrentMemberID != "m1" {
		t.Fatalf("cursor = %s", got.CurrentMemberID)
	}
}
