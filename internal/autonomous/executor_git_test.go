// prdHasGit / prdAutoCommits — PRD task sessions get git activity
// (StartOptions.Git) when the PRD has git settings and may auto-commit:
// worktree mode, or declared write dirs that bind the commit scope. These
// tests capture the SpawnRequest the executor hands to SpawnFn and assert
// Git accordingly (see prdAutoCommits in executor.go). Existing no-git PRD
// flows keep Git=false, unchanged.

package autonomous

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// spawnGitCapture runs one executeOne cycle with a spawning SpawnFn that
// captures the SpawnRequest, and a VerifyFn that passes, then reports the
// captured request (nil when spawn never ran).
func spawnGitCapture(t *testing.T, m *Manager, prd *PRD, task *Task) *SpawnRequest {
	t.Helper()

	var captured *SpawnRequest
	spawnFn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		reqCopy := req
		captured = &reqCopy
		return SpawnResult{SessionID: "sess-git-test"}, nil
	}
	verifyFn := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}

	if err := m.executeOne(context.Background(), prd, task, spawnFn, verifyFn, 0, pipeline.QualityGateConfig{}, nil); err != nil {
		t.Fatalf("executeOne: %v", err)
	}
	return captured
}

func TestSpawnRequest_GitTrue_WhenPRDHasGitSettings(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", dir, "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "story", Tasks: []Task{{Title: "task"}}},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	// Git settings: a remote-backed PRD with an explicit branch.
	prd.Git = GitWorkflow{
		Provider:   "github",
		URL:        "https://git.example.com/foo/bar.git",
		BaseBranch: "main",
		Branch:     "feat/test-branch",
	}
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	task := &prd.Story[0].Tasks[0]

	req := spawnGitCapture(t, m, prd, task)
	if req == nil {
		t.Fatal("spawn was not called")
	}
	if !req.Git {
		t.Errorf("SpawnRequest.Git = false, want true: PRD has git settings (Git.URL set); the task session must run git activity")
	}
}

func TestSpawnRequest_GitFalse_WhenPRDHasNoGitSettings(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", dir, "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "story", Tasks: []Task{{Title: "task"}}},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	// No git settings: zero GitWorkflow, no project profile.
	prd.Git = GitWorkflow{}
	prd.ProjectProfile = ""
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	task := &prd.Story[0].Tasks[0]

	req := spawnGitCapture(t, m, prd, task)
	if req == nil {
		t.Fatal("spawn was not called")
	}
	if req.Git {
		t.Errorf("SpawnRequest.Git = true, want false: PRD has no git settings; existing flow must be unchanged")
	}
}

func TestSpawnRequest_GitTrue_WhenPRDHasProjectProfile(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("spec", dir, "", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "story", Tasks: []Task{{Title: "task"}}},
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	// Project profile = development against a project repo (git-remote work).
	// Direct mode (no worktree branch): git activity requires declared write
	// dirs, which bound the auto-commit's staging scope.
	prd.Git = GitWorkflow{}
	prd.ProjectProfile = "my-project"
	prd.WriteDirs = []string{dir + "/scoped"}
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)
	prd, _ = m.Store().GetPRD(prd.ID)
	task := &prd.Story[0].Tasks[0]

	req := spawnGitCapture(t, m, prd, task)
	if req == nil {
		t.Fatal("spawn was not called")
	}
	if !req.Git {
		t.Errorf("SpawnRequest.Git = false, want true: PRD has a project profile (project repo) with declared write dirs; the task session should run git activity within that scope")
	}
}

func TestPrdHasGit(t *testing.T) {
	cases := []struct {
		name string
		prd  *PRD
		want bool
	}{
		{"nil PRD", nil, false},
		{"empty PRD", &PRD{}, false},
		{"git url set", &PRD{Git: GitWorkflow{URL: "https://x/y.git"}}, true},
		{"git branch set", &PRD{Git: GitWorkflow{Branch: "feat"}}, true},
		{"git auto-pr set", &PRD{Git: GitWorkflow{AutoPR: true}}, true},
		{"project profile", &PRD{ProjectProfile: "proj"}, true},
		{"both empty", &PRD{Git: GitWorkflow{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := prdHasGit(tc.prd); got != tc.want {
				t.Errorf("prdHasGit() = %v, want %v", got, tc.want)
			}
		})
	}
}
