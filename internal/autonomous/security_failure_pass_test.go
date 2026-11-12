// Tests for the security failure pass (security_failure_pass.go), run
// through c.securityFailurePass() with a fake for every seam: the failed
// log (Manager.securityFailureLogFn), the advisory lookup
// (setSecurityFailureAdvisoryFn), the registry (a temp repository), the
// gh write seams (cancel, rerun, delete, run list — each with a call
// counter) and the coordinator alert sink. Run with:
//
//	env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run TestSecurityFailurePass
package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	secPassSHA      = "cccc18bc26b80b0be05f9c7d2e4a3b2c1d0e9f8a7b6c"
	secPassRepo     = "dmz006/datawatch"
	secPassRunID    = "4242"
	secPassAdvisory = "CVE-2031-0042"
)

// secPassDepLog is a failed log that names a dependency and an advisory
// but no fixed version.
const secPassDepLog = "scanning...\npackage: example.com/example-mod\nFound " + secPassAdvisory + " in example.com/example-mod\n"

type secPassHarness struct {
	t       *testing.T
	m       *Manager
	c       *Coordinator
	actions *actionCounters

	mu        sync.Mutex
	logCalls  int
	runLists  int
	advCalls  int
	alerts    []string // level|title
	lastBody  string
	logText   string
	logErr    error
	advResult SecurityFailureAdvisoryResult
}

// newSecPassHarness builds the harness in the given mode. registry is
// the accepted-risks file body ("" plants no registry at all, so the
// read fails).
func newSecPassHarness(t *testing.T, mode, registry string) *secPassHarness {
	t.Helper()
	repoDir := t.TempDir()
	if registry != "" {
		repoDir = writeRiskRepo(t, registry)
	}
	m, c, _ := newPollHarness(t, repoDir, RepoCIWatchConfig{Mode: "shadow", Repos: []string{repoDir}, PollSeconds: 60})
	h := &secPassHarness{t: t, m: m, c: c, actions: &actionCounters{}, logText: secPassDepLog}
	h.actions.wire(m)
	cfg := m.Config()
	cfg.Automation.SecurityFailure = SecurityFailureConfig{Mode: mode}
	m.SetConfig(cfg)

	m.securityFailureLogFn = func(ctx context.Context, ownerRepo string, runID int64) (string, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.logCalls++
		return h.logText, h.logErr
	}
	m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
		h.mu.Lock()
		h.runLists++
		h.mu.Unlock()
		return nil, nil
	}
	restore := setSecurityFailureAdvisoryFn(func(ctx context.Context, advisoryID string) SecurityFailureAdvisoryResult {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.advCalls++
		return h.advResult
	})
	t.Cleanup(restore)
	c.alertFn = func(level, title, body string) string {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.alerts = append(h.alerts, level+"|"+title)
		h.lastBody = body
		return "al-1"
	}
	return h
}

// addRecord stores a failing security record for the given workflow.
func (h *secPassHarness) addRecord(workflow string) *RepoCIWatchRecord {
	h.t.Helper()
	rec, err := BuildRepoCIWatchRecord(secPassRepo, "main", "main", secPassSHA, workflow, secPassRunID,
		"https://github.com/dmz006/datawatch/actions/runs/"+secPassRunID, "failing", "security", time.Now())
	if err != nil {
		h.t.Fatalf("BuildRepoCIWatchRecord: %v", err)
	}
	saved, err := h.m.SaveRepoCIWatchRecord(rec)
	if err != nil {
		h.t.Fatalf("SaveRepoCIWatchRecord: %v", err)
	}
	return saved
}

// addLinkedPRD stores a completed Automaton whose last completed task
// carries secPassSHA as its post-verify SHA.
func (h *secPassHarness) addLinkedPRD() *PRD {
	h.t.Helper()
	prd, err := h.m.CreatePRD("spec", "/tmp", "", "", "")
	if err != nil {
		h.t.Fatalf("CreatePRD: %v", err)
	}
	if err := h.m.Store().SetStories(prd.ID, []Story{{
		ID: "s1", Title: "story",
		Tasks: []Task{{ID: "t1", Title: "release", Status: TaskCompleted, PostVerifySHA: secPassSHA}},
	}}); err != nil {
		h.t.Fatalf("SetStories: %v", err)
	}
	p, _ := h.m.Store().GetPRD(prd.ID)
	p.Status = PRDCompleted
	if err := h.m.Store().SavePRD(p); err != nil {
		h.t.Fatalf("SavePRD: %v", err)
	}
	return p
}

func (h *secPassHarness) record() *RepoCIWatchRecord {
	h.t.Helper()
	recs := h.m.ListRepoCIWatchRecords()
	if len(recs) != 1 {
		h.t.Fatalf("want exactly 1 repo CI watch record, got %d", len(recs))
	}
	return recs[0]
}

func (h *secPassHarness) counts() (logs, adv, alerts int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.logCalls, h.advCalls, len(h.alerts)
}

// assertNoGHWrites checks every gh seam: no cancel, rerun, delete and no
// run list.
func (h *secPassHarness) assertNoGHWrites() {
	h.t.Helper()
	cancels, reruns, deletes := h.actions.snapshot()
	h.mu.Lock()
	lists := h.runLists
	h.mu.Unlock()
	if cancels != 0 || reruns != 0 || deletes != 0 || lists != 0 {
		h.t.Errorf("gh seams called: cancel=%d rerun=%d delete=%d run list=%d, want all 0", cancels, reruns, deletes, lists)
	}
}

func prdStatus(t *testing.T, m *Manager, id string) PRDStatus {
	t.Helper()
	p, ok := m.Store().GetPRD(id)
	if !ok {
		t.Fatalf("PRD %s missing", id)
	}
	return p.Status
}

func TestSecurityFailurePass(t *testing.T) {
	t.Run("F1 a security run is never rerun, cancelled or deleted", func(t *testing.T) {
		for _, mode := range []string{SecurityFailureModeShadow, SecurityFailureModeOn} {
			h := newSecPassHarness(t, mode, riskRegistryFixture)
			h.addRecord("security-scan")
			h.addLinkedPRD()
			h.c.securityFailurePass()
			h.c.securityFailurePass()
			if rec := h.record(); rec.Security == nil {
				t.Fatalf("mode %s: the run was not assessed", mode)
			}
			h.assertNoGHWrites()
		}
	})

	t.Run("F2 an unreadable registry is registry_error and never keep", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeShadow, "") // no registry file
		h.addRecord("security-scan")
		h.c.securityFailurePass()
		s := h.record().Security
		if s == nil {
			t.Fatal("no report stored")
		}
		if s.RegistryError == "" {
			t.Error("registry_error is empty for an unreadable registry")
		}
		if s.Recommendation == recommendKeep {
			t.Error("an unreadable registry must never claim keep")
		}
		if s.Recommendation != recommendUnknown {
			t.Errorf("recommendation = %q, want %q", s.Recommendation, recommendUnknown)
		}
		if s.RegistryFound {
			t.Error("registry_found is true for an unreadable registry")
		}
	})

	t.Run("F3 a failed advisory lookup leaves the date empty and the recommendation unknown", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeShadow, riskRegistryFixture)
		h.advResult = SecurityFailureAdvisoryResult{ErrText: "gh api: rate limited"}
		h.addRecord("security-scan")
		h.c.securityFailurePass()
		s := h.record().Security
		if s == nil {
			t.Fatal("no report stored")
		}
		if s.PublishedAt != "" {
			t.Errorf("published_at = %q, want empty", s.PublishedAt)
		}
		if s.Recommendation != recommendUnknown {
			t.Errorf("recommendation = %q, want %q", s.Recommendation, recommendUnknown)
		}
		if s.LookupError == "" {
			t.Error("lookup_error is empty after a failed lookup")
		}
	})

	t.Run("F4 a failure that is not a dependency or CVE is report_only and stops nothing", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeOn, riskRegistryFixture)
		h.logText = "gitleaks: 1 leak found in config.yaml\n"
		h.addRecord("secret-scan")
		prd := h.addLinkedPRD()
		h.c.securityFailurePass()
		s := h.record().Security
		if s == nil {
			t.Fatal("no report stored")
		}
		if s.Action != actionReportOnly {
			t.Errorf("action = %q, want %q", s.Action, actionReportOnly)
		}
		if got := prdStatus(t, h.m, prd.ID); got != PRDCompleted {
			t.Errorf("linked Automaton status = %q, want it untouched (%q)", got, PRDCompleted)
		}
		if _, adv, _ := h.counts(); adv != 0 {
			t.Errorf("advisory lookups = %d, want 0 (no advisory in the log)", adv)
		}
		h.assertNoGHWrites()
	})

	t.Run("F5 a failing run with no linked Automaton is report_only", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeOn, riskRegistryFixture)
		h.addRecord("security-scan")
		h.c.securityFailurePass()
		s := h.record().Security
		if s == nil {
			t.Fatal("no report stored")
		}
		if s.Action != actionReportOnly {
			t.Errorf("action = %q, want %q", s.Action, actionReportOnly)
		}
		if !strings.Contains(s.Note, "no Automaton line") {
			t.Errorf("note = %q, want it to say no Automaton line carries the commit", s.Note)
		}
	})

	t.Run("F6 off mode performs no log read and no lookup", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeOff, riskRegistryFixture)
		h.addRecord("security-scan")
		prd := h.addLinkedPRD()
		h.c.securityFailurePass()
		logs, adv, alerts := h.counts()
		if logs != 0 || adv != 0 || alerts != 0 {
			t.Errorf("off mode: log reads=%d lookups=%d alerts=%d, want all 0", logs, adv, alerts)
		}
		if h.record().Security != nil {
			t.Error("off mode stored a report")
		}
		if got := prdStatus(t, h.m, prd.ID); got != PRDCompleted {
			t.Errorf("linked Automaton status = %q, want %q", got, PRDCompleted)
		}
		h.assertNoGHWrites()
	})

	t.Run("F7 shadow records the would-do entry, changes no Automaton and raises no alert", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeShadow, riskRegistryFixture)
		h.addRecord("security-scan")
		prd := h.addLinkedPRD()
		before, _ := h.m.Store().SnapshotPRD(prd.ID)
		h.c.securityFailurePass()
		rec := h.record()
		if rec.Security == nil || rec.Security.Action != actionWouldStop {
			t.Fatalf("report = %+v, want action %q", rec.Security, actionWouldStop)
		}
		if !repoCIWatchHasWouldDo(rec, "stop", secPassRunID) {
			t.Errorf("would_do = %+v, want an entry with action stop for run %s", rec.WouldDo, secPassRunID)
		}
		after, _ := h.m.Store().SnapshotPRD(prd.ID)
		if after.Status != before.Status || after.Rev != before.Rev || after.BlockedReason != before.BlockedReason {
			t.Errorf("shadow changed the Automaton: status %q->%q rev %d->%d", before.Status, after.Status, before.Rev, after.Rev)
		}
		if after.Triage != nil {
			t.Error("shadow wrote a triage record")
		}
		if _, _, alerts := h.counts(); alerts != 0 {
			t.Errorf("shadow raised %d alerts, want 0", alerts)
		}
	})

	t.Run("on stops the linked Automaton and copies the report to its triage record", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeOn, riskRegistryFixture)
		h.advResult = SecurityFailureAdvisoryResult{PublishedDate: "2031-03-04T05:06:07Z"}
		h.addRecord("security-scan")
		prd := h.addLinkedPRD()
		h.c.securityFailurePass()

		p, _ := h.m.Store().SnapshotPRD(prd.ID)
		if p.Status != PRDBlocked {
			t.Fatalf("Automaton status = %q, want %q", p.Status, PRDBlocked)
		}
		if !strings.Contains(p.BlockedReason, secPassAdvisory) || strings.Contains(p.BlockedReason, "\n") {
			t.Errorf("blocked reason = %q, want a one-line summary naming %s", p.BlockedReason, secPassAdvisory)
		}
		if p.Triage == nil || p.Triage.Security == nil {
			t.Fatalf("triage record = %+v, want it to carry the security report", p.Triage)
		}
		if p.Triage.Security.Action != actionStopped {
			t.Errorf("triage report action = %q, want %q", p.Triage.Security.Action, actionStopped)
		}
		repoSec := h.record().Security
		if repoSec == nil || repoSec.Action != actionStopped {
			t.Fatalf("repo record report = %+v, want action %q", repoSec, actionStopped)
		}
		if len(h.record().WouldDo) != 0 {
			t.Errorf("on mode wrote would_do entries: %+v", h.record().WouldDo)
		}

		_, _, alerts := h.counts()
		if alerts != 1 {
			t.Fatalf("alerts = %d, want 1", alerts)
		}
		h.mu.Lock()
		title, body := h.alerts[0], h.lastBody
		h.mu.Unlock()
		if !strings.Contains(title, "Automaton") {
			t.Errorf("alert title %q must say Automaton", title)
		}
		for _, want := range []string{"Automaton", "example.com/example-mod", secPassAdvisory, "2031-03-04T05:06:07Z",
			"cve/CVE-2031-0042", "2026-01-01", "2026-01-02", "2026-12-31", "Recommendation: keep"} {
			if !strings.Contains(body, want) {
				t.Errorf("alert body lacks %q:\n%s", want, body)
			}
		}

		// A later triage pass neither drops the report nor re-alerts.
		h.c.triagePass()
		p, _ = h.m.Store().SnapshotPRD(prd.ID)
		if p.Triage == nil || p.Triage.Security == nil {
			t.Error("a later triage pass dropped the Security report")
		}
		if _, _, alerts := h.counts(); alerts != 1 {
			t.Errorf("alerts after triage pass = %d, want still 1", alerts)
		}
	})

	t.Run("a second poll neither re-assesses nor re-alerts", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeOn, riskRegistryFixture)
		h.addRecord("security-scan")
		h.addLinkedPRD()
		h.c.securityFailurePass()
		logs1, adv1, alerts1 := h.counts()
		if logs1 != 1 || adv1 != 1 || alerts1 != 1 {
			t.Fatalf("first pass: log reads=%d lookups=%d alerts=%d, want 1/1/1", logs1, adv1, alerts1)
		}
		h.c.securityFailurePass()
		h.c.securityFailurePass()
		logs2, adv2, alerts2 := h.counts()
		if logs2 != logs1 || adv2 != adv1 || alerts2 != alerts1 {
			t.Errorf("repeat passes: log reads=%d lookups=%d alerts=%d, want unchanged %d/%d/%d",
				logs2, adv2, alerts2, logs1, adv1, alerts1)
		}
	})

	t.Run("only failing security records in the effective workflow set are visited", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeOn, riskRegistryFixture)
		h.addRecord("build") // not in the security workflow set
		h.c.securityFailurePass()
		if logs, _, _ := h.counts(); logs != 0 {
			t.Errorf("log reads = %d for a workflow outside the security set, want 0", logs)
		}
		if h.record().Security != nil {
			t.Error("a record outside the security set got a report")
		}
	})

	t.Run("an unreadable log is reported once and never retried", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeOn, riskRegistryFixture)
		h.logText, h.logErr = "", errors.New("gh: HTTP 502")
		h.addRecord("security-scan")
		h.addLinkedPRD()
		h.c.securityFailurePass()
		s := h.record().Security
		if s == nil || s.Action != actionReportOnly || !strings.Contains(s.Note, "failed log could not be read") {
			t.Fatalf("report = %+v, want report_only with a log-read note", s)
		}
		h.c.securityFailurePass()
		if logs, _, _ := h.counts(); logs != 1 {
			t.Errorf("log reads = %d, want 1", logs)
		}
	})

	t.Run("the report survives a repo CI watch refresh of the record", func(t *testing.T) {
		h := newSecPassHarness(t, SecurityFailureModeShadow, riskRegistryFixture)
		h.addRecord("security-scan")
		h.c.securityFailurePass()
		if h.record().Security == nil {
			t.Fatal("no report stored")
		}
		h.m.repoCIRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]RepoCIWatchRun, error) {
			if sha != fakeHeadSHA {
				return nil, nil
			}
			r := fakeRun(4242, "security-scan")
			r.Status, r.Conclusion = "completed", "failure"
			return []RepoCIWatchRun{r}, nil
		}
		// The record under test is keyed by secPassSHA; the refresh below
		// re-records the run at the head SHA, so seed the existing one.
		rec := h.record()
		head, _ := BuildRepoCIWatchRecord(secPassRepo, "main", "main", fakeHeadSHA, "security-scan", "4242", rec.RunURL, "failing", "security", time.Now())
		head.Security = rec.Security
		if _, err := h.m.SaveRepoCIWatchRecord(head); err != nil {
			t.Fatal(err)
		}
		h.c.repoCIWatchPass()
		for _, r := range h.m.ListRepoCIWatchRecords() {
			if r.SHA == fakeHeadSHA && r.Security == nil {
				t.Error("the poll refresh dropped the Security report")
			}
		}
	})
}
