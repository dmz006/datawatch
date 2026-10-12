// BL191 Q6 (v5.10.0) — per-story / per-task guardrail invocation tests.

package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeGuardrailAlwaysPass returns a green verdict for every invocation.
func fakeGuardrailAlwaysPass(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
	return GuardrailVerdict{Outcome: "pass", Summary: "ok " + req.Level + ":" + req.UnitID}, nil
}

func fakeGuardrailBlocking(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
	return GuardrailVerdict{
		Outcome:  "block",
		Severity: "high",
		Summary:  "blocked at " + req.Level + " level",
		Issues:   []string{"would cause an incident"},
	}, nil
}

func TestPerTaskGuardrails_NoConfigured_NoOp(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = nil
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailBlocking) // would block if invoked

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed (no guardrails should run)", prd.Status)
	}
	if len(prd.Story[0].Tasks[0].Verdicts) != 0 {
		t.Fatalf("task got verdicts despite empty PerTaskGuardrails")
	}
}

func TestPerTaskGuardrails_AllPass_AppendsVerdicts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"rules", "security"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailAlwaysPass)

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", prd.Status)
	}
	tk := prd.Story[0].Tasks[0]
	if len(tk.Verdicts) != 2 {
		t.Fatalf("task verdict count = %d, want 2", len(tk.Verdicts))
	}
	for i, name := range []string{"rules", "security"} {
		if tk.Verdicts[i].Guardrail != name {
			t.Fatalf("verdict %d guardrail = %q, want %q", i, tk.Verdicts[i].Guardrail, name)
		}
		if tk.Verdicts[i].Outcome != "pass" {
			t.Fatalf("verdict %d outcome = %q, want pass", i, tk.Verdicts[i].Outcome)
		}
	}
}

func TestPerTaskGuardrails_Block_HaltsPRD(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"security"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailBlocking)

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{
			{Title: "first", Spec: "do"},
			{Title: "second", Spec: "more"},
		},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	if prd.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked", prd.Status)
	}
	first := prd.Story[0].Tasks[0]
	if first.Status != TaskBlocked {
		t.Fatalf("first task = %s, want blocked", first.Status)
	}
	// Second task must NOT have run — block halts the walk.
	second := prd.Story[0].Tasks[1]
	if second.Status == TaskCompleted || second.Status == TaskInProgress {
		t.Fatalf("second task = %s; expected pending (walk should have halted)", second.Status)
	}
}

func TestPerStoryGuardrails_FireAfterAllTasksDone(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerStoryGuardrails = []string{"docs-diagrams-architecture"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailAlwaysPass)

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{
			{Title: "T1", Spec: "a"},
			{Title: "T2", Spec: "b"},
		},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", prd.Status)
	}
	if len(prd.Story[0].Verdicts) != 1 {
		t.Fatalf("story verdict count = %d, want 1", len(prd.Story[0].Verdicts))
	}
	if prd.Story[0].Verdicts[0].Guardrail != "docs-diagrams-architecture" {
		t.Fatalf("story verdict name = %q", prd.Story[0].Verdicts[0].Guardrail)
	}
}

func TestPerStoryGuardrails_Block_HaltsPRD(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerStoryGuardrails = []string{"release-readiness"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	m.SetGuardrail(fakeGuardrailBlocking)

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	_ = m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify)
	prd, _ = m.Store().GetPRD(prd.ID)

	if prd.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked", prd.Status)
	}
	if len(prd.Story[0].Verdicts) != 1 || prd.Story[0].Verdicts[0].Outcome != "block" {
		t.Fatalf("story verdict missing or wrong outcome: %+v", prd.Story[0].Verdicts)
	}
}

func TestPerTaskGuardrails_NoFnWired_SilentNoOp(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = []string{"rules"}
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	// Intentionally no SetGuardrail call.

	prd, _ := m.CreatePRD("spec", "/p", "claude", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed (guardrail fn nil → silent no-op)", prd.Status)
	}
}

// ── BL416 Phase 3 — verification script execution path ─────────────────

// writeVerifyScript writes body to <dir>/.datawatch/verify/<name> and
// returns the project-relative path stored in VerificationScript.
func writeVerifyScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	vdir := filepath.Join(dir, ".datawatch", "verify")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(".datawatch", "verify", name)
}

// newScriptGuardrailPRD returns a manager + PRD whose ProjectDir is a
// fresh temp dir, with the given task/story guardrail lists configured.
func newScriptGuardrailPRD(t *testing.T, taskGs, storyGs []string) (*Manager, *PRD, string) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.PerTaskGuardrails = taskGs
	cfg.PerStoryGuardrails = storyGs
	m, err := NewManager(t.TempDir(), cfg, fakeDecompose)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	prd, err := m.CreatePRD("spec", dir, "claude", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	return m, prd, dir
}

// fatalGuardrail fails the test if the LLM judgment path is ever invoked.
func fatalGuardrail(t *testing.T) GuardrailFn {
	return func(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		t.Fatalf("GuardrailFn called for %s/%s despite passing verification script", inv.Level, inv.Guardrail)
		return GuardrailVerdict{}, nil
	}
}

func TestPerTaskGuardrails_ScriptPass_NoLLMCall(t *testing.T) {
	m, prd, dir := newScriptGuardrailPRD(t, []string{"rules", "security"}, nil)
	m.SetGuardrail(fatalGuardrail(t))
	script := writeVerifyScript(t, dir, "t1.sh", "echo ok\nexit 0\n")
	tk := &Task{ID: "t1", Title: "T", Spec: "do", VerificationScript: script}

	blocked, err := m.runPerTaskGuardrails(context.Background(), prd, tk)
	if err != nil || blocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
	if len(tk.Verdicts) != 2 {
		t.Fatalf("verdict count = %d, want 2", len(tk.Verdicts))
	}
	for i, name := range []string{"rules", "security"} {
		v := tk.Verdicts[i]
		if v.Guardrail != name || v.Outcome != "pass" || v.Severity != "info" || len(v.Issues) != 0 {
			t.Fatalf("verdict %d = %+v", i, v)
		}
		if !strings.Contains(v.Summary, script) || !strings.Contains(v.Summary, "exit 0") {
			t.Fatalf("verdict %d summary = %q", i, v.Summary)
		}
		if v.VerdictAt.IsZero() {
			t.Fatalf("verdict %d VerdictAt not set", i)
		}
	}
}

func TestPerStoryGuardrails_ScriptPass_NoLLMCall(t *testing.T) {
	m, prd, dir := newScriptGuardrailPRD(t, nil, []string{"rules", "security"})
	m.SetGuardrail(fatalGuardrail(t))
	script := writeVerifyScript(t, dir, "s1.sh", "exit 0\n")
	st := &Story{ID: "s1", Title: "S", Description: "story", VerificationScript: script}

	blocked, err := m.runPerStoryGuardrails(context.Background(), prd, st)
	if err != nil || blocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
	if len(st.Verdicts) != 2 {
		t.Fatalf("verdict count = %d, want 2", len(st.Verdicts))
	}
	for i, v := range st.Verdicts {
		if v.Outcome != "pass" || !strings.Contains(v.Summary, script) {
			t.Fatalf("verdict %d = %+v", i, v)
		}
	}
}

func TestPerTaskGuardrails_ScriptPass_NoFnWired_StillRecordsVerdict(t *testing.T) {
	m, prd, dir := newScriptGuardrailPRD(t, []string{"rules"}, nil)
	script := writeVerifyScript(t, dir, "t1.sh", "exit 0\n")
	tk := &Task{ID: "t1", Title: "T", VerificationScript: script}

	if _, err := m.runPerTaskGuardrails(context.Background(), prd, tk); err != nil {
		t.Fatal(err)
	}
	if len(tk.Verdicts) != 1 || tk.Verdicts[0].Outcome != "pass" {
		t.Fatalf("verdicts = %+v, want one deterministic pass", tk.Verdicts)
	}
}

// recordingGuardrail captures every invocation and returns ret.
type recordingGuardrail struct {
	invs []GuardrailInvocation
	ret  GuardrailVerdict
}

func (r *recordingGuardrail) fn(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
	r.invs = append(r.invs, inv)
	return r.ret, nil
}

func TestPerTaskGuardrails_ScriptFail_FeedsJudgment(t *testing.T) {
	m, prd, dir := newScriptGuardrailPRD(t, []string{"rules", "security"}, nil)
	rec := &recordingGuardrail{ret: GuardrailVerdict{Outcome: "block", Severity: "high", Summary: "judge saw failure", Issues: []string{"x"}}}
	m.SetGuardrail(rec.fn)
	script := writeVerifyScript(t, dir, "t1.sh", "echo MARKER-TEST-FAILED-42\nexit 1\n")
	tk := &Task{ID: "t1", Title: "T", Spec: "do", VerificationScript: script}

	blocked, err := m.runPerTaskGuardrails(context.Background(), prd, tk)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("expected blocked=true from judgment verdict")
	}
	if len(rec.invs) != 2 {
		t.Fatalf("fn calls = %d, want 2", len(rec.invs))
	}
	for i, inv := range rec.invs {
		if !inv.ScriptRan || inv.ScriptExitCode != 1 || !strings.Contains(inv.ScriptOutput, "MARKER-TEST-FAILED-42") {
			t.Fatalf("inv %d script evidence = ran:%v exit:%d out:%q", i, inv.ScriptRan, inv.ScriptExitCode, inv.ScriptOutput)
		}
	}
	for i, name := range []string{"rules", "security"} {
		v := tk.Verdicts[i]
		if v.Guardrail != name || v.Outcome != "block" || v.Summary != "judge saw failure" {
			t.Fatalf("verdict %d = %+v, want fn's verdict", i, v)
		}
	}
}

func TestPerStoryGuardrails_ScriptFail_FeedsJudgment(t *testing.T) {
	m, prd, dir := newScriptGuardrailPRD(t, nil, []string{"rules"})
	rec := &recordingGuardrail{ret: GuardrailVerdict{Outcome: "warn", Summary: "judged"}}
	m.SetGuardrail(rec.fn)
	script := writeVerifyScript(t, dir, "s1.sh", "echo STORY-BROKE\nexit 1\n")
	st := &Story{ID: "s1", Title: "S", VerificationScript: script}

	if _, err := m.runPerStoryGuardrails(context.Background(), prd, st); err != nil {
		t.Fatal(err)
	}
	if len(rec.invs) != 1 || !rec.invs[0].ScriptRan || rec.invs[0].ScriptExitCode != 1 ||
		!strings.Contains(rec.invs[0].ScriptOutput, "STORY-BROKE") {
		t.Fatalf("invs = %+v", rec.invs)
	}
	if len(st.Verdicts) != 1 || st.Verdicts[0].Summary != "judged" {
		t.Fatalf("verdicts = %+v", st.Verdicts)
	}
}

func TestPerTaskGuardrails_ScriptUnrunnable_NeverAutoPass(t *testing.T) {
	m, prd, _ := newScriptGuardrailPRD(t, []string{"rules"}, nil)
	rec := &recordingGuardrail{ret: GuardrailVerdict{Outcome: "block", Summary: "judged"}}
	m.SetGuardrail(rec.fn)
	tk := &Task{ID: "t1", Title: "T", VerificationScript: "../escape.sh"}

	if _, err := m.runPerTaskGuardrails(context.Background(), prd, tk); err != nil {
		t.Fatal(err)
	}
	if len(rec.invs) != 1 {
		t.Fatalf("fn calls = %d, want 1 (unrunnable script must fall back to judgment)", len(rec.invs))
	}
	inv := rec.invs[0]
	if !inv.ScriptRan || inv.ScriptExitCode != -1 || !strings.HasPrefix(inv.ScriptOutput, "verification script could not be run: ") {
		t.Fatalf("inv evidence = ran:%v exit:%d out:%q", inv.ScriptRan, inv.ScriptExitCode, inv.ScriptOutput)
	}
}

func TestPerTaskGuardrails_NoScript_BackwardCompat(t *testing.T) {
	m, prd, _ := newScriptGuardrailPRD(t, []string{"rules", "security"}, nil)
	rec := &recordingGuardrail{ret: GuardrailVerdict{Outcome: "pass", Summary: "llm ok"}}
	m.SetGuardrail(rec.fn)
	tk := &Task{ID: "t1", Title: "T", Spec: "do the thing"}

	if _, err := m.runPerTaskGuardrails(context.Background(), prd, tk); err != nil {
		t.Fatal(err)
	}
	if len(rec.invs) != 2 {
		t.Fatalf("fn calls = %d, want 2", len(rec.invs))
	}
	for i, name := range []string{"rules", "security"} {
		inv := rec.invs[i]
		want := GuardrailInvocation{
			PRDID:      prd.ID,
			Level:      "task",
			UnitID:     "t1",
			UnitTitle:  "T",
			UnitSpec:   "do the thing",
			Guardrail:  name,
			ProjectDir: prd.ProjectDir,
			OwnerPeer:  prd.OwnerPeer,
			// Resolved routing always rides inv; with no overrides it is
			// the global verification config.
			GuardrailBackend: m.cfg.VerificationBackend,
			GuardrailModel:   m.cfg.VerificationModel,
		}
		if !reflect.DeepEqual(inv, want) {
			t.Fatalf("inv %d =\n %+v\nwant\n %+v", i, inv, want)
		}
		v := tk.Verdicts[i]
		if v.Guardrail != name || v.Outcome != "pass" || v.Summary != "llm ok" {
			t.Fatalf("verdict %d = %+v", i, v)
		}
	}
}

func TestPerTaskGuardrails_ScriptPass_ScanStillRuns(t *testing.T) {
	m, prd, dir := newScriptGuardrailPRD(t, []string{"secrets-scan", "rules"}, nil)
	m.SetGuardrail(fatalGuardrail(t))
	script := writeVerifyScript(t, dir, "t1.sh", "exit 0\n")
	// Planted credential the secrets scanner must still find.
	if err := os.WriteFile(filepath.Join(dir, "config.py"), []byte(`PASSWORD = "hunter2-supersecret"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tk := &Task{ID: "t1", Title: "T", VerificationScript: script}

	if _, err := m.runPerTaskGuardrails(context.Background(), prd, tk); err != nil {
		t.Fatal(err)
	}
	if len(tk.Verdicts) != 2 {
		t.Fatalf("verdict count = %d, want 2", len(tk.Verdicts))
	}
	scanV := tk.Verdicts[0]
	if scanV.Guardrail != "secrets-scan" || scanV.Outcome == "pass" || len(scanV.Issues) == 0 ||
		strings.Contains(scanV.Summary, "verification script") {
		t.Fatalf("scan verdict replaced or scan did not run: %+v", scanV)
	}
	if tk.Verdicts[1].Guardrail != "rules" || tk.Verdicts[1].Outcome != "pass" {
		t.Fatalf("rules verdict = %+v", tk.Verdicts[1])
	}
}

func TestGuardrailInvocation_BackendResolution(t *testing.T) {
	newM := func(t *testing.T, taskGs, storyGs []string) (*Manager, *PRD, *recordingGuardrail) {
		m, prd, _ := newScriptGuardrailPRD(t, taskGs, storyGs)
		m.cfg.VerificationBackend = "ollama"
		m.cfg.VerificationModel = "global-model"
		rec := &recordingGuardrail{ret: GuardrailVerdict{Outcome: "pass"}}
		m.SetGuardrail(rec.fn)
		return m, prd, rec
	}

	t.Run("task override wins", func(t *testing.T) {
		m, prd, rec := newM(t, []string{"rules"}, nil)
		tk := &Task{ID: "t1", GuardrailBackend: "openrouter", GuardrailModel: "x"}
		if _, err := m.runPerTaskGuardrails(context.Background(), prd, tk); err != nil {
			t.Fatal(err)
		}
		if len(rec.invs) != 1 || rec.invs[0].GuardrailBackend != "openrouter" || rec.invs[0].GuardrailModel != "x" {
			t.Fatalf("invs = %+v", rec.invs)
		}
	})

	t.Run("PRD level reaches story", func(t *testing.T) {
		m, prd, rec := newM(t, nil, []string{"rules"})
		prd.GuardrailBackend = "anthropic"
		prd.GuardrailModel = "prd-model"
		if _, err := m.runPerStoryGuardrails(context.Background(), prd, &Story{ID: "s1"}); err != nil {
			t.Fatal(err)
		}
		if len(rec.invs) != 1 || rec.invs[0].GuardrailBackend != "anthropic" || rec.invs[0].GuardrailModel != "prd-model" {
			t.Fatalf("invs = %+v", rec.invs)
		}
	})

	t.Run("no overrides uses global backend but not global model", func(t *testing.T) {
		// Fixed live 2026-10-12 (BL418 live validation): model used to
		// fall through to the flat ask-only global VerificationModel
		// here too, which a B124 session-routed guardrail call then
		// trusted as a genuine per-unit override and sent straight to
		// a real tool-using session — the exact bug already fixed for
		// verify in resolveVerifierBackendModel, unfixed here until now.
		m, prd, rec := newM(t, []string{"rules"}, []string{"rules"})
		if _, err := m.runPerTaskGuardrails(context.Background(), prd, &Task{ID: "t1"}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.runPerStoryGuardrails(context.Background(), prd, &Story{ID: "s1"}); err != nil {
			t.Fatal(err)
		}
		if len(rec.invs) != 2 {
			t.Fatalf("fn calls = %d, want 2", len(rec.invs))
		}
		for i, inv := range rec.invs {
			if inv.GuardrailBackend != "ollama" {
				t.Fatalf("inv %d backend = %q, want ollama (global fallback)", i, inv.GuardrailBackend)
			}
			if inv.GuardrailModel != "" {
				t.Fatalf("inv %d model = %q, want empty — global model must not leak into a session-routed guardrail call", i, inv.GuardrailModel)
			}
		}
	})
}

func TestPerTaskGuardrails_ScriptRunsOncePerUnit(t *testing.T) {
	m, prd, dir := newScriptGuardrailPRD(t, []string{"rules", "security", "release-readiness"}, nil)
	rec := &recordingGuardrail{ret: GuardrailVerdict{Outcome: "pass"}}
	m.SetGuardrail(rec.fn)
	// Fails so every guardrail goes through fn — the script must still run once.
	script := writeVerifyScript(t, dir, "t1.sh", "echo run >> counter.txt\nexit 1\n")
	tk := &Task{ID: "t1", VerificationScript: script}

	if _, err := m.runPerTaskGuardrails(context.Background(), prd, tk); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "counter.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "run\n"); n != 1 {
		t.Fatalf("script ran %d times, want exactly 1", n)
	}
	if len(rec.invs) != 3 {
		t.Fatalf("fn calls = %d, want 3", len(rec.invs))
	}
}
