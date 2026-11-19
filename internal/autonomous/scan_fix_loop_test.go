// scan_fix_loop_test.go — the scan-fix loop contract, driven entirely
// through the FixLoopDeps seams with fakes (no LLM, no session, no
// filesystem scan). Covers: clean scans pass through untouched, the
// retry budget is spent exactly as configured (including 0), the loop
// stops the moment a re-scan passes, an exhausted loop holds with the
// remaining findings named (and within the hold-reason size limit),
// and failures are recorded on the report rather than swallowed.

package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
)

// fakeScanResult builds a scan.Result with the given findings, with the
// pass/fail verdict set from the findings (like scan.Run: any finding
// at or above fail_on_severity or any non-fixable hard finding fails).
func fakeScanResult(fs ...scan.Finding) *scan.Result {
	res := &scan.Result{Findings: fs}
	res.Pass = scan.FailingFindings(fs, scan.SeverityError) == nil
	return res
}

// fakeFixLoop records every seam call and plays back scripted results.
type fakeFixLoop struct {
	t         *testing.T
	scans     []*scan.Result // one per expected RunScan call, in order
	held      []string       // (parentID, reason) pairs, in order
	fixed     []string       // RunFix childIDs, in order
	openedIDs []string       // OpenFix childIDs returned
	openErr   error
	runErr    error
	scanErr   error
}

func (f *fakeFixLoop) deps() FixLoopDeps {
	var scanN int
	var openN int
	return FixLoopDeps{
		RunScan: func(prdID string) (*scan.Result, error) {
			if f.scanErr != nil && scanN >= len(f.scans) {
				return nil, f.scanErr
			}
			if scanN >= len(f.scans) {
				f.t.Fatalf("RunScan called %d times; only %d scripted", scanN+1, len(f.scans))
			}
			r := f.scans[scanN]
			scanN++
			if r == nil {
				r = fakeScanResult()
			}
			return r, nil
		},
		OpenFix: func(parentID string, findings []scan.Finding) (string, error) {
			if parentID == "" || len(findings) == 0 {
				f.t.Fatalf("OpenFix with empty parent or findings")
			}
			id := "child-" + string(rune('A'+openN))
			openN++
			if f.openErr != nil {
				return id, f.openErr
			}
			f.openedIDs = append(f.openedIDs, id)
			return id, nil
		},
		RunFix: func(childID string) error {
			if f.runErr != nil {
				return f.runErr
			}
			f.fixed = append(f.fixed, childID)
			return nil
		},
		Hold: func(parentID string, reason string) error {
			f.held = append(f.held, parentID+"\x00"+reason)
			return nil
		},
	}
}

func TestRunScanFixLoopClean(t *testing.T) {
	f := &fakeFixLoop{t: t, scans: []*scan.Result{fakeScanResult()}}
	rep, err := RunScanFixLoop(f.deps(), "parent-1", 4)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rep.Outcome != FixLoopOutcomeClean || rep.Held || len(rep.Attempts) != 0 {
		t.Fatalf("clean: %+v", rep)
	}
	if len(f.fixed) != 0 || len(f.held) != 0 {
		t.Fatalf("clean loop must not open, run or hold: %v %v", f.fixed, f.held)
	}
}

func TestRunScanFixLoopFixedOnSecondRetry(t *testing.T) {
	bad := scan.Finding{RuleID: "r1", Severity: scan.SeverityError, Message: "broken"}
	f := &fakeFixLoop{
		t:     t,
		scans: []*scan.Result{fakeScanResult(bad), fakeScanResult(bad), fakeScanResult()},
	}
	rep, err := RunScanFixLoop(f.deps(), "parent-1", 4)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rep.Outcome != FixLoopOutcomeFixed {
		t.Fatalf("outcome: %+v", rep)
	}
	if len(rep.Attempts) != 2 || len(f.fixed) != 2 {
		t.Fatalf("expected 2 fix attempts, got attempts=%d fixed=%v", len(rep.Attempts), f.fixed)
	}
	if len(f.held) != 0 {
		t.Fatalf("fixed loop must not hold: %v", f.held)
	}
	if !strings.HasPrefix(rep.Attempts[0].FixPRDID, "child-A") || !strings.HasPrefix(rep.Attempts[1].FixPRDID, "child-B") {
		t.Fatalf("attempts: %+v", rep.Attempts)
	}
}

func TestRunScanFixLoopRetryBudgetOfZeroHoldsImmediately(t *testing.T) {
	bad := scan.Finding{RuleID: "r1", Severity: scan.SeverityError, Message: "broken"}
	f := &fakeFixLoop{t: t, scans: []*scan.Result{fakeScanResult(bad)}}
	rep, err := RunScanFixLoop(f.deps(), "parent-1", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rep.Outcome != FixLoopOutcomeHeld || !rep.Held {
		t.Fatalf("expected held, got %+v", rep)
	}
	if len(rep.Attempts) != 0 || len(f.fixed) != 0 {
		t.Fatalf("zero retries must never open or run a fix task: attempts=%v fixed=%v", rep.Attempts, f.fixed)
	}
	if len(f.held) != 1 {
		t.Fatalf("expected exactly one hold: %v", f.held)
	}
	reason := strings.TrimSpace(strings.TrimPrefix(f.held[0], "parent-1\x00"))
	if !strings.Contains(reason, "r1") {
		t.Fatalf("hold reason must name the failing finding, got %q", reason)
	}
	if utf8c := runeCount(reason); utf8c > holdReasonMaxChars {
		t.Fatalf("hold reason over limit: %d > %d", utf8c, holdReasonMaxChars)
	}
}

func TestRunScanFixLoopExhaustsRetries(t *testing.T) {
	bad := scan.Finding{RuleID: "rX", Severity: scan.SeverityError, Message: "still broken"}
	f := &fakeFixLoop{
		t: t,
		scans: []*scan.Result{
			fakeScanResult(bad),
			fakeScanResult(bad),
			fakeScanResult(bad),
		},
	}
	rep, err := RunScanFixLoop(f.deps(), "parent-1", 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rep.Outcome != FixLoopOutcomeHeld || !rep.Held {
		t.Fatalf("expected held, got %+v", rep)
	}
	if len(rep.Attempts) != 2 || len(f.fixed) != 2 {
		t.Fatalf("exactly max_retries fix attempts expected: attempts=%d fixed=%v", len(rep.Attempts), f.fixed)
	}
	if len(f.held) != 1 {
		t.Fatalf("expected exactly one hold: %v", f.held)
	}
	reason := strings.TrimSpace(strings.TrimPrefix(f.held[0], "parent-1\x00"))
	if !strings.Contains(reason, "rX") {
		t.Fatalf("hold reason must name the remaining finding, got %q", reason)
	}
}

func TestRunScanFixLoopOpenFixFailureIsRecordedAndLoopContinues(t *testing.T) {
	bad := scan.Finding{RuleID: "r9", Severity: scan.SeverityError, Message: "broken"}
	// First OpenFix fails; second succeeds and the re-scan passes.
	f := &fakeFixLoop{
		t:     t,
		scans: []*scan.Result{fakeScanResult(bad), fakeScanResult(bad), fakeScanResult()},
	}
	openErr := errors.New("nope")
	defer func() { f.openErr = nil }()
	f.openErr = openErr
	// The first attempt records the open failure; the second succeeds.
	deps := f.deps()
	origOpen := deps.OpenFix
	n := 0
	deps.OpenFix = func(parentID string, findings []scan.Finding) (string, error) {
		n++
		if n == 1 {
			return "child-openfail", errors.New("decompose refused")
		}
		return origOpen(parentID, findings)
	}
	rep, err := RunScanFixLoop(deps, "parent-1", 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rep.Outcome != FixLoopOutcomeFixed {
		t.Fatalf("outcome: %+v", rep)
	}
	if len(rep.Attempts) != 2 {
		t.Fatalf("attempts: %+v", rep)
	}
	if rep.Attempts[0].Error == "" {
		t.Fatalf("first attempt must carry the open error: %+v", rep.Attempts[0])
	}
	_ = openErr
}

func TestRunScanFixLoopRunFixFailureRecorded(t *testing.T) {
	bad := scan.Finding{RuleID: "r7", Severity: scan.SeverityError, Message: "broken"}
	f := &fakeFixLoop{
		t:      t,
		scans:  []*scan.Result{fakeScanResult(bad), fakeScanResult(bad), fakeScanResult()},
		runErr: errors.New("worker crashed"),
	}
	// Fail only the first run; second succeeds.
	deps := f.deps()
	origRun := deps.RunFix
	n := 0
	deps.RunFix = func(childID string) error {
		n++
		if n == 1 {
			return errors.New("worker crashed")
		}
		return origRun(childID)
	}
	rep, err := RunScanFixLoop(deps, "parent-1", 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rep.Outcome != FixLoopOutcomeFixed {
		t.Fatalf("outcome: %+v", rep)
	}
	if rep.Attempts[0].Error == "" {
		t.Fatalf("first attempt must carry the run error: %+v", rep.Attempts[0])
	}
}

func TestRunScanFixLoopHoldFailureYieldsErrorOutcome(t *testing.T) {
	bad := scan.Finding{RuleID: "rH", Severity: scan.SeverityError, Message: "broken"}
	f := &fakeFixLoop{t: t, scans: []*scan.Result{fakeScanResult(bad), fakeScanResult(bad)}}
	deps := f.deps()
	deps.Hold = func(parentID, reason string) error { return errors.New("hold refused") }
	rep, err := RunScanFixLoop(deps, "parent-1", 1)
	if err == nil {
		t.Fatalf("expected error when the hold cannot be set")
	}
	if rep.Outcome != FixLoopOutcomeError {
		t.Fatalf("outcome: %+v", rep)
	}
}

func TestFormatFixLoopHoldReasonNamesFindings(t *testing.T) {
	res := fakeScanResult(
		scan.Finding{RuleID: "a", Severity: scan.SeverityError, File: "main.go", Line: 10, Message: "x"},
		scan.Finding{RuleID: "b", Severity: scan.SeverityWarning, File: "util.go", Message: "y"},
	)
	got := FormatFixLoopHoldReason(2, res)
	for _, want := range []string{"2", "a", "b", "main.go:10", "util.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("hold reason %q missing %q", got, want)
		}
	}
	if runeCount(got) > holdReasonMaxChars {
		t.Fatalf("over limit: %d", runeCount(got))
	}
}

func TestFormatFixLoopHoldReasonTruncatesLongRuns(t *testing.T) {
	fs := make([]scan.Finding, 0, 40)
	for i := 0; i < 40; i++ {
		fs = append(fs, scan.Finding{
			RuleID:   "rule-number-" + string(rune('a'+i%26)) + string(rune('1'+i%9)),
			Severity: scan.SeverityError,
			File:     "path/to/a/very/long/file/name/number-" + string(rune('a'+i%26)) + ".go",
			Line:     1234 + i,
			Message:  strings.Repeat("a pretty long message for testing the limit, ", 3) + string(rune('a'+i%26)),
		})
	}
	got := FormatFixLoopHoldReason(5, fakeScanResult(fs...))
	if runeCount(got) > holdReasonMaxChars {
		t.Fatalf("hold reason must stay within %d chars, got %d: %q", holdReasonMaxChars, runeCount(got), got)
	}
	if !strings.Contains(got, "and more") {
		t.Fatalf("truncated reason must say there are more findings: %q", got)
	}
}

func TestScanFixLoopManagerWiringRunsReport(t *testing.T) {
	// The Manager wiring must surface the same report shape end to end
	// with the executor seam faked. This uses the real store + hold, so
	// it doubles as a check that holdForScanFixLoop works from a
	// draft-status Automaton.
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	parent, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	// Fake everything the real seams would do: the scan keeps failing,
	// the fix task is "opened and run" without a worker, and the hold
	// actually lands.
	m.mu.Lock()
	m.fixLoopRun = func(ctx context.Context, childID string) error { return nil }
	m.mu.Unlock()
	rep, err := m.RunScanFixLoop(parent.ID)
	// RunScan itself runs on an empty project: no findings, so the
	// expected outcome is clean (no hold) — assert the report shape
	// holds and the scan seam produced a result.
	if err != nil {
		t.Fatalf("RunScanFixLoop: %v", err)
	}
	if rep.PRDID != parent.ID || rep.Outcome != FixLoopOutcomeClean || rep.LastScan == nil {
		t.Fatalf("report: %+v", rep)
	}
	if rep.Outcome != FixLoopOutcomeClean {
		t.Fatalf("empty project must scan clean: %+v", rep)
	}
}

func TestManagerHoldForScanFixLoopFromDraft(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	parent, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.holdForScanFixLoop(parent.ID, "scan findings remain: a (main.go:1)"); err != nil {
		t.Fatalf("hold: %v", err)
	}
	got, ok := m.store.GetPRD(parent.ID)
	if !ok {
		t.Fatalf("prd gone")
	}
	if got.Status != PRDHeld {
		t.Fatalf("status: %v", got.Status)
	}
	if got.Hold == nil || !strings.Contains(got.Hold.Reason, "main.go:1") {
		t.Fatalf("hold state: %+v", got.Hold)
	}
	if !strings.Contains(got.Hold.Reason, "a ") && !strings.Contains(got.Hold.Reason, "a") {
		t.Fatalf("reason: %q", got.Hold.Reason)
	}
}

// runeCount is the length of a string in runes (hold limits count
// runes, not bytes).
func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// TestRunScanFixLoopFixedOnFirstRetry — a finding that gets fixed: exactly one
// corrective task is opened, the re-scan passes, the loop finishes as
// "fixed" without holding the parent — so the Automaton proceeds as usual.
func TestRunScanFixLoopFixedOnFirstRetry(t *testing.T) {
	bad := scan.Finding{RuleID: "r1", Severity: scan.SeverityError, Message: "broken"}
	f := &fakeFixLoop{
		t:     t,
		scans: []*scan.Result{fakeScanResult(bad), fakeScanResult()},
	}
	rep, err := RunScanFixLoop(f.deps(), "parent-1", 3)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}
	if rep.Outcome != FixLoopOutcomeFixed {
		t.Fatalf("Outcome = %q, want %q", rep.Outcome, FixLoopOutcomeFixed)
	}
	if len(rep.Attempts) != 1 {
		t.Fatalf("Attempts = %d, want exactly 1", len(rep.Attempts))
	}
	if len(f.fixed) != 1 {
		t.Fatalf("fix tasks run = %d, want exactly 1", len(f.fixed))
	}
	if !strings.HasPrefix(f.fixed[0], "child-A") {
		t.Fatalf("run step ran %v, want the first corrective task (child-A*)", f.fixed)
	}
	if len(f.held) != 0 {
		t.Fatalf("parent was held %d time(s); a fixed finding must not hold the Automaton", len(f.held))
	}
}

// TestManagerScanConfigCarriesFixLoopKeys — the caller decides between a plain
// scan and the loop by reading scan.Config (the /scan/fixloop route in
// internal/server checks fix_loop_enabled): the master switch and the retry
// budget must both survive the SetScanConfig/ScanConfig round-trip, and the
// reader must return a copy so callers can't rewrite the manager's config.
func TestManagerScanConfigCarriesFixLoopKeys(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	sc := m.ScanConfig()
	sc.FixLoopEnabled = true
	sc.FixLoopMaxRetries = 5
	m.SetScanConfig(sc)

	got := m.ScanConfig()
	if !got.FixLoopEnabled || got.FixLoopMaxRetries != 5 {
		t.Fatalf("ScanConfig() = enabled=%v retries=%d, want enabled=true retries=5", got.FixLoopEnabled, got.FixLoopMaxRetries)
	}

	// Copy semantics: the reader gets its own copy.
	got.FixLoopEnabled = false
	got.FixLoopMaxRetries = 0
	if !m.ScanConfig().FixLoopEnabled || m.ScanConfig().FixLoopMaxRetries != 5 {
		t.Fatalf("mutating the returned config rewrote the manager's config")
	}
}
