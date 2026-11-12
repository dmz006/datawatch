package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
)

func setChildInherit(m *Manager, on bool) {
	cfg := m.Config()
	cfg.Automation.ChildInherit = &on
	m.SetConfig(cfg)
}

func childInheritDecision(t *testing.T, p *PRD) Decision {
	t.Helper()
	for _, d := range p.Decisions {
		if d.Kind == childInheritDecisionKind {
			return d
		}
	}
	t.Fatalf("no %s decision on %s: %+v", childInheritDecisionKind, p.ID, p.Decisions)
	return Decision{}
}

func countChildInherit(p *PRD) int {
	n := 0
	for _, d := range p.Decisions {
		if d.Kind == childInheritDecisionKind {
			n++
		}
	}
	return n
}

// gitIn runs git in dir with a throwaway identity; never touches tmux.
func childInheritGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// unlinkedChild is a fresh record with no parent link. The Automation store
// refuses a child Automaton (one with a parent link) as a member, so the
// membership paths are exercised on a record that is not linked yet.
func unlinkedChild(t *testing.T, m *Manager) *PRD {
	t.Helper()
	c, err := m.store.CreatePRD("child", "", "", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newParentPRD(t *testing.T, m *Manager) *PRD {
	t.Helper()
	p, err := m.CreatePRD("parent work", t.TempDir(), "claude-code", "claude-sonnet-5-5", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	m.store.WithLock(func() {
		p.Title = "Parent title"
		p.ProjectProfile = "proj-a"
		p.TagIDs = []string{"t1", "t2"}
		p.PrimaryTagID = "t2"
		p.Priority = 70
		p.PrioritySource = PrioritySourceExplicit
		p.VerificationBackend = "claude-code"
		p.VerificationModel = "claude-haiku-5-5"
	})
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestChildInheritCopiesFromParent(t *testing.T) {
	m := freshDefectIntakeManager(t)
	parent := newParentPRD(t, m)
	child, err := m.CreatePRDWithParent("child work", "", "", "", EffortNormal, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentPRDID != parent.ID || child.Depth != parent.Depth+1 {
		t.Fatalf("parent link %q depth %d", child.ParentPRDID, child.Depth)
	}
	if child.ProjectDir != parent.ProjectDir || child.ProjectProfile != "proj-a" {
		t.Fatalf("project %q profile %q", child.ProjectDir, child.ProjectProfile)
	}
	if child.Backend != "claude-code" || child.Model != "claude-sonnet-5-5" ||
		child.VerificationBackend != "claude-code" || child.VerificationModel != "claude-haiku-5-5" {
		t.Fatalf("llm %s/%s verify %s/%s", child.Backend, child.Model, child.VerificationBackend, child.VerificationModel)
	}
	if strings.Join(child.TagIDs, ",") != "t1,t2" || child.PrimaryTagID != "t2" {
		t.Fatalf("tags %v primary %q", child.TagIDs, child.PrimaryTagID)
	}
	if child.Priority != 70 || child.PrioritySource != PrioritySourceExplicit {
		t.Fatalf("priority %d/%s", child.Priority, child.PrioritySource)
	}
	d := childInheritDecision(t, child)
	for _, want := range []string{parent.ID, "Parent title", "project directory", "backend and model", "tags", "priority"} {
		if !strings.Contains(d.Note, want) {
			t.Errorf("decision note %q lacks %q", d.Note, want)
		}
	}
	if strings.Contains(strings.ToLower(d.Note), "prd") {
		t.Errorf("decision note uses internal wording: %q", d.Note)
	}
}

func TestChildInheritOffCopiesNothing(t *testing.T) {
	m := freshDefectIntakeManager(t)
	setChildInherit(m, false)
	parent := newParentPRD(t, m)
	child, err := m.CreatePRDWithParent("child work", "", "", "", EffortNormal, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.ProjectDir != "" || child.ProjectProfile != "" || child.Backend != "" || len(child.TagIDs) != 0 ||
		child.Priority == 70 || countChildInherit(child) != 0 {
		t.Fatalf("child changed with child_inherit off: %+v", child)
	}
}

func TestChildInheritExplicitValuesWin(t *testing.T) {
	m := freshDefectIntakeManager(t)
	parent := newParentPRD(t, m)
	dir := t.TempDir()
	child, err := m.CreatePRDWithParent("child work", dir, "ollama", "qwen", EffortNormal, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.ProjectDir != dir || child.Backend != "ollama" || child.Model != "qwen" {
		t.Fatalf("explicit lost: dir %q llm %s/%s", child.ProjectDir, child.Backend, child.Model)
	}
	// Tags, priority and verification were not given, so they inherit.
	if len(child.TagIDs) != 2 || child.Priority != 70 || child.VerificationModel != "claude-haiku-5-5" {
		t.Fatalf("inheritable fields missing: %+v", child)
	}
	// An explicit field set through childExplicit is kept as well.
	c2, _ := m.store.CreatePRD("other", "", "", "", EffortNormal)
	c2.Priority, c2.PrioritySource, c2.TagIDs = 5, PrioritySourceExplicit, []string{"mine"}
	m.inheritFromParent(c2, parent, childExplicit{Priority: true, Tags: true, Project: true,
		VerificationBackend: true, VerificationModel: true})
	if c2.Priority != 5 || strings.Join(c2.TagIDs, ",") != "mine" || c2.ProjectProfile != "" || c2.VerificationModel != "" {
		t.Fatalf("explicit flags ignored: %+v", c2)
	}
}

func TestChildInheritModelOnlyTravelsWithBackend(t *testing.T) {
	m := freshDefectIntakeManager(t)
	parent := newParentPRD(t, m)
	// A different explicit backend does not take the parent's model.
	c, err := m.CreatePRDWithParent("c", "", "ollama", "", EffortNormal, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Backend != "ollama" || c.Model != "" {
		t.Fatalf("model leaked across backends: %s/%s", c.Backend, c.Model)
	}
	// The same backend does.
	c2, err := m.CreatePRDWithParent("c2", "", "claude-code", "", EffortNormal, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Backend != "claude-code" || c2.Model != "claude-sonnet-5-5" {
		t.Fatalf("same-backend model not inherited: %s/%s", c2.Backend, c2.Model)
	}
	// A parent with no backend gives no model either.
	pp, _ := m.store.CreatePRD("bare", "", "", "", EffortNormal)
	pp.Model = "orphan-model"
	c3, _ := m.store.CreatePRD("c3", "", "", "", EffortNormal)
	m.inheritFromParent(c3, pp, childExplicit{})
	if c3.Backend != "" || c3.Model != "" {
		t.Fatalf("model travelled alone: %s/%s", c3.Backend, c3.Model)
	}
}

func TestChildInheritTierClamp(t *testing.T) {
	m := freshDefectIntakeManager(t)
	parent := newParentPRD(t, m) // Sonnet worker, Haiku verifier
	// A non-explicit Opus pre-set never survives a Sonnet parent.
	c, _ := m.store.CreatePRD("c", "", "claude-code", "claude-opus-5-5", EffortNormal)
	m.inheritFromParent(c, parent, childExplicit{})
	if modelTier(c.Model) == modelTierTop || c.Model != "claude-sonnet-5-5" {
		t.Fatalf("child kept %s under a Sonnet parent", c.Model)
	}
	// Clamp unit cases.
	var inh, clamp []string
	be, mo := "x", "claude-opus-5-5"
	inheritLLMPair(&be, &mo, "", "claude-haiku-5-5", false, false, "backend and model", &inh, &clamp)
	if mo != "claude-haiku-5-5" || len(clamp) != 1 || !strings.Contains(clamp[0], "held to the parent's") {
		t.Fatalf("clamp: %s clamp=%v", mo, clamp)
	}
	// Fable is clamped like Opus; an Opus parent clamps nothing.
	clamp = nil
	be, mo = "x", "claude-fable-5-2"
	inheritLLMPair(&be, &mo, "", "claude-sonnet-5-5", false, false, "l", &inh, &clamp)
	if mo != "claude-sonnet-5-5" {
		t.Fatalf("fable not clamped: %s", mo)
	}
	be, mo = "x", "claude-opus-5-5"
	clamp = nil
	inheritLLMPair(&be, &mo, "", "claude-opus-5-5", false, false, "l", &inh, &clamp)
	if mo != "claude-opus-5-5" || len(clamp) != 0 {
		t.Fatalf("opus parent clamped: %s %v", mo, clamp)
	}
	// An explicit model is never clamped.
	be, mo = "x", "claude-opus-5-5"
	inheritLLMPair(&be, &mo, "", "claude-sonnet-5-5", false, true, "l", &inh, &clamp)
	if mo != "claude-opus-5-5" {
		t.Fatalf("explicit model clamped: %s", mo)
	}
	// Unknown names rank 0.
	if modelTier("qwen3") != modelTierUnknown || modelTier("Claude-HAIKU-5") != modelTierSmall {
		t.Fatal("modelTier")
	}
}

func TestChildInheritResolvesWorktreeToRepository(t *testing.T) {
	m := freshDefectIntakeManager(t)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	childInheritGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	childInheritGit(t, repo, "add", "f")
	childInheritGit(t, repo, "commit", "-q", "-m", "init")
	wt := filepath.Join(root, "wt")
	childInheritGit(t, repo, "worktree", "add", "-q", "-b", "wtbranch", wt)

	parent, _ := m.store.CreatePRD("p", wt, "", "", EffortNormal)
	child, _ := m.store.CreatePRD("c", wt, "", "", EffortNormal) // creation gave it the worktree path
	m.inheritFromParent(child, parent, childExplicit{})
	if evalOrClean(child.ProjectDir) != evalOrClean(repo) {
		t.Fatalf("child dir %q, want repository %q", child.ProjectDir, repo)
	}
	// A plain checkout (and a subdirectory of one) is kept as it is.
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{repo, sub} {
		pp, _ := m.store.CreatePRD("p", d, "", "", EffortNormal)
		cc, _ := m.store.CreatePRD("c", "", "", "", EffortNormal)
		m.inheritFromParent(cc, pp, childExplicit{})
		if cc.ProjectDir != d {
			t.Fatalf("plain dir %q became %q", d, cc.ProjectDir)
		}
	}
	// An explicit directory stays even when it is a worktree path.
	c3, _ := m.store.CreatePRD("c3", wt, "", "", EffortNormal)
	m.inheritFromParent(c3, parent, childExplicit{ProjectDir: true})
	if c3.ProjectDir != wt {
		t.Fatalf("explicit dir replaced: %q", c3.ProjectDir)
	}
}

func TestChildInheritWorktreeParentFallbacks(t *testing.T) {
	m := freshDefectIntakeManager(t)
	gone := filepath.Join(t.TempDir(), "gone")
	parent, _ := m.store.CreatePRD("p", gone, "", "", EffortNormal)
	parent.Git.Branch = "automaton/x"
	// No readable repo, no base repository: skipped and said so.
	child, _ := m.store.CreatePRD("c", gone, "", "", EffortNormal)
	m.inheritFromParent(child, parent, childExplicit{})
	if !strings.Contains(childInheritDecision(t, child).Note, "skipped: project directory") {
		t.Fatalf("note %q", childInheritDecision(t, child).Note)
	}
	// With a base repository configured it is used.
	cfg := m.Config()
	base := t.TempDir()
	cfg.WorktreeBaseRepo = base
	m.SetConfig(cfg)
	c2, _ := m.store.CreatePRD("c2", gone, "", "", EffortNormal)
	m.inheritFromParent(c2, parent, childExplicit{})
	if c2.ProjectDir != filepath.Clean(base) {
		t.Fatalf("base repo not used: %q", c2.ProjectDir)
	}
	// A non-git directory of an ordinary parent is copied as it is.
	plain := t.TempDir()
	pp, _ := m.store.CreatePRD("pp", plain, "", "", EffortNormal)
	c3, _ := m.store.CreatePRD("c3", "", "", "", EffortNormal)
	m.inheritFromParent(c3, pp, childExplicit{})
	if c3.ProjectDir != plain {
		t.Fatalf("non-git dir %q", c3.ProjectDir)
	}
}

func TestChildInheritJoinsParentAutomationAfterParent(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h) // m0 -> m1 over prds 0 and 1
	parent := h.prds[0]
	child := unlinkedChild(t, h.m)
	h.m.inheritFromParent(child, parent, childExplicit{})
	got, _ := h.m.automations.Get(a.ID)
	mem := insertedMemberByPRD(t, got, child.ID)
	if automationMember(got, "m0").OnComplete != mem.ID || mem.OnComplete != "m1" {
		t.Fatalf("order: m0->%q child->%q", automationMember(got, "m0").OnComplete, mem.OnComplete)
	}
	if mem.AddedBy != childInheritActor {
		t.Fatalf("actor %q", mem.AddedBy)
	}
	if !strings.Contains(childInheritDecision(t, child).Note, "joined after the parent") {
		t.Fatalf("note %q", childInheritDecision(t, child).Note)
	}
}

func TestChildInheritAutomationRespectsHeldInsertsAndPolicy(t *testing.T) {
	// An active Automation holds the insert as a pending insert.
	h, _ := newControlHarness(t, 3)
	a := h.graph(h.cm(0, "m1", "", ""), h.cm(1, "", "", ""))
	child := unlinkedChild(t, h.m)
	h.m.inheritFromParent(child, h.prds[0], childExplicit{})
	got, _ := h.m.automations.Get(a.ID)
	if len(got.PendingInserts) != 1 || got.PendingInserts[0].PRDID != child.ID || got.PendingInserts[0].AfterMemberID != "m0" {
		t.Fatalf("pending inserts %+v", got.PendingInserts)
	}
	if !strings.Contains(childInheritDecision(t, child).Note, "held for approval") {
		t.Fatalf("note %q", childInheritDecision(t, child).Note)
	}
	// A draft under require_operator holds an agent's insert too.
	h2, _ := newControlHarness(t, 3)
	cfg := h2.m.Config()
	cfg.Automation.AgentInsertPolicy = PolicyRequireOperator
	h2.m.SetConfig(cfg)
	a2 := insertDraft(t, h2)
	c2 := unlinkedChild(t, h2.m)
	h2.m.inheritFromParent(c2, h2.prds[0], childExplicit{})
	got2, _ := h2.m.automations.Get(a2.ID)
	if len(got2.PendingInserts) != 1 || got2.PendingInserts[0].PRDID != c2.ID || len(got2.Members) != 2 {
		t.Fatalf("policy ignored: members %d pending %+v", len(got2.Members), got2.PendingInserts)
	}
}

func TestChildInheritAutomationSkips(t *testing.T) {
	// No Automation: skipped quietly (no skipped note).
	m := freshDefectIntakeManager(t)
	parent := newParentPRD(t, m)
	c, _ := m.CreatePRDWithParent("c", "", "", "", EffortNormal, parent.ID)
	if n := childInheritDecision(t, c).Note; strings.Contains(n, "Automation") {
		t.Fatalf("note mentions an Automation: %q", n)
	}
	// A cancelled Automation: skipped, not an error, child not added.
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	if _, err := h.m.automations.Update(a.ID, 0, func(cur *Automation) error { cur.Status = AutomationCancelled; return nil }); err != nil {
		t.Fatal(err)
	}
	c2 := unlinkedChild(t, h.m)
	h.m.inheritFromParent(c2, h.prds[0], childExplicit{})
	got, _ := h.m.automations.Get(a.ID)
	if len(got.Members) != 2 || !strings.Contains(childInheritDecision(t, c2).Note, "skipped: Automation") {
		t.Fatalf("members %d note %q", len(got.Members), childInheritDecision(t, c2).Note)
	}
	// A parent that is not a member of its Automation: skipped.
	h3, _ := newControlHarness(t, 3)
	a3 := insertDraft(t, h3)
	parent3 := h3.prds[2]
	h3.m.store.WithLock(func() { parent3.AutomationID = a3.ID })
	c3 := unlinkedChild(t, h3.m)
	h3.m.inheritFromParent(c3, parent3, childExplicit{})
	if !strings.Contains(childInheritDecision(t, c3).Note, "is not a member") {
		t.Fatalf("note %q", childInheritDecision(t, c3).Note)
	}
	// A real child (with a parent link) is refused by the Automation store's
	// child rule; the refusal is recorded, never raised.
	h5, _ := newControlHarness(t, 3)
	a5 := insertDraft(t, h5)
	c5, err := h5.m.CreatePRDWithParent("child", "", "", "", EffortNormal, h5.prds[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	got5, _ := h5.m.automations.Get(a5.ID)
	if len(got5.Members) != 2 || !strings.Contains(childInheritDecision(t, c5).Note, "insert refused") {
		t.Fatalf("members %d note %q", len(got5.Members), childInheritDecision(t, c5).Note)
	}
	// explicit.Automation leaves placement to the caller.
	h4, _ := newControlHarness(t, 3)
	a4 := insertDraft(t, h4)
	c4, _ := h4.m.store.CreatePRD("c4", "", "", "", EffortNormal)
	h4.m.inheritFromParent(c4, h4.prds[0], childExplicit{Automation: true})
	got4, _ := h4.m.automations.Get(a4.ID)
	if len(got4.Members) != 2 {
		t.Fatalf("explicit automation placement ignored: %d members", len(got4.Members))
	}
}

func TestChildInheritRunsOnceAndGuards(t *testing.T) {
	m := freshDefectIntakeManager(t)
	parent := newParentPRD(t, m)
	c, _ := m.CreatePRDWithParent("c", "", "", "", EffortNormal, parent.ID)
	m.inheritFromParent(c, parent, childExplicit{})
	m.inheritFromParent(c, parent, childExplicit{})
	if countChildInherit(c) != 1 {
		t.Fatalf("inherit recorded %d times", countChildInherit(c))
	}
	m.inheritFromParent(nil, parent, childExplicit{})
	m.inheritFromParent(c, nil, childExplicit{})
	m.inheritFromParent(parent, parent, childExplicit{})
	if countChildInherit(parent) != 0 {
		t.Fatal("a record inherited from itself")
	}
	if _, err := m.CreatePRDWithParent("c", "", "", "", EffortNormal, "nope"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown parent: %v", err)
	}
	plain, err := m.CreatePRDWithParent("plain", "", "", "", EffortNormal, "")
	if err != nil || plain.ParentPRDID != "" || countChildInherit(plain) != 0 {
		t.Fatalf("empty parent id: %v %+v", err, plain)
	}
}

func TestChildInheritNothingToInheritIsRecorded(t *testing.T) {
	m := freshDefectIntakeManager(t)
	parent, _ := m.store.CreatePRD("bare parent", "", "", "", EffortNormal)
	c, _ := m.store.CreatePRD("c", "", "", "", EffortNormal)
	m.inheritFromParent(c, parent, childExplicit{})
	if n := childInheritDecision(t, c).Note; !strings.Contains(n, "nothing to inherit") {
		t.Fatalf("note %q", n)
	}
	if inheritTitle(parent) != "bare parent" {
		t.Fatalf("title %q", inheritTitle(parent))
	}
	parent.Title = strings.Repeat("é", 80)
	if got := []rune(inheritTitle(parent)); len(got) != 61 {
		t.Fatalf("title length %d", len(got))
	}
}

func TestChildInheritDefectIntakeWithParent(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := insertDraft(t, h)
	fi := &fakeDefectIntake{}
	h.m.SetDefectIntake(fi)
	h.m.SetDefectIntakeAutomation("intake")
	// A bug whose parent is known inherits when intake runs, once.
	bug, _ := h.m.store.CreatePRD(defectIntakeBugSpec, "", "", "", EffortNormal)
	bug.ParentPRDID = h.prds[2].ID
	h.m.intakeDefect(bug)
	if countChildInherit(bug) != 1 || len(fi.calls) != 1 {
		t.Fatalf("inherits %d offers %d", countChildInherit(bug), len(fi.calls))
	}
	// A bug that already joined its parent's Automation is not offered again.
	joined := unlinkedChild(t, h.m)
	h.m.store.WithLock(func() { joined.Spec = defectIntakeBugSpec })
	h.m.inheritFromParent(joined, h.prds[0], childExplicit{})
	h.m.store.WithLock(func() { joined.ParentPRDID = h.prds[0].ID })
	h.m.intakeDefect(joined)
	rec := recordedDefectIntake(t, joined)
	if rec.Outcome != DefectIntakeSkipped || !strings.Contains(rec.Message, a.ID) || len(fi.calls) != 1 {
		t.Fatalf("record %+v offers %d", rec, len(fi.calls))
	}
}

func TestChildInheritFixPRDInheritsFromScanned(t *testing.T) {
	m := freshDefectIntakeManager(t)
	parent := newParentPRD(t, m)
	m.scanResults.Store(parent.ID, &scan.Result{Findings: []scan.Finding{{Scanner: "rules", RuleID: "r1", Message: "a finding"}}})
	child, err := m.CreateFixPRD(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentPRDID != parent.ID || child.ProjectProfile != "proj-a" || child.Priority != 70 || countChildInherit(child) != 1 {
		t.Fatalf("fix child %+v", child)
	}
}
