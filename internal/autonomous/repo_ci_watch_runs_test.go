package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gitTestRepo returns a temporary git checkout on branch main. Each
// script is one commit: every line before the "tag:" lines is written
// into app.txt, that commit is made, and only THEN the tags are put on
// it (an annotated one for "tag: NAME [annotated]", a lightweight one
// otherwise). A committer identity is set so the test does not depend
// on the host's git config.
func gitTestRepo(t *testing.T, scripts ...string) (repo string) {
	t.Helper()
	repo = t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main", repo)
	git("config", "user.email", "watch@test.invalid")
	git("config", "user.name", "ci-watch")
	for i, script := range scripts {
		var writes, tags []string
		for _, line := range strings.Split(script, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "tag:") {
				tags = append(tags, strings.TrimSpace(strings.TrimPrefix(line, "tag:")))
			} else {
				writes = append(writes, line)
			}
		}
		body := strings.Join(writes, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", "-A")
		git("commit", "-q", "-m", "commit "+strconv.Itoa(i))
		for _, tag := range tags {
			if name, ok := strings.CutSuffix(tag, " [annotated]"); ok {
				git("tag", "-a", name, "-m", "annotated "+name)
			} else {
				git("tag", tag)
			}
		}
	}
	return repo
}

// TestRepoCIWatchRunsTagFiltering verifies the release-tag rule:
// `git tag --sort=-v:refname` keeps only v<major>.<minor>.<patch>-shaped
// names in descending version order, and drops version markers ("v2"),
// unshaped names ("release-2026") and unseparated presuffix names
// ("v1.0.2rc").
func TestRepoCIWatchRunsTagFiltering(t *testing.T) {
	repo := gitTestRepo(t,
		"release one\ntag: v1.0.0",
		"release two\ntag: v1.0.1",
		"release three\ntag: v1.2.0\ntag: v2\ntag: release-2026\ntag: v1.0.2rc",
	)
	got, err := defaultRepoCITags(context.Background(), repo)
	if err != nil {
		t.Fatalf("defaultRepoCITags: %v", err)
	}
	want := []string{"v1.2.0", "v1.0.1", "v1.0.0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("defaultRepoCITags = %q, want %q", got, want)
	}
}

// TestRepoCIWatchRunsPeel verifies an annotated tag and a lightweight
// tag both peel to the commit the tag points at (defaultRepoCIPeel),
// that an annotated tag peels to the same commit as its target
// annotation (not the tag object), and that an unknown tag is an error.
func TestRepoCIWatchRunsPeel(t *testing.T) {
	repo := gitTestRepo(t,
		"annotated one\ntag: v1.0.0 [annotated]",
		"light two\ntag: v0.9.0",
	)
	ctx := context.Background()
	sha1 := gitIn(t, repo, "rev-parse", "v1.0.0^{commit}")
	sha2 := gitIn(t, repo, "rev-parse", "v0.9.0^{commit}")
	got1, err := defaultRepoCIPeel(ctx, repo, "v1.0.0")
	if err != nil {
		t.Fatalf("peel annotated tag: %v", err)
	}
	if got1 != sha1 {
		t.Errorf("peel(v1.0.0) = %s, want %s (the annotation's commit, not the tag object)", got1, sha1)
	}
	got2, err := defaultRepoCIPeel(ctx, repo, "v0.9.0")
	if err != nil {
		t.Fatalf("peel lightweight tag: %v", err)
	}
	if got2 != sha2 {
		t.Errorf("peel(v0.9.0) = %s, want %s", got2, sha2)
	}
	if got1 == got2 {
		t.Errorf("the two tags point at the same commit %s — the fixtures differ by one commit", got1)
	}
	if _, err := defaultRepoCIPeel(ctx, repo, "v9.9.9"); err == nil {
		t.Error("peeling an unknown tag should fail")
	}
}

// TestRepoCIWatchRunsHeadResolution verifies the head-of-main
// resolution: the origin remote URL (defaultRepoCIOrigin),
// refs/remotes/origin/main after a real fetch against a local file
// remote (defaultRepoCIHead), and the "owner/repo" extraction from all
// the github URL styles (https, ssh:// and scp); a non-github remote
// refuses.
func TestRepoCIWatchRunsHeadResolution(t *testing.T) {
	upstream := t.TempDir()
	repo := gitTestRepo(t, "mainline")
	gitIn(t, upstream, "init", "-q")
	gitIn(t, repo, "remote", "add", "origin", upstream)
	gitIn(t, upstream, "fetch", "-q", repo, "+refs/heads/main:refs/heads/main")
	gitIn(t, repo, "fetch", "-q", "origin")

	ctx := context.Background()
	origin, err := defaultRepoCIOrigin(ctx, repo)
	if err != nil {
		t.Fatalf("defaultRepoCIOrigin: %v", err)
	}
	if origin != upstream {
		t.Errorf("origin = %q, want %q", origin, upstream)
	}
	head, err := defaultRepoCIHead(ctx, repo)
	if err != nil {
		t.Fatalf("defaultRepoCIHead: %v", err)
	}
	if head != gitIn(t, repo, "rev-parse", "refs/remotes/origin/main") {
		t.Errorf("head = %s, want refs/remotes/origin/main", head)
	}
	if head != gitIn(t, repo, "rev-parse", "main") {
		t.Errorf("head %s drifted from local main %s — the fetch should have synced", head, gitIn(t, repo, "rev-parse", "main"))
	}

	// githubOwnerRepo (ci_watch.go) — actual behavior: github.com hosts
	// only; owner/repo is the repo's URL path split (last two segments),
	// scp-style being user@host:path. Non-github hosts and hosts with no
	// path are rejected.
	cases := map[string]string{
		"https://github.com/dmz006/datawatch.git":   "dmz006/datawatch",
		"https://github.com/dmz006/datawatch":       "dmz006/datawatch",
		"ssh://git@github.com/dmz006/datawatch.git": "dmz006/datawatch",
		"git@github.com:dmz006/datawatch.git":       "dmz006/datawatch",
		"git@github.com:dmz006/datawatch":           "dmz006/datawatch",
		"https://gitlab.com/other/project.git":      "",
		"git@github.com/dmz006/datawatch.git":       "", // no colon: not a recognised host form
	}
	for in, want := range cases {
		if got := githubOwnerRepo(in); got != want {
			t.Errorf("githubOwnerRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// TestRepoCIWatchRunsListParsing verifies a `gh run list` body is read
// as a list of runs with every field (databaseId, name,
// workflowName, status, conclusion, url, event, headBranch,
// createdAt, startedAt), that an empty or null body is an empty list
// (a commit with no runs is not an error), and that undecodable
// output is an error.
func TestRepoCIWatchRunsListParsing(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	body := `[
	  {"databaseId": 42, "name": "CI on main", "workflowName": "CI",
	   "status": "completed", "conclusion": "failure",
	   "url": "https://github.com/x/y/actions/runs/42",
	   "event": "push", "headBranch": "main",
	   "createdAt": "2026-10-17T12:00:00Z", "startedAt": "2026-10-17T12:00:05Z"}
	]`
	runs, err := parseRepoCIWatchRuns([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	r := runs[0]
	if r.ID != 42 || r.Name != "CI on main" || r.WorkflowName != "CI" ||
		r.Status != "completed" || r.Conclusion != "failure" ||
		r.URL != "https://github.com/x/y/actions/runs/42" ||
		r.Event != "push" || r.HeadBranch != "main" {
		t.Errorf("fields mismatch: %+v", r)
	}
	if !r.CreatedAt.Equal(t0) || !r.StartedAt.Equal(t0.Add(5*time.Second)) {
		t.Errorf("timestamps: created=%s started=%s", r.CreatedAt, r.StartedAt)
	}

	for _, empty := range [][]byte{nil, []byte(""), []byte("null"), []byte("[]")} {
		runs, err := parseRepoCIWatchRuns(empty)
		if err != nil {
			t.Errorf("parse(%q): %v", empty, err)
		}
		if runs == nil || len(runs) != 0 {
			t.Errorf("parse(%q) = %#v, want an empty non-nil list", empty, runs)
		}
	}
	if _, err := parseRepoCIWatchRuns([]byte(`{"not":"a list"}`)); err == nil {
		t.Error("undecodable JSON should be an error")
	}
}
