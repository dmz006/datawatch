// B165 — boot-resume gate: at daemon boot a PRD left in PRDRunning state is
// NOT resumed unconditionally. It is parked (PRDBlocked + plain-English
// reason + alert) when its newest recorded activity is stale versus
// resume_max_age_seconds (0 = never resume), or its project directory no
// longer exists (removed temp workspace). The per-PRD decision is recorded
// in the Manager's boot-resume report, exposed via Status().BootResume.

package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func gateFixture(t *testing.T) (*Manager, *API) {
	t.Helper()
	dir := t.TempDir()
	fakeLLM := func(_ DecomposeRequest) (string, error) {
		return `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`, nil
	}
	m, err := NewManager(dir, DefaultConfig(), fakeLLM)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	api := NewAPI(m)
	api.spawnFn = spawn
	api.verify = verify
	drainOnCleanup(t, m, api)
	return m, api
}

// gatePRD creates a PRD with one pending task, sets Status and (when
// age > 0) backdates CreatedAt/UpdatedAt to `age` before now. Uses the
// shared store pointers directly (same package) to avoid SavePRD's
// UpdatedAt stamp.
func gatePRD(t *testing.T, m *Manager, projectDir string, age time.Duration) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("gate spec", projectDir, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDRunning
	if age > 0 {
		older := time.Now().Add(-age)
		p.CreatedAt = older
		p.UpdatedAt = older
		for i := range p.Story {
			p.Story[i].CreatedAt = older
			p.Story[i].UpdatedAt = older
			for j := range p.Story[i].Tasks {
				p.Story[i].Tasks[j].CreatedAt = older
				p.Story[i].Tasks[j].UpdatedAt = older
				p.Story[i].Tasks[j].StartedAt = &older
			}
		}
	}
	return p
}

// waitForStatus polls until the PRD reaches one of the wanted statuses or
// the deadline passes.
func waitForStatus(t *testing.T, m *Manager, prdID string, deadline time.Duration, want ...PRDStatus) *PRD {
	t.Helper()
	timeout := time.NewTimer(deadline)
	defer timeout.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
poll:
	for {
		p, ok := m.Store().SnapshotPRD(prdID)
		if !ok {
			t.Fatalf("PRD %q disappeared", prdID)
		}
		for _, w := range want {
			if p.Status == w {
				return p
			}
		}
		select {
		case <-timeout.C:
			break poll
		case <-tick.C:
		}
	}
	p, _ := m.Store().SnapshotPRD(prdID)
	return p
}

func testProjectDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return d
}

// (a) Fresh PRD (1 min old, dir exists, default gate) → resumed.
func TestBootResumeGateFresheresumes(t *testing.T) {
	m, api := gateFixture(t)
	proj := testProjectDir(t)
	gatePRD(t, m, proj, time.Minute)

	var spawned atomic.Int32
	api.spawnFn = func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	api.resumeRunningPRDs()
	// Look up the actual PRD id (newid random) via ListPRDs.
	ids := idsOf(t, m)
	if len(ids) != 1 {
		t.Fatalf("want 1 PRD, got %d", len(ids))
	}
	p := waitForStatus(t, m, ids[0], 10*time.Second, PRDCompleted, PRDFailed)
	if p.Status != PRDCompleted {
		t.Errorf("fresh PRD want PRDCompleted, got %q", p.Status)
	}
	if spawned.Load() == 0 {
		t.Error("spawn never called — fresh PRD was not resumed")
	}
	rep := m.BootResumeReport()
	if len(rep) != 1 || rep[0].Outcome != "resumed" {
		t.Errorf("report want 1x resumed, got %+v", rep)
	}
}

// (b) 0-limit: even a 1-minute-fresh PRD is parked, never resumed.
func TestBootResumeGateZeroLimitParksFresh(t *testing.T) {
	m, api := gateFixture(t)
	m.mu.Lock()
	m.cfg.ResumeMaxAgeSeconds = 0
	m.mu.Unlock()
	proj := testProjectDir(t)
	prd := gatePRD(t, m, proj, time.Minute)

	var spawned atomic.Int32
	api.spawnFn = func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	api.resumeRunningPRDs()
	p := waitForStatus(t, m, prd.ID, 2*time.Second, PRDBlocked)
	if p.Status != PRDBlocked {
		t.Fatalf("0-limit PRD want PRDBlocked, got %q", p.Status)
	}
	if p.BlockedReason == "" {
		t.Error("parked PRD has empty BlockedReason")
	}
	if !strings.Contains(p.BlockedReason, "resume_max_age_seconds") || !strings.Contains(p.BlockedReason, "0") {
		t.Errorf("BlockedReason %q — should explain 0 = never resumed", p.BlockedReason)
	}
	if spawned.Load() != 0 {
		t.Errorf("parked PRD was spawned %d times", spawned.Load())
	}
	rep := m.BootResumeReport()
	if len(rep) != 1 || rep[0].Outcome != "parked" {
		t.Errorf("report want 1x parked, got %+v", rep)
	}
}

// (c) -1 limit: 30-day-stale PRD resumes (limit skipped), dir exists.
func TestBootResumeGateMinusOneSkipsStale(t *testing.T) {
	m, api := gateFixture(t)
	m.mu.Lock()
	m.cfg.ResumeMaxAgeSeconds = -1
	m.mu.Unlock()
	proj := testProjectDir(t)
	gatePRD(t, m, proj, 30*24*time.Hour)

	var spawned atomic.Int32
	api.spawnFn = func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	api.resumeRunningPRDs()
	ids := idsOf(t, m)
	if len(ids) != 1 {
		t.Fatalf("want 1 PRD, got %d", len(ids))
	}
	p := waitForStatus(t, m, ids[0], 10*time.Second, PRDCompleted, PRDFailed)
	if p.Status != PRDCompleted {
		t.Errorf("-1-limited stale PRD want PRDCompleted, got %q", p.Status)
	}
	if spawned.Load() == 0 {
		t.Error("spawn never called — stale PRD was not resumed under -1 limit")
	}
}

// (d) 1-h limit, 2-h-old PRD → parked with an age-based reason.
func TestBootResumeGateStaleParked(t *testing.T) {
	m, api := gateFixture(t)
	m.mu.Lock()
	m.cfg.ResumeMaxAgeSeconds = 3600
	m.mu.Unlock()
	proj := testProjectDir(t)
	prd := gatePRD(t, m, proj, 2*time.Hour)

	var spawned atomic.Int32
	api.spawnFn = func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	api.resumeRunningPRDs()
	p := waitForStatus(t, m, prd.ID, 2*time.Second, PRDBlocked)
	if p.Status != PRDBlocked {
		t.Fatalf("stale PRD want PRDBlocked, got %q", p.Status)
	}
	if !strings.Contains(p.BlockedReason, "older than") {
		t.Errorf("BlockedReason %q — should mention the age limit", p.BlockedReason)
	}
	if spawned.Load() != 0 {
		t.Errorf("parked PRD was spawned %d times", spawned.Load())
	}
	rep := m.BootResumeReport()
	if len(rep) != 1 || rep[0].Outcome != "parked" || rep[0].AgeSeconds < 3500 {
		t.Errorf("report want 1x parked with age ~7200s, got %+v", rep)
	}
}

// (e) Project dir removed (temp workspace) → parked no matter how fresh.
func TestBootResumeGateMissingDirParks(t *testing.T) {
	m, api := gateFixture(t)
	proj := testProjectDir(t)
	prd := gatePRD(t, m, proj, time.Minute)
	if err := os.RemoveAll(proj); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	var spawned atomic.Int32
	api.spawnFn = func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	api.resumeRunningPRDs()
	p := waitForStatus(t, m, prd.ID, 2*time.Second, PRDBlocked)
	if p.Status != PRDBlocked {
		t.Fatalf("missing-dir PRD want PRDBlocked, got %q", p.Status)
	}
	if !strings.Contains(p.BlockedReason, "project directory") {
		t.Errorf("BlockedReason %q — should name the removed project directory", p.BlockedReason)
	}
	if spawned.Load() != 0 {
		t.Errorf("parked PRD was spawned %d times", spawned.Load())
	}
	rep := m.BootResumeReport()
	if len(rep) != 1 || rep[0].Outcome != "parked" {
		t.Errorf("report want 1x parked, got %+v", rep)
	}
}

// (f) Default gate config is 1-hour (3600 s), and a fresh PRD under default
// config resumes.
func TestBootResumeGateDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ResumeMaxAgeSeconds != 3600 {
		t.Errorf("DefaultConfig ResumeMaxAgeSeconds = %d, want 3600", cfg.ResumeMaxAgeSeconds)
	}
	const bootResumeDefaultMaxAgeSeconds = 3600
	if cfg.ResumeMaxAgeSeconds != bootResumeDefaultMaxAgeSeconds {
		t.Error("default mismatch with named constant")
	}

	m, api := gateFixture(t)
	proj := testProjectDir(t)
	gatePRD(t, m, proj, time.Minute)

	var spawned atomic.Int32
	api.spawnFn = func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	api.resumeRunningPRDs()
	ids := idsOf(t, m)
	if len(ids) != 1 {
		t.Fatalf("want 1 PRD, got %d", len(ids))
	}
	p := waitForStatus(t, m, ids[0], 10*time.Second, PRDCompleted, PRDFailed)
	if p.Status != PRDCompleted {
		t.Errorf("fresh PRD under default config want PRDCompleted, got %q", p.Status)
	}
	if spawned.Load() == 0 {
		t.Error("spawn never called — fresh PRD not resumed under default config")
	}
}

// (g) Mixed batch: one resumes, one parks — both appear in the per-PRD
// report and in Status().BootResume (the LoopStatus surface).
func TestBootResumeGateReport(t *testing.T) {
	m, api := gateFixture(t)
	m.mu.Lock()
	m.cfg.ResumeMaxAgeSeconds = 3600
	m.mu.Unlock()

	freshDir := testProjectDir(t)
	staleDir := testProjectDir(t)

	var spawned atomic.Int32
	api.spawnFn = func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		spawned.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}

	// Park the stale PRD in its own pass before the fresh one exists: a fresh
	// executor persists every PRD, which would otherwise race the park's
	// status write on the stale record.
	stale := gatePRD(t, m, staleDir, 3*time.Hour)
	api.resumeRunningPRDs()
	sp := waitForStatus(t, m, stale.ID, 2*time.Second, PRDBlocked)
	fresh := gatePRD(t, m, freshDir, time.Minute)
	api.resumeRunningPRDs()

	fp := waitForStatus(t, m, fresh.ID, 10*time.Second, PRDCompleted, PRDFailed, PRDBlocked)
	if fp.Status != PRDCompleted {
		t.Errorf("fresh PRD want PRDCompleted, got %q", fp.Status)
	}
	if sp.Status != PRDBlocked {
		t.Errorf("stale PRD want PRDBlocked, got %q", sp.Status)
	}
	if spawned.Load() == 0 {
		t.Error("spawn never called — fresh PRD not resumed")
	}

	// Per-PRD report: exactly one entry per PRD, outcomes correct.
	rep := m.BootResumeReport()
	if len(rep) != 2 {
		t.Fatalf("report want 2 entries, got %d: %+v", len(rep), rep)
	}
	byID := map[string]BootResumeEntry{}
	for _, e := range rep {
		byID[e.PRDID] = e
	}
	if e := byID[fresh.ID]; e.Outcome != "resumed" {
		t.Errorf("fresh entry want resumed, got %+v", e)
	}
	if e := byID[stale.ID]; e.Outcome != "parked" || e.Reason == "" {
		t.Errorf("stale entry want parked with reason, got %+v", e)
	}

	// LoopStatus surface.
	st := m.Status()
	if len(st.BootResume) != 2 {
		t.Errorf("Status().BootResume want 2 entries, got %d", len(st.BootResume))
	}
}

// Alert wiring: parking a PRD raises exactly one alert with the reason.
func TestBootResumeGateAlertOnPark(t *testing.T) {
	m, api := gateFixture(t)
	m.mu.Lock()
	m.cfg.ResumeMaxAgeSeconds = 0
	m.mu.Unlock()
	proj := testProjectDir(t)
	prd := gatePRD(t, m, proj, time.Minute)

	var (
		mu     sync.Mutex
		alerts []string
	)
	m.SetAlertFn(func(title, body string) {
		mu.Lock()
		alerts = append(alerts, title+" | "+body)
		mu.Unlock()
	})

	api.resumeRunningPRDs()
	p := waitForStatus(t, m, prd.ID, 2*time.Second, PRDBlocked)
	if p.Status != PRDBlocked {
		t.Fatalf("want PRDBlocked, got %q", p.Status)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(alerts) != 1 {
		t.Fatalf("want exactly 1 alert, got %d: %v", len(alerts), alerts)
	}
	if !strings.Contains(alerts[0], "parked") {
		t.Errorf("alert %q — should say the PRD was parked", alerts[0])
	}
}

// decideBootResume is deterministic — unit-check all branches directly.
func TestDecideBootResumeUnit(t *testing.T) {
	now := time.Now()
	exists := testProjectDir(t)
	missing := filepath.Join(t.TempDir(), "gone")

	freshPRD := func(dir string) *PRD {
		return &PRD{ProjectDir: dir, CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}
	}
	stalePRD := func(dir string) *PRD {
		p := &PRD{ProjectDir: dir, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour)}
		ts := now.Add(-48 * time.Hour)
		p.Story = []Story{{Tasks: []Task{{ID: "t1", UpdatedAt: ts, StartedAt: &ts}}}}
		return p
	}
	// older PRD with a recently-active task → newest activity is the task,
	// so the PRD is fresh again.
	newerTask := func(dir string) *PRD {
		old := now.Add(-48 * time.Hour)
		p := &PRD{ProjectDir: dir, CreatedAt: old, UpdatedAt: old}
		ts := now.Add(-30 * time.Second)
		p.Story = []Story{{Tasks: []Task{{ID: "t1", UpdatedAt: ts, StartedAt: &ts}}}}
		return p
	}

	cases := []struct {
		name       string
		prd        *PRD
		maxAge     int
		wantResume bool
		reasonSnip string
	}{
		{"fresh-resumes-at-default", freshPRD(exists), 3600, true, ""},
		{"zero-never-resumes", freshPRD(exists), 0, false, "resume_max_age_seconds"},
		{"stale-48h-parks-at-1h", stalePRD(exists), 3600, false, "older than"},
		{"stale-resumes-at-minus-one", stalePRD(exists), -1, true, ""},
		{"missing-dir-parks-fresh", freshPRD(missing), 3600, false, "project directory"},
		{"newer-task-counts", newerTask(exists), 3600, true, ""},
		{"missing-story-dir-parks", stalePRD(exists), -1, false, "project directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// missing-story-dir-parks: a task's dir is also checked.
			if tc.name == "missing-story-dir-parks" {
				tc.prd.Story[0].Tasks[0].ProjectDir = missing
			}
			dec := decideBootResume(tc.prd, now, tc.maxAge)
			if dec.resume != tc.wantResume {
				t.Fatalf("resume = %v (%q), want %v", dec.resume, dec.reason, tc.wantResume)
			}
			if tc.reasonSnip != "" && !strings.Contains(dec.reason, tc.reasonSnip) {
				t.Errorf("reason %q — want it to mention %q", dec.reason, tc.reasonSnip)
			}
			if !tc.wantResume && dec.reason == "" {
				t.Error("park decision must carry a plain-English reason")
			}
		})
	}
}

func idsOf(t *testing.T, m *Manager) []string {
	t.Helper()
	var ids []string
	for _, p := range m.Store().ListPRDs() {
		ids = append(ids, p.ID)
	}
	return ids
}
