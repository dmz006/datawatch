package autonomous

// Tests for the pull request release push, run against the fake git host
// helper in release_fake_host_test.go. The remote is a bare repository on
// local disk and the pull request surface is the in-memory fake, reached
// through an adapter that satisfies ReleaseHost. No hosting service is
// reached and no credential is read.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

const pullRequestTestBranch = "release/v1.2.3"

// fakeReleaseHostAdapter exposes a fakeReleaseHost through the ReleaseHost
// interface. Pull request ids are the fake's numbers as decimal strings.
type fakeReleaseHostAdapter struct {
	host *fakeReleaseHost
}

func (a fakeReleaseHostAdapter) OpenPullRequest(_ context.Context, _, base, head, title, body string) (string, error) {
	pr, err := a.host.OpenPullRequest(head, base, title, body)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(pr.Number), nil
}

func (a fakeReleaseHostAdapter) PullRequestChecks(_ context.Context, _, id string) (string, error) {
	n, err := parsePullRequestID(id)
	if err != nil {
		return "", err
	}
	return a.host.PullRequestChecks(n)
}

func (a fakeReleaseHostAdapter) MergePullRequest(_ context.Context, _, id string) error {
	n, err := parsePullRequestID(id)
	if err != nil {
		return err
	}
	return a.host.MergePullRequest(n)
}

// parsePullRequestID converts an adapter id back to the fake's number.
func parsePullRequestID(id string) (int, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return 0, fmt.Errorf("pull request id %q is not a number: %w", id, err)
	}
	return n, nil
}

// newPullRequestReleaseOptions returns options for a pull request release of
// clone's HEAD onto pullRequestTestBranch, with a fast poll and a short
// window so the tests never wait on a real schedule.
func newPullRequestReleaseOptions(t *testing.T, clone string, store *ReleaseRecordStore) pullRequestReleaseOptions {
	t.Helper()
	// The release branch must exist locally for the push to name it.
	runLocalGit(t, clone, "checkout", "-B", pullRequestTestBranch)
	return pullRequestReleaseOptions{
		Dir:              clone,
		Project:          "datawatch",
		Tag:              releaseTestTag,
		Branch:           pullRequestTestBranch,
		Base:             "main",
		Remote:           "origin",
		SecretScanMode:   "enforce",
		CredentialSource: fakeCredentialSource(),
		PollInterval:     time.Millisecond,
		Window:           time.Second,
		Store:            store,
	}
}

// newReleaseRecordStoreForTest opens a release record store under t.TempDir().
func newReleaseRecordStoreForTest(t *testing.T) *ReleaseRecordStore {
	t.Helper()
	store, err := NewReleaseRecordStore(t.TempDir())
	if err != nil {
		t.Fatalf("open release record store: %v", err)
	}
	return store
}

// releaseAuditEntries returns the release push audit entries among events.
func releaseAuditEntries(events []audit.Entry) []audit.Entry {
	var out []audit.Entry
	for _, e := range events {
		if e.Action == audit.ActionReleasePush {
			out = append(out, e)
		}
	}
	return out
}

// findReleaseAuditEntry returns the first release push entry whose ref and
// outcome match, or false when none does.
func findReleaseAuditEntry(events []audit.Entry, ref, outcome string) (audit.Entry, bool) {
	for _, e := range releaseAuditEntries(events) {
		if e.Details["ref"] == ref && e.Details["outcome"] == outcome {
			return e, true
		}
	}
	return audit.Entry{}, false
}

// TestPullRequestReleaseMergesWhenChecksSucceed runs a release with the fake
// checks reporting success. It checks the release branch reached the bare
// remote, one pull request was opened from it into main, that pull request
// was merged, and the release record shows the merged outcome.
func TestPullRequestReleaseMergesWhenChecksSucceed(t *testing.T) {
	remote, clone, _ := newReleaseClone(t)
	head := runLocalGit(t, clone, "rev-parse", "HEAD")
	store := newReleaseRecordStoreForTest(t)
	opts := newPullRequestReleaseOptions(t, clone, store)

	host := newFakeReleaseHost()
	host.SetCheckStatus(pullRequestChecksSuccess)

	res, err := runPullRequestReleasePush(context.Background(), opts, fakeReleaseHostAdapter{host: host})
	if err != nil {
		t.Fatalf("pull request release: %v", err)
	}
	if res.Outcome != releasePushOutcomeMerged || !res.Pushed {
		t.Fatalf("outcome = %q pushed=%v, want merged and pushed", res.Outcome, res.Pushed)
	}

	// The release branch reached the bare remote at the release commit.
	if got := remoteRefValue(t, remote, "refs/heads/"+pullRequestTestBranch); got != head {
		t.Fatalf("remote %s = %s, want release commit %s", pullRequestTestBranch, got, head)
	}

	// Exactly one pull request was opened from the release branch into main,
	// and it was merged once its checks reported success.
	pr, ok := host.PullRequest(1)
	if !ok {
		t.Fatal("no pull request opened")
	}
	if pr.HeadBranch != pullRequestTestBranch || pr.BaseBranch != "main" {
		t.Fatalf("pull request = %+v, want head %s into main", pr, pullRequestTestBranch)
	}
	if pr.Title != "Release "+releaseTestTag {
		t.Fatalf("pull request title = %q, want the default release title", pr.Title)
	}
	if !pr.Merged {
		t.Fatal("pull request not merged after successful checks")
	}

	rec, ok := store.GetRelease("datawatch", releaseTestTag)
	if !ok {
		t.Fatal("no release record written")
	}
	if rec.PushOutcome != releasePushOutcomeMerged {
		t.Fatalf("record push outcome = %q, want %q", rec.PushOutcome, releasePushOutcomeMerged)
	}
	if rec.PushedCommit != head {
		t.Fatalf("record pushed commit = %s, want %s", rec.PushedCommit, head)
	}
}

// TestPullRequestReleaseDoesNotMergeWhenChecksFail runs a release with the
// fake checks reporting failure. It checks the pull request is left open and
// unmerged, the release record shows the checks_failed outcome, and the
// release returns an error naming the failed checks.
func TestPullRequestReleaseDoesNotMergeWhenChecksFail(t *testing.T) {
	_, clone, _ := newReleaseClone(t)
	store := newReleaseRecordStoreForTest(t)
	opts := newPullRequestReleaseOptions(t, clone, store)

	host := newFakeReleaseHost()
	host.SetCheckStatus(pullRequestChecksFailure)

	res, err := runPullRequestReleasePush(context.Background(), opts, fakeReleaseHostAdapter{host: host})
	if err == nil {
		t.Fatal("release with failing checks: want error")
	}
	if !strings.Contains(err.Error(), "checks failed") {
		t.Fatalf("error = %v, want it to name the failed checks", err)
	}
	if res.Outcome != releasePushOutcomeChecksFailed {
		t.Fatalf("outcome = %q, want %q", res.Outcome, releasePushOutcomeChecksFailed)
	}

	pr, ok := host.PullRequest(1)
	if !ok {
		t.Fatal("no pull request opened")
	}
	if pr.Merged {
		t.Fatal("pull request merged despite failing checks")
	}

	rec, ok := store.GetRelease("datawatch", releaseTestTag)
	if !ok {
		t.Fatal("no release record written for the failed release")
	}
	if rec.PushOutcome != releasePushOutcomeChecksFailed {
		t.Fatalf("record push outcome = %q, want %q", rec.PushOutcome, releasePushOutcomeChecksFailed)
	}
}

// TestPullRequestReleaseAuditRecordsEachPushAndMerge checks every push and
// merge writes its own release push audit entry: the release branch push with
// outcome pushed, and the default-branch merge with outcome merged. Each entry
// names the credential source and never carries a token.
func TestPullRequestReleaseAuditRecordsEachPushAndMerge(t *testing.T) {
	_, clone, _ := newReleaseClone(t)
	events := captureReleaseGitAudit(t)
	store := newReleaseRecordStoreForTest(t)
	opts := newPullRequestReleaseOptions(t, clone, store)

	host := newFakeReleaseHost()
	host.SetCheckStatus(pullRequestChecksSuccess)

	if _, err := runPullRequestReleasePush(context.Background(), opts, fakeReleaseHostAdapter{host: host}); err != nil {
		t.Fatalf("pull request release: %v", err)
	}

	got := releaseAuditEntries(*events)
	if len(got) != 2 {
		t.Fatalf("release push audit entries = %d, want 2 (branch push and merge): %+v", len(got), got)
	}

	pushEntry, ok := findReleaseAuditEntry(*events, "refs/heads/"+pullRequestTestBranch, releasePushOutcomePushed)
	if !ok {
		t.Fatalf("no audit entry for the release branch push: %+v", got)
	}
	mergeEntry, ok := findReleaseAuditEntry(*events, "refs/heads/main", releasePushOutcomeMerged)
	if !ok {
		t.Fatalf("no audit entry for the merge into main: %+v", got)
	}

	for name, e := range map[string]audit.Entry{"push": pushEntry, "merge": mergeEntry} {
		if e.Subject != "datawatch" {
			t.Fatalf("%s audit subject = %q, want the project", name, e.Subject)
		}
		if e.Details["credential_source"] != fakeCredentialSource() {
			t.Fatalf("%s audit credential_source = %v, want %q", name, e.Details["credential_source"], fakeCredentialSource())
		}
		if e.Details["remote"] != "origin" {
			t.Fatalf("%s audit remote = %v, want origin", name, e.Details["remote"])
		}
	}
}

// TestPullRequestReleaseAuditRecordsFailedMergeNotMerged checks that when
// checks fail, the audit log holds the branch push but no merge entry, so an
// operator reading the log can see the pull request was never merged.
func TestPullRequestReleaseAuditRecordsFailedMergeNotMerged(t *testing.T) {
	_, clone, _ := newReleaseClone(t)
	events := captureReleaseGitAudit(t)
	store := newReleaseRecordStoreForTest(t)
	opts := newPullRequestReleaseOptions(t, clone, store)

	host := newFakeReleaseHost()
	host.SetCheckStatus(pullRequestChecksFailure)

	if _, err := runPullRequestReleasePush(context.Background(), opts, fakeReleaseHostAdapter{host: host}); err == nil {
		t.Fatal("release with failing checks: want error")
	}

	if _, ok := findReleaseAuditEntry(*events, "refs/heads/"+pullRequestTestBranch, releasePushOutcomePushed); !ok {
		t.Fatal("no audit entry for the release branch push")
	}
	if _, ok := findReleaseAuditEntry(*events, "refs/heads/main", releasePushOutcomeMerged); ok {
		t.Fatal("audit records a merge although checks failed")
	}
}
