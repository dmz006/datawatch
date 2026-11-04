package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCommitOnCompletion_RecordsStagingSkipsOverSession exercises the
// completion-commit path end to end and asserts that the session itself
// records every candidate the staging pass refused — each with its reason —
// instead of the operator having to mine the log:
//
//   - a file matching a never-stage pattern             → "never-stage"
//   - a file .gitignore rules ignore (git check-ignore) → "ignored"
//   - a file outside the session's declared write scope → "outside-plan"
//
// while the in-scope, in-plan change still lands in the commit.
func TestCommitOnCompletion_RecordsStagingSkipsOverSession(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)

	scopeDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(filepath.Join(scopeDir, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Baseline: the in-scope work, the never-stage file and the ignored
	// file all start tracked so every refusal below is a *change* git
	// reports. The .gitignore rule that hides docs/report.txt is committed
	// in a second commit, after the file, because check-ignore exempts
	// files sitting in the index.
	for _, f := range []struct {
		path    string
		content string
	}{{
		filepath.Join(scopeDir, "notes.md"), "baseline notes\n",
	}, {
		filepath.Join(scopeDir, "secrets", "api.key"), "key\n",
	}, {
		filepath.Join(scopeDir, "report.txt"), "report v1\n",
	}} {
		if err := os.WriteFile(f.path, []byte(f.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", f.path, err)
		}
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-m", "baseline")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("report.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "--", ".gitignore")
	mustGit(t, dir, "commit", "-m", "baseline .gitignore")

	// The run's changes: one in-plan edit plus the three refusals.
	mustWrite(t, filepath.Join(scopeDir, "notes.md"), "planned notes\n")                 // in scope: must be committed
	mustWrite(t, filepath.Join(scopeDir, "secrets", "api.key"), "rotated-key\n")         // never-stage
	mustGit(t, dir, "rm", "--cached", "-q", "--", filepath.Join(scopeDir, "report.txt")) // ignored
	mustWrite(t, filepath.Join(dir, "scratch-x.txt"), "scratch\n")                       // outside-plan

	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err != nil {
		t.Fatal(err)
	}
	mgr := &Manager{store: store, hostname: "testhost"}

	id := "c0af"
	fullID := "testhost-c0af"
	now := time.Now()
	sess := &Session{
		ID:               id,
		FullID:           fullID,
		Hostname:         "testhost",
		State:            StateRunning,
		CreatedAt:        now,
		UpdatedAt:        now,
		OneShot:          true,
		Git:              true,
		ValidateCommands: []string{"true"},
		ProjectDir:       dir,
		Task:             "record staging skips",
		WriteScope:       []string{scopeDir},
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}

	g.SetNeverStagePatterns([]string{"docs/secrets/"})
	mgr.commitOnCompletion(sess, g)

	// 1. The in-scope change still committed, and the session recorded it.
	if sess.CommitSHA == "" {
		t.Fatal("expected a completion commit; CommitSHA empty")
	}
	head, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if sess.CommitSHA != strings.TrimSpace(head) {
		t.Errorf("CommitSHA = %q, want HEAD %q", sess.CommitSHA, strings.TrimSpace(head))
	}
	notes, err := gitOutput(dir, "show", "HEAD:docs/notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notes, "planned notes") {
		t.Errorf("in-scope change not committed: HEAD:docs/notes.md = %q", notes)
	}

	// 2. Each skipped path recorded on the session with its reason.
	skips := map[string]string{}
	for _, s := range sess.StagingSkipped {
		skips[s.Path] = s.Reason
	}
	if len(skips) != 3 {
		t.Errorf("session.StagingSkipped = %v, want exactly 3 entries", sess.StagingSkipped)
	}
	for _, want := range []struct{ path, reason string }{
		{"docs/secrets/api.key", "never-stage"},
		{"docs/report.txt", "ignored"},
		{"scratch-x.txt", "outside-plan"},
	} {
		if got := skips[want.path]; got != want.reason {
			t.Errorf("skip[%s] = %q, want %q (all: %v)", want.path, got, want.reason, skips)
		}
	}

	// 3. The skipped paths never entered the commit.
	keyAt, err := gitOutput(dir, "show", "HEAD:docs/secrets/api.key")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(keyAt, "rotated-key") {
		t.Error("never-stage path leaked into the commit")
	}
	tree, err := gitOutput(dir, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tree, "docs/report.txt") {
		t.Errorf("ignored path still tracked at HEAD:\n%s", tree)
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch-x.txt")); err != nil {
		t.Errorf("outside-plan file should remain untracked on disk: %v", err)
	}

	// 4. The recorded skips survive the session round-trip through the
	// store — that is what the UI / operator reads.
	got, ok := store.Get(fullID)
	if !ok {
		t.Fatal("session missing from store")
	}
	if len(got.StagingSkipped) != 3 {
		t.Errorf("stored session staging_skipped = %v, want 3 entries", got.StagingSkipped)
	}
}

// TestCommitOnCompletion_NoSkipsLeavesSessionListEmpty: when nothing is
// refused, the recorded list is empty, not stale.
func TestCommitOnCompletion_NoSkipsLeavesSessionListEmpty(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)

	scopeDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopeDir, "notes.md"), []byte("planned notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	t.Cleanup(func() { _ = store.Close() })
	if err != nil {
		t.Fatal(err)
	}
	mgr := &Manager{store: store, hostname: "testhost"}

	id := "d1be"
	fullID := "testhost-d1be"
	now := time.Now()
	sess := &Session{
		ID:               id,
		FullID:           fullID,
		Hostname:         "testhost",
		State:            StateRunning,
		CreatedAt:        now,
		UpdatedAt:        now,
		OneShot:          true,
		Git:              true,
		ValidateCommands: []string{"true"},
		ProjectDir:       dir,
		Task:             "clean run",
		WriteScope:       []string{scopeDir},
	}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}

	mgr.commitOnCompletion(sess, g)

	if sess.CommitSHA == "" {
		t.Fatal("expected a completion commit")
	}
	if len(sess.StagingSkipped) != 0 {
		t.Errorf("empty-skip run recorded %v, want nothing", sess.StagingSkipped)
	}
	got, ok := store.Get(fullID)
	if !ok {
		t.Fatal("session missing from store")
	}
	if len(got.StagingSkipped) != 0 {
		t.Errorf("stored session staging_skipped = %v, want empty", got.StagingSkipped)
	}
}

// mustWrite is os.WriteFile that fails the test on error.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
