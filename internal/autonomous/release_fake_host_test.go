package autonomous

// Test helpers for the release path. They stand in for a hosting
// service without contacting one: the remote is a bare repository on
// local disk, the pull request surface is an in-memory fake, and the
// credential source is a label only, never a real token. Only the local
// git binary is run; no hosting command-line tool is ever invoked.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeCredentialSource returns the credential source label the release
// path reports in its audit when it runs against the fake host. It names
// the source only; there is no token behind it.
func fakeCredentialSource() string {
	return "fake-host:test-credential"
}

// newBareRemote creates a bare repository under t.TempDir() whose HEAD
// points at main, and returns its path. It stands in for the remote that
// a direct push goes to.
func newBareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	runLocalGit(t, "", "init", "--bare", remote)
	runLocalGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	return remote
}

// newCloneWithInitialCommit clones remote into t.TempDir(), makes one
// initial commit on main, and returns the clone's path. The commit is
// not pushed; callers push it when they need to.
func newCloneWithInitialCommit(t *testing.T, remote string) string {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	runLocalGit(t, "", "clone", remote, clone)
	runLocalGit(t, clone, "checkout", "-B", "main")
	readme := filepath.Join(clone, "README.md")
	if err := os.WriteFile(readme, []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	runLocalGit(t, clone, "add", "README.md")
	runLocalGit(t, clone, "commit", "-m", "initial commit")
	return clone
}

// runLocalGit runs the local git binary with a fixed identity so tests do
// not depend on the developer's global configuration. dir may be empty.
func runLocalGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// FakePullRequest is one pull request held in memory by fakeReleaseHost.
type FakePullRequest struct {
	Number     int
	HeadBranch string
	BaseBranch string
	Title      string
	Body       string
	Merged     bool
}

// fakeReleaseHost implements the three pull request operations the
// release path needs (open, report checks, merge) entirely in memory.
// Its check state is settable so a test can drive the merge gate.
type fakeReleaseHost struct {
	mu          sync.Mutex
	nextNumber  int
	prs         map[int]*FakePullRequest
	checkStatus string // "pending", "success" or "failure"
}

// newFakeReleaseHost returns a host with no pull requests and checks
// reporting "pending".
func newFakeReleaseHost() *fakeReleaseHost {
	return &fakeReleaseHost{
		nextNumber:  1,
		prs:         map[int]*FakePullRequest{},
		checkStatus: "pending",
	}
}

// SetCheckStatus sets the state that every pull request's checks report.
func (h *fakeReleaseHost) SetCheckStatus(status string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkStatus = status
}

// OpenPullRequest records a new pull request from head into base and
// returns it. The fake host has no URL, so the number is the identity.
func (h *fakeReleaseHost) OpenPullRequest(head, base, title, body string) (*FakePullRequest, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if head == "" || base == "" {
		return nil, fmt.Errorf("open pull request: head and base are required")
	}
	pr := &FakePullRequest{
		Number:     h.nextNumber,
		HeadBranch: head,
		BaseBranch: base,
		Title:      title,
		Body:       body,
	}
	h.prs[pr.Number] = pr
	h.nextNumber++
	copied := *pr
	return &copied, nil
}

// PullRequestChecks reports the check state of pull request number.
func (h *fakeReleaseHost) PullRequestChecks(number int) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.prs[number]; !ok {
		return "", fmt.Errorf("pull request %d not found", number)
	}
	return h.checkStatus, nil
}

// MergePullRequest merges pull request number. It refuses unless the
// checks are "success", the same gate a real host applies.
func (h *fakeReleaseHost) MergePullRequest(number int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	pr, ok := h.prs[number]
	if !ok {
		return fmt.Errorf("pull request %d not found", number)
	}
	if pr.Merged {
		return fmt.Errorf("pull request %d already merged", number)
	}
	if h.checkStatus != "success" {
		return fmt.Errorf("pull request %d checks are %q, not success", number, h.checkStatus)
	}
	pr.Merged = true
	return nil
}

// PullRequest returns a copy of pull request number, for assertions.
func (h *fakeReleaseHost) PullRequest(number int) (FakePullRequest, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	pr, ok := h.prs[number]
	if !ok {
		return FakePullRequest{}, false
	}
	return *pr, true
}

// TestFakeReleaseHostBareRemoteAcceptsPush proves the bare remote takes a
// direct push from a clone that has one initial commit on main.
func TestFakeReleaseHostBareRemoteAcceptsPush(t *testing.T) {
	remote := newBareRemote(t)
	clone := newCloneWithInitialCommit(t, remote)

	runLocalGit(t, clone, "push", "origin", "main")

	want := runLocalGit(t, clone, "rev-parse", "HEAD")
	got := runLocalGit(t, remote, "rev-parse", "refs/heads/main")
	if got != want {
		t.Fatalf("remote main = %s, want clone HEAD %s", got, want)
	}
}

// TestFakeReleaseHostPullRequestLifecycle drives open, checks and merge
// through the in-memory host, including the merge gate on check state.
func TestFakeReleaseHostPullRequestLifecycle(t *testing.T) {
	h := newFakeReleaseHost()

	pr, err := h.OpenPullRequest("release/v1.2.3", "main", "Release v1.2.3", "notes")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if pr.Number != 1 || pr.HeadBranch != "release/v1.2.3" || pr.BaseBranch != "main" {
		t.Fatalf("unexpected pull request: %+v", pr)
	}
	if _, err := h.OpenPullRequest("", "main", "x", ""); err == nil {
		t.Fatal("open with empty head: want error")
	}

	if st, err := h.PullRequestChecks(pr.Number); err != nil || st != "pending" {
		t.Fatalf("checks = %q, %v; want pending", st, err)
	}
	if err := h.MergePullRequest(pr.Number); err == nil {
		t.Fatal("merge while pending: want error")
	}

	h.SetCheckStatus("failure")
	if err := h.MergePullRequest(pr.Number); err == nil {
		t.Fatal("merge while failing: want error")
	}

	h.SetCheckStatus("success")
	if err := h.MergePullRequest(pr.Number); err != nil {
		t.Fatalf("merge on success: %v", err)
	}
	if got, ok := h.PullRequest(pr.Number); !ok || !got.Merged {
		t.Fatalf("pull request not merged: %+v ok=%v", got, ok)
	}
	if err := h.MergePullRequest(pr.Number); err == nil {
		t.Fatal("second merge: want error")
	}
	if _, err := h.PullRequestChecks(99); err == nil {
		t.Fatal("checks for unknown number: want error")
	}
	if err := h.MergePullRequest(99); err == nil {
		t.Fatal("merge unknown number: want error")
	}
	if _, ok := h.PullRequest(99); ok {
		t.Fatal("unknown pull request reported as present")
	}
}

// TestFakeCredentialSourceLabelHasNoToken checks the label names a source
// and carries no secret-looking material.
func TestFakeCredentialSourceLabelHasNoToken(t *testing.T) {
	label := fakeCredentialSource()
	if label == "" {
		t.Fatal("credential source label is empty")
	}
	for _, marker := range []string{"ghp_", "gho_", "token=", "Bearer "} {
		if strings.Contains(label, marker) {
			t.Fatalf("label %q contains token-like marker %q", label, marker)
		}
	}
}
