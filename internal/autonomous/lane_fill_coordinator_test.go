package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// laneFillFake is a starter that records every call of any kind. Run, like
// the real API, flips the Automaton to running unless noFlip is set.
type laneFillFake struct {
	mu    sync.Mutex
	m     *Manager
	calls []string

	runErr, approveErr, setErr, decomposeErr error
	noFlip                                   bool
}

func (f *laneFillFake) record(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *laneFillFake) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *laneFillFake) Run(id string) error {
	f.record("run:" + id)
	if f.runErr != nil {
		return f.runErr
	}
	if !f.noFlip {
		f.m.store.MutatePRD(id, func(p *PRD) bool { p.Status = PRDRunning; return true })
	}
	return nil
}

func (f *laneFillFake) Resume(id, actor string) (any, error) {
	f.record("resume:" + id)
	return nil, nil
}

func (f *laneFillFake) Approve(id, actor, note string) (any, error) {
	f.record("approve:" + id + ":" + actor)
	if f.approveErr != nil {
		return nil, f.approveErr
	}
	f.m.store.MutatePRD(id, func(p *PRD) bool { p.Status = PRDApproved; return true })
	return nil, nil
}

func (f *laneFillFake) Decompose(id string) (any, error) {
	f.record("decompose:" + id)
	return nil, f.decomposeErr
}

func (f *laneFillFake) SetPRDLLM(id string, backend, effort, model, dp, dm *string, actor string) (any, error) {
	f.record(fmt.Sprintf("setllm:%s:%s:%s:%s", id, deref(backend), deref(model), actor))
	if f.setErr != nil {
		return nil, f.setErr
	}
	return f.m.SetPRDLLM(id, backend, effort, model, dp, dm, actor)
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

type laneFillEvidenceFake struct {
	ev  PoolEvidence
	err error
}

func (f *laneFillEvidenceFake) Check(ctx context.Context) (PoolEvidence, error) {
	return f.ev, f.err
}

type laneFillHarness struct {
	t     *testing.T
	dir   string
	m     *Manager
	c     *Coordinator
	fake  *laneFillFake
	now   time.Time
	base  time.Time
	mu    sync.Mutex
	audit []map[string]any
	nPRD  int
}

// laneFillConfig is a small lane model: one GPU lane, one Claude lane, and no
// decompose-ahead unless a test asks for it.
func laneFillCoordConfig(mode string) Config {
	cfg := DefaultConfig()
	cfg.LaneFill = LaneFillConfig{
		Mode: mode, GPULanes: 1, ClaudeLanes: 1, IdleAlertMinutes: 30,
		DecomposeAheadMin: 0, PollSeconds: 60, GPUBackend: "pi-prompt", ClaudeBackend: "claude-code",
	}
	return cfg
}

func newLaneFillHarness(t *testing.T, mode string) *laneFillHarness {
	t.Helper()
	return newLaneFillHarnessIn(t, t.TempDir(), laneFillCoordConfig(mode))
}

func newLaneFillHarnessIn(t *testing.T, dir string, cfg Config) *laneFillHarness {
	t.Helper()
	h := newLaneFillHarnessUnstarted(t, dir, cfg)
	h.c.Start()
	t.Cleanup(h.c.Stop)
	return h
}

// newLaneFillHarnessUnstarted builds the harness without starting the
// coordinator, so a test can set fields the loop reads.
func newLaneFillHarnessUnstarted(t *testing.T, dir string, cfg Config) *laneFillHarness {
	t.Helper()
	m, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	h := &laneFillHarness{t: t, dir: dir, m: m, now: base, base: base}
	h.fake = &laneFillFake{m: m}
	h.c = NewCoordinator(m, h.fake)
	h.c.now = func() time.Time { return h.now }
	h.c.auditFn = func(action string, d map[string]any) {
		if action != "lane_fill" {
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		h.audit = append(h.audit, d)
	}
	h.c.SetLaneFillEvidenceSource(&laneFillEvidenceFake{ev: PoolEvidence{CheckedAt: base}})
	return h
}

func (h *laneFillHarness) addPRD(title string, status PRDStatus, pref, backend string) *PRD {
	h.t.Helper()
	p, err := h.m.store.CreatePRD("spec "+title, "/p", backend, "", "")
	if err != nil {
		h.t.Fatal(err)
	}
	h.nPRD++
	p.Title, p.Status, p.LanePreference = title, status, pref
	p.CreatedAt = h.base.Add(time.Duration(h.nPRD) * time.Minute)
	if err := h.m.store.SavePRD(p); err != nil {
		h.t.Fatal(err)
	}
	return p
}

// addDoneTask gives the Automaton one completed task.
func (h *laneFillHarness) addDoneTask(prdID string) {
	h.t.Helper()
	s, err := h.m.store.AddStory(prdID, "story", "")
	if err != nil {
		h.t.Fatal(err)
	}
	task, err := h.m.store.AddTask(prdID, s.ID, "task", "spec", nil)
	if err != nil {
		h.t.Fatal(err)
	}
	h.m.store.MutateTask(task.ID, func(tk *Task) { tk.Status = TaskCompleted })
}

func (h *laneFillHarness) setMode(mode string) {
	cfg := h.m.Config()
	cfg.LaneFill.Mode = mode
	h.m.SetConfig(cfg)
}

func (h *laneFillHarness) poll() {
	h.c.laneFillPoll(context.Background())
	h.c.laneFillWait()
}

func (h *laneFillHarness) status() LaneFillStatus { return h.m.LaneFillStatus(0) }

func (h *laneFillHarness) audits() []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]map[string]any(nil), h.audit...)
}

func findDecision(t *testing.T, st LaneFillStatus, lane string, action LaneFillAction) LaneFillDecision {
	t.Helper()
	for _, d := range st.Decisions {
		if d.LaneID == lane && d.Action == action {
			return d
		}
	}
	t.Fatalf("no %s decision for lane %q in %+v", action, lane, st.Decisions)
	return LaneFillDecision{}
}

func TestLaneFillShadowRecordsAndStartsNothing(t *testing.T) {
	h := newLaneFillHarness(t, "") // empty mode means shadow
	h.c.SetReady()
	gpu := h.addPRD("gpu work", PRDApproved, "gpu", "claude-code")
	review := h.addPRD("claude work", PRDNeedsReview, "claude", "claude-code")
	h.addPRD("draft work", PRDDraft, "", "")

	h.poll()

	st := h.status()
	if st.Mode != "shadow" {
		t.Fatalf("mode = %q, want shadow", st.Mode)
	}
	g := findDecision(t, st, "gpu-1", LaneFillActionStartPRD)
	c := findDecision(t, st, "claude-1", LaneFillActionStartPRD)
	if g.PRDID != gpu.ID || c.PRDID != review.ID {
		t.Fatalf("lanes took %s and %s, want %s and %s", g.PRDID, c.PRDID, gpu.ID, review.ID)
	}
	for _, d := range st.Decisions {
		if d.Executed || d.Error != "" {
			t.Errorf("shadow decision executed or errored: %+v", d)
		}
		if d.Mode != LaneFillModeShadow {
			t.Errorf("decision mode = %q", d.Mode)
		}
	}
	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("shadow mode made starter calls: %v", calls)
	}
	if !st.LastPollAt.Equal(h.now) {
		t.Errorf("last_poll_at = %v, want %v", st.LastPollAt, h.now)
	}
	if len(st.Lanes) != 2 || st.Lanes[0].State != "idle" || st.QueuedEligible.GPU != 1 || st.QueuedEligible.Claude != 1 {
		t.Errorf("lanes %+v queued %+v", st.Lanes, st.QueuedEligible)
	}
}

func TestLaneFillShadowNoEvidenceSourceLeavesGPULaneBlocked(t *testing.T) {
	h := newLaneFillHarness(t, "shadow")
	h.c.SetLaneFillEvidenceSource(nil)
	h.c.SetReady()
	h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")

	h.poll()

	st := h.status()
	if st.Evidence.Error != "no evidence source configured" {
		t.Fatalf("evidence error = %q", st.Evidence.Error)
	}
	d := findDecision(t, st, "gpu-1", LaneFillActionBlocked)
	if !strings.Contains(d.Blocker, "no evidence source configured") {
		t.Errorf("gpu lane blocker = %q", d.Blocker)
	}
	for _, d := range st.Decisions {
		if d.LaneType == LaneTypeGPU && d.Action == LaneFillActionStartPRD {
			t.Errorf("a GPU lane was called idle without evidence: %+v", d)
		}
	}
}

func TestLaneFillShadowLoopPollsOnItsOwn(t *testing.T) {
	h := newLaneFillHarnessUnstarted(t, t.TempDir(), laneFillCoordConfig("shadow"))
	h.c.now = time.Now
	h.c.laneFillTick = 5 * time.Millisecond
	h.c.SetReady()
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")
	h.c.Start()
	t.Cleanup(h.c.Stop)

	deadline := time.Now().Add(5 * time.Second)
	for len(h.status().Decisions) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the loop never recorded a decision")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("shadow loop made calls: %v", calls)
	}
}

func TestLaneFillOnStartsRightWorkOnRightLanes(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	// A claude-code Automaton that prefers the pool: its backend must move.
	gpu := h.addPRD("gpu work", PRDApproved, "gpu", "claude-code")
	// A reviewed-and-decomposed Automaton for the Claude lane: approve first.
	review := h.addPRD("claude work", PRDNeedsReview, "claude", "claude-code")

	h.poll()

	want := []string{
		"setllm:" + gpu.ID + ":pi-prompt::lane_fill",
		"run:" + gpu.ID,
		"approve:" + review.ID + ":lane_fill",
		"run:" + review.ID,
	}
	if got := h.fake.all(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v\nwant    %v", got, want)
	}
	st := h.status()
	for _, d := range st.Decisions {
		if d.Action == LaneFillActionStartPRD && (!d.Executed || d.Error != "") {
			t.Errorf("start not recorded as executed: %+v", d)
		}
	}
	if p, _ := h.m.store.GetPRD(gpu.ID); p.Backend != "pi-prompt" || p.Status != PRDRunning {
		t.Errorf("gpu PRD backend %q status %q", p.Backend, p.Status)
	}

	// The next poll sees both running and starts nothing more.
	h.now = h.now.Add(time.Minute)
	h.poll()
	if got := h.fake.all(); len(got) != len(want) {
		t.Fatalf("second poll made calls: %v", got)
	}
	st = h.status()
	if st.Lanes[0].State != "occupied" || st.Lanes[1].State != "occupied" {
		t.Errorf("lanes after start: %+v", st.Lanes)
	}
}

func TestLaneFillOnRefusedApproveIsRecordedAndNotHammered(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	h.fake.approveErr = errors.New("plan review blocks approval")
	review := h.addPRD("claude work", PRDNeedsReview, "claude", "claude-code")

	h.poll()

	d := findDecision(t, h.status(), "claude-1", LaneFillActionStartPRD)
	if d.Executed || !strings.Contains(d.Error, "plan review blocks approval") || d.Blocker != d.Error {
		t.Fatalf("refusal not recorded: %+v", d)
	}
	for _, call := range h.fake.all() {
		if strings.HasPrefix(call, "run:") {
			t.Fatalf("ran an Automaton whose approve was refused: %v", h.fake.all())
		}
	}
	// A second poll inside the retry window does not call again; one after it does.
	h.now = h.now.Add(time.Minute)
	h.poll()
	if n := len(h.fake.all()); n != 1 {
		t.Fatalf("calls inside the retry window = %v", h.fake.all())
	}
	h.now = h.now.Add(laneFillRetryAfter)
	h.fake.approveErr = nil
	h.poll()
	got := h.fake.all()
	if len(got) != 3 || got[1] != "approve:"+review.ID+":lane_fill" || got[2] != "run:"+review.ID {
		t.Fatalf("calls after the retry window = %v", got)
	}
	d = findDecision(t, h.status(), "claude-1", LaneFillActionStartPRD)
	if !d.Executed || d.Error != "" || d.Blocker != "" {
		t.Errorf("retry not recorded: %+v", d)
	}
}

func TestLaneFillOnRunErrorIsRecorded(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	h.fake.runErr = errors.New("capacity held")
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")

	h.poll()

	d := findDecision(t, h.status(), "claude-1", LaneFillActionStartPRD)
	if d.Executed || !strings.Contains(d.Error, "capacity held") {
		t.Fatalf("run error not recorded: %+v", d)
	}
}

func TestLaneFillOnDecomposeAndDecomposeAheadCallDecompose(t *testing.T) {
	// An idle lane with no ready work decomposes the draft.
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	draft := h.addPRD("draft work", PRDDraft, "claude", "")
	h.poll()
	got := h.fake.all()
	if len(got) != 1 || got[0] != "decompose:"+draft.ID {
		t.Fatalf("calls = %v", got)
	}
	d := findDecision(t, h.status(), "claude-1", LaneFillActionDecompose)
	if !d.Executed {
		t.Errorf("decompose not executed: %+v", d)
	}

	// With the Claude lane busy, a short ready queue decomposes ahead.
	cfg := laneFillCoordConfig("on")
	cfg.LaneFill.GPULanes = 0
	cfg.LaneFill.DecomposeAheadMin = 2
	h2 := newLaneFillHarnessIn(t, t.TempDir(), cfg)
	h2.c.SetReady()
	h2.addPRD("busy", PRDRunning, "claude", "claude-code")
	next := h2.addPRD("next draft", PRDDraft, "claude", "")
	h2.poll()
	got = h2.fake.all()
	if len(got) != 1 || got[0] != "decompose:"+next.ID {
		t.Fatalf("decompose ahead calls = %v", got)
	}
	var found bool
	for _, d := range h2.status().Decisions {
		found = found || (d.Action == LaneFillActionDecomposeAhead && d.Executed)
	}
	if !found {
		t.Errorf("no executed decompose_ahead in %+v", h2.status().Decisions)
	}
}

func TestLaneFillOnMergeFlagCallsNothing(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	p := h.addPRD("finished work", PRDBlocked, "", "claude-code")
	h.addDoneTask(p.ID)

	h.poll()

	var found bool
	for _, d := range h.status().Decisions {
		if d.Action == LaneFillActionReportMerge && d.PRDID == p.ID {
			found = true
			if d.Executed {
				t.Errorf("report_merge marked executed: %+v", d)
			}
		}
	}
	if !found {
		t.Fatalf("no report_merge decision in %+v", h.status().Decisions)
	}
	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("a merge flag caused calls: %v", calls)
	}
}

type laneFillProbeFake struct{ ahead bool }

func (p laneFillProbeFake) NeedsMerge(ctx context.Context, id string) (bool, error) {
	return p.ahead, nil
}

func TestLaneFillOnMergeProbeConfirmsCommitsAhead(t *testing.T) {
	for _, ahead := range []bool{true, false} {
		h := newLaneFillHarness(t, "shadow")
		h.c.SetReady()
		h.c.SetLaneFillMergeProbe(laneFillProbeFake{ahead: ahead})
		p := h.addPRD("finished work", PRDFailed, "", "claude-code")
		h.addDoneTask(p.ID)
		h.poll()
		var found bool
		for _, d := range h.status().Decisions {
			found = found || d.Action == LaneFillActionReportMerge
		}
		if found != ahead {
			t.Errorf("probe ahead=%v: report_merge=%v", ahead, found)
		}
	}
}

func TestLaneFillOffRecordsNothingAndCallsNothing(t *testing.T) {
	h := newLaneFillHarness(t, "off")
	h.c.SetReady()
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")
	h.addPRD("draft", PRDDraft, "", "")

	h.poll()

	st := h.status()
	if st.Mode != "off" || len(st.Decisions) != 0 || len(st.Lanes) != 0 {
		t.Fatalf("status in off mode: %+v", st)
	}
	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("off mode made calls: %v", calls)
	}
	if got := len(h.audits()); got != 0 {
		t.Fatalf("off mode audited %d records", got)
	}
	if _, err := os.Stat(filepath.Join(h.m.store.dir, laneFillRecordsFile)); err == nil {
		t.Errorf("off mode wrote a records file")
	}
}

func TestLaneFillStatusWithoutCoordinator(t *testing.T) {
	var nilMgr *Manager
	if st := nilMgr.LaneFillStatus(10); st.Mode != "shadow" || len(st.Lanes) != 0 || len(st.Decisions) != 0 {
		t.Errorf("nil manager status: %+v", st)
	}
	cfg := laneFillCoordConfig("on")
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := m.LaneFillStatus(10)
	if st.Mode != "on" || st.Lanes == nil || len(st.Lanes) != 0 || st.Decisions == nil || len(st.Decisions) != 0 {
		t.Errorf("status without a coordinator: %+v", st)
	}
	if _, err := json.Marshal(st); err != nil {
		t.Errorf("marshal: %v", err)
	}
}

func TestLaneFillRecordsDedupeCountsRepeatsAndAuditsOncePerRecord(t *testing.T) {
	h := newLaneFillHarness(t, "shadow")
	h.c.SetReady()
	h.addPRD("gpu work", PRDApproved, "gpu", "pi-prompt")
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")

	for i := 0; i < 4; i++ {
		h.poll()
		h.now = h.now.Add(time.Minute)
	}

	st := h.status()
	if len(st.Decisions) != 2 {
		t.Fatalf("records = %d, want 2: %+v", len(st.Decisions), st.Decisions)
	}
	for _, d := range st.Decisions {
		if d.RepeatCount != 4 {
			t.Errorf("repeat_count = %d, want 4: %+v", d.RepeatCount, d)
		}
		if !d.LastSeenAt.Equal(h.base.Add(3 * time.Minute)) {
			t.Errorf("last_seen_at = %v", d.LastSeenAt)
		}
		if !d.At.Equal(h.base) {
			t.Errorf("at = %v, want the first sighting %v", d.At, h.base)
		}
	}
	audits := h.audits()
	if len(audits) != 2 {
		t.Fatalf("audit calls = %d, want one per appended record (2)", len(audits))
	}
	a := audits[0]
	for _, k := range []string{"lane_id", "action", "prd_id", "reason", "blocker", "mode", "executed"} {
		if _, ok := a[k]; !ok {
			t.Errorf("audit details lack %q: %v", k, a)
		}
	}

	// A different decision for the lane appends a new record.
	for _, p := range h.m.store.ListPRDs() {
		if p.LanePreference == "claude" {
			h.m.store.MutatePRD(p.ID, func(p *PRD) bool { p.Status = PRDCancelled; return true })
		}
	}
	h.poll()
	if got := len(h.status().Decisions); got != 3 {
		t.Fatalf("records after a changed decision = %d, want 3", got)
	}
	if got := len(h.audits()); got != 3 {
		t.Fatalf("audit calls = %d, want 3", got)
	}
}

func TestLaneFillRecordsBoundedAtAThousandNewestFirst(t *testing.T) {
	dir := t.TempDir()
	r := newLaneFillRecords(dir)
	base := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	for i := 0; i < laneFillRecordLimit+50; i++ {
		d := LaneFillDecision{LaneID: "gpu-1", Action: LaneFillActionStartPRD, PRDID: fmt.Sprintf("p%04d", i), Mode: LaneFillModeShadow}
		r.observe(d, base.Add(time.Duration(i)*time.Second))
		r.endPoll()
	}
	list := r.list(0)
	if len(list) != laneFillRecordLimit {
		t.Fatalf("kept %d records, want %d", len(list), laneFillRecordLimit)
	}
	if list[0].PRDID != fmt.Sprintf("p%04d", laneFillRecordLimit+49) || list[len(list)-1].PRDID != "p0050" {
		t.Errorf("order wrong: first %s last %s", list[0].PRDID, list[len(list)-1].PRDID)
	}
	if got := r.list(5); len(got) != 5 || got[0].PRDID != list[0].PRDID {
		t.Errorf("limit 5 returned %d", len(got))
	}
	reloaded := newLaneFillRecords(dir)
	if got := reloaded.list(0); len(got) != laneFillRecordLimit || got[0].PRDID != list[0].PRDID {
		t.Errorf("reloaded %d records", len(got))
	}
}

func TestLaneFillRecordsFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := newLaneFillRecords(dir)
	base := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	e, _ := r.observe(LaneFillDecision{
		LaneID: "gpu-1", LaneType: LaneTypeGPU, Action: LaneFillActionStartPRD, PRDID: "aaaa1111",
		Title: "t", Reason: "why", Mode: LaneFillModeOn,
	}, base)
	r.update(e, func(d *LaneFillDecision, _ *time.Time) { d.Executed, d.Error = true, "none" })
	r.endPoll()

	back := newLaneFillRecords(dir).list(0)
	if len(back) != 1 {
		t.Fatalf("reloaded %d records", len(back))
	}
	want := r.list(0)[0]
	if back[0].PRDID != want.PRDID || !back[0].Executed || back[0].Error != "none" || back[0].RepeatCount != 1 ||
		!back[0].LastSeenAt.Equal(base) || back[0].Reason != "why" || back[0].Mode != LaneFillModeOn {
		t.Errorf("round trip changed the record: %+v vs %+v", back[0], want)
	}
	raw, err := os.ReadFile(filepath.Join(dir, laneFillRecordsFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"at", "mode", "lane_id", "lane_type", "action", "prd_id", "title", "reason", "blocker", "executed", "error", "repeat_count", "last_seen_at"} {
		if !strings.Contains(string(raw), `"`+k+`"`) {
			t.Errorf("records file lacks field %q", k)
		}
	}
}

func TestLaneFillRecordsCorruptFileTolerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, laneFillRecordsFile)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newLaneFillRecords(dir)
	if got := r.list(0); len(got) != 0 {
		t.Fatalf("corrupt file gave %d records", len(got))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back []LaneFillDecision
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("corrupt file was not replaced with valid JSON: %v (%s)", err, raw)
	}
}

func TestLaneFillRecordsCorruptFileDoesNotStopTheCoordinator(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "autonomous"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "autonomous", laneFillRecordsFile)
	if err := os.WriteFile(path, []byte("\x00garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newLaneFillHarnessIn(t, root, laneFillCoordConfig("shadow"))
	h.c.SetReady()
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")
	h.poll()
	if len(h.status().Decisions) == 0 {
		t.Fatal("no decisions after opening a corrupt records file")
	}
}

func TestLaneFillRestartReloadsDecisions(t *testing.T) {
	dir := t.TempDir()
	h := newLaneFillHarnessIn(t, dir, laneFillCoordConfig("shadow"))
	h.c.SetReady()
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")
	h.poll()
	before := h.status().Decisions
	if len(before) == 0 {
		t.Fatal("no decisions recorded")
	}
	h.c.Stop()

	h2 := newLaneFillHarnessIn(t, dir, laneFillCoordConfig("shadow"))
	after := h2.status().Decisions
	if len(after) != len(before) || after[0].PRDID != before[0].PRDID || after[0].RepeatCount != before[0].RepeatCount {
		t.Fatalf("decisions after restart: %+v, before %+v", after, before)
	}
}

func TestLaneFillRestartDecidesNothingBeforeReady(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.addPRD("claude work", PRDApproved, "claude", "claude-code")

	h.poll() // not Ready: boot recovery has not finished

	if st := h.status(); len(st.Decisions) != 0 || !st.LastPollAt.IsZero() {
		t.Fatalf("decisions before Ready: %+v", st)
	}
	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("calls before Ready: %v", calls)
	}

	h.c.SetReady()
	h.poll()
	if got := h.fake.all(); len(got) != 1 || !strings.HasPrefix(got[0], "run:") {
		t.Fatalf("calls after Ready: %v", got)
	}
}

func TestLaneFillRestartDoesNotStartARunningAutomatonAgain(t *testing.T) {
	dir := t.TempDir()
	h := newLaneFillHarnessIn(t, dir, laneFillCoordConfig("on"))
	h.c.SetReady()
	p := h.addPRD("claude work", PRDApproved, "claude", "claude-code")
	h.poll()
	if got := h.fake.all(); len(got) != 1 || got[0] != "run:"+p.ID {
		t.Fatalf("calls = %v", got)
	}
	h.c.Stop()

	// A new daemon over the same stores: the Automaton is running.
	h2 := newLaneFillHarnessIn(t, dir, laneFillCoordConfig("on"))
	h2.c.SetReady()
	h2.poll()
	if calls := h2.fake.all(); len(calls) != 0 {
		t.Fatalf("restart started a running Automaton again: %v", calls)
	}
	st := h2.status()
	if st.Lanes[1].State != "occupied" || st.Lanes[1].Occupant == nil || st.Lanes[1].Occupant.PRDID != p.ID {
		t.Errorf("claude lane after restart: %+v", st.Lanes)
	}
}

func TestLaneFillRestartStartedAutomatonNotStartedTwiceAcrossPolls(t *testing.T) {
	h := newLaneFillHarness(t, "on")
	h.c.SetReady()
	h.fake.noFlip = true // the status does not flip, as when a start is still settling
	p := h.addPRD("claude work", PRDApproved, "claude", "claude-code")
	other := h.addPRD("other claude work", PRDApproved, "claude", "claude-code")

	h.poll()
	h.now = h.now.Add(time.Minute)
	h.poll()

	got := h.fake.all()
	if len(got) != 1 || got[0] != "run:"+p.ID {
		t.Fatalf("calls = %v: the started Automaton must hold its lane and not be started twice", got)
	}
	_ = other
}

func TestLaneFillRestartModeChangeAppliesLive(t *testing.T) {
	h := newLaneFillHarness(t, "off")
	h.c.SetReady()
	p := h.addPRD("claude work", PRDApproved, "claude", "claude-code")

	h.poll()
	if st := h.status(); st.Mode != "off" || len(st.Decisions) != 0 {
		t.Fatalf("off: %+v", st)
	}

	h.setMode("shadow")
	h.poll()
	if st := h.status(); st.Mode != "shadow" || len(st.Decisions) == 0 {
		t.Fatalf("shadow: %+v", st)
	}
	if calls := h.fake.all(); len(calls) != 0 {
		t.Fatalf("shadow made calls: %v", calls)
	}

	h.setMode("on")
	h.poll()
	if got := h.fake.all(); len(got) != 1 || got[0] != "run:"+p.ID {
		t.Fatalf("calls after switching on: %v", got)
	}
	if st := h.status(); st.Mode != "on" {
		t.Errorf("mode = %q", st.Mode)
	}
}
