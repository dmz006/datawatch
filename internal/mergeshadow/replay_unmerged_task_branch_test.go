package mergeshadow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// replayScenarioPath is the fixture that describes the repository to build.
const replayScenarioPath = "../../testdata/mergeshadow/unmerged-task-branch/scenario.json"

// replayCommit is one commit: its message and the files it writes.
type replayCommit struct {
	Message string            `json:"message"`
	Files   map[string]string `json:"files"`
}

// replayTask is one task branch. From is "main" (the base ref) or "automaton"
// (the Automaton branch). Merged branches are merged back into the Automaton
// branch after their commits.
type replayTask struct {
	Name    string         `json:"name"`
	From    string         `json:"from"`
	Merged  bool           `json:"merged"`
	Commits []replayCommit `json:"commits"`
}

// replayScenario is the content of scenario.json.
type replayScenario struct {
	Description      string         `json:"description"`
	AutomatonID      string         `json:"automaton_id"`
	Project          string         `json:"project"`
	Branch           string         `json:"branch"`
	BaseRef          string         `json:"base_ref"`
	Main             replayCommit   `json:"main"`
	AutomatonCommits []replayCommit `json:"automaton_commits"`
	TaskBranches     []replayTask   `json:"task_branches"`
}

func loadReplayScenario(t *testing.T) replayScenario {
	t.Helper()
	data, err := os.ReadFile(replayScenarioPath) // #nosec G304 -- fixed fixture path
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	var s replayScenario
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse scenario: %v", err)
	}
	return s
}

// commitReplayFiles writes each file of c into repo and commits them.
func commitReplayFiles(t *testing.T, repo string, c replayCommit) {
	t.Helper()
	for name, body := range c.Files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0o750); err != nil {
			t.Fatal(err)
		}
		write(t, repo, name, body)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", c.Message)
}

// buildReplayRepo builds the repository the scenario describes in a fresh
// temporary directory and returns its path.
func buildReplayRepo(t *testing.T, s replayScenario) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", s.BaseRef)
	commitReplayFiles(t, repo, s.Main)

	git(t, repo, "checkout", "-q", "-b", s.Branch, s.BaseRef)
	for _, c := range s.AutomatonCommits {
		commitReplayFiles(t, repo, c)
	}

	for _, task := range s.TaskBranches {
		from := s.BaseRef
		if task.From == "automaton" {
			from = s.Branch
		}
		git(t, repo, "checkout", "-q", "-b", task.Name, from)
		for _, c := range task.Commits {
			commitReplayFiles(t, repo, c)
		}
		if task.Merged {
			git(t, repo, "checkout", "-q", s.Branch)
			git(t, repo, "merge", "-q", "--no-ff", "-m", "merge "+task.Name, task.Name)
		}
	}
	git(t, repo, "checkout", "-q", s.BaseRef)
	return repo
}

// TestReplayUnmergedTaskBranchBlocksPlan replays the unmerged-task-branch
// scenario through the runner. The plan must be blocked by the
// unmerged_task_branch finding, name only the branch that never merged, leave
// the trial merge and gates unrun, and stay unapplied.
func TestReplayUnmergedTaskBranchBlocksPlan(t *testing.T) {
	s := loadReplayScenario(t)
	repo := buildReplayRepo(t, s)
	dataDir := t.TempDir()

	in := Input{
		AutomatonID: s.AutomatonID,
		Project:     s.Project,
		RepoPath:    repo,
		Branch:      s.Branch,
		BaseRef:     s.BaseRef,
		DataDir:     dataDir,
		Pipeline:    config.MergePipelineConfig{VersionFiles: []string{"version.go"}},
		ChangelogTasks: []ChangelogTask{{
			ID:     "t9",
			Kind:   ChangelogTaskKind,
			Output: "## [v1.3.0] - 2026-11-09\n\n### Added\n- thing\n",
		}},
		GateTimeout: time.Minute,
	}

	gateCalls := 0
	gates := func(context.Context, string, time.Duration) []GateResult {
		gateCalls++
		return nil
	}

	plan, err := Runner{Gates: gates, Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.Applied {
		t.Error("plan marked applied; a blocked plan must stay unapplied")
	}
	if gateCalls != 0 {
		t.Errorf("gates ran %d time(s) against a blocked plan, want 0", gateCalls)
	}
	if _, err := os.Stat(filepath.Join(dataDir, scratchDirName)); !os.IsNotExist(err) {
		t.Errorf("trial merge scratch directory exists (err %v); the trial merge must not run", err)
	}

	if len(plan.Findings) != 1 {
		t.Fatalf("findings %+v, want exactly one", plan.Findings)
	}
	f := plan.Findings[0]
	if f.Code != "unmerged_task_branch" || !f.Blocking {
		t.Errorf("finding %+v, want blocking unmerged_task_branch", f)
	}
	if !strings.Contains(f.Message, "automaton/aa000001--tasks-add-thing-aa000002") {
		t.Errorf("finding message %q does not name the unmerged task branch", f.Message)
	}
	if strings.Contains(f.Message, "add-readme") {
		t.Errorf("finding message %q names a task branch that was merged", f.Message)
	}
	for _, g := range plan.Gates {
		if g.Status != string(GateSkipped) {
			t.Errorf("gate %s status %q, want skipped", g.Name, g.Status)
		}
	}

	stored, err := Load(dataDir, s.AutomatonID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Applied {
		t.Error("stored plan marked applied")
	}
	if len(stored.Findings) != 1 || stored.Findings[0].Code != "unmerged_task_branch" {
		t.Errorf("stored findings %+v, want the unmerged_task_branch finding", stored.Findings)
	}
}
