// Smoke-verification overrides (feature story: set the configured
// smoke-task verification backend/model on decompose while the PRD is
// still needs_review, honoring the never-clobber rule for explicit
// per-task overrides).
//
//   - ApplySmokeVerificationOverrides targets smoke/final-gate tasks only
//   - empty config => zero diff (default-off proof)
//   - explicit operator-set slots are never clobbered
//   - overrides survive an operator edit of a needs_review PRD
//   - Decompose wires the call in at the needs_review transition
//   - the kind-tagging regex boundary rules
//   - ParsePlanning accepts (and tolerates the absence of) a task "kind"

package autonomous

import (
	"fmt"
	"reflect"
	"testing"
)

func newSmokeManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// smokeSeedPRD creates a PRD in m with one story holding the given tasks
// (IDs backfilled when empty) and saves it in the given status.
func smokeSeedPRD(t *testing.T, m *Manager, st PRDStatus, tasks ...Task) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("smoke spec", t.TempDir(), "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	for i := range tasks {
		if tasks[i].ID == "" {
			tasks[i].ID = fmt.Sprintf("t%d", i+1)
		}
	}
	prd.Story = []Story{{ID: "s1", Title: "story one", Tasks: tasks}}
	prd.Status = st
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd
}

func smokeTask(t *testing.T, m *Manager, prdID, taskID string) *Task {
	t.Helper()
	stored, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatal("PRD not found in store")
	}
	for si := range stored.Story {
		for ti := range stored.Story[si].Tasks {
			if stored.Story[si].Tasks[ti].ID == taskID {
				return &stored.Story[si].Tasks[ti]
			}
		}
	}
	t.Fatalf("task %q not found in prd %q", taskID, prdID)
	return nil
}

func TestSmokeVerificationOverrides_TargetsGetOverride(t *testing.T) {
	m := newSmokeManager(t)
	prd := smokeSeedPRD(t, m, PRDNeedsReview,
		Task{Kind: TaskKindSmoke, Title: "Run the live API checks", Spec: "smoke it"},
		Task{Title: "final gate verification", Spec: "check the whole feature"},
		Task{Title: "Write documentation", Spec: "docs"},
	)

	ApplySmokeVerificationOverrides(prd, "claude-code", "smoke-model")

	// The two targets got the task-level override...
	smokeTaskWant := func(id string) {
		tk := smokeTask(t, m, prd.ID, id)
		if tk.VerificationBackend != "claude-code" || tk.VerificationModel != "smoke-model" {
			t.Errorf("task %s: verification = %q/%q, want claude-code/smoke-model", id, tk.VerificationBackend, tk.VerificationModel)
		}
	}
	smokeTaskWant("t1")
	smokeTaskWant("t2")

	// ...and ResolveVerificationLLM reports a task-level source for both,
	// while the ordinary task's resolution falls through the unchanged
	// chain (global level, nothing leaked onto it).
	cfg := DefaultConfig()
	plain := smokeTask(t, m, prd.ID, "t3")
	if got := ResolveVerificationLLM(prd, plain, nil, cfg); got.Source != VerifierSourceGlobal || got.Backend != "" || got.Model != "" {
		t.Errorf("ordinary task: resolved = %+v, want untouched global chain", got)
	}
	for _, id := range []string{"t1", "t2"} {
		tk := smokeTask(t, m, prd.ID, id)
		got := ResolveVerificationLLM(prd, tk, nil, cfg)
		if got.Source != VerifierSourceTask {
			t.Errorf("task %s: source = %q, want task", id, got.Source)
		}
		if got.Backend != "claude-code" || got.Model != "smoke-model" {
			t.Errorf("task %s: resolved %q/%q, want claude-code/smoke-model", id, got.Backend, got.Model)
		}
	}
}

func TestSmokeVerificationOverrides_EmptyConfig_ZeroDiff(t *testing.T) {
	m := newSmokeManager(t)
	prd := smokeSeedPRD(t, m, PRDNeedsReview,
		Task{Kind: TaskKindSmoke, Title: "smoke run", Spec: "a", VerificationBackend: "explicit-b", VerificationModel: "explicit-m", VerificationEffort: "high"},
		Task{Title: "final-gate", Spec: "b"},
		Task{Title: "ordinary", Spec: "c"},
	)
	before := make([]Task, len(prd.Story[0].Tasks))
	copy(before, prd.Story[0].Tasks)

	ApplySmokeVerificationOverrides(prd, "", "")
	ApplySmokeVerificationOverrides(nil, "", "") // nil PRD is a no-op too

	for i := range before {
		got := prd.Story[0].Tasks[i]
		if !reflect.DeepEqual(before[i], got) {
			t.Errorf("task %s: empty-config apply changed the task: before %+v, after %+v", before[i].ID, before[i], got)
		}
	}
}

func TestSmokeVerificationOverrides_NoClobberAndPartialApply(t *testing.T) {
	m := newSmokeManager(t)
	prd := smokeSeedPRD(t, m, PRDNeedsReview,
		// Fully explicit: both slots operator-set — must survive intact.
		Task{Kind: TaskKindSmoke, Title: "smoke run", Spec: "a",
			VerificationBackend: "ollama", VerificationModel: "my-model"},
		// Explicit model only: a backend-only apply must keep it.
		Task{Title: "final gate verification", Spec: "b",
			VerificationModel: "picked-model"},
	)

	// Backend set, model empty: model slot left to the explicit/chain
	// value; backend fills only where empty.
	ApplySmokeVerificationOverrides(prd, "claude-code", "")

	full := smokeTask(t, m, prd.ID, "t1")
	if full.VerificationBackend != "ollama" || full.VerificationModel != "my-model" {
		t.Errorf("explicit task clobbered: got %q/%q, want ollama/my-model", full.VerificationBackend, full.VerificationModel)
	}
	gate := smokeTask(t, m, prd.ID, "t2")
	if gate.VerificationBackend != "claude-code" {
		t.Errorf("gate task backend = %q, want claude-code", gate.VerificationBackend)
	}
	if gate.VerificationModel != "picked-model" {
		t.Errorf("gate task model = %q, want existing explicit picked-model", gate.VerificationModel)
	}
	got := ResolveVerificationLLM(prd, gate, nil, DefaultConfig())
	if got.Source != VerifierSourceTask || got.Backend != "claude-code" || got.Model != "picked-model" {
		t.Errorf("gate task resolved = %+v, want task claude-code/picked-model", got)
	}
}

func TestSmokeVerificationOverrides_SurvivesNeedsReviewEdit(t *testing.T) {
	m := newSmokeManager(t)
	prd := smokeSeedPRD(t, m, PRDNeedsReview,
		Task{Kind: TaskKindSmoke, Title: "smoke run", Spec: "old spec"},
		Task{Title: "final gate verification", Spec: "gate spec"},
	)
	ApplySmokeVerificationOverrides(prd, "claude-code", "smoke-model")
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	// Operator edits a task spec on the needs_review PRD (the live edit
	// path) — the overrides must still be present afterwards.
	if _, err := m.EditTaskSpec(prd.ID, "t2", "updated gate spec", "op"); err != nil {
		t.Fatalf("EditTaskSpec: %v", err)
	}
	for _, id := range []string{"t1", "t2"} {
		tk := smokeTask(t, m, prd.ID, id)
		if tk.VerificationBackend != "claude-code" || tk.VerificationModel != "smoke-model" {
			t.Errorf("task %s: override lost after edit: %q/%q", id, tk.VerificationBackend, tk.VerificationModel)
		}
	}
	if g := smokeTask(t, m, prd.ID, "t2").Spec; g != "updated gate spec" {
		t.Errorf("edit lost: spec = %q", g)
	}
}

func TestSmokeVerificationTargetRegex(t *testing.T) {
	cases := []struct {
		title string
		kind  string
		want  bool
	}{
		{title: "Smoke test", want: true},
		{title: "FINAL GATE", want: true},
		{title: "final-gate", want: true},
		{title: "final gate verification", want: true},
		{title: "smokescreen", want: false},
		{title: "ordinary work", want: false},
		{kind: TaskKindSmoke, want: true},
		{kind: TaskKindFinalGate, want: true},
		{kind: "something-else", want: false}, // unknown kinds are tolerated, ignored
	}
	for _, c := range cases {
		got := isSmokeVerificationTarget(Task{Title: c.title, Kind: c.kind})
		if got != c.want {
			t.Errorf("isSmokeVerificationTarget(%q, kind=%q) = %v, want %v", c.title, c.kind, got, c.want)
		}
	}
}

func TestSmokeVerification_ParsePlanning_ToleratesKind(t *testing.T) {
	withKind := `{"title":"T","stories":[{"title":"S","tasks":[
		{"title":"a","spec":"do a","kind":"smoke"},
		{"title":"b","spec":"do b","kind":"weird-unknown"},
		{"title":"c","spec":"do c"}]}]}`
	_, stories, err := ParsePlanning(withKind)
	if err != nil {
		t.Fatalf("ParsePlanning with kind: %v", err)
	}
	if got := stories[0].Tasks[0].Kind; got != "smoke" {
		t.Errorf("task a kind = %q, want smoke", got)
	}
	if got := stories[0].Tasks[2].Kind; got != "" {
		t.Errorf("task c kind = %q, want empty (absent)", got)
	}

	plain := `{"title":"T","stories":[{"title":"S","tasks":[{"title":"a","spec":"do a"}]}]}`
	if _, s2, err := ParsePlanning(plain); err != nil {
		t.Fatalf("ParsePlanning without kind: %v", err)
	} else if got := s2[0].Tasks[0].Kind; got != "" {
		t.Errorf("plain plan kind = %q, want empty", got)
	}
}

func TestSmokeVerification_DecomposeWiring(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SmokeVerificationBackend = "claude-code"
	cfg.SmokeVerificationModel = "smoke-model"
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"T","stories":[{"title":"S","tasks":[
			{"title":"Run smoke checks","spec":"do smoke","kind":"smoke"},
			{"title":"final gate verification","spec":"verify all"},
			{"title":"ordinary task","spec":"misc"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if updated.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", updated.Status)
	}
	for i := range updated.Story[0].Tasks {
		tk := &updated.Story[0].Tasks[i]
		want := tk.Kind == TaskKindSmoke || tk.Title == "final gate verification"
		if want && (tk.VerificationBackend != "claude-code" || tk.VerificationModel != "smoke-model") {
			t.Errorf("task %q: overrides not applied on decompose: %q/%q", tk.Title, tk.VerificationBackend, tk.VerificationModel)
		}
		if !want && (tk.VerificationBackend != "" || tk.VerificationModel != "") {
			t.Errorf("task %q: overrides leaked onto an ordinary task: %q/%q", tk.Title, tk.VerificationBackend, tk.VerificationModel)
		}
	}
}

func TestSmokeVerification_DecomposeWiring_DefaultOff(t *testing.T) {
	cfg := DefaultConfig() // no smoke fields — the default-off path
	fn := func(req DecomposeRequest) (string, error) {
		return `{"title":"T","stories":[{"title":"S","tasks":[
			{"title":"Run smoke checks","spec":"do smoke","kind":"smoke"}]}]}`, nil
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	updated, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	for i := range updated.Story[0].Tasks {
		tk := &updated.Story[0].Tasks[i]
		if tk.VerificationBackend != "" || tk.VerificationModel != "" {
			t.Errorf("default-off: task %q got %q/%q, want untouched", tk.Title, tk.VerificationBackend, tk.VerificationModel)
		}
	}
}
