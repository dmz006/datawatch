package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A remote name never starts with "-". Every function that takes one refuses
// such a value, and git runs nothing the value names.
func TestRemoteNameStartingWithDashRefused(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "work"},
		{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "one"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	failed := func(r PushResult) error {
		if r.Kind == "" {
			return nil
		}
		return errors.New(r.Reason)
	}
	head := []OutgoingRef{{Src: "HEAD", Dst: "refs/heads/work"}}
	entries := map[string]func(remote string) error{
		"DefaultBranch":       func(r string) error { _, err := DefaultBranch(ctx, repo, r); return err },
		"PushToBranch":        func(r string) error { return failed(PushToBranch(ctx, repo, r, "work")) },
		"PushWorkBranch":      func(r string) error { return failed(PushWorkBranch(ctx, repo, r, "work")) },
		"PushFromWorker":      func(r string) error { return failed(PushFromWorker(ctx, repo, r, "work")) },
		"PushToDefaultBranch": func(r string) error { return failed(PushToDefaultBranch(ctx, repo, r)) },
		"PushBranchRef":       func(r string) error { return PushBranchRef(ctx, repo, r, "work") },
		"PushTag":             func(r string) error { return PushTag(ctx, repo, r, "v1") },
		"CheckOutgoing":       func(r string) error { return failed(CheckOutgoing(ctx, repo, r, head)) },
		"PushCheckedRefs":     func(r string) error { return failed(PushCheckedRefs(ctx, repo, r, head)) },
		"GenericGit.Push":     func(r string) error { return (&GenericGit{}).Push(ctx, repo, r, "work") },
		"UnpushedCommits":     func(r string) error { _, _, err := UnpushedCommits(ctx, repo, r); return err },
		"ForeignUnpushedCommits": func(r string) error {
			_, err := ForeignUnpushedCommits(ctx, repo, r)
			return err
		},
		"ScanUnpushed": func(r string) error { _, err := ScanUnpushed(ctx, repo, r); return err },
	}
	for name, call := range entries {
		t.Run(name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "ran")
			for _, option := range []string{"--upload-pack=", "--receive-pack=", "--exec="} {
				if err := call(option + "touch " + marker + "; false"); err == nil {
					t.Errorf("%s...: not refused", option)
				}
				if _, err := os.Stat(marker); err == nil {
					t.Fatalf("%s...: git ran the command in the name", option)
				}
			}
		})
	}
}
