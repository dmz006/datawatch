package autonomous

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// brFake is a MergeGitRunner that records calls and answers by joined args.
type brFake struct {
	out   map[string]string
	fail  map[string]bool
	calls []string
}

func (f *brFake) Run(_ context.Context, _ string, args ...string) (string, error) {
	k := strings.Join(args, " ")
	f.calls = append(f.calls, k)
	if f.fail[k] {
		return f.out[k], errors.New("exit 1")
	}
	return f.out[k], nil
}

func (f *brFake) called(k string) bool {
	for _, c := range f.calls {
		if c == k {
			return true
		}
	}
	return false
}

func TestBranchRefreshGitCommitsBehindAndHead(t *testing.T) {
	f := &brFake{out: map[string]string{"rev-list --count HEAD..main": "3\n", "rev-parse main": "abc123\n"}}
	b := NewBranchRefreshGit(f)
	if n, err := b.CommitsBehind(context.Background(), "d", "main"); err != nil || n != 3 {
		t.Fatalf("behind = %d, %v", n, err)
	}
	if h, err := b.MainHead(context.Background(), "d", "main"); err != nil || h != "abc123" {
		t.Fatalf("head = %q, %v", h, err)
	}
}

func TestBranchRefreshGitMergeClean(t *testing.T) {
	f := &brFake{}
	files, err := NewBranchRefreshGit(f).Merge(context.Background(), "d", "main")
	if err != nil || files != nil {
		t.Fatalf("got %v, %v", files, err)
	}
	if !reflect.DeepEqual(f.calls, []string{"merge --no-ff --no-edit main"}) {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestBranchRefreshGitMergeChangelogOnly(t *testing.T) {
	f := &brFake{
		out:  map[string]string{"diff --name-only --diff-filter=U": "CHANGELOG.md\n"},
		fail: map[string]bool{"merge --no-ff --no-edit main": true},
	}
	files, err := NewBranchRefreshGit(f).Merge(context.Background(), "d", "main")
	if err != nil || len(files) != 0 {
		t.Fatalf("got %v, %v", files, err)
	}
	for _, k := range []string{"checkout --theirs -- CHANGELOG.md", "add -- CHANGELOG.md", "commit --no-edit"} {
		if !f.called(k) {
			t.Errorf("missing call %q in %v", k, f.calls)
		}
	}
	if f.called("merge --abort") {
		t.Error("must not abort")
	}
}

func TestBranchRefreshGitMergeRealConflictAborts(t *testing.T) {
	f := &brFake{
		out:  map[string]string{"diff --name-only --diff-filter=U": "CHANGELOG.md\nmain.go\n"},
		fail: map[string]bool{"merge --no-ff --no-edit main": true},
	}
	files, err := NewBranchRefreshGit(f).Merge(context.Background(), "d", "main")
	if err != nil || !reflect.DeepEqual(files, []string{"main.go"}) {
		t.Fatalf("got %v, %v", files, err)
	}
	if !f.called("merge --abort") {
		t.Error("expected merge --abort")
	}
	if f.called("checkout --theirs -- CHANGELOG.md") || f.called("commit --no-edit") {
		t.Errorf("must not touch files after a real conflict: %v", f.calls)
	}
}
