// A protected-paths hold may be released as a false positive only on positive
// proof: the tree that is re-read is the one the task worked in, the file is a
// rules file that carries the daemon's own sections, and there is a diff that
// consists of nothing else. These tests cover each way that proof can be
// missing; each one must keep the hold.
package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const ppgCIPath = ".github/workflows/ci.yaml"

const ppgCIBase = "jobs:\n  check:\n    steps:\n      - run: make lint\n\n      # Unscoped tmux kill audit.\n      - run: make tmux-check\n"

// ppgCommit writes content to rel in repo, commits it and returns the sha.
func ppgCommit(t *testing.T, repo, rel, content string) string {
	t.Helper()
	ppgWrite(t, repo, rel, content)
	uwGit(t, repo, "add", "--", rel)
	uwGit(t, repo, "commit", "-q", "-m", "add "+rel)
	return uwGit(t, repo, "rev-parse", "HEAD")
}

func ppgWrite(t *testing.T, repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ppgHold holds task on rel against base and returns the question id.
func ppgHold(t *testing.T, m *Manager, prd *PRD, task *Task, rel, base string) string {
	t.Helper()
	task.Status = TaskInProgress
	if err := m.Store().SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	if err := m.holdForProtectedPaths(prd, task, []string{rel}, base); err != nil {
		t.Fatalf("holdForProtectedPaths: %v", err)
	}
	tk, _ := m.Store().GetTask(task.ID)
	if tk.Status != TaskBlocked || tk.ProtectedPathHold == nil || tk.ProtectedPathHold.QuestionID == "" {
		t.Fatalf("task not held: status %s hold %+v", tk.Status, tk.ProtectedPathHold)
	}
	return tk.ProtectedPathHold.QuestionID
}

// ppgSweepKeeps runs the start sweep and requires the hold to survive it
// untouched: nothing released, the task still blocked with its hold, the
// question in wantState and no release decision.
func ppgSweepKeeps(t *testing.T, m *Manager, prd *PRD, task *Task, store *QuestionStore, questionID string, wantState QuestionState) {
	t.Helper()
	if got := m.ReleaseFalseProtectedPathHolds(context.Background()); len(got) != 0 {
		t.Errorf("released = %v; the hold must stay", got)
	}
	tk, _ := m.Store().GetTask(task.ID)
	if tk.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", tk.Status)
	}
	if h := tk.ProtectedPathHold; h == nil || h.QuestionID != questionID {
		t.Errorf("hold = %+v; want the hold with question %s", h, questionID)
	}
	if q, ok := store.Get(questionID); !ok || q.State != wantState {
		t.Errorf("question state = %q (found %v); want %q", q.State, ok, wantState)
	}
	for _, k := range fpfDecisionKinds(t, m, prd) {
		if k == protectedPathsFalsePositiveDecisionKind {
			t.Errorf("a %s decision was recorded", k)
		}
	}
}

// A protected CI file has no daemon-written sections, so no state of it is a
// false positive.
func TestReleaseKeepsCIFileWithoutDaemonSections(t *testing.T) {
	cases := map[string]string{
		"real change":             strings.Replace(ppgCIBase, "make lint", "make lint && ./extra.sh", 1),
		"blank lines only":        strings.Replace(ppgCIBase, "\n\n", "\n\n\n\n", 1),
		"trailing newlines only":  ppgCIBase + "\n\n",
		"step after fake heading": ppgCIBase + "\n# Session Guardrails\n      - run: ./unapproved.sh\n",
		"step in a fake block":    ppgCIBase + "<!-- rtk-instructions -->\n      - run: ./unapproved.sh\n<!-- /rtk-instructions -->\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			m, prd, task, store, repo := fpfRig(t)
			base := ppgCommit(t, repo, ppgCIPath, ppgCIBase)
			ppgWrite(t, repo, ppgCIPath, content)
			q := ppgHold(t, m, prd, task, ppgCIPath, base)
			ppgSweepKeeps(t, m, prd, task, store, q, QuestionOpen)
		})
	}
}

// An empty diff proves nothing about the change the task was held for.
func TestReleaseKeepsHoldOnEmptyDiff(t *testing.T) {
	cases := map[string]struct{ rel, content string }{
		"CI file":                     {ppgCIPath, ppgCIBase},
		"rules file without sections": {"AGENT.md", dscBaseRules},
		"rules file with sections":    {"AGENT.md", dscDaemonOnlyRules()},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m, prd, task, store, repo := fpfRig(t)
			base := ppgCommit(t, repo, c.rel, c.content)
			q := ppgHold(t, m, prd, task, c.rel, base)
			ppgSweepKeeps(t, m, prd, task, store, q, QuestionOpen)
		})
	}
}

// A rules file the daemon never wrote into is graded as an ordinary file.
func TestReleaseKeepsRulesFileWithoutDaemonSections(t *testing.T) {
	m, prd, task, store, repo := fpfRig(t)
	base := ppgCommit(t, repo, "AGENT.md", dscBaseRules)
	ppgWrite(t, repo, "AGENT.md", dscBaseRules+"\n\n\n")
	q := ppgHold(t, m, prd, task, "AGENT.md", base)
	ppgSweepKeeps(t, m, prd, task, store, q, QuestionOpen)
}

// A diff that cannot be read is never a false positive.
func TestReleaseKeepsHoldOnUnreadableDiff(t *testing.T) {
	cases := map[string]func(t *testing.T, m *Manager, repo, rel string) (base string){
		"git cannot read the base version": func(t *testing.T, m *Manager, repo, rel string) string {
			base := uwGit(t, repo, "rev-parse", "HEAD")
			m.driftGit = func(ctx context.Context, dir string, args ...string) (string, error) {
				if len(args) > 0 && args[0] == "show" {
					return "", errors.New("fatal: unable to read object")
				}
				return defaultDriftGitRunner(ctx, dir, args...)
			}
			return base
		},
		"base commit unknown": func(*testing.T, *Manager, string, string) string {
			return "1234567890123456789012345678901234567890"
		},
		"no base commit stored": func(*testing.T, *Manager, string, string) string { return "" },
		"working file deleted": func(t *testing.T, _ *Manager, repo, rel string) string {
			if err := os.Remove(filepath.Join(repo, filepath.FromSlash(rel))); err != nil {
				t.Fatal(err)
			}
			return uwGit(t, repo, "rev-parse", "HEAD")
		},
		"working file is a directory": func(t *testing.T, _ *Manager, repo, rel string) string {
			full := filepath.Join(repo, filepath.FromSlash(rel))
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(full, 0o755); err != nil {
				t.Fatal(err)
			}
			return uwGit(t, repo, "rev-parse", "HEAD")
		},
	}
	for name, breakIt := range cases {
		for _, f := range []struct{ rel, base, cur string }{
			{ppgCIPath, ppgCIBase, ppgCIBase + "      - run: ./unapproved.sh\n"},
			{"AGENT.md", dscBaseRules, dscDaemonOnlyRules()},
		} {
			t.Run(name+"/"+f.rel, func(t *testing.T) {
				m, prd, task, store, repo := fpfRig(t)
				ppgCommit(t, repo, f.rel, f.base)
				ppgWrite(t, repo, f.rel, f.cur)
				base := breakIt(t, m, repo, f.rel)
				q := ppgHold(t, m, prd, task, f.rel, base)
				ppgSweepKeeps(t, m, prd, task, store, q, QuestionOpen)
			})
		}
	}
}

// A symlink in place of the rules file is a change to the file, whatever it
// points at.
func TestReleaseKeepsRulesFileReplacedBySymlink(t *testing.T) {
	m, prd, task, store, repo := fpfRig(t)
	base := ppgCommit(t, repo, "AGENT.md", dscBaseRules)
	other := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(other, []byte(dscDaemonOnlyRules()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "AGENT.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(repo, "AGENT.md")); err != nil {
		t.Fatal(err)
	}
	q := ppgHold(t, m, prd, task, "AGENT.md", base)
	ppgSweepKeeps(t, m, prd, task, store, q, QuestionOpen)
}

// ppgWorktrees puts the Automaton in worktree mode: an Automaton worktree on
// its own branch, and a task worktree on a task branch cut from it. rel is
// committed at base before both are cut. It returns the base sha and the two
// directories.
func ppgWorktrees(t *testing.T, m *Manager, prd *PRD, task *Task, repo, rel, content string) (base, autoDir, taskDir string) {
	t.Helper()
	base = ppgCommit(t, repo, rel, content)
	root := t.TempDir()
	autoDir, taskDir = filepath.Join(root, "automaton"), filepath.Join(root, "task")
	uwGit(t, repo, "worktree", "add", "-q", "-b", "automaton/ppg", autoDir, base)
	uwGit(t, repo, "worktree", "add", "-q", "-b", "automaton/ppg--tasks-one", taskDir, "automaton/ppg")
	prd.ProjectDir = autoDir
	prd.Git.Branch = "automaton/ppg"
	task.WorktreePath, task.WorktreeBranch = taskDir, "automaton/ppg--tasks-one"
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return base, autoDir, taskDir
}

// ppgRemoveTaskWorktree is what cleanup does when the Automaton ends: the task
// worktree goes and the task record forgets it.
func ppgRemoveTaskWorktree(t *testing.T, m *Manager, task *Task, repo, taskDir string) {
	t.Helper()
	uwGit(t, repo, "worktree", "remove", "--force", taskDir)
	m.setTaskWorktree(task, "", "")
}

// The incident: the change was committed in the task worktree. While that
// worktree existed the sweep kept the hold. Once it was removed, the sweep read
// the Automaton worktree, which never received the held commit, found the file
// identical to the base and released the hold.
func TestReleaseKeepsHoldAfterTaskWorktreeRemoved(t *testing.T) {
	m, prd, task, store, repo := fpfRig(t)
	base, _, taskDir := ppgWorktrees(t, m, prd, task, repo, ppgCIPath, ppgCIBase)
	ppgCommit(t, taskDir, ppgCIPath, ppgCIBase+"      - run: ./unapproved.sh\n")
	q := ppgHold(t, m, prd, task, ppgCIPath, base)

	ppgSweepKeeps(t, m, prd, task, store, q, QuestionOpen)
	ppgRemoveTaskWorktree(t, m, task, repo, taskDir)
	ppgSweepKeeps(t, m, prd, task, store, q, QuestionOpen)
}

// The same with a rules file: the daemon writes its sections into the Automaton
// worktree, so that tree shows a daemon-only diff while the real edit sits on
// the task branch. The tree being read is not the task's, so it proves nothing.
func TestReleaseKeepsHoldWhenOnlyAnotherTreeShowsDaemonSections(t *testing.T) {
	m, prd, task, store, repo := fpfRig(t)
	base, autoDir, taskDir := ppgWorktrees(t, m, prd, task, repo, "AGENT.md", dscBaseRules)
	ppgCommit(t, taskDir, "AGENT.md", dscRealEditRules())
	q := ppgHold(t, m, prd, task, "AGENT.md", base)
	ppgWrite(t, autoDir, "AGENT.md", dscDaemonOnlyRules())

	ppgRemoveTaskWorktree(t, m, task, repo, taskDir)
	ppgSweepKeeps(t, m, prd, task, store, q, QuestionOpen)
}

// A daemon-only hold in a task worktree that still exists is still released.
func TestReleaseStillReleasesDaemonOnlyHoldInTaskWorktree(t *testing.T) {
	m, prd, task, _, repo := fpfRig(t)
	base, _, taskDir := ppgWorktrees(t, m, prd, task, repo, "AGENT.md", dscBaseRules)
	ppgWrite(t, taskDir, "AGENT.md", dscDaemonOnlyRules())
	ppgHold(t, m, prd, task, "AGENT.md", base)

	if got := m.ReleaseFalseProtectedPathHolds(context.Background()); !sameSet(got, []string{task.ID}) {
		t.Fatalf("released = %v; want %v", got, task.ID)
	}
}

// An answer is the operator's decision. The sweep never overrides it, even when
// the tree shows only the daemon's sections.
func TestReleaseKeepsAnsweredHold(t *testing.T) {
	for _, applied := range []bool{true, false} {
		name := "answer recorded, not yet applied"
		if applied {
			name = "change_spec applied"
		}
		t.Run(name, func(t *testing.T) {
			m, prd, task, store, repo := fpfRig(t)
			base := ppgCommit(t, repo, "AGENT.md", dscBaseRules)
			ppgWrite(t, repo, "AGENT.md", dscDaemonOnlyRules())
			q := ppgHold(t, m, prd, task, "AGENT.md", base)
			ans, err := store.Answer(q, Answer{Actor: "operator", Surface: "cli",
				ChosenOptionID: ProtectedPathsOptionChangeSpec, Quote: "the change is not approved"})
			if err != nil {
				t.Fatalf("Answer: %v", err)
			}
			if applied {
				question, _ := store.Get(q)
				m.resolveProtectedPaths(QuestionChange{Kind: QuestionChangeAnswered, Question: question, Answer: &ans})
			}
			ppgSweepKeeps(t, m, prd, task, store, q, QuestionAnswered)
		})
	}
}

// A hold whose question cannot be withdrawn stays held.
func TestReleaseKeepsHoldWhenWithdrawFails(t *testing.T) {
	m, prd, task, store, repo := fpfRig(t)
	base := ppgCommit(t, repo, "AGENT.md", dscBaseRules)
	ppgWrite(t, repo, "AGENT.md", dscDaemonOnlyRules())
	q := ppgHold(t, m, prd, task, "AGENT.md", base)
	m.heldAnswerPaths = &heldAnswerPaths{withdraw: func(string, string, string) error {
		return errors.New("the question store is read-only")
	}}
	ppgSweepKeeps(t, m, prd, task, store, q, QuestionOpen)
}

// The commit gate never clears a file the daemon does not write into.
func TestProtectedPathsGateHoldsFilesWithoutDaemonSections(t *testing.T) {
	cases := map[string]struct{ rel, base, cur string }{
		// git lists a mode-only change although the content is identical.
		"identical content":             {ppgCIPath, ppgCIBase, ppgCIBase},
		"CI file with blank lines only": {ppgCIPath, ppgCIBase, ppgCIBase + "\n\n"},
		"CI step after a fake heading":  {ppgCIPath, ppgCIBase, ppgCIBase + "\n# Session Guardrails\n      - run: ./unapproved.sh\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newGateRig(t)
			ppgWrite(t, r.prd.ProjectDir, c.rel, c.cur)
			r.m.SetConfig(withProtected(r.m.Config(), []string{c.rel}, config.ProtectedPathsActionHold))
			r.git.diff = c.rel + "\n"
			r.git.show = map[string]string{gateBaseSHA + ":" + c.rel: c.base}
			held, err := r.check(t)
			if err != nil || !held {
				t.Fatalf("held=%v err=%v; want held", held, err)
			}
		})
	}
}
