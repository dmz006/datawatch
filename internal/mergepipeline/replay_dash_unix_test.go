//go:build unix

package mergepipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// Text starting with "-" is refused before any git command is given it.
func TestReadHandMergeRefusesDashTextBeforeGit(t *testing.T) {
	dir := conflictRepo(t, "a.txt", "base\n", "main\n", "feature\n")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	script := "#!/bin/sh\necho \"$@\" >> '" + calls + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, c := range []struct{ commit, branchHead, want string }{
		{"-dashtext", head, "recorded commit"},
		{head, "-dashtext", "head of branch"},
	} {
		_ = os.Remove(calls)
		rec := &mergeshadow.Replay{Branch: "feature", Hand: mergeshadow.HandMerge{Commit: c.commit}}
		err := readHandMerge(context.Background(), &Merger{Dir: dir}, rec, c.branchHead)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: err = %v", c, err)
		}
		got, rerr := os.ReadFile(calls)
		if c.commit == head && rerr != nil {
			t.Fatalf("the stand-in git saw no call, so this proves nothing: %v", rerr)
		}
		if strings.Contains(string(got), "-dashtext") {
			t.Errorf("git was given the text: %s", got)
		}
	}
}
