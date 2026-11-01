// pushValidatedWork (git_push.go) against a real temporary bare repo
// as the remote: branch policy, default-branch policy gated on final
// gates, the never policy, a PRD override beating the daemon setting,
// and the never-force guarantee (a diverged remote is left exactly
// untouched, with a recorded not_pushed state and an alert).
package autonomous

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// --- helpers ---------------------------------------------------------

// gitIn runs `git <args...>` in dir and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGitIn(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

// commitFile writes name in dir and commits it, returning the new HEAD SHA.
func commitFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.WriteFile(dir+"/"+name, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-m", name)
	return gitIn(t, dir, "rev-parse", "HEAD")
}

// bareRefs returns every ref of the bare repo as "sha ref" lines
// (""" when it has no refs at all).
func bareRefs(t *testing.T, bare string) string {
	t.Helper()
	out, err := runGitIn(bare, "for-each-ref")
	if err != nil {
		t.Fatalf("git for-each-ref in bare repo: %v\n%s", err, out)
	}
	return strings.TrimSpace(out)
}

type pushAlertRecorder struct {
	mu  sync.Mutex
	all []string // "level|title|body"
}

func (a *pushAlertRecorder) record(level, title, body string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.all = append(a.all, level+"|"+title+"|"+body)
}

func (a *pushAlertRecorder) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.all)
}

func (a *pushAlertRecorder) first() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.all) == 0 {
		return ""
	}
	return a.all[0]
}

// pushHarness builds a Manager wired with a live push policy + origin
// remote and a recording alert fn, plus a bare (remote) and a working
// checkout (clone of the bare) with git identities configured.
type pushHarness struct {
	t      *testing.T
	m      *Manager
	pr     *PRD
	bare   string
	work   string
	alerts *pushAlertRecorder
}

func newPushHarness(t *testing.T, policy string) *pushHarness {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	alerts := &pushAlertRecorder{}
	m.SetAlertFn(alerts.record)
	m.SetGitPushSettings(func() (string, string) { return policy, "origin" })

	bare := t.TempDir()
	gitIn(t, bare, "init", "--bare")
	gitIn(t, bare, "symbolic-ref", "HEAD", "refs/heads/main")
	// Seed the bare's default branch with one commit (via a throwaway
	// clone) so the remote advertises a default branch from the start —
	// a completely empty bare advertises no refs, so the default branch
	// would be unresolvable.
	seed := t.TempDir()
	gitIn(t, seed, "clone", bare, seed)
	gitIn(t, seed, "config", "user.email", "seed@example.com")
	gitIn(t, seed, "config", "user.name", "Seed")
	commitFile(t, seed, "seeded.txt", "remote baseline\n")
	gitIn(t, seed, "push", "origin", "HEAD:refs/heads/main")

	work := t.TempDir()
	gitIn(t, work, "clone", bare, work) // clone of the bare => remote "origin"
	gitIn(t, work, "config", "user.email", "test@example.com")
	gitIn(t, work, "config", "user.name", "Test")

	prd, err := m.CreatePRD("push engine test spec", work, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return &pushHarness{t: t, m: m, pr: prd, bare: bare, work: work, alerts: alerts}
}

// savedPRD reloads the PRD from the store.
func (h *pushHarness) savedPRD(t *testing.T) *PRD {
	t.Helper()
	saved, ok := h.m.store.GetPRD(h.pr.ID)
	if !ok {
		t.Fatalf("stored PRD %s not found", h.pr.ID)
	}
	return saved
}

// --- tests -----------------------------------------------------------

func TestPushValidatedWorkBranchPolicyPushesWorkBranch(t *testing.T) {
	h := newPushHarness(t, "branch")

	gitIn(t, h.work, "checkout", "-b", "feature")
	sha := commitFile(t, h.work, "work.txt", "validated task work\n")
	h.pr.Git.Branch = "feature"
	if err := h.m.store.SavePRD(h.pr); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	h.m.pushValidatedWork(h.pr, false)

	if got := gitIn(t, h.bare, "rev-parse", "refs/heads/feature"); got != sha {
		t.Errorf("bare refs/heads/feature=%s, want %s", got, sha)
	}
	if got := gitIn(t, h.work, "rev-parse", "--abbrev-ref", "feature@{upstream}"); got != "origin/feature" {
		t.Errorf("upstream of feature=%s, want origin/feature", got)
	}
	saved := h.savedPRD(t)
	if saved.Git.PushState != PushStatePushed {
		t.Errorf("PushState=%q, want %q", saved.Git.PushState, PushStatePushed)
	}
	if saved.Git.PushedRef != "feature" || saved.Git.PushedSHA != sha {
		t.Errorf("pushed ref/sha=%s/%s, want feature/%s", saved.Git.PushedRef, saved.Git.PushedSHA, sha)
	}
	if h.alerts.count() != 0 {
		t.Errorf("unexpected alerts: %v", h.alerts.all)
	}
}

func TestPushValidatedWorkDefaultBranchPolicyGatesOnFinalGates(t *testing.T) {
	h := newPushHarness(t, "default_branch")
	sha := commitFile(t, h.work, "work.txt", "final validated work\n")
	bareBefore := bareRefs(t, h.bare)

	// final gates not passed: no push, and nothing recorded.
	h.m.pushValidatedWork(h.pr, false)
	if got := bareRefs(t, h.bare); got != bareBefore {
		t.Fatalf("bare changed after a gated-off push:\nbefore:%s\nafter:%s", bareBefore, got)
	}
	saved := h.savedPRD(t)
	if saved.Git.PushState != "" {
		t.Errorf("PushState=%q, want empty (nothing recorded)", saved.Git.PushState)
	}
	if len(saved.Decisions) != 0 {
		t.Errorf("Decisions=%v, want none", saved.Decisions)
	}
	if h.alerts.count() != 0 {
		t.Errorf("unexpected alerts: %v", h.alerts.all)
	}

	// final gates passed: fast-forward HEAD onto the bare default branch.
	h.m.pushValidatedWork(h.pr, true)
	if got := gitIn(t, h.bare, "rev-parse", "refs/heads/main"); got != sha {
		t.Errorf("bare refs/heads/main=%s, want %s", got, sha)
	}
	saved = h.savedPRD(t)
	if saved.Git.PushState != PushStatePushed {
		t.Errorf("PushState=%q, want %q", saved.Git.PushState, PushStatePushed)
	}
	if saved.Git.PushedRef != "main" || saved.Git.PushedSHA != sha {
		t.Errorf("pushed ref/sha=%s/%s, want main/%s", saved.Git.PushedRef, saved.Git.PushedSHA, sha)
	}
	if h.alerts.count() != 0 {
		t.Errorf("unexpected alerts: %v", h.alerts.all)
	}
}

func TestPushValidatedWorkPushFailureRecordsNotPushedAndAlerts(t *testing.T) {
	h := newPushHarness(t, "branch")
	sha := commitFile(t, h.work, "work.txt", "committed locally\n")
	gitIn(t, h.work, "remote", "remove", "origin") // remote gone => push cannot reach it

	h.m.pushValidatedWork(h.pr, true)

	saved := h.savedPRD(t)
	if saved.Git.PushState != PushStateNotPushed {
		t.Errorf("PushState=%q, want %q", saved.Git.PushState, PushStateNotPushed)
	}
	if !strings.Contains(saved.Git.PushWarning, "not pushed") || saved.Git.PushWarning == "" {
		t.Errorf("PushWarning=%q, want a visible not-pushed reason", saved.Git.PushWarning)
	}
	found := false
	for _, d := range saved.Decisions {
		if d.Kind == "git_not_pushed" && d.Note != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("no git_not_pushed decision with a reason recorded: %v", saved.Decisions)
	}
	if n := h.alerts.count(); n != 1 {
		t.Fatalf("alerts=%d, want exactly 1", n)
	}
	alert := h.alerts.first()
	if !strings.Contains(alert, "not pushed") || !strings.Contains(alert, "origin") {
		t.Errorf("alert %q should carry the not-pushed reason naming the remote", alert)
	}
	// The local commit must survive untouched.
	head := gitIn(t, h.work, "rev-parse", "HEAD")
	if head != sha {
		t.Errorf("local HEAD moved to %s, want %s", head, sha)
	}
}

func TestPushValidatedWorkNeverForcePushesDivergedRemote(t *testing.T) {
	t.Helper()
	h := newPushHarness(t, "default_branch")

	// Diverge the bare default branch with a commit the work clone does
	// not have (made from a second clone), while the work clone also has
	// a commit the bare does not have.
	other := t.TempDir()
	gitIn(t, other, "clone", h.bare, other)
	gitIn(t, other, "symbolic-ref", "HEAD", "refs/heads/main")
	gitIn(t, other, "config", "user.email", "other@example.com")
	gitIn(t, other, "config", "user.name", "Other")
	remoteSHA := commitFile(t, other, "remote-only.txt", "remote advance\n")
	gitIn(t, other, "push", "origin", "main")
	bareBefore := gitIn(t, h.bare, "rev-parse", "refs/heads/main")
	if bareBefore != remoteSHA {
		t.Fatalf("setup: bare main=%s, want %s", bareBefore, remoteSHA)
	}
	commitFile(t, h.work, "local-only.txt", "local advance\n")

	h.m.pushValidatedWork(h.pr, true) // would only "succeed" by overwriting history

	if got := gitIn(t, h.bare, "rev-parse", "refs/heads/main"); got != bareBefore {
		t.Fatalf("bare refs/heads/main moved from %s to %s — a force push happened", bareBefore, got)
	}
	saved := h.savedPRD(t)
	if saved.Git.PushState != PushStateNotPushed {
		t.Errorf("PushState=%q, want %q", saved.Git.PushState, PushStateNotPushed)
	}
	if !strings.Contains(strings.ToLower(saved.Git.PushWarning), "non-fast-forward") &&
		!strings.Contains(strings.ToLower(saved.Git.PushWarning), "rejected") {
		t.Errorf("PushWarning=%q, want a non-fast-forward rejection reason", saved.Git.PushWarning)
	}
	if n := h.alerts.count(); n != 1 {
		t.Fatalf("alerts=%d, want exactly 1", n)
	}
}

func TestPushValidatedWorkNeverPolicyDoesNothing(t *testing.T) {
	h := newPushHarness(t, "never")
	commitFile(t, h.work, "work.txt", "work that stays local\n")
	bareBefore := bareRefs(t, h.bare)

	h.m.pushValidatedWork(h.pr, true)

	if got := bareRefs(t, h.bare); got != bareBefore {
		t.Errorf("policy never must not push:\nbefore:%s\nafter:%s", bareBefore, got)
	}
	saved := h.savedPRD(t)
	if saved.Git.PushState != "" {
		t.Errorf("PushState=%q, want empty (nothing recorded)", saved.Git.PushState)
	}
	if len(saved.Decisions) != 0 {
		t.Errorf("Decisions=%v, want none", saved.Decisions)
	}
	if h.alerts.count() != 0 {
		t.Errorf("unexpected alerts: %v", h.alerts.all)
	}
}

func TestPushValidatedWorkPRDOverrideNeverBeatsDaemonBranch(t *testing.T) {
	h := newPushHarness(t, "branch")
	h.pr.Git.PushOnComplete = "never"
	if err := h.m.store.SavePRD(h.pr); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	commitFile(t, h.work, "work.txt", "override test work\n")
	bareBefore := bareRefs(t, h.bare)

	h.m.pushValidatedWork(h.pr, true)

	if got := bareRefs(t, h.bare); got != bareBefore {
		t.Errorf("PRD override never must win over daemon branch:\nbefore:%s\nafter:%s", bareBefore, got)
	}
	saved := h.savedPRD(t)
	if saved.Git.PushState != "" {
		t.Errorf("PushState=%q, want empty (nothing recorded)", saved.Git.PushState)
	}
	if h.alerts.count() != 0 {
		t.Errorf("unexpected alerts: %v", h.alerts.all)
	}
}
