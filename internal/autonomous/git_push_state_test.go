// Push-on-complete state package tests: backward-compat loading of old
// stored PRDs, effective policy precedence (PRD override > daemon setting
// > default), SetPRDGitPush validation, and recordPushOutcome state
// transitions including the alert hook contract.
package autonomous

import (
	"encoding/json"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

// Old JSON (pre-push-state fields) must load with zero push state and
// every pre-existing field intact — no migration, no clobbering.
func TestGitPushOldJSONPRDLoadsUnchanged(t *testing.T) {
	old := `{"id":"abcd1234","spec":"old prd","project_dir":"/tmp/x","status":"draft","decisions":[],
"git":{"provider":"github","url":"https://github.com/owner/repo","base_branch":"main","branch":"prd/abcd1234","auto_pr":true,"pr_url":"https://github.com/owner/repo/pull/7"}}`
	var prd PRD
	if err := json.Unmarshal([]byte(old), &prd); err != nil {
		t.Fatalf("unmarshal old PRD JSON: %v", err)
	}
	if prd.ID != "abcd1234" || !prd.Git.AutoPR || prd.Git.Branch != "prd/abcd1234" || prd.Git.PRURL == "" {
		t.Errorf("pre-existing fields changed: %+v", prd)
	}
	if prd.Git.PushOnComplete != "" || prd.Git.PushState != "" ||
		prd.Git.PushedRef != "" || prd.Git.PushedSHA != "" ||
		prd.Git.PushedAt != nil || prd.Git.PushWarning != "" {
		t.Errorf("old PRD must load with zero push state, got %+v", prd.Git)
	}

	// Round-trip through the real store the same way the daemon persists.
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.store.SavePRD(&prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD: not found after save")
	}
	if !got.Git.AutoPR || got.Git.Branch != "prd/abcd1234" || got.Git.PushState != "" || got.Git.PushWarning != "" {
		t.Errorf("round-trip altered the PRD: %+v", got)
	}
}

func newGitPushTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestGitPushEffectivePolicyPrecedence(t *testing.T) {
	m := newGitPushTestManager(t)
	m.SetGitPushSettings(func() (string, string) { return "default_branch", "upstream" })

	// 1. Per-PRD override wins over the daemon setting.
	prd := &PRD{ID: "p1", Git: GitWorkflow{PushOnComplete: "never"}}
	policy, remote := m.effectivePushPolicy(prd)
	if policy != git.PushNever || remote != "upstream" {
		t.Errorf("override: got (%q,%q), want (never, upstream)", policy, remote)
	}

	// 2. No override → daemon setting applies.
	prd2 := &PRD{ID: "p2"}
	policy, remote = m.effectivePushPolicy(prd2)
	if policy != git.PushDefaultBranch || remote != "upstream" {
		t.Errorf("daemon: got (%q,%q), want (default_branch, upstream)", policy, remote)
	}

	// 3. No daemon fn wired, no override → default branch policy + origin.
	m2 := newGitPushTestManager(t)
	policy, remote = m2.effectivePushPolicy(&PRD{ID: "p3"})
	if policy != git.PushBranch || remote != "origin" {
		t.Errorf("default: got (%q,%q), want (branch, origin)", policy, remote)
	}

	// 4. Unknown values (override or daemon) fall back to branch.
	m.SetGitPushSettings(func() (string, string) { return "sometimes", "upstream" })
	if policy, _ := m.effectivePushPolicy(&PRD{ID: "p4"}); policy != git.PushBranch {
		t.Errorf("bad daemon value: got %q, want branch", policy)
	}
	m.SetGitPushSettings(func() (string, string) { return "never", "upstream" })
	if policy, _ := m.effectivePushPolicy(&PRD{ID: "p5", Git: GitWorkflow{PushOnComplete: "whenever"}}); policy != git.PushBranch {
		t.Errorf("bad override: got %q, want branch", policy)
	}

	// 5. "Never" override + daemon remote still resolves.
	if policy, remote := m.effectivePushPolicy(&PRD{ID: "p6", Git: GitWorkflow{PushOnComplete: "never"}}); policy != git.PushNever || remote != "upstream" {
		t.Errorf("never override: got (%q,%q)", policy, remote)
	}

	// 6. Daemon fn with empty remote → origin.
	m.SetGitPushSettings(func() (string, string) { return "branch", "  " })
	if _, remote := m.effectivePushPolicy(&PRD{ID: "p7"}); remote != "origin" {
		t.Errorf("blank daemon remote: got %q, want origin", remote)
	}
}

func TestSetPRDGitPush_Valid(t *testing.T) {
	m := newGitPushTestManager(t)
	prd, _ := m.CreatePRD("spec", "", "", "", "")

	got, err := m.SetPRDGitPush(prd.ID, "  default_branch ")
	if err != nil {
		t.Fatalf("SetPRDGitPush: %v", err)
	}
	if got.Git.PushOnComplete != "default_branch" {
		t.Errorf("got %q, want default_branch (trimmed)", got.Git.PushOnComplete)
	}
	persisted, _ := m.store.GetPRD(prd.ID)
	if persisted.Git.PushOnComplete != "default_branch" {
		t.Errorf("persisted override = %q, want default_branch", persisted.Git.PushOnComplete)
	}
}

func TestSetPRDGitPush_Invalid(t *testing.T) {
	m := newGitPushTestManager(t)
	prd, _ := m.CreatePRD("spec", "", "", "", "")

	if _, err := m.SetPRDGitPush(prd.ID, "sometimes"); err == nil {
		t.Fatal("expected error for invalid policy")
	}
	persisted, _ := m.store.GetPRD(prd.ID)
	if persisted.Git.PushOnComplete != "" {
		t.Errorf("invalid value must not be stored, got %q", persisted.Git.PushOnComplete)
	}
}

func TestSetPRDGitPush_Clear(t *testing.T) {
	m := newGitPushTestManager(t)
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	if _, err := m.SetPRDGitPush(prd.ID, "never"); err != nil {
		t.Fatalf("set never: %v", err)
	}
	got, err := m.SetPRDGitPush(prd.ID, "")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got.Git.PushOnComplete != "" {
		t.Errorf("clear: got %q, want empty", got.Git.PushOnComplete)
	}
	persisted, _ := m.store.GetPRD(prd.ID)
	if persisted.Git.PushOnComplete != "" {
		t.Errorf("persisted after clear = %q, want empty", persisted.Git.PushOnComplete)
	}
}

func TestSetPRDGitPush_NotFound(t *testing.T) {
	m := newGitPushTestManager(t)
	if _, err := m.SetPRDGitPush("nope", "never"); err == nil {
		t.Error("expected error for unknown PRD id")
	}
}

func TestRecordGitPushOutcomeSuccessFailureSuccess(t *testing.T) {
	m := newGitPushTestManager(t)
	prd, _ := m.CreatePRD("spec", "", "", "", "")
	var alerts [][]string
	m.SetAlertFn(func(level, title, body string) { alerts = append(alerts, []string{level, title, body}) })

	// 1. Success: pushed state recorded, no alert, decision appended.
	before := len(prd.Decisions)
	m.recordPushOutcome(prd, git.PushResult{
		Pushed: true, Remote: "origin", Ref: "refs/heads/prd/p1", SHA: "abc123",
	}, git.PushBranch)
	if prd.Git.PushState != PushStatePushed || prd.Git.PushedRef != "refs/heads/prd/p1" ||
		prd.Git.PushedSHA != "abc123" || prd.Git.PushedAt == nil || prd.Git.PushWarning != "" {
		t.Fatalf("after push: %+v", prd.Git)
	}
	if len(prd.Decisions) != before+1 || prd.Decisions[len(prd.Decisions)-1].Kind != "git_pushed" {
		t.Errorf("pushed decision missing: %+v", prd.Decisions)
	}
	if len(alerts) != 0 {
		t.Errorf("success must not alert, got %d", len(alerts))
	}

	// 2. Failure: not_pushed state, warning with reason, decision note,
	//    exactly one warn alert naming the PRD and carrying the reason.
	before = len(prd.Decisions)
	m.recordPushOutcome(prd, git.PushResult{
		Remote: "origin", Ref: "refs/heads/prd/p1", Kind: git.PushFailAuth,
		Reason: "authentication failed for origin",
	}, git.PushBranch)
	if prd.Git.PushState != PushStateNotPushed {
		t.Errorf("PushState = %q, want not_pushed", prd.Git.PushState)
	}
	wantWarn := "not pushed: authentication failed for origin"
	if prd.Git.PushWarning != wantWarn {
		t.Errorf("PushWarning = %q, want %q", prd.Git.PushWarning, wantWarn)
	}
	if len(prd.Decisions) != before+1 {
		t.Fatalf("decision not appended")
	}
	last := prd.Decisions[len(prd.Decisions)-1]
	if last.Kind != "git_not_pushed" || last.Note != "authentication failed for origin" {
		t.Errorf("failure decision = %+v", last)
	}
	// Push-time fields from the last success remain recorded.
	if prd.Git.PushedSHA != "abc123" || prd.Git.PushedAt == nil {
		t.Errorf("failure must not erase last pushed ref/sha/time: %+v", prd.Git)
	}
	if len(alerts) != 1 {
		t.Fatalf("failure must alert exactly once, got %d", len(alerts))
	}
	lvl, title, body := alerts[0][0], alerts[0][1], alerts[0][2]
	if lvl != "warn" || title != "Automaton "+prd.ID+" not pushed" || body != "authentication failed for origin" {
		t.Errorf("alert = (%q,%q,%q)", lvl, title, body)
	}

	// 3. NothingToPush counts as success.
	m.recordPushOutcome(prd, git.PushResult{
		NothingToPush: true, Remote: "origin", Ref: "refs/heads/prd/p1", SHA: "abc123",
	}, git.PushBranch)
	if prd.Git.PushState != PushStatePushed {
		t.Errorf("NothingToPush: PushState = %q, want pushed", prd.Git.PushState)
	}
	if len(alerts) != 1 {
		t.Errorf("NothingToPush must not alert, total = %d", len(alerts))
	}

	// 4. Failure again, then success again: exactly one more alert, and
	//    the success must clear the stale warning.
	m.recordPushOutcome(prd, git.PushResult{
		Remote: "origin", Ref: "refs/heads/prd/p1", Kind: git.PushFailNonFastForward,
		Reason: "non-fast-forward rejected",
	}, git.PushBranch)
	if len(alerts) != 2 || alerts[1][2] != "non-fast-forward rejected" {
		t.Fatalf("second failure alert wrong: %v", alerts)
	}
	m.recordPushOutcome(prd, git.PushResult{
		Pushed: true, Remote: "origin", Ref: "refs/heads/prd/p1", SHA: "def456",
	}, git.PushBranch)
	if prd.Git.PushState != PushStatePushed || prd.Git.PushWarning != "" || prd.Git.PushedSHA != "def456" {
		t.Errorf("final success: %+v", prd.Git)
	}
	if len(alerts) != 2 {
		t.Errorf("success must not alert, total = %d, want 2", len(alerts))
	}

	// Everything persisted for operator read-back.
	got, _ := m.store.GetPRD(prd.ID)
	if got.Git.PushState != PushStatePushed || got.Git.PushWarning != "" || got.Git.PushedSHA != "def456" {
		t.Errorf("persisted push state = %+v", got.Git)
	}
}
