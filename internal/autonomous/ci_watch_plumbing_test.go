// ci-watch completion guardrail plumbing tests
// (config default, PRD record round-trip, library entry). The detection
// logic itself lives elsewhere.

package autonomous

import (
	"context"
	"encoding/json"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCIWatchTimeout_DefaultAndConfigured(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if got := m.ciWatchTimeout(); got != 1500*time.Second {
		t.Errorf("unset: ciWatchTimeout() = %v, want 1500s", got)
	}

	cfg := m.Config()
	cfg.CIWatchTimeoutSeconds = -5
	m.SetConfig(cfg)
	if got := m.ciWatchTimeout(); got != 1500*time.Second {
		t.Errorf("negative: ciWatchTimeout() = %v, want 1500s", got)
	}

	cfg.CIWatchTimeoutSeconds = 90
	m.SetConfig(cfg)
	if got := m.ciWatchTimeout(); got != 90*time.Second {
		t.Errorf("configured: ciWatchTimeout() = %v, want 90s", got)
	}
}

func TestCIWatchTimeout_ConfigJSONTag(t *testing.T) {
	raw, err := json.Marshal(Config{CIWatchTimeoutSeconds: 600})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"ci_watch_timeout_seconds":600`) {
		t.Errorf("Config JSON missing ci_watch_timeout_seconds: %s", raw)
	}
	zero, _ := json.Marshal(Config{})
	if strings.Contains(string(zero), "ci_watch_timeout_seconds") {
		t.Errorf("zero Config should omit ci_watch_timeout_seconds: %s", zero)
	}
}

func TestPRDCIWatch_JSONRoundTrip(t *testing.T) {
	// Zero value: field omitted entirely.
	raw, err := json.Marshal(PRD{ID: "p1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"ci_watch"`) {
		t.Errorf("nil CIWatch should be omitted: %s", raw)
	}

	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	in := PRD{
		ID: "p2",
		CIWatch: &CIWatchResult{
			Status:   "failed_content",
			SHA:      "abc123",
			Repo:     "owner/repo",
			Summary:  "1 of 2 runs failed",
			Category: "content",
			Issues:   []string{"unit test TestFoo failed"},
			Runs: []CIWatchRun{
				{ID: 42, Name: "CI", Status: "completed", Conclusion: "failure", URL: "https://example.invalid/runs/42"},
				{ID: 43, Name: "Lint", Status: "completed", Conclusion: "success"},
			},
			CheckedAt: at,
		},
	}
	raw, err = json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, k := range []string{`"ci_watch"`, `"status":"failed_content"`, `"sha":"abc123"`, `"runs"`, `"checked_at"`, `"conclusion":"failure"`} {
		if !strings.Contains(string(raw), k) {
			t.Errorf("marshal missing %s: %s", k, raw)
		}
	}
	var out PRD
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in.CIWatch, out.CIWatch) {
		t.Errorf("round-trip mismatch:\n in=%+v\nout=%+v", in.CIWatch, out.CIWatch)
	}
}

func TestCIWatchRun_ZeroOptionalFieldsOmitted(t *testing.T) {
	raw, _ := json.Marshal(CIWatchResult{Status: "skipped"})
	for _, k := range []string{`"sha"`, `"repo"`, `"runs"`, `"summary"`, `"category"`, `"issues"`} {
		if strings.Contains(string(raw), k) {
			t.Errorf("zero CIWatchResult should omit %s: %s", k, raw)
		}
	}
	if !strings.Contains(string(raw), `"checked_at"`) {
		t.Errorf("checked_at must always be present: %s", raw)
	}
}

func TestGuardrailLibrary_CIWatchPresentAndOffByDefault(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	e, ok := m.lookupGuardrailEntry("ci-watch")
	if !ok {
		t.Fatal("ci-watch missing from guardrail library")
	}
	if e.Type != "judgment" || e.ScanType != "" {
		t.Errorf("ci-watch entry type = %q scan=%q, want judgment/no scan type", e.Type, e.ScanType)
	}
	if !strings.Contains(e.Description, "CI runs") {
		t.Errorf("ci-watch description unexpected: %q", e.Description)
	}
	// Library order: ci-watch immediately follows rules-review.
	lib := m.GuardrailLibrary()
	for i, g := range lib {
		if g.Name == "rules-review" {
			if i+1 >= len(lib) || lib[i+1].Name != "ci-watch" {
				t.Errorf("ci-watch should directly follow rules-review in the library")
			}
		}
	}
	// Off by default: the resolved PRD-level list is empty.
	if got := m.resolveGuardrails(&PRD{ID: "p"}, "prd"); len(got) != 0 {
		t.Errorf("default PRD-level guardrails = %v, want empty", got)
	}
}

// ---- PostVerifySHA pinning (2026-10-13 incident, root cause iii) ----

const (
	ciPinOwn  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ciPinLive = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestCIWatchPinsOwnCommitNotLiveHEAD(t *testing.T) {
	// The task completed at commit ciPinOwn; live HEAD has since moved to
	// ciPinLive (sibling PRD / operator). ci-watch must list and record
	// runs for ciPinOwn, and never consult the live-HEAD seam.
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	liveConsulted := false
	var gitSHAArg, runsArg string
	m.mu.Lock()
	m.ciGitFn = func(context.Context, string) (string, string, bool, error) {
		liveConsulted = true
		return ciPinLive, ciTestOrigin, true, nil
	}
	m.ciGitSHAFn = func(_ context.Context, _ string, sha string) (string, bool, error) {
		gitSHAArg = sha
		return ciTestOrigin, true, nil
	}
	m.ciRunsFn = func(_ context.Context, _ string, sha string) ([]CIWatchRun, error) {
		runsArg = sha
		return ciRun("completed", "success"), nil
	}
	m.ciPollInterval = time.Millisecond
	m.mu.Unlock()
	ghWasAvailable := ghAvailable
	ghAvailable = func() bool { return true }
	defer func() { ghAvailable = ghWasAvailable }()

	prd := &PRD{
		ID: "p1", ProjectDir: t.TempDir(), PerPRDGuardrails: []string{"ci-watch"},
		Story: []Story{{Title: "S", Tasks: []Task{{ID: "t1", Title: "T", Status: TaskCompleted, PostVerifySHA: ciPinOwn}}}},
	}
	blocked, fix, err := m.runPRDCompletionCIWatch(context.Background(), prd)
	if err != nil || blocked || fix != nil {
		t.Fatalf("blocked=%v fix=%v err=%v, want false/nil/nil for passing pinned CI", blocked, fix, err)
	}
	if liveConsulted {
		t.Fatalf("live-HEAD seam consulted; a completed task recorded PostVerifySHA %s, so the watch must pin it", ciPinOwn)
	}
	if gitSHAArg != ciPinOwn {
		t.Fatalf("gitSHA seam got %q, want the task's own commit %s", gitSHAArg, ciPinOwn)
	}
	if runsArg != ciPinOwn {
		t.Fatalf("runs listed for %q, want the task's own commit %s (not live HEAD %s)", runsArg, ciPinOwn, ciPinLive)
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "pass" || prd.CIWatch.SHA != ciPinOwn {
		t.Fatalf("CIWatch = %+v, want pass with SHA %s", prd.CIWatch, ciPinOwn)
	}
}

func TestCIWatchFallsBackToLiveHEADWhenNoPostVerifySHA(t *testing.T) {
	// No task recorded a PostVerifySHA (legacy store / capture failure):
	// identical behavior to the pre-pinning code path, on live HEAD.
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	gitSHAConsulted := false
	var runsArg string
	m.mu.Lock()
	m.ciGitFn = func(context.Context, string) (string, string, bool, error) {
		return ciPinLive, ciTestOrigin, true, nil
	}
	m.ciGitSHAFn = func(context.Context, string, string) (string, bool, error) {
		gitSHAConsulted = true
		return ciTestOrigin, true, nil
	}
	m.ciRunsFn = func(_ context.Context, _ string, sha string) ([]CIWatchRun, error) {
		runsArg = sha
		return ciRun("completed", "success"), nil
	}
	m.ciPollInterval = time.Millisecond
	m.mu.Unlock()
	ghWasAvailable := ghAvailable
	ghAvailable = func() bool { return true }
	defer func() { ghAvailable = ghWasAvailable }()

	prd := &PRD{
		ID: "p1", ProjectDir: t.TempDir(), PerPRDGuardrails: []string{"ci-watch"},
		Story: []Story{{Title: "S", Tasks: []Task{{ID: "t1", Title: "T", Status: TaskCompleted}}}}, // no pin
	}
	blocked, fix, err := m.runPRDCompletionCIWatch(context.Background(), prd)
	if err != nil || blocked || fix != nil {
		t.Fatalf("blocked=%v fix=%v err=%v, want false/nil/nil for passing live-HEAD CI", blocked, fix, err)
	}
	if gitSHAConsulted {
		t.Fatalf("pinned-SHA seam consulted although no task recorded a PostVerifySHA")
	}
	if runsArg != ciPinLive {
		t.Fatalf("runs listed for %q, want live HEAD %s (fallback behavior)", runsArg, ciPinLive)
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "pass" || prd.CIWatch.SHA != ciPinLive {
		t.Fatalf("CIWatch = %+v, want pass with live-HEAD SHA %s", prd.CIWatch, ciPinLive)
	}
}

func TestCIWatchPinnedNotPushedSkips(t *testing.T) {
	// Pinned commit is not reachable from any remote-tracking ref: skip
	// (recorded), never block, runs never listed.
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.mu.Lock()
	m.ciGitFn = func(context.Context, string) (string, string, bool, error) {
		t.Error("live-HEAD seam consulted although a completed task recorded a PostVerifySHA")
		return ciPinLive, ciTestOrigin, true, nil
	}
	m.ciGitSHAFn = func(_ context.Context, _ string, sha string) (string, bool, error) {
		return ciTestOrigin, false, nil
	}
	m.ciRunsFn = func(context.Context, string, string) ([]CIWatchRun, error) {
		t.Error("runs listed although the pinned commit isn't reachable from any remote-tracking ref")
		return nil, nil
	}
	m.mu.Unlock()

	prd := &PRD{
		ID: "p1", ProjectDir: t.TempDir(), PerPRDGuardrails: []string{"ci-watch"},
		Story: []Story{{Title: "S", Tasks: []Task{{ID: "t1", Title: "T", Status: TaskCompleted, PostVerifySHA: ciPinOwn}}}},
	}
	blocked, fix, err := m.runPRDCompletionCIWatch(context.Background(), prd)
	if err != nil || blocked || fix != nil {
		t.Fatalf("blocked=%v fix=%v err=%v, want false/nil/nil for an unpushed pinned commit", blocked, fix, err)
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "skipped" || prd.CIWatch.SHA != ciPinOwn {
		t.Fatalf("CIWatch = %+v, want skipped with the pinned SHA %s", prd.CIWatch, ciPinOwn)
	}
}

// TestPRDLastPostVerifySHA locks down the pin selection: the LAST
// completed task's pin in story order wins; failed tasks and unpinned
// completed tasks are ignored.
func TestPRDLastPostVerifySHA(t *testing.T) {
	cases := []struct {
		name string
		prd  *PRD
		want string
	}{
		{"no tasks", &PRD{}, ""},
		{"unpinned completed task", &PRD{Story: []Story{{Tasks: []Task{{Status: TaskCompleted}}}}}, ""},
		{"failed pin ignored, prior completed pin kept", &PRD{Story: []Story{{Tasks: []Task{
			{Status: TaskCompleted, PostVerifySHA: ciPinOwn},
			{Status: TaskFailed, PostVerifySHA: ciPinLive},
		}}}}, ciPinOwn},
		{"latest completed pin wins", &PRD{Story: []Story{
			{Tasks: []Task{{Status: TaskCompleted, PostVerifySHA: ciPinLive}}},
			{Tasks: []Task{{Status: TaskCompleted, PostVerifySHA: ciPinOwn}}},
		}}, ciPinOwn},
		{"later empty story must not clear the pin", &PRD{Story: []Story{
			{Tasks: []Task{{Status: TaskCompleted, PostVerifySHA: ciPinOwn}}},
			{Tasks: []Task{}},
		}}, ciPinOwn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := prdLastPostVerifySHA(tc.prd); got != tc.want {
				t.Fatalf("prdLastPostVerifySHA = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDefaultCIGitSHA_PinnedNotTipStillReachable exercises the real
// reachability rule: a pinned commit that is no longer a branch tip but
// IS an ancestor of a remote-tracking ref reads as pushed (git branch -r
// --contains; equivalently merge-base --is-ancestor); a local-only
// commit reads as not pushed.
func TestDefaultCIGitSHA_PinnedNotTipStillReachable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := driftTestRepo(t)
	bare := t.TempDir()
	driftGit(t, bare, "init", "-q", "--bare", "-b", "main")

	driftWrite(t, dir, "a.txt", "a")
	driftGit(t, dir, "add", "a.txt")
	driftGit(t, dir, "commit", "-q", "-m", "c1")
	driftGit(t, dir, "branch", "-M", "main")
	driftGit(t, dir, "remote", "add", "origin", bare)
	driftGit(t, dir, "push", "-q", "-u", "origin", "main")
	pushedSHA := driftGit(t, dir, "rev-parse", "HEAD")
	// Move local HEAD forward without pushing: the pushed commit is now
	// no longer a branch tip in the local clone either... (it still is —
	// local main just extended). Pushed SHA stays an ancestor of
	// origin/main; the new tip is not reachable from any remote ref.
	driftWrite(t, dir, "b.txt", "b")
	driftGit(t, dir, "add", "b.txt")
	driftGit(t, dir, "commit", "-q", "-m", "c2")
	newTip := driftGit(t, dir, "rev-parse", "HEAD")

	origin, pushed, err := defaultCIGitSHA(context.Background(), dir, pushedSHA)
	if err != nil {
		t.Fatalf("defaultCIGitSHA(pushed): %v", err)
	}
	if !pushed {
		t.Fatalf("pinned commit %s was pushed and is an ancestor of origin/main, want pushed=true (not tip-equality)", pushedSHA)
	}
	if origin == "" {
		t.Fatalf("origin URL empty, want %s", bare)
	}
	if _, unpushed, err := defaultCIGitSHA(context.Background(), dir, newTip); err != nil || unpushed {
		t.Fatalf("new tip %s never pushed: pushed=%v err=%v, want false, nil", newTip, unpushed, err)
	}
	// Not a git repo at all: error (distinct from "not pushed").
	if _, _, err := defaultCIGitSHA(context.Background(), t.TempDir(), pushedSHA); err == nil {
		t.Fatalf("non-repo dir: want error, got nil")
	}
}
