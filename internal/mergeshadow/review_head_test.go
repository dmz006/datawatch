package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newReviewHeadRepo builds a repository whose branch "work" has two commits and
// returns its path, the first commit and the head commit.
func newReviewHeadRepo(t *testing.T) (repo, first, head string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "a.txt", "a\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")
	git(t, repo, "checkout", "-q", "-b", "work")
	write(t, repo, "a.txt", "a1\n")
	git(t, repo, "commit", "-q", "-am", "one")
	first = strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	write(t, repo, "a.txt", "a2\n")
	git(t, repo, "commit", "-q", "-am", "two")
	head = strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	return repo, first, head
}

func TestCheckReviewHead(t *testing.T) {
	repo, first, head := newReviewHeadRepo(t)
	ctx := context.Background()

	cases := []struct {
		name     string
		reviewed string
		wantCode string
	}{
		{"review names head", head, ""},
		{"review names head by short sha", head[:12], ""},
		{"review names earlier commit", first, ReviewStaleCode},
		{"review names no commit", "  ", ReviewStaleCode},
		{"review names unknown commit", "deadbeefdeadbeef", ReviewStaleCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := CheckReviewHead(ctx, repo, "work", tc.reviewed)
			if err != nil {
				t.Fatalf("CheckReviewHead: %v", err)
			}
			if tc.wantCode == "" {
				if len(findings) != 0 {
					t.Fatalf("findings %+v, want none", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Code != tc.wantCode || !findings[0].Blocking {
				t.Fatalf("findings %+v, want one blocking %s", findings, tc.wantCode)
			}
		})
	}
}

func TestCheckReviewHeadErrors(t *testing.T) {
	repo, _, head := newReviewHeadRepo(t)
	ctx := context.Background()

	if _, err := CheckReviewHead(ctx, "", "work", head); err == nil {
		t.Error("empty repo path: want error")
	}
	if _, err := CheckReviewHead(ctx, repo, "", head); err == nil {
		t.Error("empty branch: want error")
	}
	if _, err := CheckReviewHead(ctx, repo, "-x", head); err == nil {
		t.Error("branch starting with dash: want error")
	}
	if _, err := CheckReviewHead(ctx, repo, "work", "-x"); err == nil {
		t.Error("review commit starting with dash: want error")
	}
	if _, err := CheckReviewHead(ctx, repo, "no-such-branch", head); err == nil {
		t.Error("missing branch: want error")
	}
}
