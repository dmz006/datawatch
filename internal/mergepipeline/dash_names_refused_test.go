//go:build !windows

package mergepipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Every branch, commit, tag, ref or remote name a caller of this package
// hands over, which git would get as an argument of its own, is refused before
// any git runs when it starts with "-".
func TestDashNamesRefusedBeforeGit(t *testing.T) {
	ctx := context.Background()
	for name, call := range map[string]func(t *testing.T, dir, v string) (refused bool){
		"Push branch": func(_ *testing.T, dir, v string) bool {
			return ExecShipRunner{}.Push(ctx, dir, v, "") != nil
		},
		"PushCommit branch": func(_ *testing.T, dir, v string) bool {
			return ExecShipRunner{}.PushCommit(ctx, dir, "HEAD", v, "") != nil
		},
		"PushCommit commit": func(_ *testing.T, dir, v string) bool {
			return ExecShipRunner{}.PushCommit(ctx, dir, v, "main", "") != nil
		},
		"Tag": func(_ *testing.T, dir, v string) bool {
			return ExecShipRunner{}.Tag(ctx, dir, v, "message") != nil
		},
		"MergeRequest.Ref": func(_ *testing.T, dir, v string) bool {
			_, err := (&Merger{Dir: dir}).MergeBranch(ctx, MergeRequest{Branch: "feature", Ref: v})
			return err != nil
		},
		"proposal commit": func(_ *testing.T, dir, v string) bool {
			return (&Merger{Dir: dir}).fastForwardTo(ctx, &modelProposal{Commit: v}).Result == AttemptFastForwardFail
		},
		"Runner.MainBranch, Run": func(t *testing.T, dir, v string) bool {
			r := &Runner{Dir: dir, Project: "p", MainBranch: v, Resolver: resolverWithRecord(t, noTagSteps()), Isolate: true}
			_, err := r.Run(ctx, RunRequest{Title: "x", Branches: []string{"feature"}})
			return err != nil
		},
		"Runner.MainBranch, ReconcilePushes": func(_ *testing.T, dir, v string) bool {
			_, err := (&Runner{Dir: dir, MainBranch: v}).ReconcilePushes(ctx)
			return err != nil
		},
		"ValidateInput.Remote": func(_ *testing.T, dir, v string) bool {
			res := ValidateReleaseBump(ctx, ValidateInput{Dir: dir, VersionFiles: testVersionFiles, Remote: v})
			for _, c := range res.Checks {
				if c.Pass {
					return false
				}
			}
			return !res.Pass && len(res.Checks) > 0
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			started := filepath.Join(bin, "started")
			shim := "#!/bin/sh\necho \"$*\" >>'" + started + "'\nexec '" + realGit + "' \"$@\"\n"
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			if !call(t, s.dir, "--odd-value") {
				t.Error("the value was not refused")
			}
			if lines, err := os.ReadFile(started); err == nil {
				t.Errorf("git ran:\n%s", lines)
			}
		})
	}
}
