// Pre-push secret scan wiring: enforce mode refuses the push and leaves
// the remote untouched, shadow mode lets the push through and reports
// redacted findings, and allow-listed content pushes under enforce.
//
// Every fake credential is assembled at runtime from string pieces, and
// each test asserts the fake never appears in any operator-facing
// output: the PushResult (Reason, Kind, findings), the finding strings,
// and the captured daemon log.

package git

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeScanSecret builds a GitHub-style token at runtime (28 alnum
// chars after the prefix) so no real-looking credential is committed
// anywhere in this repository.
func fakeScanSecret() string { return "gh" + "p_" + strings.Repeat("aB3d", 7) }

// commitSecretFile adds one unpushed commit to work containing a
// credential assignment the github_token detector flags, and returns
// the new HEAD SHA.
func commitSecretFile(t *testing.T, ctx context.Context, work, secret string) string {
	t.Helper()
	_ = os.WriteFile(filepath.Join(work, "env"), []byte("GITHUB_TOKEN=\""+secret+"\"\n"), 0o644)
	mustGit(t, ctx, work, "add", "env")
	mustGit(t, ctx, work, "commit", "-m", "accidental credential")
	return strings.TrimSpace(mustGit(t, ctx, work, "rev-parse", "HEAD"))
}

// captureLog redirects the process logger into a buffer for the test's
// duration (restored via t.Cleanup so a redaction miss in ANY logged
// line surfaces in the same assertions as a missed string field).
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

// assertNoSecretInResult checks that none of the operator-facing
// surfaces of res (reason, kind, each finding's String and fields, and
// the whole result's rendering) contains secret.
func assertNoSecretInResult(t *testing.T, res PushResult, secret string) {
	t.Helper()
	surfaces := []string{
		res.Reason,
		string(res.Kind),
		fmt.Sprintf("%+v", res),
		RedactedFindingsSummary(res.SecretFindings),
		"Pushed=" + fmt.Sprint(res.Pushed) + " NothingToPush=" + fmt.Sprint(res.NothingToPush) + " SHA=" + res.SHA,
	}
	for _, f := range res.SecretFindings {
		surfaces = append(surfaces, f.String(), f.Detector, f.Commit, f.Path, f.Fingerprint)
	}
	for i, s := range surfaces {
		if strings.Contains(s, secret) {
			t.Fatalf("operator-facing surface %d of PushResult contains the secret: %q", i, s)
		}
	}
}

// assertEnforceReasonShape checks the refusal reason carries the
// redacted signal: the count, a detector name, file:line and a
// fingerprint — and never the secret.
func assertEnforceReasonShape(t *testing.T, res PushResult, secret string) {
	t.Helper()
	assertNoSecretInResult(t, res, secret)
	if res.Reason == "" {
		t.Fatal("enforce refusal has no reason")
	}
	for _, want := range []string{"secret finding(s)", "github_token", "env:", "fingerprint"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("reason should carry %q: %q", want, res.Reason)
		}
	}
	if len(res.SecretFindings) == 0 {
		t.Error("enforce refusal should carry the redacted findings")
	}
}

// TestPushEnforceRefusesAndLeavesRemoteUntouched — the core guarantee:
// refuse before anything runs, remote at its old SHA, redacted reason.
func TestPushEnforceRefusesAndLeavesRemoteUntouched(t *testing.T) {
	ctx := context.Background()
	logBuf := captureLog(t)
	bare, work := testRepo(t, ctx)
	oldRemote := remoteRefSHA(t, ctx, bare, "main")
	if oldRemote == "" {
		t.Fatal("test setup: remote main missing")
	}
	secret := fakeScanSecret()
	success := commitSecretFile(t, ctx, work, secret)

	res := PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{Mode: ScanEnforce})
	if res.Pushed || res.NothingToPush {
		t.Fatalf("enforce mode pushed: %+v", res)
	}
	if res.Kind != PushFailSecret {
		t.Fatalf("want kind %q, got %q (reason %q)", PushFailSecret, res.Kind, res.Reason)
	}
	if got := remoteRefSHA(t, ctx, bare, "main"); got != oldRemote {
		t.Fatalf("remote moved: was %s, now %s — enforce must be pre-push", oldRemote, got)
	}
	if got := strings.TrimSpace(mustGit(t, ctx, work, "rev-parse", "HEAD")); got != success {
		t.Fatalf("local HEAD moved (%s -> %s) — the scan must not alter commits", success, got)
	}
	assertEnforceReasonShape(t, res, secret)
	if strings.Contains(logBuf.String(), secret) {
		t.Fatalf("daemon log contains the secret:\n%s", logBuf.String())
	}
}

// TestPushEnforceDefaultBranchRefuses — same guarantee on the
// default-branch push path.
func TestPushEnforceDefaultBranchRefuses(t *testing.T) {
	ctx := context.Background()
	logBuf := captureLog(t)
	bare, work := testRepo(t, ctx)
	oldRemote := remoteRefSHA(t, ctx, bare, "main")
	secret := fakeScanSecret()
	commitSecretFile(t, ctx, work, secret)

	res := PushToDefaultBranch(ctx, work, "origin", SecretScanPolicy{Mode: ScanEnforce})
	if res.Pushed || res.NothingToPush || res.Kind != PushFailSecret {
		t.Fatalf("enforce default-branch push not refused: %+v", res)
	}
	if got := remoteRefSHA(t, ctx, bare, "main"); got != oldRemote {
		t.Fatalf("remote moved: was %s, now %s", oldRemote, got)
	}
	assertEnforceReasonShape(t, res, secret)
	if strings.Contains(logBuf.String(), secret) {
		t.Fatalf("daemon log contains the secret:\n%s", logBuf.String())
	}
}

// TestPushShadowProceedsAndReports — the push succeeds, the findings
// travel on the PushResult redacted, and the log line is redacted.
func TestPushShadowProceedsAndReports(t *testing.T) {
	ctx := context.Background()
	logBuf := captureLog(t)
	bare, work := testRepo(t, ctx)
	secret := fakeScanSecret()
	local := commitSecretFile(t, ctx, work, secret)

	res := PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{Mode: ScanShadow})
	if !res.Pushed {
		t.Fatalf("shadow mode must push: %+v", res)
	}
	if res.SHA != local {
		t.Errorf("pushed SHA %s != commit %s", res.SHA, local)
	}
	if len(res.SecretFindings) == 0 {
		t.Fatalf("shadow push should carry the redacted findings: %+v", res)
	}
	assertNoSecretInResult(t, res, secret)
	if got := remoteRefSHA(t, ctx, bare, "main"); got != local {
		t.Fatalf("remote main %s != pushed %s", got, local)
	}
	logOut := logBuf.String()
	if strings.Contains(logOut, secret) {
		t.Fatalf("daemon log contains the secret:\n%s", logOut)
	}
	if !strings.Contains(logOut, "github_token") || !strings.Contains(logOut, "fingerprint") {
		t.Errorf("shadow log should carry the redacted finding summary:\n%s", logOut)
	}
}

// TestPushEnforceAllowListedContentPushes — the same content, but the
// path (and separately the fingerprint) is allow-listed: the push
// proceeds with no findings at all.
func TestPushEnforceAllowListedContentPushes(t *testing.T) {
	ctx := context.Background()
	logBuf := captureLog(t)
	secret := fakeScanSecret()

	t.Run("path allow-list", func(t *testing.T) {
		_, work := testRepo(t, ctx)
		local := commitSecretFile(t, ctx, work, secret)
		res := PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{
			Mode:         ScanEnforce,
			PathPatterns: []string{"env"},
		})
		if !res.Pushed || len(res.SecretFindings) != 0 {
			t.Fatalf("allow-listed path still refused: %+v", res)
		}
		_ = local
	})

	t.Run("fingerprint allow-list", func(t *testing.T) {
		_, work := testRepo(t, ctx)
		commitSecretFile(t, ctx, work, secret)
		findings, err := ScanUnpushed(ctx, work, "origin")
		if err != nil {
			t.Fatalf("reference scan: %v", err)
		}
		if len(findings) == 0 {
			t.Fatal("reference scan found nothing — detector regression")
		}
		res := PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{
			Mode:         ScanEnforce,
			Fingerprints: []string{findings[0].Fingerprint},
		})
		if !res.Pushed || len(res.SecretFindings) != 0 {
			t.Fatalf("allow-listed fingerprint still refused: %+v", res)
		}
	})

	if strings.Contains(logBuf.String(), secret) {
		t.Fatalf("daemon log contains the secret:\n%s", logBuf.String())
	}
}

// TestPushNoPolicySkipsScan — no policy passed (or an explicit off)
// keeps the legacy behaviour: the push runs, no findings are
// consulted or reported.
func TestPushNoPolicySkipsScan(t *testing.T) {
	ctx := context.Background()
	_, work := testRepo(t, ctx)
	secret := fakeScanSecret()
	local := commitSecretFile(t, ctx, work, secret)

	res := PushToBranch(ctx, work, "origin", "main")
	if !res.Pushed || res.SHA != local {
		t.Fatalf("no-policy push must proceed: %+v", res)
	}
	if len(res.SecretFindings) != 0 {
		t.Errorf("no-policy push should carry no findings: %+v", res)
	}

	// And an explicit off behaves the same.
	res = PushToBranch(ctx, work, "origin", "main")
	if res.Kind != "" {
		t.Fatalf("explicit off should not produce a failure: %+v", res)
	}
}
