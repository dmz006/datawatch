// Merged-branch finish: an Automaton whose recorded merge commit is already in
// main is completed by the boot-resume pass (and by Manager.Run) without
// starting a judge or a session.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run MergedBranchFinish
package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mergedFinishGit is a fake MergeGitRunner answering the ancestor query.
type mergedFinishGit struct {
	inMain   bool
	err      error
	ancestor atomic.Int32
}

func (g *mergedFinishGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	if len(args) > 0 {
		switch args[0] {
		case "remote":
			return "origin\n", nil
		case "symbolic-ref":
			return "refs/remotes/origin/main\n", nil
		case "merge-base":
			g.ancestor.Add(1)
			if g.err != nil {
				return "", g.err
			}
			if !g.inMain {
				return "", errors.New("exit status 1")
			}
			return "", nil
		}
	}
	return "", nil
}

type mergedFinishRig struct {
	m        *Manager
	api      *API
	prd      *PRD
	git      *mergedFinishGit
	judges   atomic.Int32
	spawns   atomic.Int32
	fakeTime time.Time
}

// mergedFinishFixture builds a PRDRunning Automaton with every task done and a
// merge record (unless commit is empty), a fake clock and the fake layers.
func mergedFinishFixture(t *testing.T, commit string, git *mergedFinishGit) *mergedFinishRig {
	t.Helper()
	m, api := gateFixture(t)
	r := &mergedFinishRig{m: m, api: api, git: git}
	r.fakeTime = time.Now().Add(37 * time.Minute)
	m.now = func() time.Time { return r.fakeTime }
	m.mergedFinishGit = git
	m.mu.Lock()
	m.cfg.VerificationModel = "ollama/qwen3.8:27b"
	m.mu.Unlock()
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		r.judges.Add(1)
		return GuardrailVerdict{Outcome: "pass"}, nil
	})
	api.spawnFn = func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		r.spawns.Add(1)
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	prd := gatePRD(t, m, ciProjectDir(t), time.Minute)
	prd.PerPRDGuardrails = []string{"rules-review"}
	mergedAt := time.Now().Add(-10 * time.Minute)
	prd.MergedAt = &mergedAt
	prd.MergeCommit = commit
	for i := range prd.Story {
		for j := range prd.Story[i].Tasks {
			prd.Story[i].Tasks[j].Status = TaskCompleted
		}
	}
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	r.prd = prd
	return r
}

func (r *mergedFinishRig) status(t *testing.T) *PRD {
	t.Helper()
	p, ok := r.m.Store().SnapshotPRD(r.prd.ID)
	if !ok {
		t.Fatal("PRD disappeared")
	}
	return p
}

func TestMergedBranchFinishCompletesWhenCommitInMain(t *testing.T) {
	r := mergedFinishFixture(t, "abc123", &mergedFinishGit{inMain: true})
	r.api.resumeRunningPRDs()
	p := r.status(t)
	if p.Status != PRDCompleted {
		t.Fatalf("want PRDCompleted, got %q", p.Status)
	}
	found := false
	for _, d := range p.Decisions {
		if d.Kind == mergedBranchFinishKind {
			found = true
			if !d.At.Equal(r.fakeTime) {
				t.Errorf("decision time %v, want fake clock %v", d.At, r.fakeTime)
			}
		}
	}
	if !found {
		t.Error("no merged_branch_finish decision recorded")
	}
	if n := r.judges.Load(); n != 0 {
		t.Errorf("judge launcher calls = %d, want 0", n)
	}
	if n := r.spawns.Load(); n != 0 {
		t.Errorf("session spawner calls = %d, want 0", n)
	}
}

func TestMergedBranchFinishRunOnMergedReturnsNilStartsNothing(t *testing.T) {
	r := mergedFinishFixture(t, "abc123", &mergedFinishGit{inMain: true})
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		r.spawns.Add(1)
		return SpawnResult{SessionID: "x"}, nil
	}
	if err := r.m.Run(context.Background(), r.prd.ID, spawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if p := r.status(t); p.Status != PRDCompleted {
		t.Errorf("want PRDCompleted, got %q", p.Status)
	}
	if r.judges.Load() != 0 || r.spawns.Load() != 0 {
		t.Errorf("Run started work: judges=%d spawns=%d", r.judges.Load(), r.spawns.Load())
	}
}

// normalPath asserts the Automaton was not finished by the merged check and its
// judge ran once.
func mergedFinishWantNormalPath(t *testing.T, r *mergedFinishRig) {
	t.Helper()
	r.api.resumeRunningPRDs()
	// Let the executor finish on its own; WaitIdle would cancel it.
	pollUntil(t, 10*time.Second, "judge run and executor finished", func() bool { return r.judges.Load() >= 1 && r.status(t).Status != PRDRunning })
	p := r.status(t)
	for _, d := range p.Decisions {
		if d.Kind == mergedBranchFinishKind {
			t.Fatalf("normal path must not record %s", mergedBranchFinishKind)
		}
	}
	if n := r.judges.Load(); n != 1 {
		t.Errorf("judge started %d times, want 1", n)
	}
	if n := r.spawns.Load(); n != 0 {
		t.Errorf("all tasks done, spawner calls = %d, want 0", n)
	}
}

func TestMergedBranchFinishCommitNotInMainRunsJudge(t *testing.T) {
	r := mergedFinishFixture(t, "abc123", &mergedFinishGit{inMain: false})
	mergedFinishWantNormalPath(t, r)
	if r.git.ancestor.Load() == 0 {
		t.Error("ancestor query never asked")
	}
}

func TestMergedBranchFinishNoMergeCommitTakesNormalPath(t *testing.T) {
	r := mergedFinishFixture(t, "", &mergedFinishGit{inMain: true})
	mergedFinishWantNormalPath(t, r)
	if n := r.git.ancestor.Load(); n != 0 {
		t.Errorf("ancestor asked %d times without a merge commit", n)
	}
}

func TestMergedBranchFinishGitErrorTakesNormalPath(t *testing.T) {
	r := mergedFinishFixture(t, "abc123", &mergedFinishGit{err: errors.New("fatal: bad object " + strings.Repeat("0", 4))})
	mergedFinishWantNormalPath(t, r)
}
