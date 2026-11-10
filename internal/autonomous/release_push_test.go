package autonomous

// Tests for the direct release push, run against the fake git host helper
// in release_fake_host_test.go: the remote is a bare repository on local
// disk, and only the local git binary runs. No hosting service is reached.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

const releaseTestTag = "v1.2.3"

// newReleaseClone builds the state a release push starts from: the initial
// commit is already on the bare remote's main (so the remote advertises its
// default branch), and a release commit sits unpushed on top of it in the
// clone. It returns the remote, the clone and the initial commit the remote
// holds before the release push.
func newReleaseClone(t *testing.T) (remote, clone, initial string) {
	t.Helper()
	remote = newBareRemote(t)
	clone = newCloneWithInitialCommit(t, remote)
	runLocalGit(t, clone, "push", "origin", "main")
	initial = runLocalGit(t, clone, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(clone, "RELEASE.md"), []byte("release\n"), 0o644); err != nil {
		t.Fatalf("write release file: %v", err)
	}
	runLocalGit(t, clone, "add", "RELEASE.md")
	runLocalGit(t, clone, "commit", "-m", "release commit")
	return remote, clone, initial
}

// remoteRefNames lists the branch and tag refs held by the bare remote.
func remoteRefNames(t *testing.T, remote string) []string {
	t.Helper()
	out := runLocalGit(t, remote, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/tags")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// remoteRefValue resolves ref on the bare remote, failing the test if absent.
func remoteRefValue(t *testing.T, remote, ref string) string {
	t.Helper()
	return runLocalGit(t, remote, "rev-parse", "--verify", ref)
}

// captureReleaseGitAudit wires a sink that collects release git audit
// entries for the duration of the test.
func captureReleaseGitAudit(t *testing.T) *[]audit.Entry {
	t.Helper()
	var got []audit.Entry
	SetReleaseGitAuditSink(func(e audit.Entry) { got = append(got, e) })
	t.Cleanup(func() { SetReleaseGitAuditSink(nil) })
	return &got
}

// TestReleasePushDirectPushPushesBranchAndTag runs a direct push with
// push_and_tag on and checks both the default branch and the annotated tag
// reach the bare remote.
func TestReleasePushDirectPushPushesBranchAndTag(t *testing.T) {
	remote, clone, _ := newReleaseClone(t)
	head := runLocalGit(t, clone, "rev-parse", "HEAD")

	res, err := runDirectReleasePush(context.Background(), releasePushOptions{
		Dir:              clone,
		Project:          "datawatch",
		Tag:              releaseTestTag,
		Remote:           "origin",
		PushAndTag:       true,
		ReleasePath:      config.ReleasePathDirectPush,
		SecretScanMode:   "enforce",
		Unattended:       false,
		CredentialSource: fakeCredentialSource(),
	})
	if err != nil {
		t.Fatalf("direct push: %v", err)
	}
	if res.Outcome != releasePushOutcomePushed || !res.Pushed {
		t.Fatalf("outcome = %q pushed=%v, want pushed", res.Outcome, res.Pushed)
	}

	if got := remoteRefValue(t, remote, "refs/heads/main"); got != head {
		t.Fatalf("remote main = %s, want clone HEAD %s", got, head)
	}
	// The annotated tag object is peeled to the release commit.
	if got := remoteRefValue(t, remote, "refs/tags/"+releaseTestTag+"^{}"); got != head {
		t.Fatalf("remote tag %s peels to %s, want %s", releaseTestTag, got, head)
	}
}

// TestReleasePushTagOnlyWhenPushAndTagOff checks that with push_and_tag off
// the release tag is created locally and nothing reaches the remote.
func TestReleasePushTagOnlyWhenPushAndTagOff(t *testing.T) {
	remote, clone, initial := newReleaseClone(t)

	res, err := runDirectReleasePush(context.Background(), releasePushOptions{
		Dir:        clone,
		Project:    "datawatch",
		Tag:        releaseTestTag,
		Remote:     "origin",
		PushAndTag: false,
	})
	if err != nil {
		t.Fatalf("prepare release: %v", err)
	}
	if res.Outcome != releasePushOutcomePrepared || res.Pushed {
		t.Fatalf("outcome = %q pushed=%v, want prepared and not pushed", res.Outcome, res.Pushed)
	}

	// The tag exists locally.
	runLocalGit(t, clone, "rev-parse", "--verify", "refs/tags/"+releaseTestTag)

	// The remote keeps its initial main and holds no tag.
	if got := remoteRefValue(t, remote, "refs/heads/main"); got != initial {
		t.Fatalf("remote main = %s, want unchanged initial %s", got, initial)
	}
	for _, ref := range remoteRefNames(t, remote) {
		if strings.HasPrefix(ref, "refs/tags/") {
			t.Fatalf("remote has tag %s, want none", ref)
		}
	}
}

// TestReleasePushUnattendedDefaultBranchRefusedUnderShadow checks that an
// unattended push to the default branch with git.secret_scan.mode shadow is
// refused, and nothing reaches the remote.
func TestReleasePushUnattendedDefaultBranchRefusedUnderShadow(t *testing.T) {
	remote, clone, initial := newReleaseClone(t)

	res, err := runDirectReleasePush(context.Background(), releasePushOptions{
		Dir:              clone,
		Project:          "datawatch",
		Tag:              releaseTestTag,
		Remote:           "origin",
		PushAndTag:       true,
		ReleasePath:      config.ReleasePathDirectPush,
		SecretScanMode:   "shadow",
		Unattended:       true,
		CredentialSource: fakeCredentialSource(),
	})
	if err == nil {
		t.Fatal("unattended default-branch push under shadow: want refusal error")
	}
	if !strings.Contains(err.Error(), "refusing unattended push") {
		t.Fatalf("error = %v, want the unattended refusal", err)
	}
	if res.Outcome != releasePushOutcomeRefused || res.Pushed {
		t.Fatalf("outcome = %q pushed=%v, want refused and not pushed", res.Outcome, res.Pushed)
	}

	if got := remoteRefValue(t, remote, "refs/heads/main"); got != initial {
		t.Fatalf("remote main = %s after refusal, want unchanged initial %s", got, initial)
	}
	for _, ref := range remoteRefNames(t, remote) {
		if strings.HasPrefix(ref, "refs/tags/") {
			t.Fatalf("remote has tag %s after refusal, want none", ref)
		}
	}
}

// TestReleasePushAuditNamesCredentialSourceNotToken runs a direct push with a
// sentinel token in the environment and checks the push audit entry carries
// the credential source label and never the token value.
func TestReleasePushAuditNamesCredentialSourceNotToken(t *testing.T) {
	const sentinel = "sentinel-token-4c1e8d05-do-not-log"
	const source = "environment-variable-name:DATAWATCH_GIT_TOKEN"
	t.Setenv("DATAWATCH_GIT_TOKEN", sentinel)

	_, clone, _ := newReleaseClone(t)
	events := captureReleaseGitAudit(t)

	if _, err := runDirectReleasePush(context.Background(), releasePushOptions{
		Dir:              clone,
		Project:          "datawatch",
		Tag:              releaseTestTag,
		Remote:           "origin",
		PushAndTag:       true,
		ReleasePath:      config.ReleasePathDirectPush,
		SecretScanMode:   "enforce",
		Unattended:       false,
		CredentialSource: source,
	}); err != nil {
		t.Fatalf("direct push: %v", err)
	}

	var pushEntry *audit.Entry
	for i := range *events {
		if (*events)[i].Action == audit.ActionReleasePush {
			pushEntry = &(*events)[i]
			break
		}
	}
	if pushEntry == nil {
		t.Fatalf("no %s audit entry recorded among %d entries", audit.ActionReleasePush, len(*events))
	}
	if pushEntry.Details["credential_source"] != source {
		t.Fatalf("credential_source = %v, want %q", pushEntry.Details["credential_source"], source)
	}
	if pushEntry.Details["outcome"] != releasePushOutcomePushed {
		t.Fatalf("outcome = %v, want %q", pushEntry.Details["outcome"], releasePushOutcomePushed)
	}

	raw, err := json.Marshal(pushEntry)
	if err != nil {
		t.Fatalf("marshal audit entry: %v", err)
	}
	if strings.Contains(string(raw), sentinel) {
		t.Fatalf("audit entry contains the token value: %s", raw)
	}
	if !strings.Contains(string(raw), source) {
		t.Fatalf("audit entry lacks the credential source label: %s", raw)
	}
}
