package autonomous

// Triage smoke test. Two parts, like the lane fill restart smoke.
//
// Part 1 (TestTriageSmoke) always runs. It is in process, with no network
// and no daemon: a real Manager over a real file-backed store built the
// way the daemon builds it at boot, a real Coordinator driven by ticks the
// test fires, and no fakes anywhere except the ones a smoke allows — the
// automation starter (no session is ever started) and the alert sink
// (the operator's pager, here a capture list). One Automation is seeded
// with one failed task and one blocked by the retry budget. One
// coordinator tick must record both waiting Automata and raise nothing —
// the alert waits the configured 30 minutes. Past that window one pass
// must raise exactly one alert per stuck Automaton, each carrying the
// failed task, its error, the session end reason and the retry budget
// standing, and then never again across further passes and across a
// daemon-style reload over the same store. Nothing is auto-retried or
// restarted, and a record goes away the moment its Automaton recovers.
//
// Part 2 (TestTriageSmokeLive) is an opt-in live check against a SANDBOX
// daemon. It is skipped unless the environment names the daemon:
//
//	DW_LIVE_TRIAGE_SMOKE=https://host:port  base URL of the sandbox daemon
//	                                         (required; no default, so the
//	                                         test can never reach a
//	                                         production daemon by accident)
//	DW_BASE_TOKEN=...                          bearer token, if the daemon has one
//	DW_BASE_INSECURE=0                         verify TLS (default: skip
//	                                           verify, as the other live
//	                                           scripts do)
//
//	Run it against a sandbox daemon the operator started:
//
//		DW_LIVE_TRIAGE_SMOKE=https://127.0.0.1:18443 \
//		  env -u TMUX -u TMUX_TMPDIR go test -count=1 -run 'TestTriageSmoke(Live)?$' -v ./internal/autonomous/
//
// The live part only calls GET /api/autonomous/triage. It never writes,
// never raises an alert and never touches a process on the host.

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const triageSmokeLiveEnv = "DW_LIVE_TRIAGE_SMOKE"

// triageSmokeThreshold is the alert window in whole minutes — the
// configured value that has to elapse before a stuck Automaton alerts.
const triageSmokeThreshold = 30

// triageSmokeStart is the fake clock's first reading.
var triageSmokeStart = time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)

// triageSmokeClock is the fake clock the coordinator reads through
// c.now, the same seam the daemon runs on real time.
type triageSmokeClock struct{ t time.Time }

func (c *triageSmokeClock) Now() time.Time { return c.t }

func (c *triageSmokeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// triageSmokeRun is one daemon lifetime under test: a real Manager over a
// file-backed store and a real Coordinator on top of it, wired exactly
// as the daemon wires them at boot. The only stand-ins are the
// AutomationStarter (recorded, never launched) and the alert sink
// (captured, never sent), plus the injected clock.
type triageSmokeRun struct {
	t      *testing.T
	dir    string
	m      *Manager
	c      *Coordinator
	fake   *fakeStarter
	clock  *triageSmokeClock
	alerts []string // the fake alert sink: one "LEVEL | TITLE — BODY" entry per alert raised
	audits []string // captured audit actions
}

// startTriageSmoke builds the Manager and Coordinator the way the daemon
// does at boot and returns them ready to run, with the coordinator's own
// timer pushed out of reach so only the passes the test fires run.
func startTriageSmoke(t *testing.T, dir string) *triageSmokeRun {
	t.Helper()
	clock := &triageSmokeClock{t: triageSmokeStart}

	cfg := DefaultConfig()
	th := triageSmokeThreshold
	cfg.Automation.TriageAlertMinutes = &th
	// A concrete, small retry budget so the alert's budget line is
	// stable and readable: the whole point is that the operator sees
	// exactly what is left before the ceiling.
	cfg.RetryPolicy = map[string]int{"infra": 2, "content": 1, "scope": 1, "policy": 1}
	cfg.RetryCeilingTotal = 7

	m, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	fake := &fakeStarter{m: m}
	c := NewCoordinator(m, fake)
	if c == nil {
		t.Fatal("NewCoordinator returned nil")
	}
	r := &triageSmokeRun{t: t, dir: dir, m: m, c: c, fake: fake, clock: clock}

	m.SetAlertSink(func(level, title, body string) string {
		r.alerts = append(r.alerts, level+" | "+title+" — "+body)
		return ""
	})
	m.SetAuditFn(func(action string, _ map[string]any) {
		r.audits = append(r.audits, action)
	})

	c.now = clock.Now
	c.idleTick = time.Hour
	c.laneFillTick = time.Hour
	c.Start()
	t.Cleanup(func() {
		c.Stop()
		m.Stop()
	})
	return r
}

// seedTriageSmoke puts the Automation in the state the smoke is about:
// one member that failed a task (task error plus the reason the session
// ended) and one blocked because the retry budget is spent.
func seedTriageSmoke(t *testing.T, r *triageSmokeRun) (failedID, blockedID string) {
	t.Helper()
	add := func(title string) string {
		t.Helper()
		p, err := r.m.store.CreatePRD("spec "+title, "/smoke", "pi-prompt", "", "")
		if err != nil {
			t.Fatalf("CreatePRD %q: %v", title, err)
		}
		p.Title = title
		if err := r.m.store.SavePRD(p); err != nil {
			t.Fatalf("save %q: %v", title, err)
		}
		return p.ID
	}
	failedID = add("build the queue")
	blockedID = add("ship the queue")

	p, ok := r.m.store.GetPRD(failedID)
	if !ok {
		t.Fatal("failed PRD gone")
	}
	p.Status = PRDFailed
	p.Story = []Story{{Tasks: []Task{{
		ID: "t-fail", Title: "run the build", Status: TaskFailed,
		Error:            "the build session died before writing the plan",
		SessionEndReason: "operator ended it",
		UpdatedAt:        r.clock.Now(),
	}}}}
	if err := r.m.store.SavePRD(p); err != nil {
		t.Fatalf("save failed PRD: %v", err)
	}

	q, ok := r.m.store.GetPRD(blockedID)
	if !ok {
		t.Fatal("blocked PRD gone")
	}
	q.Status = PRDBlocked
	q.BlockedReason = "retry budget exhausted (infra 2/2)"
	if err := r.m.store.SavePRD(q); err != nil {
		t.Fatalf("save blocked PRD: %v", err)
	}

	a, err := r.m.CreateAutomation(AutomationInput{
		Name: "ship the queue line",
		Members: []AutomationMember{
			{ID: "m0", PRDID: failedID, OnComplete: "m1"},
			{ID: "m1", PRDID: blockedID},
		},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if _, err := r.m.automations.Update(a.ID, 0, func(a *Automation) error {
		now := r.clock.Now()
		a.Status = AutomationActive
		a.StartedAt = &now
		a.CurrentMemberID = "m0"
		a.Members[0].Status = MemberRunning
		a.Members[0].StartedAt = &now
		return nil
	}); err != nil {
		t.Fatalf("activate automation: %v", err)
	}
	// The coordinator is Ready: a stuck member of an active Automation
	// must be triaged, not hidden while waiting for boot recovery.
	r.c.SetReady()
	return failedID, blockedID
}

func (r *triageSmokeRun) record(t *testing.T, prdID string) *TriageRecord {
	t.Helper()
	p, ok := r.m.store.GetPRD(prdID)
	if !ok {
		t.Fatalf("prd %s not found", prdID)
	}
	return p.Triage
}

// pass runs exactly one triage pass — the same thing a coordinator idle
// tick does for triage.
func (r *triageSmokeRun) pass() { r.c.triagePass() }

func TestTriageSmoke(t *testing.T) {
	dir := t.TempDir()
	r := startTriageSmoke(t, dir)
	failedID, blockedID := seedTriageSmoke(t, r)

	// One tick: both waiting Automata are recorded and visible to the
	// triage surfaces, but nothing alerts inside the window.
	r.pass()
	recF := r.record(t, failedID)
	recB := r.record(t, blockedID)
	if recF == nil || recB == nil {
		t.Fatalf("after one pass the triage records are %+v / %+v, want one per stuck Automaton", recF, recB)
	}
	if recF.Status != "failed" || recF.TaskTitle != "run the build" || recF.TaskID != "t-fail" {
		t.Errorf("failed record fields = %+v", recF)
	}
	if recF.Error != "the build session died before writing the plan" || recF.SessionEndReason != "operator ended it" {
		t.Errorf("failed record error = %q, end reason = %q", recF.Error, recF.SessionEndReason)
	}
	if recF.AutomationName != "ship the queue line" || recF.MemberID != "m0" {
		t.Errorf("failed record automation context = %q / %q", recF.AutomationName, recF.MemberID)
	}
	if recF.RetryBudget.Infra.Limit != 2 || recF.RetryBudget.Infra.Left != 2 {
		t.Errorf("failed record infra budget = %+v, want 2 limit 2 left", recF.RetryBudget.Infra)
	}
	if recF.RetryBudget.CeilingTotal != 7 || recF.RetryBudget.CeilingLeft != 7 {
		t.Errorf("failed record ceiling = %d of %d, want 7 of 7", recF.RetryBudget.CeilingLeft, recF.RetryBudget.CeilingTotal)
	}
	if recB.Status != "blocked" || recB.BlockedReason != "retry budget exhausted (infra 2/2)" {
		t.Errorf("blocked record = %+v", recB)
	}
	if recF.AlertedAt != nil {
		t.Errorf("record raised an alert inside its window: AlertedAt %v", recF.AlertedAt)
	}
	if got := len(r.m.ListTriageRecords()); got != 2 {
		t.Errorf("manager triage list has %d records, want the two waiting Automata", got)
	}
	if got := r.m.TriageAlertMinutes(); got != triageSmokeThreshold {
		t.Errorf("alert threshold = %d min, want %d", got, triageSmokeThreshold)
	}
	if len(r.alerts) != 0 {
		t.Fatalf("alerts inside the window: %v", r.alerts)
	}
	runs, resumes := r.fake.counts()
	if len(runs) != 0 || len(resumes) != 0 {
		t.Fatalf("a triage pass started something: runs %v resumes %v", runs, resumes)
	}

	// Inside the window, while the clock walks across it: still no alert
	// and still exactly the two records — the boundary only closes one
	// minute later.
	for step := 0; step < triageSmokeThreshold-1; step++ {
		r.clock.Advance(time.Minute)
		r.pass()
	}
	if len(r.alerts) != 0 {
		t.Fatalf("alerts inside the %d minute window: %v", triageSmokeThreshold, r.alerts)
	}
	if got := len(r.m.ListTriageRecords()); got != 2 {
		t.Fatalf("records inside the window = %d, want 2", got)
	}
	runs, resumes = r.fake.counts()
	if len(runs) != 0 || len(resumes) != 0 {
		t.Fatalf("the window walk started something: runs %v resumes %v", runs, resumes)
	}

	// Past the window: one pass raises exactly one alert per stuck
	// Automaton, and the alert carries the failed task, its error, the
	// session end reason and the retry budget standing.
	r.clock.Advance(time.Minute)
	before := len(r.alerts)
	r.pass()
	if got := len(r.alerts) - before; got != 2 {
		t.Fatalf("after the window the pass raised %d alerts, want 2; all: %v", got, r.alerts)
	}
	body := func(substr string) string {
		for _, a := range r.alerts {
			if strings.Contains(a, substr) {
				return a
			}
		}
		t.Fatalf("no alert mentions %q; alerts: %v", substr, r.alerts)
		return ""
	}
	failedAlert := body("build the queue")
	for _, want := range []string{
		"Failed task: run the build (id t-fail)",
		"Task error: the build session died before writing the plan",
		"Session end reason: operator ended it",
		"Retry budget:",
		"infra 0/2 (2 left)",
		"ceiling 7 left of 7 total",
		"Stuck since",
		"Member of Automation \"ship the queue line\"",
	} {
		if !strings.Contains(failedAlert, want) {
			t.Errorf("failed alert lacks %q\ngot:\n%s", want, failedAlert)
		}
	}
	blockedAlert := body("\"ship the queue\" (id")
	if !strings.Contains(blockedAlert, "Blocked reason: retry budget exhausted (infra 2/2)") {
		t.Errorf("blocked alert lacks its blocked reason\ngot:\n%s", blockedAlert)
	}
	if rec := r.record(t, failedID); rec.AlertedAt == nil || !rec.AlertedAt.Equal(r.clock.t) {
		rec := r.record(t, failedID)
		t.Errorf("AlertedAt stamp = %v, want the moment the window closed (%v)", rec.AlertedAt, r.clock.t)
	}

	// Later passes: the same stuck Automata re-raise nothing.
	// Later passes: the same stuck Automata re-raise nothing.
	r.clock.Advance(30 * time.Minute)
	r.pass()
	r.pass()
	if got := len(r.alerts); got != 2 {
		t.Fatalf("later passes re-raised alerts: %d alerts: %v", got, r.alerts)
	}
	runs, resumes = r.fake.counts()
	if len(runs) != 0 || len(resumes) != 0 {
		t.Fatalf("waiting for operator triage re-tried something: runs %v resumes %v", runs, resumes)
	}
	for _, a := range r.audits {
		low := strings.ToLower(a)
		if strings.Contains(low, "retry") || strings.Contains(low, "resume") || strings.Contains(low, "start") {
			t.Errorf("audit log shows an auto action: %q", a)
		}
	}

	// A daemon-style reload: a fresh Manager over the very same
	// directory, a fresh Coordinator. The waiting Automata are still
	// there, still visible, still not re-alerted — the decision to alert
	// was made once, on this store.
	r.c.Stop()
	r.m.Stop()
	r2 := startTriageSmoke(t, dir)
	r2.c.SetReady()
	if got := len(r2.m.ListTriageRecords()); got != 2 {
		t.Fatalf("after reload the triage list has %d records, want 2", got)
	}
	r2.pass()
	if len(r2.alerts) != 0 {
		t.Fatalf("the reloaded daemon re-raised stale alerts: %v", r2.alerts)
	}
	runs, resumes = r2.fake.counts()
	if len(runs) != 0 || len(resumes) != 0 {
		t.Fatalf("the reloaded daemon started something: runs %v resumes %v", runs, resumes)
	}

	// Recovery: the Automata leave their stuck state and the records go
	// with them — the triage surface is empty again.
	for _, id := range []string{failedID, blockedID} {
		p, ok := r2.m.store.GetPRD(id)
		if !ok {
			t.Fatalf("prd %s gone before recovery", id)
		}
		p.Status = PRDApproved
		if err := r2.m.store.SavePRD(p); err != nil {
			t.Fatalf("recover %s: %v", id, err)
		}
	}
	r2.pass()
	if got := len(r2.m.ListTriageRecords()); got != 0 {
		t.Fatalf("recovered Automata still listed: %d records", got)
	}
	if len(r2.alerts) != 0 {
		t.Fatalf("recovery raised alerts: %v", r2.alerts)
	}
	t.Logf("triage smoke: 2 stuck Automata recorded in one pass, 2 alerts exactly once past the %dm window, "+
		"no re-tries, no re-raise across a reload, clean after recovery", triageSmokeThreshold)
}

// TestTriageSmokeLive reads a sandbox daemon's triage read. It is skipped
// unless DW_LIVE_TRIAGE_SMOKE names the daemon.
func TestTriageSmokeProductionWiring(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Stop()

	type readSurface interface {
		ListTriageRecords() []*TriageRecord
		TriageAlertMinutes() int
	}
	var api any = NewAPI(m)
	surf, ok := api.(readSurface)
	if !ok {
		t.Fatal("the autonomous API does not expose the triage read the daemon route resolves; triage would stay 503 in a live daemon")
	}
	if got := surf.TriageAlertMinutes(); got != DefaultAutomationTriageAlertMinutes {
		t.Errorf("adapter threshold = %d, want %d", got, DefaultAutomationTriageAlertMinutes)
	}

	p, err := m.CreatePRD("spec build the queue", "/smoke", "pi-prompt", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	p.Title = "build the queue"
	p.Status = PRDFailed
	p.Story = []Story{{Tasks: []Task{{
		ID:               "t-fail",
		Title:            "run the build",
		Status:           TaskFailed,
		Error:            "the build session died before writing the plan",
		SessionEndReason: "operator ended it",
	}}}}
	if err := m.store.SavePRD(p); err != nil {
		t.Fatalf("save PRD: %v", err)
	}
	rec := m.BuildTriageRecordFor(p)
	if rec == nil {
		t.Fatal("no triage record for the failed Automaton")
	}
	if err := m.SaveTriageRecord(p.ID, rec); err != nil {
		t.Fatalf("SaveTriageRecord: %v", err)
	}
	got := surf.ListTriageRecords()
	if len(got) != 1 || got[0].PRDID != p.ID || got[0].Status != "failed" {
		t.Fatalf("adapter read-back = %+v, want the one saved record", got)
	}
}

func TestTriageSmokeLive(t *testing.T) {
	base := strings.TrimRight(os.Getenv(triageSmokeLiveEnv), "/")
	if base == "" {
		t.Skipf("set %s to the sandbox daemon base URL to run the live triage check", triageSmokeLiveEnv)
	}
	c := &triageSmokeClient{
		base:  base,
		token: os.Getenv("DW_BASE_TOKEN"),
		hc: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: os.Getenv("DW_BASE_INSECURE") != "0", // #nosec G402 -- sandbox daemons use self-signed certs
				},
			},
		},
	}

	body := c.get(t, "/api/autonomous/triage")
	var tl struct {
		Records      json.RawMessage `json:"records"`
		AlertMinutes *int            `json:"alert_minutes"`
	}
	if err := json.Unmarshal(body, &tl); err != nil {
		t.Fatalf("triage read does not parse: %v (raw %q)", err, body)
	}
	if !isJSONList(tl.Records) {
		t.Fatalf("triage read has no records list (raw %q)", string(tl.Records))
	}
	if tl.AlertMinutes == nil {
		t.Fatal("triage read has no alert_minutes in effect")
	}
	if *tl.AlertMinutes < 0 {
		t.Fatalf("alert_minutes = %d, want 0 (off) or positive whole minutes", *tl.AlertMinutes)
	}
	var n int
	if err := json.Unmarshal(tl.Records, &n); err == nil {
		t.Logf("sandbox triage: %d waiting Automata, alert threshold %d min", n, *tl.AlertMinutes)
	}
}

func isJSONList(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	return strings.HasPrefix(s, "[")
}

// triageSmokeClient is a minimal GET client for the sandbox daemon.
type triageSmokeClient struct {
	base  string
	token string
	hc    *http.Client
}

// get fetches one path and returns the body. Any status other than 200 fails.
func (c *triageSmokeClient) get(t *testing.T, path string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", path, err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read GET %s body: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d body=%s", path, resp.StatusCode, out)
	}
	return out
}
