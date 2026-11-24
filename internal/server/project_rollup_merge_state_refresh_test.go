package server

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// rollupGit runs one git command in dir with a fixed identity.
func rollupGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func rollupLineMergeState(t *testing.T, s *Server, project, id string) string {
	t.Helper()
	var r projectRollup
	mustDecodeRollup(t, rollupHTTP(t, s, project).Body.String(), &r)
	var all []rollupAutomaton
	all = append(all, r.Unassigned.Automata...)
	for _, a := range r.Arcs {
		all = append(all, a.Automata...)
	}
	for _, a := range all {
		if a.ID == id {
			return a.MergeState
		}
	}
	t.Fatalf("automaton %s not in the roll-up: %+v", id, r.Unassigned)
	return ""
}

// TestProjectRollup_MergeStateFollowsGitWithoutStoreWrite: a finished
// Automaton's work branch is merged by hand, which writes nothing to the
// Automaton store. The roll-up used to be keyed only by the store revision,
// so it kept serving finished_not_merged until some Automaton was written.
// A roll-up that holds finished work with a branch must be rebuilt once the
// merge-state window has passed.
func TestProjectRollup_MergeStateFollowsGitWithoutStoreWrite(t *testing.T) {
	repo := t.TempDir()
	rollupGit(t, repo, "init", "-q", "-b", "main")
	rollupGit(t, repo, "commit", "-q", "--allow-empty", "-m", "base")
	rollupGit(t, repo, "checkout", "-q", "-b", "automaton/fin1")
	rollupGit(t, repo, "commit", "-q", "--allow-empty", "-m", "work")
	rollupGit(t, repo, "checkout", "-q", "main")

	cfg := autonomous.DefaultConfig()
	cfg.WorktreeBaseRepo = repo
	mgr, err := autonomous.NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetMergeStateCacheWindow(30 * time.Millisecond)
	s := &Server{cfg: config.DefaultConfig(), autonomousMgr: newTestAPI(mgr)}
	s.cfg.Projects = map[string]config.ProjectConfigEntry{"acme": {Dir: repo}}

	done := time.Now()
	fin := rollupStoredPRD(t, mgr, repo, autonomous.PRDCompleted,
		autonomous.Task{ID: "t1", Status: autonomous.TaskCompleted, CompletedAt: &done})
	fin.Git.Branch = "automaton/fin1"
	fin.Git.BaseBranch = "main"
	if err := mgr.Store().SavePRD(fin); err != nil {
		t.Fatal(err)
	}

	if got := rollupLineMergeState(t, s, "acme", fin.ID); got != autonomous.MergeStateFinishedNotMerged {
		t.Fatalf("before the merge: merge_state = %q, want %q", got, autonomous.MergeStateFinishedNotMerged)
	}

	// Land the branch by hand: no store write, so the revision does not move.
	revision := mgr.Store().Revision()
	rollupGit(t, repo, "merge", "-q", "--no-ff", "-m", "land", "automaton/fin1")
	if mgr.Store().Revision() != revision {
		t.Fatal("the test must not write to the Automaton store")
	}

	// Past the merge-state window (the roll-up's merge proof is remembered
	// for the negative TTL, so wait that out through the same clock the
	// proof cache uses).
	time.Sleep(80 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		if got = rollupLineMergeState(t, s, "acme", fin.ID); got == "" {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("after the branch landed and the window passed: merge_state = %q, want empty (the roll-up served a stale cached body)", got)
}

// TestProjectRollup_NoBranchWorkStaysCachedPastTheWindow: a roll-up with no
// finished work on a branch has no merge state to go stale, so it keeps its
// cache past the window and the expiry does not add rebuilds for it.
func TestProjectRollup_NoBranchWorkStaysCachedPastTheWindow(t *testing.T) {
	dir := t.TempDir()
	s, mgr := newAutomationRESTServer(t, t.TempDir())
	mgr.SetMergeStateCacheWindow(20 * time.Millisecond)
	s.cfg.Projects = map[string]config.ProjectConfigEntry{"acme": {Dir: dir}}
	rollupStoredPRD(t, mgr, dir, autonomous.PRDRunning, autonomous.Task{ID: "t1", Status: autonomous.TaskPending})

	first := rollupHTTP(t, s, "acme").Body.String()
	time.Sleep(60 * time.Millisecond)
	if again := rollupHTTP(t, s, "acme").Body.String(); again != first {
		t.Fatal("a roll-up with no finished branch work must keep serving the cached body")
	}
}

// TestRollupCache_GetFreshExpiresByAge: a stored body is served until it is
// older than maxAge, then rebuilt; maxAge zero never expires.
func TestRollupCache_GetFreshExpiresByAge(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newRollupCache(4)
	c.now = func() time.Time { return now }
	key := rollupCacheKey{project: "p", windowDays: 7, revision: 1}
	builds := 0
	build := func() []byte { builds++; return []byte("b") }

	c.getFresh(key, time.Minute, build)
	now = now.Add(time.Minute)
	if _, hit := c.getFresh(key, time.Minute, build); !hit || builds != 1 {
		t.Fatalf("a body exactly at maxAge must still be served: hit=%v builds=%d", hit, builds)
	}
	now = now.Add(time.Second)
	if _, hit := c.getFresh(key, time.Minute, build); hit || builds != 2 {
		t.Fatalf("a body past maxAge must be rebuilt: hit=%v builds=%d", hit, builds)
	}
	now = now.Add(24 * time.Hour)
	if _, hit := c.getFresh(key, 0, build); !hit || builds != 2 {
		t.Fatalf("maxAge 0 must never expire: hit=%v builds=%d", hit, builds)
	}
	if c.len() != 1 {
		t.Fatalf("a rebuild must replace the entry, len=%d", c.len())
	}
}
