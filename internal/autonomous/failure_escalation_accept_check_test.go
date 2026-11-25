package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// acceptCase is a held task in a git project whose last worker session printed
// the completion marker. Each test adds the evidence it wants Accept to see.
type acceptCase struct {
	m    *Manager
	api  *API
	repo string
	prd  *PRD
	task *Task
}

func newAcceptCase(t *testing.T) *acceptCase {
	t.Helper()
	repo := snapTestRepo(t)
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, repo, nil)
	task, _ := heldTaskOf(t, m, api, prd)
	setHeldSession(t, m, prd.ID, task.ID, "sess-done")
	m.SetSessionOutputFn(func(string) (string, bool) { return "DATAWATCH_COMPLETE: all good\n", true })
	return &acceptCase{m: m, api: api, repo: repo, prd: prd, task: escalationTask(t, m, prd.ID)}
}

// edit changes the held task under the store lock.
func (c *acceptCase) edit(t *testing.T, fn func(*Task)) {
	t.Helper()
	task := escalationTask(t, c.m, c.prd.ID)
	c.m.store.WithLock(func() { fn(task) })
	if err := c.m.store.SaveTask(task); err != nil {
		t.Fatal(err)
	}
}

func (c *acceptCase) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(c.repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commit writes the files and commits exactly them under subject.
func (c *acceptCase) commit(t *testing.T, subject string, files ...string) string {
	t.Helper()
	for _, f := range files {
		c.write(t, f, subject+"\n")
		snapTestGit(t, c.repo, "add", "-f", f)
	}
	snapTestGit(t, c.repo, "commit", "-q", "-m", subject)
	return snapTestGit(t, c.repo, "rev-parse", "HEAD")
}

// taskSubject is the subject the daemon gives the held task's own commit.
func (c *acceptCase) taskSubject() string {
	return "task(" + c.prd.ID[:min(8, len(c.prd.ID))] + "): " + c.task.Title
}

func (c *acceptCase) accept() error {
	_, err := c.api.ResolveTaskReview(c.prd.ID, c.task.ID, ReviewResolution{Action: reviewActionAccept}, "alice")
	return err
}

func (c *acceptCase) wantAccepted(t *testing.T) {
	t.Helper()
	if err := c.accept(); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got := escalationTask(t, c.m, c.prd.ID); got.Status != TaskCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
}

func (c *acceptCase) wantRefused(t *testing.T, parts ...string) {
	t.Helper()
	err := c.accept()
	if err == nil {
		t.Fatalf("accept succeeded, want a refusal naming %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("refusal %q does not name %q", err, p)
		}
	}
	got := escalationTask(t, c.m, c.prd.ID)
	if got.Status != TaskReviewNeeded || got.CompletedAt != nil || got.ReviewSummary == nil {
		t.Fatalf("refused accept changed the task: status=%q completed=%v", got.Status, got.CompletedAt)
	}
}

func (c *acceptCase) lastAcceptNote(t *testing.T) string {
	t.Helper()
	cur, _ := c.m.store.GetPRD(c.prd.ID)
	for i := len(cur.Decisions) - 1; i >= 0; i-- {
		if d := cur.Decisions[i]; d.Kind == reviewDecisionKind && strings.Contains(d.Note, "accepted by") {
			return d.Note
		}
	}
	t.Fatal("no accept decision recorded")
	return ""
}

func TestAcceptCheck_RealCommitAndFilesAccepted(t *testing.T) {
	c := newAcceptCase(t)
	c.edit(t, func(tk *Task) { tk.FilesPlanned = []string{"pkg/a.go"} })
	sha := c.commit(t, c.taskSubject(), "pkg/a.go")
	var audited map[string]any
	c.m.mu.Lock()
	c.m.auditFn = func(a string, d map[string]any) {
		if a == "autonomous_review_accept" {
			audited = d
		}
	}
	c.m.mu.Unlock()
	c.wantAccepted(t)
	if audited["commit"] != sha {
		t.Errorf("audited commit = %v, want %s", audited["commit"], sha)
	}
	if got := escalationTask(t, c.m, c.prd.ID); got.PostVerifySHA != sha {
		t.Errorf("PostVerifySHA = %q, want %q", got.PostVerifySHA, sha)
	}
}

// The subjects come from the daemon's own commit helpers, so the match cannot
// drift from what the daemon writes. The long, uneven title is normalised.
func TestAcceptCheck_DaemonCommitHelpers(t *testing.T) {
	title := "Fix:   the " + strings.Repeat("very ", 30) + "long\ntitle"
	t.Run("snapshot only", func(t *testing.T) {
		c := newAcceptCase(t)
		c.edit(t, func(tk *Task) { tk.Title = title })
		c.write(t, "pkg/a.go", "a\n")
		if err := session.NewProjectGit(c.repo).PreSessionCommit(c.prd.ID, title, []string{c.repo}); err != nil {
			t.Fatal(err)
		}
		c.wantRefused(t, "no task commit", "snapshot before task")
	})
	t.Run("task commit", func(t *testing.T) {
		c := newAcceptCase(t)
		c.edit(t, func(tk *Task) { tk.Title, tk.FilesPlanned = title, []string{"pkg/a.go"} })
		c.write(t, "pkg/a.go", "a\n")
		sha, err := session.NewProjectGit(c.repo).CommitTaskWork("sess-done",
			session.TaskCommitLabel{PRDID: c.prd.ID, TaskTitle: title, Prompt: "do task 1"}, []string{c.repo})
		if err != nil || sha == "" {
			t.Fatalf("commit: sha=%q err=%v", sha, err)
		}
		c.wantAccepted(t)
	})
}

func TestAcceptCheck_SnapshotOnlyRefused(t *testing.T) {
	c := newAcceptCase(t)
	snap := "snapshot before task " + c.task.Title + " (PRD " + c.prd.ID + ")"
	sha := c.commit(t, snap, "pkg/a.go")
	// A snapshot recorded as an attempt commit does not count either.
	c.edit(t, func(tk *Task) { tk.RecordAttemptCommit(sha) })
	c.wantRefused(t, "no task commit", "snapshot before task")
}

func TestAcceptCheck_NoCommitRefused(t *testing.T) {
	c := newAcceptCase(t)
	c.wantRefused(t, "no task commit")
}

func TestAcceptCheck_OtherCommitsDoNotCount(t *testing.T) {
	c := newAcceptCase(t)
	short := c.prd.ID[:min(8, len(c.prd.ID))]
	c.commit(t, "task("+short+"): Some other task", "pkg/other.go")
	c.commit(t, "task(ffffffff): "+c.task.Title, "pkg/foreign.go")
	c.commit(t, c.task.Title+" notes and more", "pkg/notes.go")
	c.wantRefused(t, "no task commit")
}

func TestAcceptCheck_OlderCommitWithSameTitleDoesNotCount(t *testing.T) {
	c := newAcceptCase(t)
	c.write(t, "pkg/old.go", "old\n")
	snapTestGit(t, c.repo, "add", "pkg/old.go")
	t.Setenv("GIT_COMMITTER_DATE", "2020-01-01T00:00:00Z")
	snapTestGit(t, c.repo, "commit", "-q", "-m", "fix: "+c.task.Title)
	c.wantRefused(t, "no task commit")
}

func TestAcceptCheck_WorkerCommitNamedByTitleAccepted(t *testing.T) {
	c := newAcceptCase(t)
	c.commit(t, "fix: "+c.task.Title, "pkg/a.go")
	c.wantAccepted(t)
}

func TestAcceptCheck_MissingNamedFileRefused(t *testing.T) {
	for name, set := range map[string]func(*Task){
		"files":           func(tk *Task) { tk.FilesPlanned = []string{"pkg/a.go", "pkg/missing.go"} },
		"files_generated": func(tk *Task) { tk.FilesGenerated = []string{"pkg/missing.go"} },
		"escaping path":   func(tk *Task) { tk.FilesPlanned = []string{"../pkg/missing.go"} },
	} {
		t.Run(name, func(t *testing.T) {
			c := newAcceptCase(t)
			c.edit(t, set)
			c.commit(t, c.taskSubject(), "pkg/a.go")
			c.wantRefused(t, "missing", "pkg/missing.go")
		})
	}
}

// A file in the task commit counts even when the Automaton worktree no longer
// has it checked out, and so does a file only in the Automaton worktree.
func TestAcceptCheck_FileInCommitOrWorktree(t *testing.T) {
	c := newAcceptCase(t)
	c.edit(t, func(tk *Task) {
		tk.FilesPlanned = []string{"pkg/a.go", "pkg/", ".datawatch/verify/x.sh"}
	})
	c.commit(t, c.taskSubject(), "pkg/a.go")
	if err := os.Remove(filepath.Join(c.repo, "pkg/a.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(c.repo, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	c.write(t, ".datawatch/verify/x.sh", "true\n")
	c.wantAccepted(t)
}

// The commit sits on the task branch only: it is found there, and its tree
// proves the named file.
func TestAcceptCheck_CommitOnTaskBranch(t *testing.T) {
	c := newAcceptCase(t)
	base := snapTestGit(t, c.repo, "rev-parse", "--abbrev-ref", "HEAD")
	snapTestGit(t, c.repo, "checkout", "-q", "-b", "automaton/x--tasks-y")
	c.commit(t, c.taskSubject(), "pkg/a.go")
	snapTestGit(t, c.repo, "checkout", "-q", base)
	c.edit(t, func(tk *Task) {
		tk.FilesPlanned = []string{"pkg/a.go"}
		tk.WorktreeBranch = "automaton/x--tasks-y"
		tk.WorktreePath = filepath.Join(c.repo, "no-such-worktree")
	})
	c.wantAccepted(t)
}

func TestAcceptCheck_RetriedTaskPreTaskSHAIsItsCommit(t *testing.T) {
	c := newAcceptCase(t)
	sha := c.commit(t, c.taskSubject(), "pkg/a.go")
	c.edit(t, func(tk *Task) {
		tk.FilesPlanned = []string{"pkg/a.go"}
		tk.PreTaskSHA, tk.BaseSHA = sha, sha
	})
	c.wantAccepted(t)
}

func TestAcceptCheck_ReadOnly(t *testing.T) {
	const report = ".datawatch/reports/audit.md"
	t.Run("report present, no commit", func(t *testing.T) {
		c := newAcceptCase(t)
		c.edit(t, func(tk *Task) { tk.ReadOnly, tk.ReportPath = true, report })
		c.write(t, report, "findings\n")
		c.wantAccepted(t)
	})
	t.Run("report missing", func(t *testing.T) {
		c := newAcceptCase(t)
		c.edit(t, func(tk *Task) { tk.ReadOnly, tk.ReportPath = true, report })
		c.wantRefused(t, "missing", report)
	})
	t.Run("nothing to prove it", func(t *testing.T) {
		c := newAcceptCase(t)
		c.edit(t, func(tk *Task) { tk.ReadOnly = true })
		c.wantRefused(t, "no report path")
	})
}

func TestAcceptCheck_VerificationScript(t *testing.T) {
	const script = ".datawatch/verify/check.sh"
	setup := func(t *testing.T, body string) *acceptCase {
		c := newAcceptCase(t)
		c.edit(t, func(tk *Task) { tk.VerificationScript = script })
		c.commit(t, c.taskSubject(), "pkg/a.go")
		if body != "" {
			c.write(t, script, body)
		}
		return c
	}
	t.Run("passes, niced, outside tmux", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/fake,1,0")
		t.Setenv("TMUX_TMPDIR", "/tmp/fake")
		setup(t, `[ -z "$TMUX$TMUX_TMPDIR" ] || exit 7
[ "$(nice)" -ge 19 ] || exit 8
test -f pkg/a.go
`).wantAccepted(t)
	})
	t.Run("fails", func(t *testing.T) {
		setup(t, "echo two tests broke\nexit 3\n").wantRefused(t, script, "exit", "3", "two tests broke")
	})
	t.Run("script missing", func(t *testing.T) {
		setup(t, "").wantRefused(t, script, "missing")
	})
	t.Run("runs past the limit", func(t *testing.T) {
		c := setup(t, "sleep 30\n")
		c.m.mu.Lock()
		c.m.cfg.ScriptGateTimeoutSeconds = 1
		c.m.mu.Unlock()
		c.wantRefused(t, script, "limit")
	})
}

func waitForAcceptMarker(t *testing.T, path string) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	t.Fatalf("%s never appeared: the task's check did not run", path)
}

// The check can run for minutes. A decision taken on the task meanwhile wins:
// the accept must not complete a task that is no longer held.
func TestAcceptCheck_TaskChangedDuringCheck(t *testing.T) {
	c := newAcceptCase(t)
	const script = ".datawatch/verify/check.sh"
	c.edit(t, func(tk *Task) { tk.VerificationScript = script })
	c.commit(t, c.taskSubject(), "pkg/a.go")
	started, release := filepath.Join(t.TempDir(), "started"), filepath.Join(t.TempDir(), "release")
	c.write(t, script, "touch '"+started+"'\nwhile [ ! -f '"+release+"' ]; do sleep 0.05; done\n")
	done := make(chan error, 1)
	go func() { done <- c.accept() }()
	waitForAcceptMarker(t, started)
	c.edit(t, func(tk *Task) { tk.Status = TaskCancelled })
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("accept completed a task that left review during the check")
	}
	if got := escalationTask(t, c.m, c.prd.ID); got.Status != TaskCancelled {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
}

func TestAcceptCheck_FilesOutsideNamedOnlyWarn(t *testing.T) {
	c := newAcceptCase(t)
	c.edit(t, func(tk *Task) { tk.FilesPlanned = []string{"pkg/a.go"} })
	c.commit(t, c.taskSubject(), "pkg/a.go", "docs/extra.md")
	c.wantAccepted(t)
	if note := c.lastAcceptNote(t); !strings.Contains(note, "warning") || !strings.Contains(note, "docs/extra.md") {
		t.Errorf("accept decision does not warn about docs/extra.md: %q", note)
	}
}

func TestAcceptCheck_NoGitProjectRefused(t *testing.T) {
	c := newAcceptCase(t)
	if err := os.RemoveAll(filepath.Join(c.repo, ".git")); err != nil {
		t.Fatal(err)
	}
	c.wantRefused(t, "no task commit")
}
