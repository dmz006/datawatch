// Direct-mode auto-commit gating — a direct-mode PRD (no worktree,
// Git.Branch empty) with no declared write dirs must NOT get Git activity on
// its task sessions: with git settings present but no declared scope, the
// session's write scope would silently widen to the whole project directory
// and the auto-commit would stage and commit everything changed there. PRDs
// that DO get git activity: worktree mode (unconditionally — the scope is the
// worktree itself) and direct mode with declared write dirs (scoped commit).

package autonomous

import (
	"reflect"
	"testing"
)

func TestDirectModeWithoutWriteDirsDoesNotAutoCommit(t *testing.T) {
	cases := []struct {
		name           string
		configure      func(prd *PRD, projectDir string)
		wantGit        bool
		wantWriteScope []string // nil = do not assert
	}{
		{
			name: "direct mode, no write dirs, git url set: no git activity",
			configure: func(prd *PRD, _ string) {
				prd.Git = GitWorkflow{URL: "https://git.example.com/foo/bar.git"}
				prd.Git.Branch = "" // direct mode: no worktree branch
			},
			wantGit:        false,
			wantWriteScope: nil,
		},
		{
			name: "direct mode, no write dirs, project profile set: no git activity",
			configure: func(prd *PRD, _ string) {
				prd.Git = GitWorkflow{}
				prd.ProjectProfile = "x"
			},
			wantGit:        false,
			wantWriteScope: nil,
		},
		{
			name: "direct mode, no git settings: no git activity (unchanged)",
			configure: func(prd *PRD, _ string) {
				prd.Git = GitWorkflow{}
				prd.ProjectProfile = ""
			},
			wantGit:        false,
			wantWriteScope: nil,
		},
		{
			name: "direct mode, write dirs declared, git settings set: scoped git activity",
			configure: func(prd *PRD, projectDir string) {
				prd.Git = GitWorkflow{URL: "https://git.example.com/foo/bar.git"}
				prd.WriteDirs = []string{projectDir + "/scoped"}
			},
			wantGit:        true,
			wantWriteScope: nil, // asserted per-case below against the declared dirs
		},
		{
			name: "worktree mode, no write dirs: git activity over the worktree (unchanged)",
			configure: func(prd *PRD, _ string) {
				prd.Git = GitWorkflow{
					URL:        "https://git.example.com/foo/bar.git",
					BaseBranch: "main",
					Branch:     "feat/branch",
				}
			},
			wantGit: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
			tc.configure(prd, dir)
			prd.Status = PRDRunning
			_ = m.Store().SavePRD(prd)
			prd, _ = m.Store().GetPRD(prd.ID)
			task := &prd.Story[0].Tasks[0]

			req := spawnGitCapture(t, m, prd, task)
			if req == nil {
				t.Fatal("spawn was not called")
			}
			if req.Git != tc.wantGit {
				t.Errorf("SpawnRequest.Git = %v, want %v", req.Git, tc.wantGit)
			}
			if tc.name == "direct mode, write dirs declared, git settings set: scoped git activity" {
				want := []string{dir + "/scoped"}
				if !reflect.DeepEqual(req.WriteScope, want) {
					t.Errorf("SpawnRequest.WriteScope = %v, want %v (the declared write dirs)", req.WriteScope, want)
				}
			}
			if tc.name == "worktree mode, no write dirs: git activity over the worktree (unchanged)" {
				want := []string{dir}
				if !reflect.DeepEqual(req.WriteScope, want) {
					t.Errorf("SpawnRequest.WriteScope = %v, want %v (the PRD project dir / worktree)", req.WriteScope, want)
				}
			}
		})
	}
}
