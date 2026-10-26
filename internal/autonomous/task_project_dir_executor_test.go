package autonomous

// BL447 — executor-level coverage for the per-task ProjectDir override.
// For each mechanism: no override gives exactly the prior PRD.ProjectDir
// value, and an override scopes to the task dir.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// tpdSetup builds an approved PRD in prdDir with one story/task; the task
// carries taskDir as its override when non-empty.
func tpdSetup(t *testing.T, prdDir, taskDir string) (*Manager, *PRD) {
	t.Helper()
	cfg := DefaultConfig()
	// v9.10.18 — the guardrail judge (fake fn) stands in for a real
	// model; the resolver refuses to launch it with none configured.
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", prdDir, "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do it", ProjectDir: taskDir}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd
}

// fakeSpawnCapture mirrors cmd/datawatch's spawn closure: pre-task
// capture runs in req.ProjectDir and nowhere else.
func fakeSpawnCapture(reqs *[]SpawnRequest, mu *sync.Mutex) SpawnFn {
	return func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		mu.Lock()
		*reqs = append(*reqs, req)
		mu.Unlock()
		res := SpawnResult{SessionID: "sess-" + req.TaskID}
		if out, err := exec.CommandContext(ctx, "git", "-C", req.ProjectDir, "rev-parse", "HEAD").Output(); err == nil {
			res.PreTaskSHA = strings.TrimSpace(string(out))
		}
		res.PreTaskDirtyFiles = CapturePreTaskDirtyFiles(ctx, req.ProjectDir, nil)
		return res, nil
	}
}

func okVerify(context.Context, *PRD, *Task) (VerificationResult, error) {
	return VerificationResult{OK: true, Summary: "ok"}, nil
}

func tpdRepoWithBase(t *testing.T) string {
	t.Helper()
	dir := driftTestRepo(t)
	driftWrite(t, dir, "base.txt", "1\n")
	driftGit(t, dir, "add", "base.txt")
	driftGit(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func TestBL447_Spawn_CwdAndScopeBlock(t *testing.T) {
	prdDir := tpdRepoWithBase(t)
	taskDir := tpdRepoWithBase(t)
	for _, tc := range []struct {
		name, override, want string
	}{
		{"no override", "", prdDir},
		{"override", taskDir, taskDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, prd := tpdSetup(t, prdDir, tc.override)
			var reqs []SpawnRequest
			var mu sync.Mutex
			if err := m.Run(context.Background(), prd.ID, fakeSpawnCapture(&reqs, &mu), okVerify); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(reqs) != 1 {
				t.Fatalf("spawns = %d, want 1", len(reqs))
			}
			if reqs[0].ProjectDir != tc.want {
				t.Fatalf("SpawnRequest.ProjectDir = %q, want %q", reqs[0].ProjectDir, tc.want)
			}
			task := prd.Story[0].Tasks[0]
			wantBlock := TaskScopeBlock(prd, &task)
			if tc.override == "" && wantBlock != ScopeBlock(prd) {
				t.Fatal("regression: TaskScopeBlock must equal ScopeBlock without an override")
			}
			if !strings.HasPrefix(reqs[0].Spec, wantBlock+"\n") {
				t.Fatalf("spec does not start with the task scope block:\n%s", reqs[0].Spec)
			}
			if tc.override != "" && !strings.Contains(reqs[0].Spec, taskDir) {
				t.Fatalf("override scope block does not name the task dir:\n%s", reqs[0].Spec)
			}
		})
	}
}

func TestBL447_PreTaskSHAAnchor(t *testing.T) {
	prdDir := tpdRepoWithBase(t)
	taskDir := tpdRepoWithBase(t)
	// Diverge the repos so their HEADs differ.
	driftWrite(t, taskDir, "other.txt", "x\n")
	driftGit(t, taskDir, "add", "other.txt")
	driftGit(t, taskDir, "commit", "-q", "-m", "other")
	prdHead := driftGit(t, prdDir, "rev-parse", "HEAD")
	taskHead := driftGit(t, taskDir, "rev-parse", "HEAD")
	for _, tc := range []struct {
		name, override, want string
	}{
		{"no override", "", prdHead},
		{"override", taskDir, taskHead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, prd := tpdSetup(t, prdDir, tc.override)
			var reqs []SpawnRequest
			var mu sync.Mutex
			if err := m.Run(context.Background(), prd.ID, fakeSpawnCapture(&reqs, &mu), okVerify); err != nil {
				t.Fatalf("Run: %v", err)
			}
			got, _ := m.Store().GetPRD(prd.ID)
			if sha := got.Story[0].Tasks[0].PreTaskSHA; sha != tc.want {
				t.Fatalf("PreTaskSHA = %q, want %q", sha, tc.want)
			}
		})
	}
}

func TestBL447_VerifierScriptEvidenceDir(t *testing.T) {
	prdDir := t.TempDir()
	taskDir := t.TempDir()
	for dir, tag := range map[string]string{prdDir: "PRD-DIR", taskDir: "TASK-DIR"} {
		if err := os.WriteFile(filepath.Join(dir, "verify.sh"), []byte("#!/bin/sh\necho "+tag+"\nexit 1\n"), 0o755); err != nil { // #nosec G306 -- test script must be executable
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, override, wantDir, wantOut string
	}{
		{"no override", "", prdDir, "PRD-DIR"},
		{"override", taskDir, taskDir, "TASK-DIR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, prd := tpdSetup(t, prdDir, tc.override)
			prd.PerTaskGuardrails = []string{"rules"}
			prd.PerStoryGuardrails = []string{"rules"}
			var invs []GuardrailInvocation
			m.SetGuardrail(func(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
				invs = append(invs, inv)
				return GuardrailVerdict{Guardrail: inv.Guardrail, Outcome: "pass"}, nil
			})
			task := &prd.Story[0].Tasks[0]
			task.VerificationScript = "verify.sh"
			if _, _, err := m.runPerTaskGuardrails(context.Background(), prd, task); err != nil {
				t.Fatal(err)
			}
			// The script exits 1 so the guardrail fn is consulted with the
			// evidence (a passing script short-circuits it).
			// Story-level: every task shares the same resolved dir, so the
			// story evidence follows it.
			story := &prd.Story[0]
			story.VerificationScript = "verify.sh"
			if _, _, err := m.runPerStoryGuardrails(context.Background(), prd, story); err != nil {
				t.Fatal(err)
			}
			if len(invs) != 2 {
				t.Fatalf("guardrail calls = %d, want 2", len(invs))
			}
			for _, inv := range invs {
				if inv.ProjectDir != tc.wantDir {
					t.Fatalf("%s invocation ProjectDir = %q, want %q", inv.Level, inv.ProjectDir, tc.wantDir)
				}
				if !strings.Contains(inv.ScriptOutput, tc.wantOut) {
					t.Fatalf("%s script evidence = %q, want %q", inv.Level, inv.ScriptOutput, tc.wantOut)
				}
			}
		})
	}
}

func TestBL447_StoryEvidenceDir_MixedTasksStayOnPRD(t *testing.T) {
	prd := &PRD{ProjectDir: "/p"}
	s := &Story{Tasks: []Task{{ProjectDir: "/a"}, {ProjectDir: "/a"}}}
	if got := storyEvidenceDir(prd, s); got != "/a" {
		t.Fatalf("uniform override: got %q, want /a", got)
	}
	s.Tasks[1].ProjectDir = ""
	if got := storyEvidenceDir(prd, s); got != "/p" {
		t.Fatalf("mixed: got %q, want /p", got)
	}
	if got := storyEvidenceDir(prd, &Story{}); got != "/p" {
		t.Fatalf("empty story: got %q, want /p", got)
	}
}

func TestBL447_VerifierMemoryFindingDir(t *testing.T) {
	for _, tc := range []struct{ name, override, want string }{
		{"no override", "", "/proj447"},
		{"override", "/proj447/sub", "/proj447/sub"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, prd := tpdSetup(t, "/proj447", tc.override)
			prd.MemorySeed = MemorySeedConfig{Enabled: true, MaxPerScope: 10}
			_ = m.Store().SavePRD(prd)
			var dirs []string
			var mu sync.Mutex
			m.SetMemoryVerifierFn(func(_ context.Context, _, projectDir, _, _ string) error {
				mu.Lock()
				dirs = append(dirs, projectDir)
				mu.Unlock()
				return nil
			})
			spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
				return SpawnResult{SessionID: "s-" + req.TaskID}, nil
			}
			verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
				return VerificationResult{OK: false, Issues: []string{"x"}}, nil
			}
			_ = m.Run(context.Background(), prd.ID, spawn, verify)
			if len(dirs) == 0 {
				t.Fatal("verifierFn never called")
			}
			for _, d := range dirs {
				if d != tc.want {
					t.Fatalf("verifierFn projectDir = %q, want %q", d, tc.want)
				}
			}
		})
	}
}

func TestBL447_FileDriftUsesTaskDir(t *testing.T) {
	prdDir := tpdRepoWithBase(t)
	taskDir := tpdRepoWithBase(t)
	for _, tc := range []struct{ name, override, want string }{
		{"no override", "", prdDir},
		{"override", taskDir, taskDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, prd := tpdSetup(t, prdDir, tc.override)
			task := &prd.Story[0].Tasks[0]
			task.PreTaskSHA = driftGit(t, tc.want, "rev-parse", "HEAD")
			var dirs []string
			m.driftGit = func(ctx context.Context, d string, args ...string) (string, error) {
				dirs = append(dirs, d)
				return defaultDriftGitRunner(ctx, d, args...)
			}
			m.checkForeignFileDrift(context.Background(), prd, task)
			if len(dirs) != 1 || dirs[0] != tc.want {
				t.Fatalf("drift git dirs = %v, want [%s]", dirs, tc.want)
			}
		})
	}
	// Gate: an override still runs the check when the PRD has no dir.
	m, prd := tpdSetup(t, "", taskDir)
	task := &prd.Story[0].Tasks[0]
	task.PreTaskSHA = driftGit(t, taskDir, "rev-parse", "HEAD")
	calls := 0
	m.driftGit = func(ctx context.Context, d string, args ...string) (string, error) {
		calls++
		return defaultDriftGitRunner(ctx, d, args...)
	}
	m.checkForeignFileDrift(context.Background(), prd, task)
	if calls != 1 {
		t.Fatalf("empty PRD dir + override: git calls = %d, want 1", calls)
	}
}

// stubQualityGates swaps runQualityGateTests for a recorder.
func stubQualityGates(t *testing.T) (*[]string, *sync.Mutex) {
	t.Helper()
	var dirs []string
	var mu sync.Mutex
	orig := runQualityGateTests
	runQualityGateTests = func(_ string, dir string, _ int, _ []string) pipeline.TestResult {
		mu.Lock()
		dirs = append(dirs, dir)
		mu.Unlock()
		return pipeline.TestResult{PassCount: 1}
	}
	t.Cleanup(func() { runQualityGateTests = orig })
	return &dirs, &mu
}

func TestBL447_QualityGateTestDir(t *testing.T) {
	prdDir := t.TempDir()
	taskDir := t.TempDir()
	for _, tc := range []struct {
		name, override string
		want           []string
	}{
		// Baseline + per-task run, both in the PRD dir (pre-BL447 shape).
		{"no override", "", []string{prdDir, prdDir}},
		// PRD baseline, then a lazily captured task-dir baseline, then the
		// per-task run in the task dir.
		{"override", taskDir, []string{prdDir, taskDir, taskDir}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dirs, mu := stubQualityGates(t)
			m, prd := tpdSetup(t, prdDir, tc.override)
			prd.QualityGates = &pipeline.QualityGateConfig{Enabled: true, TestCommand: "true"}
			_ = m.Store().SavePRD(prd)
			spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
				return SpawnResult{SessionID: "s-" + req.TaskID}, nil
			}
			if err := m.Run(context.Background(), prd.ID, spawn, okVerify); err != nil {
				t.Fatalf("Run: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if strings.Join(*dirs, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("quality-gate dirs = %v, want %v", *dirs, tc.want)
			}
			got, _ := m.Store().GetPRD(prd.ID)
			if got.Story[0].Tasks[0].QualityGateResult == nil {
				t.Fatal("quality gate did not run for the task")
			}
		})
	}
}

func TestBL447_QualityGateBaselineCachedPerDir(t *testing.T) {
	dirs, _ := stubQualityGates(t)
	b := newQualityGateBaselines(pipeline.QualityGateConfig{Enabled: true, TestCommand: "x"}, "/p", nil, nil)
	if _, ok := b.forDir("/p"); ok {
		t.Fatal("PRD dir without a captured baseline must report none (pre-BL447 behaviour)")
	}
	b.forDir("/t")
	b.forDir("/t")
	if len(*dirs) != 1 || (*dirs)[0] != "/t" {
		t.Fatalf("baseline runs = %v, want one in /t", *dirs)
	}
	var nilB *qualityGateBaselines
	if _, ok := nilB.forDir("/t"); ok {
		t.Fatal("nil cache must report no baseline")
	}
}

func TestBL447_LintTaskScopeFor(t *testing.T) {
	prd := &PRD{ProjectDir: "/home/u/proj"}
	none := &Task{}
	if got, want := LintTaskScopeFor(prd, none, "t", "", []string{"/home/u/other/x.go"}),
		LintTaskScope(prd, "t", "", []string{"/home/u/other/x.go"}); strings.Join(got, ";") != strings.Join(want, ";") || len(got) == 0 {
		t.Fatalf("no override must match LintTaskScope: got %v want %v", got, want)
	}
	over := &Task{ProjectDir: "/home/u/other"}
	if got := LintTaskScopeFor(prd, over, "t", "", []string{"/home/u/other/x.go"}); len(got) != 0 {
		t.Fatalf("override: file in task dir flagged: %v", got)
	}
}

// Incident replay: the PRD dir and the task override are separate repos.
// While the task runs, an unrelated commit lands in the PRD repo; the task
// commits in its own repo. The task's anchor, diff and drift check must
// all see only the override repo.
func TestBL447_IncidentReplay_SeparateRepos(t *testing.T) {
	prdDir := tpdRepoWithBase(t)
	taskDir := tpdRepoWithBase(t)
	driftWrite(t, taskDir, "seed.txt", "s\n")
	driftGit(t, taskDir, "add", "seed.txt")
	driftGit(t, taskDir, "commit", "-q", "-m", "seed")
	// A pre-existing dirty file in the PRD repo that a foreign commit sweeps in.
	driftWrite(t, prdDir, "base.txt", "operator edit\n")
	taskHead := driftGit(t, taskDir, "rev-parse", "HEAD")

	m, prd := tpdSetup(t, prdDir, taskDir)
	var reqs []SpawnRequest
	var mu sync.Mutex
	inner := fakeSpawnCapture(&reqs, &mu)
	spawn := func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		res, err := inner(ctx, req)
		// During the task: a foreign commit in the PRD repo...
		driftWrite(t, prdDir, "foreign.txt", "f\n")
		driftGit(t, prdDir, "add", "foreign.txt", "base.txt")
		driftGit(t, prdDir, "commit", "-q", "-m", "foreign")
		// ...and the task's own commit in its repo.
		driftWrite(t, taskDir, "task.txt", "task work\n")
		driftGit(t, taskDir, "add", "task.txt")
		driftGit(t, taskDir, "commit", "-q", "-m", "task")
		return res, err
	}
	var diff string
	verify := func(ctx context.Context, p *PRD, task *Task) (VerificationResult, error) {
		// Mirrors cmd/datawatch's verifier: diff in the resolved task dir.
		out, err := exec.CommandContext(ctx, "git", "-C", ResolveTaskProjectDir(p, task),
			"diff", "--name-only", task.PreTaskSHA+"..HEAD").Output()
		if err != nil {
			return VerificationResult{}, err
		}
		diff = strings.TrimSpace(string(out))
		return VerificationResult{OK: true}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	task := got.Story[0].Tasks[0]
	if task.PreTaskSHA != taskHead {
		t.Fatalf("PreTaskSHA = %q, want override-repo HEAD %q", task.PreTaskSHA, taskHead)
	}
	if err := exec.Command("git", "-C", taskDir, "cat-file", "-e", task.PreTaskSHA+"^{commit}").Run(); err != nil {
		t.Fatalf("PreTaskSHA not in override repo: %v", err)
	}
	if diff != "task.txt" {
		t.Fatalf("captured diff = %q, want only task.txt", diff)
	}
	if len(task.ForeignFileDrift) != 0 {
		t.Fatalf("drift reported from PRD-repo commit: %v", task.ForeignFileDrift)
	}
	for _, d := range got.Decisions {
		if d.Kind != "file_drift_detected" {
			continue
		}
		// In-run recording (the worker's own task.txt commit) is expected;
		// what the replay must rule out is a PRE-EXISTING finding from
		// the foreign PRD-repo commit.
		note := d.Note
		if !strings.Contains(note, "in-run edit by the task's own worker") {
			t.Fatalf("foreign-drift decision from PRD-repo commit (not an in-run note): %+v", d)
		}
		if !strings.Contains(note, "task.txt") {
			t.Fatalf("in-run note should name the task's own file task.txt: %+v", d)
		}
		if strings.Contains(note, "foreign.txt") || strings.Contains(note, "base.txt") {
			t.Fatalf("decision swept in PRD-repo files: %+v", d)
		}
	}
}
