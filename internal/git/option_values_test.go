//go:build !windows

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// standInGit puts a git first on PATH that records every argument it is given
// and answers from replies: the first entry whose key is part of the command
// line. It runs nothing else.
func standInGit(t *testing.T, replies map[string]string) (args func() []string) {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >>'" + record + "'; done\ncase \"$*\" in\n"
	n := 0
	for key, reply := range replies {
		n++
		file := filepath.Join(dir, "reply"+string(rune('a'+n)))
		if err := os.WriteFile(file, []byte(reply), 0o600); err != nil {
			t.Fatal(err)
		}
		script += "*'" + key + "'*) cat '" + file + "' ;;\n"
	}
	script += "esac\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		data, _ := os.ReadFile(record)
		return strings.Split(string(data), "\n")
	}
}

const oddValue = "--odd-value"

func noOddArgument(t *testing.T, args []string) {
	t.Helper()
	for _, a := range args {
		if strings.HasPrefix(a, oddValue) {
			t.Errorf("git was given %q as an argument of its own", a)
		}
	}
}

// A push entry point given a remote name that starts with "-" starts no git
// command that carries it.
func TestOddRemoteNameNeverReachesGit(t *testing.T) {
	ctx := context.Background()
	args := standInGit(t, nil)
	dir := t.TempDir()
	PushToBranch(ctx, dir, oddValue, "work")
	PushWorkBranch(ctx, dir, oddValue, "work")
	PushFromWorker(ctx, dir, oddValue, "work")
	PushToDefaultBranch(ctx, dir, oddValue)
	CheckOutgoing(ctx, dir, oddValue, []OutgoingRef{{Src: "HEAD", Dst: "refs/heads/work"}})
	PushCheckedRefs(ctx, dir, oddValue, []OutgoingRef{{Src: "HEAD", Dst: "refs/heads/work"}})
	_ = PushBranchRef(ctx, dir, oddValue, "work")
	_ = PushTag(ctx, dir, oddValue, "v1")
	_, _ = DefaultBranch(ctx, dir, oddValue)
	_, _, _ = UnpushedCommits(ctx, dir, oddValue)
	_, _ = ForeignUnpushedCommits(ctx, dir, oddValue)
	_, _ = ScanUnpushed(ctx, dir, oddValue)
	noOddArgument(t, args())
}

// Only lines that carry an object id and a ref name are read from a remote's
// answer, so neither value can be taken for an option later.
func TestRemoteAnswerKeepsOnlyRefLines(t *testing.T) {
	id := strings.Repeat("a", 40)
	standInGit(t, map[string]string{
		"get-url": "somewhere\n",
		"ls-remote": "ref: " + oddValue + "\tHEAD\n" +
			oddValue + strings.Repeat("a", 40) + "\trefs/heads/main\n" +
			id + "\t" + oddValue + "\n" +
			id + "\tHEAD\n" +
			id + "\trefs/heads/work\n" +
			strings.Repeat("b", 64) + "\trefs/tags/v1^{}\n",
	})
	st, _, err := readRemote(context.Background(), t.TempDir(), "origin")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.refs) != 3 || st.refs["refs/heads/work"] != id || st.refs["HEAD"] != id || st.refs["refs/tags/v1^{}"] == "" {
		t.Errorf("refs %v", st.refs)
	}
	if got := baseRef(st); got != "" {
		t.Errorf("base ref %q", got)
	}
}

// A commit's parents are the object ids git itself reads, nothing else a
// commit object's header happens to say.
func TestCommitParentsAreObjectIDs(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	raw := "tree " + a + "\nparent " + a + "\nparent " + b + "\nauthor x\ncommitter x\nparent " + oddValue + "\n\nparent " + oddValue + "\n"
	if got := commitParents(raw); strings.Join(got, ",") != a+","+b {
		t.Errorf("parents %q", got)
	}
}

// A tag object that names no object id is refused before git is asked about it.
func TestTagNamingNoObjectIDRefused(t *testing.T) {
	args := standInGit(t, map[string]string{"cat-file tag": "object " + oddValue + "\ntype commit\n"})
	id := strings.Repeat("a", 40)
	refs := []checkedRef{{dst: "refs/tags/v1", oldID: zeroObjectID, newID: id, objectType: "tag"}}
	_, err := buildManifest(context.Background(), t.TempDir(), remoteState{}, refs)
	if err == nil || !strings.Contains(err.Error(), "names no object") {
		t.Errorf("err %v", err)
	}
	noOddArgument(t, args())
}

// A line of the unpushed listing that is not an object id stops the scan.
func TestUnpushedListingKeepsOnlyObjectIDs(t *testing.T) {
	args := standInGit(t, map[string]string{"rev-list": oddValue + "\n"})
	if _, err := ScanUnpushed(context.Background(), t.TempDir(), "origin"); err == nil {
		t.Error("not refused")
	}
	noOddArgument(t, args())
}
