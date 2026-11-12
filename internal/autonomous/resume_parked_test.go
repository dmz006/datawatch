package autonomous

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// parkForTest runs the boot gate on a running PRD with limit 0 so it parks.
func parkForTest(t *testing.T, m *Manager, api *API, prd *PRD) BootResumeEntry {
	t.Helper()
	m.mu.Lock()
	m.cfg.ResumeMaxAgeSeconds = 0
	m.mu.Unlock()
	if _, _, parked := api.gateBootResume(prd, PRDRunning); !parked {
		t.Fatal("expected the gate to park the PRD")
	}
	rep := m.BootResumeReport()
	return rep[len(rep)-1]
}

func TestResumeParked_LiveSessionAdoptedNotDuplicated(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	fs := newFakeSessions("sess-live")
	fs.wire(m)
	cur := setupBootedAdoptState(t, m, "sess-live")

	entry := parkForTest(t, m, api, cur)
	if entry.Outcome != "parked" || len(entry.LiveSessions) != 1 || entry.LiveSessions[0] != "sess-live" {
		t.Fatalf("parked entry must carry the live session, got %+v", entry)
	}
	got, _ := m.Store().GetPRD(cur.ID)
	if got.Status != PRDBlocked {
		t.Fatalf("want blocked, got %s", got.Status)
	}

	var spawnN, verifyN int32
	spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		atomic.AddInt32(&spawnN, 1)
		return SpawnResult{}, fmt.Errorf("must not spawn a second worker")
	}
	countingVerify := func(ctx context.Context, p *PRD, k *Task) (VerificationResult, error) {
		atomic.AddInt32(&verifyN, 1)
		return verify(ctx, p, k)
	}
	api.SetExecutors(spawn, countingVerify)
	if err := api.WaitIdle(10 * time.Second); err != nil { // boot scan: PRD is blocked, no-op
		t.Fatal(err)
	}
	// WaitIdle cancelled nothing live; resume now.
	if _, err := api.ResumeParked(cur.ID, "operator", "go"); err != nil {
		t.Fatalf("ResumeParked: %v", err)
	}
	pollPRDTerminal(t, m, cur.ID, 5*time.Second)
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Store().SnapshotPRD(cur.ID)
	if out.Status != PRDCompleted {
		t.Fatalf("want completed, got %s (%q)", out.Status, out.Story[0].Tasks[0].Error)
	}
	if out.BlockedReason != "" {
		t.Fatalf("BlockedReason not cleared: %q", out.BlockedReason)
	}
	if n := atomic.LoadInt32(&spawnN); n != 0 {
		t.Fatalf("spawned %d workers; want 0", n)
	}
	if n := fs.killCount(); n != 0 {
		t.Fatalf("live session killed: %v", fs.killed())
	}
	if n := fs.waitCount(); n != 1 {
		t.Fatalf("executor started %d times (waits); want exactly once", n)
	}
	if n := atomic.LoadInt32(&verifyN); n != 1 {
		t.Fatalf("verify %d times; want 1", n)
	}
	if d := lastDecision(out, "resume"); d == nil || d.Actor != "operator" || d.Note != "go" {
		t.Fatalf("resume decision missing/wrong: %+v", d)
	}
	rep := m.BootResumeReport()
	if last := rep[len(rep)-1]; last.Outcome != "resumed_by_operator" || len(last.LiveSessions) != 1 {
		t.Fatalf("want resumed_by_operator entry with live session, got %+v", last)
	}
	if st := m.Status(); len(st.BootResume) != len(rep) {
		t.Fatalf("LoopStatus.BootResume has %d entries, report %d", len(st.BootResume), len(rep))
	}
}

func TestResumeParked_RejectsNonParkedStatus(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	for _, st := range []PRDStatus{PRDDraft, PRDRunning, PRDApproved} {
		p, _ := m.Store().GetPRD(prd.ID)
		p.Status = st
		_ = m.Store().SavePRD(p)
		_, err := api.ResumeParked(prd.ID, "operator", "")
		if err == nil || !strings.Contains(err.Error(), string(PRDBlocked)) {
			t.Fatalf("status %s: want error naming blocked, got %v", st, err)
		}
	}
	// Blocked for another reason (no park marker) is also refused.
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDBlocked
	_ = m.Store().SavePRD(p)
	if _, err := api.ResumeParked(prd.ID, "operator", ""); err == nil {
		t.Fatal("blocked PRD without a boot-park decision must be refused")
	}
	if _, err := api.ResumeParked("nope", "operator", ""); err == nil {
		t.Fatal("unknown PRD must error")
	}
}

func TestResumeParked_ReasonsNameResumeActionNotApprove(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	var alertBody string
	m.SetAlertFn(func(title, body string) { alertBody = body })
	fs := newFakeSessions()
	fs.wire(m)
	prd := setupBootedAdoptState(t, m, "")

	cases := map[string]string{}
	// disabled
	cases["disabled"] = decideBootResume(prd, time.Now(), 0, m.bootResumeHooks()).reason
	// age: stale record
	old := *prd
	old.UpdatedAt = time.Now().Add(-48 * time.Hour)
	old.CreatedAt = old.UpdatedAt
	old.Story = nil
	cases["age"] = decideBootResume(&old, time.Now(), 60, m.bootResumeHooks()).reason
	// missing directory
	gone := *prd
	gone.ProjectDir = "/nonexistent/resume-parked-test-dir"
	cases["dir"] = decideBootResume(&gone, time.Now(), -1, m.bootResumeHooks()).reason

	for name, r := range cases {
		if strings.Contains(strings.ToLower(r), "approve") {
			t.Errorf("%s reason mentions approve: %s", name, r)
		}
		if !strings.Contains(r, "autonomous_prd_resume") || !strings.Contains(r, "resume action") {
			t.Errorf("%s reason does not name the resume action: %s", name, r)
		}
	}
	parkForTest(t, m, api, prd)
	if strings.Contains(strings.ToLower(alertBody), "approve") || !strings.Contains(alertBody, "resume action") {
		t.Errorf("alert text wrong: %s", alertBody)
	}
}

func TestResumeParked_ParkedEntryCarriesLiveSessionForMissingDir(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	fs := newFakeSessions("sess-live")
	fs.wire(m)
	cur := setupBootedAdoptState(t, m, "sess-live")
	cur.ProjectDir = "/nonexistent/resume-parked-test-dir"
	_ = m.Store().SavePRD(cur)
	m.mu.Lock()
	m.cfg.ResumeMaxAgeSeconds = -1
	m.mu.Unlock()
	if _, _, parked := api.gateBootResume(cur, PRDRunning); !parked {
		t.Fatal("missing dir must park")
	}
	rep := m.BootResumeReport()
	e := rep[len(rep)-1]
	if e.Outcome != "parked" || len(e.LiveSessions) != 1 || e.LiveSessions[0] != "sess-live" {
		t.Fatalf("want live session on parked entry, got %+v", e)
	}
}
