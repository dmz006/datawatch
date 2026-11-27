//go:build linux

package mergepipeline

// The shadow's copy of a shallow project with very many tags stays small. The
// repositories here are made up and small enough that even the push the copy
// must not make stays within a few hundred megabytes.

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// taggedCommits is the size of the fixture: one annotated tag per commit.
const taggedCommits = 1500

// newShallowTaggedRepo makes a shallow repository whose main has an annotated
// release tag on every commit, with a branch "work" one commit ahead of main
// and a merge of it as the hand merge.
func newShallowTaggedRepo(t *testing.T) (dir string, hand mergeshadow.HandMerge) {
	t.Helper()
	committerEnv(t)
	dir = t.TempDir()
	runGit(t, dir, "init", "-q", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "S")
	runGit(t, dir, "config", "user.email", "s@example.invalid")
	var in bytes.Buffer
	for i := 1; i <= taggedCommits+1; i++ {
		msg, data := fmt.Sprintf("c%d", i), fmt.Sprintf("line %d\n", i)
		fmt.Fprintf(&in, "commit refs/heads/main\nmark :%d\ncommitter S <s@example.invalid> %d +0000\ndata %d\n%s\n", i, 1700000000+i, len(msg), msg)
		if i > 1 {
			fmt.Fprintf(&in, "from :%d\n", i-1)
		}
		fmt.Fprintf(&in, "M 644 inline f.txt\ndata %d\n%s\n", len(data), data)
		if i > 1 {
			fmt.Fprintf(&in, "tag v0.0.%d\nfrom :%d\ntagger S <s@example.invalid> %d +0000\ndata 1\nr\n", i, i, 1700000000+i)
		}
	}
	imp := exec.Command("git", "fast-import", "--quiet")
	imp.Dir, imp.Stdin = dir, &in
	if out, err := imp.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v: %s", err, out)
	}
	cut := runGit(t, dir, "rev-parse", fmt.Sprintf("main~%d", taggedCommits-1))
	if err := os.WriteFile(filepath.Join(dir, ".git", "shallow"), []byte(cut+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "tag", "light", "main~3")
	runGit(t, dir, "reset", "-q", "--hard")
	hand.Base = runGit(t, dir, "rev-parse", "main")
	runGit(t, dir, "checkout", "-q", "-b", "work")
	putFile(t, dir, "work.txt", "new\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "work")
	hand.Tip = runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "checkout", "-q", "main")
	runGit(t, dir, "merge", "-q", "--no-ff", "-m", "Merge work", "work")
	hand.Commit = runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "tag", "-a", "v0.1.0", "-m", "the hand release")
	if got := runGit(t, dir, "rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("fixture: shallow = %s", got)
	}
	return dir, hand
}

// peakKiB runs cmd and returns the most memory it, or any process it started
// and waited for, held.
func peakKiB(cmd *exec.Cmd) (int64, string, error) {
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil {
		return 0, string(out), err
	}
	usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0, string(out), err
	}
	return int64(usage.Maxrss), string(out), err
}

func TestShadowCopyOfAShallowProjectWithManyTagsStaysSmall(t *testing.T) {
	// Two packing threads in every git process, on whatever machine this runs:
	// thread stacks count against the limit, and must not decide the result.
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[pack]\n\tthreads = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	src, hand := newShallowTaggedRepo(t)

	// The control: the fixture is big enough to show the fault. Pushing every
	// tag from a shallow repository into one that is not cut at the same place
	// costs git far more than the limit used below. (Whether the push then
	// succeeds depends on the git version, and does not matter here.)
	plain := filepath.Join(t.TempDir(), "plain.git")
	runGit(t, src, "init", "-q", "--bare", "--initial-branch=main", plain)
	runGit(t, plain, "config", "receive.shallowUpdate", "true")
	push := exec.Command("git", "push", "-q", plain, "refs/heads/main:refs/heads/main", "refs/tags/*:refs/tags/*")
	push.Dir = src
	pushPeak, _, _ := peakKiB(push)
	t.Logf("control: pushing %d tags peaked at %d MB", taggedCommits, pushPeak/1024)
	const limit = 128 << 20
	if pushPeak*1024 <= limit*3/2 {
		t.Fatalf("control: the push peaked at only %d MB, so this fixture proves nothing", pushPeak/1024)
	}

	// The shadow's copy is made with every git step held to far less.
	work := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := git.WithProcessLimit(context.Background(), git.ProcessLimit{MemoryBytes: limit, Time: 3 * time.Minute})
	rec := &mergeshadow.Replay{Branch: "work", Hand: hand}
	c, err := makeShadowCopy(ctx, &Merger{Dir: src}, work, rec)
	if err != nil {
		t.Fatalf("make the copy under a %d MB limit: %v", limit>>20, err)
	}

	// It holds what the comparison needs and nothing else.
	wantTags := taggedCommits + 1 // one per commit after the first, and the light one
	for _, d := range []string{c.dir, c.bare} {
		if n := len(lines(runGit(t, d, "for-each-ref", "--format=x", "refs/tags"))); n != wantTags {
			t.Errorf("%s: %d tags, want %d", d, n, wantTags)
		}
		if got := runGit(t, d, "rev-parse", "main"); got != hand.Base {
			t.Errorf("%s: main = %s, want the base %s", d, got, hand.Base)
		}
		if got := runGit(t, d, "for-each-ref", "--format=%(refname)", "refs/tags/v0.1.0"); got != "" {
			t.Errorf("%s: it has the hand release's tag", d)
		}
		runGit(t, d, "fsck", "--connectivity-only", "--no-dangling")
	}
	if got := runGit(t, c.bare, "for-each-ref", "--format=%(refname)", "refs/heads"); got != "refs/heads/main" {
		t.Errorf("the copy's remote has the branches %q", got)
	}
	if got := runGit(t, c.dir, "rev-parse", "work"); got != hand.Tip {
		t.Errorf("work = %s, want %s", got, hand.Tip)
	}
	runGit(t, c.dir, "cat-file", "-e", hand.Commit+"^{commit}")
	if got := runGit(t, c.dir, "describe", "--match", "v*", hand.Base); got != fmt.Sprintf("v0.0.%d", taggedCommits+1) {
		t.Errorf("describe = %s", got)
	}
	for _, d := range []string{c.dir, c.bare} {
		if got := runGit(t, d, "rev-parse", "--is-shallow-repository"); got != "true" {
			t.Errorf("%s: shallow = %s", d, got)
		}
	}
	err = filepath.WalkDir(work, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || strings.Contains(path, "/objects/") {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil && bytes.Contains(data, []byte(src)) {
			t.Errorf("%s names the project's folder", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// A check command may push every tag to the copy's remote. That stays
	// small as well, because the remote is cut at the same place.
	putFile(t, c.dir, "from-a-check.txt", "x\n")
	runGit(t, c.dir, "add", "from-a-check.txt")
	runGit(t, c.dir, "commit", "-q", "-m", "a check command's commit")
	runGit(t, c.dir, "tag", "-a", "from-a-check", "-m", "x")
	check := exec.Command("git", "push", "-q", "--force", "origin", "HEAD:refs/heads/main", "refs/tags/*:refs/tags/*")
	check.Dir = c.dir
	checkPeak, out, err := peakKiB(check)
	t.Logf("a push of every tag from the copy to its remote peaked at %d MB", checkPeak/1024)
	if err != nil {
		t.Errorf("push to the copy's remote: %v: %s", err, tail(out, 500))
	}
	if checkPeak*1024 > limit {
		t.Errorf("a push of every tag to the copy's remote peaked at %d MB", checkPeak/1024)
	}
}

// A shadow gets the standing limit unless its caller set one.
func TestShadowGitStepsAreLimited(t *testing.T) {
	g := newReplayRig(t)
	hand := g.handMerge(handOptions{})
	var seen []git.ProcessLimit
	look := func(in *ReplayInput) {
		inner := in.Select
		in.Select = func(ctx context.Context, tip string) *mergeshadow.ReplaySelection {
			l, _ := git.ProcessLimitOf(ctx)
			seen = append(seen, l)
			return inner(ctx, tip)
		}
	}
	g.shadow(hand, look)
	if len(seen) != 1 || seen[0] != git.ShadowProcessLimit {
		t.Errorf("limits seen: %+v", seen)
	}

	// A shadow whose git cannot stay inside the limit stops and says so.
	in := g.input(hand)
	r := ShadowHandMerge(git.WithProcessLimit(context.Background(), git.ProcessLimit{MemoryBytes: 64 << 10, Time: time.Minute}), in)
	if r.State != mergeshadow.ReplayError || r.Error == "" {
		t.Errorf("state=%s error=%q", r.State, r.Error)
	}
}
