package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// uncommittedRollupRig is a Manager with one Automaton on a work branch whose
// only task is already completed, so Run goes straight to the roll-up, a fake
// git that answers every call through gitCalls, a real question store and
// fakes for the alert and audit hooks.
type uncommittedRollupRig struct {
	m     *Manager
	prd   *PRD
	git   *fakeTaskGit
	store *QuestionStore

	mu       sync.Mutex
	gitCalls int
	acts     []string
	alerts   []string
}

// attachUncommittedRollup seeds the Automaton and wires the fakes into m, and
// leaves every other setting of its config as it was.
func attachUncommittedRollup(t *testing.T, m *Manager, title, policy string, dirty map[string]string) *uncommittedRollupRig {
	t.Helper()
	dir := t.TempDir()
	cfg := m.Config()
	cfg.Automation.UncommittedWorkPolicy = policy
	m.SetConfig(cfg)

	prd, err := m.CreatePRD("test spec", dir, "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "Story", Tasks: []Task{{Title: "Task 1", Spec: "do task 1"}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Title = title
	prd.Git = GitWorkflow{Branch: "automaton/" + prd.ID}
	prd.Status = PRDRunning
	prd.Story[0].Tasks[0].Status = TaskCompleted
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	r := &uncommittedRollupRig{m: m, prd: prd, git: newFakeTaskGit(dirty)}
	m.driftGit = func(ctx context.Context, d string, args ...string) (string, error) {
		r.mu.Lock()
		r.gitCalls++
		r.mu.Unlock()
		return r.git.run(ctx, d, args...)
	}
	s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	m.SetDecisionGate(s, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{} })
	r.store = s
	m.SetAuditFn(func(action string, _ map[string]any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.acts = append(r.acts, action)
	})
	m.SetAlertSink(func(level, alertTitle, body string) string {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.alerts = append(r.alerts, level+"|"+alertTitle+"|"+body)
		return "alert-1"
	})
	return r
}

func newUncommittedRollupRig(t *testing.T, policy string, dirty map[string]string) *uncommittedRollupRig {
	t.Helper()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	return attachUncommittedRollup(t, m, "Rollup Automaton", policy, dirty)
}

// run drives the Automaton once and reloads it.
func (r *uncommittedRollupRig) run(t *testing.T) *PRD {
	t.Helper()
	if err := r.m.Run(context.Background(), r.prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := r.m.Store().GetPRD(r.prd.ID)
	if !ok {
		t.Fatal("Automaton vanished")
	}
	r.prd = got
	return got
}

func (r *uncommittedRollupRig) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gitCalls
}

func (r *uncommittedRollupRig) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.acts...)
}

func (r *uncommittedRollupRig) decisions(kind string) []Decision {
	var out []Decision
	for _, d := range r.prd.Decisions {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func TestUncommittedRollupDirtyWorktreeBlocksCompletion(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, dirtyFiles("internal/a.go", "docs/b.md"))

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	if got.BlockedReason == "" {
		t.Error("blocked reason is empty")
	}
	if got.UncommittedWork == nil || got.UncommittedWork.Kind != UncommittedHoldKindWorktree {
		t.Fatalf("hold = %+v; want a worktree hold", got.UncommittedWork)
	}
	if calls := r.git.commitCalls(); len(calls) != 0 {
		t.Errorf("the roll-up committed: %+v", calls)
	}
	if len(r.store.List(QuestionFilter{})) != 1 {
		t.Errorf("questions = %d; want 1", len(r.store.List(QuestionFilter{})))
	}

	// The operator cleans the worktree and the hold is cleared; running the
	// Automaton again must pass through the check again and complete.
	before := r.git.statusCount()
	r.git.mu.Lock()
	r.git.dirty = map[string]string{}
	r.git.mu.Unlock()
	r.m.store.WithLock(func() {
		got.UncommittedWork = nil
		got.Status = PRDRunning
	})
	if err := r.m.Store().SavePRD(got); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got = r.run(t)
	if r.git.statusCount() <= before {
		t.Errorf("status calls %d -> %d; the second run skipped the check", before, r.git.statusCount())
	}
	if got.Status != PRDCompleted {
		t.Fatalf("status after the clean re-run = %s; want completed", got.Status)
	}
}

// In the daemon the question store's change hook saves the Automaton when the
// worktree question is raised. That save must not make the roll-up read a
// concurrent writer: the Automaton has to land in blocked, not stay running.
func TestUncommittedRollupHoldWithQuestionHookBlocks(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, dirtyFiles("stray.txt"))
	r.store.SetChangeHook(func(c QuestionChange) { r.m.RecordQuestionChange(c) })

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked (decisions: %v)", got.Status, decisionKinds(got))
	}
	for _, d := range got.Decisions {
		if d.Kind == "rollup_aborted_stale" {
			t.Fatalf("the roll-up aborted as stale: %+v", d)
		}
	}
	if got.UncommittedWork == nil || got.UncommittedWork.QuestionID == "" {
		t.Fatalf("hold = %+v; want a worktree hold with its question", got.UncommittedWork)
	}
}

func TestUncommittedRollupCleanWorktreeCompletes(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, nil)

	got := r.run(t)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s; want completed", got.Status)
	}
	if r.git.statusCount() == 0 {
		t.Error("the worktree was never read")
	}
	if got.UncommittedWork != nil || len(r.store.List(QuestionFilter{})) != 0 {
		t.Errorf("a clean worktree raised a hold or question: hold=%+v", got.UncommittedWork)
	}
}

func TestUncommittedRollupWarnCompletes(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyWarn, dirtyFiles("internal/a.go"))

	got := r.run(t)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s; want completed", got.Status)
	}
	if n := len(r.decisions(uncommittedWarnedDecisionKind)); n != 1 {
		t.Errorf("warned decisions = %d; want 1", n)
	}
	if got.UncommittedWork != nil || len(r.store.List(QuestionFilter{})) != 0 {
		t.Errorf("warn raised a hold or question: hold=%+v", got.UncommittedWork)
	}
	if acts := r.actions(); len(acts) != 1 || acts[0] != uncommittedWarnedAuditAction {
		t.Errorf("audit actions = %v; want [%s]", acts, uncommittedWarnedAuditAction)
	}
	if calls := r.git.commitCalls(); len(calls) != 0 {
		t.Errorf("the roll-up committed: %+v", calls)
	}
}

func TestUncommittedRollupOffSkips(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyOff, dirtyFiles("internal/a.go"))

	got := r.run(t)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s; want completed", got.Status)
	}
	if n := r.calls(); n != 0 {
		t.Errorf("git calls = %d; want 0 with the policy off", n)
	}
	if got.UncommittedWork != nil || len(r.decisions(uncommittedWarnedDecisionKind)) != 0 {
		t.Errorf("policy off recorded something: hold=%+v", got.UncommittedWork)
	}
}

func TestUncommittedRollupListsFiles(t *testing.T) {
	paths := []string{"cmd/tool/main.go", "docs/notes.md", "internal/a.go"}
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, dirtyFiles(paths...))

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	held := r.decisions(uncommittedHeldDecisionKind)
	if len(held) != 1 {
		t.Fatalf("held decisions = %d; want 1", len(held))
	}
	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %d; want 1", len(qs))
	}
	for _, p := range paths {
		if !strings.Contains(got.BlockedReason, p) {
			t.Errorf("blocked reason lacks %q: %s", p, got.BlockedReason)
		}
		if got.UncommittedWork == nil || !containsString(got.UncommittedWork.Files, p) {
			t.Errorf("hold record lacks %q: %+v", p, got.UncommittedWork)
		}
		if !strings.Contains(held[0].Note, p) {
			t.Errorf("decision lacks %q: %s", p, held[0].Note)
		}
		if !strings.Contains(qs[0].Text, p) {
			t.Errorf("question lacks %q:\n%s", p, qs[0].Text)
		}
	}
	for _, text := range []string{got.BlockedReason, held[0].Note, qs[0].Text} {
		if strings.Contains(text, "PRD") {
			t.Errorf("operator text says PRD: %s", text)
		}
	}
	if qs[0].PRDID != got.ID || qs[0].TaskID != "" {
		t.Errorf("question scope = prd %q task %q; want the Automaton alone", qs[0].PRDID, qs[0].TaskID)
	}
}

func TestUncommittedRollupBlockedAutomatonNotMergeReady(t *testing.T) {
	th, _ := newMergePendingHarness(t, config.MergePendingModeOn, 10)
	// MergeStatus reads through the Manager's coordinator, which the harness
	// does not start.
	th.h.m.mu.Lock()
	th.h.m.coordinator = th.h.c
	th.h.m.mu.Unlock()
	// alpha is completed on its branch and shows up; the dirty Automaton
	// must not.
	r := attachUncommittedRollup(t, th.h.m, "Dirty Automaton", config.UncommittedWorkPolicyHold, dirtyFiles("internal/a.go"))

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	th.h.c.triagePass()

	ms := th.h.m.MergeStatus()
	var listed []string
	for _, e := range ms.Automata {
		listed = append(listed, e.AutomatonID)
		if e.AutomatonID == got.ID {
			t.Errorf("blocked Automaton %s is listed as merge ready: %+v", got.ID, e)
		}
	}
	if len(listed) != 1 {
		t.Errorf("merge status lists %v; want only the completed control Automaton", listed)
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// addDirty marks more files as modified in the fake worktree.
func (r *uncommittedRollupRig) addDirty(rels ...string) {
	r.git.mu.Lock()
	defer r.git.mu.Unlock()
	if r.git.dirty == nil {
		r.git.dirty = map[string]string{}
	}
	for _, p := range rels {
		r.git.dirty[p] = " M"
	}
}

// writeUneditedArtefact writes the daemon's gate report for the rig's
// Automaton into its project directory, exactly as the daemon's own write
// does (marker, body, fresh hash sidecar), and returns its tracked path
// relative to the project root.
func (r *uncommittedRollupRig) writeUneditedArtefact(t *testing.T) string {
	t.Helper()
	rel := "docs/plans/reports/" + r.prd.ID + "-gate.md"
	res, err := genartifacts.Write(genartifacts.GateReport, "All checks passed for this run.", rel, genartifacts.Options{Root: r.prd.ProjectDir})
	if err != nil {
		t.Fatalf("write artefact: %v", err)
	}
	if res.Status != genartifacts.Written {
		t.Fatalf("write artefact = %s (%s); want written", res.Status, res.Reason)
	}
	return rel
}

// takeOverArtefact edits the generated file the way a person does: the marker
// is kept, the edited line is added right after it and a note is appended.
func (r *uncommittedRollupRig) takeOverArtefact(t *testing.T, rel string) {
	t.Helper()
	full := filepath.Join(r.prd.ProjectDir, filepath.FromSlash(rel))
	content := genartifacts.Marker + "\n" + genartifacts.EditedMarker + "\n" +
		"All checks passed for this run.\nOperator: verified by hand."
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("take over artefact: %v", err)
	}
}

// changeArtefactBody rewrites the generated file's body while keeping the
// marker and the now-stale hash sidecar: content the daemon never wrote.
func (r *uncommittedRollupRig) changeArtefactBody(t *testing.T, rel string) {
	t.Helper()
	full := filepath.Join(r.prd.ProjectDir, filepath.FromSlash(rel))
	content := genartifacts.Marker + "\nChanged body the daemon never wrote."
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("change artefact body: %v", err)
	}
}

// The files the daemon itself generated at finish time are exactly what the
// finishing Automaton left dirty in its worktree; holding on them would park
// every one of them.
func TestUncommittedRollupUneditedArtefactDoesNotHold(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, nil)
	rel := r.writeUneditedArtefact(t)
	r.addDirty(rel, genartifacts.SidecarRelPath(rel))

	got := r.run(t)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s; want completed (hold: %+v)", got.Status, got.UncommittedWork)
	}
	if got.UncommittedWork != nil || len(r.store.List(QuestionFilter{})) != 0 {
		t.Errorf("the daemon's own artefact raised a hold or question: hold=%+v", got.UncommittedWork)
	}
	if r.git.statusCount() == 0 {
		t.Error("the worktree was never read")
	}
}

func TestUncommittedRollupWarnUneditedArtefactStaysQuiet(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyWarn, nil)
	rel := r.writeUneditedArtefact(t)
	r.addDirty(rel, genartifacts.SidecarRelPath(rel))

	got := r.run(t)
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s; want completed", got.Status)
	}
	if len(r.decisions(uncommittedWarnedDecisionKind)) != 0 || len(r.actions()) != 0 || len(r.store.List(QuestionFilter{})) != 0 {
		t.Errorf("the daemon's own artefact was still recorded: decisions=%d actions=%v",
			len(r.decisions(uncommittedWarnedDecisionKind)), r.actions())
	}
}

// A person edit (or any other change) to a generated file is the operator's
// work and counts like any uncommitted file again.
func TestUncommittedRollupEditedArtefactHolds(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, nil)
	rel := r.writeUneditedArtefact(t)
	r.takeOverArtefact(t, rel)
	r.addDirty(rel)

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	if got.UncommittedWork == nil || !containsString(got.UncommittedWork.Files, rel) {
		t.Fatalf("hold record lacks %q: %+v", rel, got.UncommittedWork)
	}
}

// A changed body the daemon never wrote does not carry a person edit with it:
// it is still uncommitted work.
func TestUncommittedRollupChangedBodyHolds(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, nil)
	rel := r.writeUneditedArtefact(t)
	r.changeArtefactBody(t, rel)
	r.addDirty(rel)

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	if got.UncommittedWork == nil || !containsString(got.UncommittedWork.Files, rel) {
		t.Fatalf("hold record lacks %q: %+v", rel, got.UncommittedWork)
	}
}

// With both kinds of files dirty, only the operator's stay in.
func TestUncommittedRollupMixedListsOnlyOperatorFiles(t *testing.T) {
	r := newUncommittedRollupRig(t, config.UncommittedWorkPolicyHold, dirtyFiles("internal/a.go"))
	rel := r.writeUneditedArtefact(t)
	r.addDirty(rel, genartifacts.SidecarRelPath(rel))

	got := r.run(t)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s; want blocked", got.Status)
	}
	if got.UncommittedWork == nil || !containsString(got.UncommittedWork.Files, "internal/a.go") {
		t.Fatalf("hold record lacks internal/a.go: %+v", got.UncommittedWork)
	}
	if got.UncommittedWork != nil && (containsString(got.UncommittedWork.Files, rel) ||
		containsString(got.UncommittedWork.Files, genartifacts.SidecarRelPath(rel))) {
		t.Errorf("hold record lists the daemon's own artefact: %+v", got.UncommittedWork)
	}

	held := r.decisions(uncommittedHeldDecisionKind)
	qs := r.store.List(QuestionFilter{})
	if len(held) != 1 || len(qs) != 1 {
		t.Fatalf("held=%d questions=%d; want one each", len(held), len(qs))
	}
	for _, text := range []string{got.BlockedReason, held[0].Note, qs[0].Text} {
		if !strings.Contains(text, "internal/a.go") {
			t.Errorf("operator text lacks internal/a.go: %s", text)
		}
		if strings.Contains(text, "-gate.md") {
			t.Errorf("operator text lists the daemon's own artefact: %s", text)
		}
	}
}

func TestExcludeUneditedGeneratedArtefacts(t *testing.T) {
	dir := t.TempDir()
	rel := "docs/plans/reports/abc123-gate.md"
	res, err := genartifacts.Write(genartifacts.GateReport, "body.", rel, genartifacts.Options{Root: dir})
	if err != nil || res.Status != genartifacts.Written {
		t.Fatalf("write artefact: %v %s", err, res)
	}
	side := genartifacts.SidecarRelPath(rel)
	files := []UncommittedFile{
		{Path: "internal/a.go", Status: " M"},
		{Path: rel, Status: " M"},
		{Path: side, Status: " M"},
		{Path: "docs/notes.md", Status: " M"},
	}
	got := excludeUneditedGeneratedArtefacts(dir, files)
	if len(got) != 2 || got[0].Path != "internal/a.go" || got[1].Path != "docs/notes.md" {
		t.Fatalf("kept = %+v; want the operator's two files in order", got)
	}
	// An empty root matches nothing: every file stays.
	if g2 := excludeUneditedGeneratedArtefacts("", files); len(g2) != len(files) {
		t.Errorf("empty root kept %d of %d; want all", len(g2), len(files))
	}
}
