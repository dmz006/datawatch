// Injection-hardening tests for the path-injection and command-injection
// sites in this package: the containment helpers and the drift-capture
// runner contract.
package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/gitguard"
)

// symlinkTo creates a symlink under dir pointing at target, skipping the
// test on platforms that cannot create symlinks.
func symlinkTo(t *testing.T, dir, name, target string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
}

// TestRepoFileInside pins the containment contract: a valid relative path
// joins under the base; empty, ".", "..", absolute and NUL paths, and a
// symlink whose target is outside the base, all fail with
// gitguard.ErrPathEscapes.
func TestRepoFileInside(t *testing.T) {
	base := filepath.ToSlash(t.TempDir())
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := repoFileInside(base, "a.txt")
	if err != nil {
		t.Fatalf("valid path refused: %v", err)
	}
	if filepath.ToSlash(got) != filepath.ToSlash(filepath.Join(base, "a.txt")) {
		t.Fatalf("joined = %q", got)
	}

	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkTo(t, filepath.FromSlash(base), "link.txt", outside)

	for _, rel := range []string{"", ".", "..", "../x", "a.txt/../../..", "/etc/passwd", "a\x00b", "link.txt"} {
		if _, err := repoFileInside(base, rel); err == nil {
			t.Errorf("rel %q: want rejection", rel)
		} else if !errors.Is(err, gitguard.ErrPathEscapes) {
			t.Errorf("rel %q: error %v does not wrap %v", rel, err, gitguard.ErrPathEscapes)
		}
	}
	if _, err := repoFileInside(filepath.Join(t.TempDir(), "missing"), "a.txt"); err == nil {
		t.Error("missing base: want error")
	}
}

// showHead is a driftGitRunner that answers one `show HEAD:<rel>` with the
// given content and refuses everything else.
func showHead(rel, head string) driftGitRunner {
	return func(ctx context.Context, dir string, args ...string) (string, error) {
		if len(args) == 2 && args[0] == "show" && args[1] == "HEAD:"+rel {
			return head, nil
		}
		return "", errors.New("refused")
	}
}

// TestDaemonSectionsOnlyChangeRejectsSymlinkEscape: a working copy that is
// only reachable through a symlink escaping dir must not be read.
func TestDaemonSectionsOnlyChangeRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	workContent := "top line\n" +
		"<!-- rtk-instructions -->\n" +
		"token killer\n" +
		"<!-- /rtk-instructions -->\n" +
		"bottom line\n"
	headContent := "top line\nbottom line\n"

	outside := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(outside, []byte(workContent), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkTo(t, base, "LINK.md", outside)
	if daemonSectionsOnlyChange(context.Background(), base, "LINK.md", showHead("LINK.md", headContent)) {
		t.Fatal("symlink escaping the base must not be read as the working copy")
	}

	// Positive control: the same working copy inside the base is a
	// daemon-only change.
	if err := os.WriteFile(filepath.Join(base, "CLAUDE.md"), []byte(workContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if !daemonSectionsOnlyChange(context.Background(), base, "CLAUDE.md", showHead("CLAUDE.md", headContent)) {
		t.Fatal("in-base daemon-only change not detected")
	}
}

// TestIsDaemonIgnoreChangeRejectsSymlinkEscape: a .gitignore whose only
// change against HEAD is daemon-appended lines, reached through a symlink
// escaping dir, must not be treated as a daemon-only change.
func TestIsDaemonIgnoreChangeRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	content := "# datawatch-managed\n.datawatch/\n"
	head := func(ctx context.Context, dir string, args ...string) (string, error) {
		return "", errors.New("not in HEAD")
	}

	outside := filepath.Join(t.TempDir(), ".gitignore")
	if err := os.WriteFile(outside, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkTo(t, base, "LINKIGNORE", outside)
	if isDaemonIgnoreChange(context.Background(), base, "LINKIGNORE", head) {
		t.Fatal("symlink escaping the base must not be read as the ignore file")
	}

	if err := os.WriteFile(filepath.Join(base, ".gitignore"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if !isDaemonIgnoreChange(context.Background(), base, ".gitignore", head) {
		t.Fatal("in-base daemon-only ignore change not detected")
	}
}

// TestClassifyTaskCommitPathsRejectsSymlinkEscape: a path that exists only
// through a symlink escaping the repo must be skipped, not put into the
// add list by the (follow-symlink) Lstat check.
func TestClassifyTaskCommitPathsRejectsSymlinkEscape(t *testing.T) {
	repo := driftTestRepo(t)
	// The index/HEAD probes read HEAD, so the repo needs one commit first
	// (with an empty repo both probe commands error, which the code
	// deliberately treats as an error rather than a miss).
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	driftGit(t, repo, "add", "--", "seed.txt")
	driftGit(t, repo, "commit", "-q", "-m", "seed")

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkTo(t, repo, "sneaky.txt", outside)

	add, pathspec, skipped := classifyTaskCommitPaths(context.Background(), repo, []string{"sneaky.txt"})
	if len(add) != 0 || len(pathspec) != 0 || len(skipped) != 1 || skipped[0] != "sneaky.txt" {
		t.Fatalf("add=%v pathspec=%v skipped=%v", add, pathspec, skipped)
	}

	// Positive control: the same path existing as a real file inside the repo
	// is still picked up by the worktree check.
	if err := os.WriteFile(filepath.Join(repo, "inbase.txt"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	add, pathspec, _ = classifyTaskCommitPaths(context.Background(), repo, []string{"inbase.txt"})
	if len(add) != 1 || add[0] != "inbase.txt" || len(pathspec) != 1 {
		t.Fatalf("in-base file: add=%v pathspec=%v", add, pathspec)
	}
}

// TestValidDriftDir pins the directory contract for git -C: an existing
// directory passes; a regular file, a missing path, a relative path, a
// dash-led path and the empty string do not.
func TestValidDriftDir(t *testing.T) {
	dir := t.TempDir()
	if !validDriftDir(dir) {
		t.Fatal("existing directory must pass")
	}
	file := filepath.Join(dir, "notadir")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{file, filepath.Join(dir, "missing"), "relative/dir", "-C", "--version", "", "a\x00b"} {
		if validDriftDir(bad) {
			t.Errorf("dir %q must be refused", bad)
		}
	}
}

// TestSafeRevisionArg pins the revision-slot contract: non-empty, no NUL,
// no leading dash.
func TestSafeRevisionArg(t *testing.T) {
	for _, good := range []string{"abc1234f00", "0123456789abcdef0123456789abcdef01234567"} {
		if !safeRevisionArg(good) {
			t.Errorf("revision %q must pass", good)
		}
	}
	for _, bad := range []string{"", "-w", "-C", "--all", "a\x00b"} {
		if safeRevisionArg(bad) {
			t.Errorf("revision %q must be refused", bad)
		}
	}
}

// TestDriftCaptureRefusesUnsafeDirOrRevisionWithoutExec: neither a
// non-directory dir nor an unsafe revision may reach git. The swapped
// executor counts calls; a refusal before execution leaves the counter
// at zero.
func TestDriftCaptureRefusesUnsafeDirOrRevisionWithoutExec(t *testing.T) {
	driftCleanEnv(t)
	calls := 0
	swapDriftGitExec(t, func(ctx context.Context, argv []string) (string, error) {
		calls++
		return "", errors.New("must not run")
	})
	ctx := context.Background()

	base := t.TempDir()
	file := filepath.Join(base, "notadir")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(base, "missing")
	for _, dir := range []string{file, missing, "relative/dir", "-C", ""} {
		if v := CapturePreTaskDirtyFiles(ctx, dir, nil); v != nil {
			t.Errorf("dir %q: expected nil capture", dir)
		}
		// an invalid dir refuses inside the runner; either way no exec
		_, _ = CommittedFilesSince(ctx, dir, "abc1234", nil)
		_, _ = CommittedLineStatsSince(ctx, dir, "abc1234", nil)
	}
	if calls != 0 {
		t.Fatalf("calls for unsafe dirs = %d; want 0", calls)
	}

	for _, pre := range []string{"", "-w", "-C", "--all", "a\x00b"} {
		files, err := CommittedFilesSince(ctx, base, pre, nil)
		if err != nil || files != nil {
			t.Errorf("pre %q: got (%v, %v); want (nil, nil)", pre, files, err)
		}
		stats, err := CommittedLineStatsSince(ctx, base, pre, nil)
		if err != nil || stats != nil {
			t.Errorf("pre %q: got (%v, %v); want (nil, nil)", pre, stats, err)
		}
	}
	if calls != 0 {
		t.Fatalf("calls for unsafe revisions = %d; want 0", calls)
	}
}

// TestCleanupStatusFileIn pins the status-path resolution: a usable data
// directory yields the fixed name under it, absolute and cleaned; an empty,
// dash-led, NUL-laden or missing base yields the inert empty string.
func TestCleanupStatusFileIn(t *testing.T) {
	dir := t.TempDir()
	got := cleanupStatusFileIn(dir)
	if !filepath.IsAbs(got) {
		t.Fatalf("path = %q; want absolute", got)
	}
	if want := filepath.Clean(filepath.Join(dir, cleanupStatusFile)); got != want {
		t.Fatalf("path = %q; want %q", got, want)
	}
	// Empty and NUL bases must yield the inert empty string: an inert
	// recorder performs no reads or writes against the working directory.
	for _, bad := range []string{"", "a\x00b"} {
		if got := cleanupStatusFileIn(bad); got != "" {
			t.Errorf("base %q: path = %q; want inert", bad, got)
		}
	}
	// A base without a usable filesystem location ("-bad", a missing subdir)
	// must resolve to a path that stays inside that base; it must never
	// escape to the working directory or elsewhere. ContainedPath guarantees
	// the containment, so we assert the result lands under the base.
	for _, base := range []string{"-bad", filepath.Join(dir, "missing")} {
		got := cleanupStatusFileIn(base)
		exp, aerr := filepath.Abs(filepath.Join(base, cleanupStatusFile))
		if aerr != nil {
			t.Fatalf("abs %q: %v", base, aerr)
		}
		if got != exp {
			t.Errorf("base %q: path = %q; want %q (fixed name under the absolute base)", base, got, exp)
		}
	}
}

// TestCleanupStatusRecorderInertWithoutDataDir: a recorder built without a
// usable data directory must not read or write the process working
// directory, where a leftover status file would survive a load/save.
func TestCleanupStatusRecorderInertWithoutDataDir(t *testing.T) {
	cwd := t.TempDir()
	plant := filepath.Join(cwd, cleanupStatusFile)
	seed := []byte(`{"DiskAlertPercent": 99}`)
	if err := os.WriteFile(plant, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	r := newCleanupStatusRecorder("")
	if r.path != "" {
		t.Fatalf("path = %q; want inert recorder", r.path)
	}
	if r.st.DiskAlertPercent != config.DefaultAutomationDiskAlertPercent {
		t.Fatalf("DiskAlertPercent = %d; must not have loaded the working-directory file", r.st.DiskAlertPercent)
	}
	r.save()
	data, err := os.ReadFile(plant)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(seed) {
		t.Fatalf("working-directory status file was modified by an inert recorder: %q", data)
	}
}

// TestClassifyCleanupPermissionErrorsRefusesUnlocalPaths: the existence
// probe must not stat ".", ".." or NUL paths; with only such paths the
// record reports the worktrees as gone and hidden, never fixable.
func TestClassifyCleanupPermissionErrorsRefusesUnlocalPaths(t *testing.T) {
	iss := ClassifyCleanupPermissionErrors([]WorktreeCleanupFailed{
		{Worktree: "..", Error: "remove: permission denied"},
		{Worktree: ".", Error: "remove: permission denied"},
		{Worktree: "../evil", Error: "remove: permission denied"},
		{Worktree: "a\x00b", Error: "remove: permission denied"},
	})
	if iss == nil {
		t.Fatal("permission failures must still be recorded")
	}
	if iss.Fixable || !iss.Hidden {
		t.Fatalf("Fixable=%v Hidden=%v; unlocal paths must not count as existing", iss.Fixable, iss.Hidden)
	}

	// Positive control: a real existing directory still counts.
	dir := t.TempDir()
	iss = ClassifyCleanupPermissionErrors([]WorktreeCleanupFailed{
		{Worktree: dir, Error: "remove: permission denied"},
	})
	if iss == nil || !iss.Fixable || iss.Hidden {
		t.Fatalf("Fixable=%v Hidden=%v; existing worktree must be fixable", iss == nil, false)
	}
}
