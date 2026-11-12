// Tests for the project resolution of the Sessions grouping
// (session_group_project.go): a worktree-based Automaton session with the
// project aliases registered and no project tag, a plain session in the
// repository folder, deterministic choice among candidate tags, and the
// folder fallback firing only when nothing is known.

package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// gitWorktreeFixture makes a real repository and a linked worktree of it and
// returns both folders. The worktree's .git file points at the repository.
func gitWorktreeFixture(t *testing.T) (repo, worktree string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	repo = filepath.Join(root, "datawatch")
	worktree = filepath.Join(root, "prd-worktrees", "b47d0001", "task")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	run(repo, "init", "-q")
	run(repo, "commit", "-q", "--allow-empty", "-m", "init")
	if err := os.MkdirAll(filepath.Dir(worktree), 0o750); err != nil {
		t.Fatal(err)
	}
	run(repo, "worktree", "add", "-q", "-b", "wt", worktree)
	return repo, worktree
}

func testGroupResolver(repo string, prds map[string][2]string) *groupProjectResolver {
	cfg := &config.Config{Projects: map[string]config.ProjectConfigEntry{
		"datawatch": {Dir: repo},
		"other":     {Dir: "/nowhere/other"},
	}}
	prd := func(id string) (string, string, bool) {
		v, ok := prds[id]
		return v[0], v[1], ok
	}
	return newGroupProjectResolver(cfg, prd, nil)
}

func TestGroupProjectWorktreeAutomatonSessionNoTag(t *testing.T) {
	repo, wt := gitWorktreeFixture(t)
	r := testGroupResolver(repo, map[string][2]string{"prd1": {"Fix grouping", wt}})
	auto := &session.Session{ID: "a", PRDID: "prd1", ProjectDir: wt} // no project tag
	plain := &session.Session{ID: "p", ProjectDir: repo}
	plainWT := &session.Session{ID: "w", ProjectDir: wt}
	for _, sess := range []*session.Session{auto, plain, plainWT} {
		if got := r.sessionProjectKey(sess); got != "datawatch" {
			t.Fatalf("session %s resolved to %q, want datawatch", sess.ID, got)
		}
	}
}

func TestGroupProjectOwnerBeatsWorktreeTag(t *testing.T) {
	repo, wt := gitWorktreeFixture(t)
	r := testGroupResolver(repo, map[string][2]string{"prd1": {"Fix grouping", wt}})
	sess := &session.Session{ID: "a", PRDID: "prd1", ProjectDir: wt, SystemTags: []string{"project:task"}}
	if got := r.sessionProjectKey(sess); got != "datawatch" {
		t.Fatalf("worktree-derived tag shadowed the owner's project: %q", got)
	}
}

func TestGroupProjectCandidateTagsDeterministic(t *testing.T) {
	r := testGroupResolver("/nowhere/repo", nil)
	for _, tags := range [][]string{{"project:zeta", "project:alpha"}, {"project:alpha", "project:zeta", "project:alpha"}} {
		if got := r.sessionProjectKey(&session.Session{ID: "s", SystemTags: tags}); got != "alpha" {
			t.Fatalf("tags %v -> %q, want alpha", tags, got)
		}
	}
}

func TestGroupProjectFallbacksOnlyWhenNothingKnown(t *testing.T) {
	r := testGroupResolver("/nowhere/repo", map[string][2]string{"prd1": {"Fix grouping", "/gone/worktree"}, "prd2": {"", "/gone/worktree"}})
	// Automaton known, no project: its title, never the worktree folder.
	if got := r.sessionProjectKey(&session.Session{ID: "a", PRDID: "prd1", ProjectDir: "/gone/worktree"}); got != "Fix grouping" {
		t.Fatalf("Automaton fallback = %q, want its title", got)
	}
	if got := r.sessionProjectKey(&session.Session{ID: "b", PRDID: "prd2", ProjectDir: "/gone/worktree"}); got != "prd2" {
		t.Fatalf("untitled Automaton fallback = %q, want its id", got)
	}
	// Nothing known: the working folder, then ungrouped (empty key on rows).
	if got := r.sessionProjectKey(&session.Session{ID: "c", ProjectDir: "/elsewhere/x"}); got != "/elsewhere/x" {
		t.Fatalf("folder fallback = %q", got)
	}
	if got := r.sessionProjectKey(&session.Session{ID: "d"}); got != "" {
		t.Fatalf("nothing known = %q, want empty", got)
	}
}

func TestGroupProjectBaseRepoWhenFolderGone(t *testing.T) {
	repo, _ := gitWorktreeFixture(t)
	cfg := &config.Config{Projects: map[string]config.ProjectConfigEntry{"datawatch": {Dir: repo}}}
	cfg.Autonomous.WorktreeBaseRepo = repo
	prd := func(string) (string, string, bool) { return "T", filepath.Join(t.TempDir(), "removed"), true }
	r := newGroupProjectResolver(cfg, prd, nil)
	if got := r.sessionProjectKey(&session.Session{ID: "a", PRDID: "prd1"}); got != "datawatch" {
		t.Fatalf("base repository not used when the worktree is gone: %q", got)
	}
}

func TestGroupSessionsProjectAxisWorktreeRows(t *testing.T) {
	repo, wt := gitWorktreeFixture(t)
	r := testGroupResolver(repo, map[string][2]string{"prd1": {"Fix grouping", wt}})
	var rows []sessionGroupRow
	for _, sess := range []*session.Session{
		{ID: "a", State: session.StateRunning, PRDID: "prd1", ProjectDir: wt},
		{ID: "p", State: session.StateRunning, ProjectDir: repo},
	} {
		var row sessionGroupRow
		row.ID, row.State = sess.ID, sess.State
		r.fill(&row, sess)
		rows = append(rows, row)
	}
	g, err := GroupSessions(rows, "project", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Groups) != 1 || g.Groups[0].Key != "datawatch" || g.Groups[0].Count != 2 {
		t.Fatalf("groups = %+v, want one datawatch group of 2", g.Groups)
	}
}
