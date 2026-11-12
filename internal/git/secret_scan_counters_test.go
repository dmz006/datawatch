// Concurrency and snapshot tests for the pre-push secret-scan counters.
// Every test starts from a clean counter set (resetSecretScanStats) and
// restores it, so test order cannot leak counts into these assertions.
// Secret-shaped content is assembled from fragments at runtime (see
// fakeScanSecret in secretscan_push_test.go).

package git

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// withSecretScanCountersReset zeros the counters for the test's duration.
func withSecretScanCountersReset(t *testing.T) {
	t.Helper()
	resetSecretScanStats()
	t.Cleanup(resetSecretScanStats)
}

// TestSecretScanCountersConcurrentIncrements — 8 goroutines racing on all
// four recorders add up exactly, with no lost updates.
func TestSecretScanCountersConcurrentIncrements(t *testing.T) {
	withSecretScanCountersReset(t)

	const goroutines = 8
	const perGoroutine = 50

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				RecordSecretScanRun()
				RecordSecretScanFindings(2)
				RecordSecretScanShadowReport()
				RecordSecretScanRefusal()
			}
		}()
	}
	wg.Wait()

	got := SecretScanStatsSnapshot()
	want := int64(goroutines * perGoroutine)
	if got.Runs != want {
		t.Errorf("runs = %d, want %d", got.Runs, want)
	}
	if got.Findings != 2*want {
		t.Errorf("findings = %d, want %d", got.Findings, 2*want)
	}
	if got.ShadowReports != want {
		t.Errorf("shadow_reports = %d, want %d", got.ShadowReports, want)
	}
	if got.Refusals != want {
		t.Errorf("refusals = %d, want %d", got.Refusals, want)
	}

	// Non-positive finding counts are a no-op, not a subtraction.
	RecordSecretScanFindings(0)
	RecordSecretScanFindings(-3)
	if got := SecretScanStatsSnapshot().Findings; got != 2*want {
		t.Errorf("findings changed after no-op records: %d", got)
	}
}

// TestSecretScanShadowScanCounts — a shadow scan of a temp repo with one
// flagged secret records runs=1, findings=N, refusals=0, shadow_reports=1,
// and the push still proceeds.
func TestSecretScanShadowScanCounts(t *testing.T) {
	withSecretScanCountersReset(t)
	ctx := context.Background()
	_, work := testRepo(t, ctx)
	secret := fakeScanSecret()
	commitSecretFile(t, ctx, work, secret)

	res := PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{Mode: ScanShadow})
	if !res.Pushed || len(res.SecretFindings) == 0 {
		t.Fatalf("shadow scan should push with findings: %+v", res)
	}
	wantFindings := len(res.SecretFindings)

	got := SecretScanStatsSnapshot()
	if got.Runs != 1 {
		t.Errorf("runs = %d, want 1", got.Runs)
	}
	if got.Findings != int64(wantFindings) {
		t.Errorf("findings = %d, want %d", got.Findings, wantFindings)
	}
	if got.ShadowReports != 1 {
		t.Errorf("shadow_reports = %d, want 1", got.ShadowReports)
	}
	if got.Refusals != 0 {
		t.Errorf("refusals = %d, want 0 (shadow never refuses)", got.Refusals)
	}
}

// TestSecretScanEnforceRefusalCounts — the same content under enforce
// records one refusal (and the shadow-report counter stays zero).
func TestSecretScanEnforceRefusalCounts(t *testing.T) {
	withSecretScanCountersReset(t)
	ctx := context.Background()
	_, work := testRepo(t, ctx)
	secret := fakeScanSecret()
	commitSecretFile(t, ctx, work, secret)

	res := PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{Mode: ScanEnforce})
	if res.Pushed || res.Kind != PushFailSecret {
		t.Fatalf("enforce scan should refuse: %+v", res)
	}

	got := SecretScanStatsSnapshot()
	if got.Runs != 1 {
		t.Errorf("runs = %d, want 1", got.Runs)
	}
	if got.Findings == 0 {
		t.Error("findings = 0, want the flagged secret counted")
	}
	if got.Refusals != 1 {
		t.Errorf("refusals = %d, want 1", got.Refusals)
	}
	if got.ShadowReports != 0 {
		t.Errorf("shadow_reports = %d, want 0 (this was enforce)", got.ShadowReports)
	}
	if got.LastRefusalAt.IsZero() {
		t.Error("last_refusal_at should be set after an enforce refusal")
	}
}

// TestSecretScanOffModeRecordsNothing — no policy or an explicit off runs
// no scan and touches no counter, even though the worktree holds a
// credential.
func TestSecretScanOffModeRecordsNothing(t *testing.T) {
	withSecretScanCountersReset(t)
	ctx := context.Background()
	_, work := testRepo(t, ctx)
	secret := fakeScanSecret()
	commitSecretFile(t, ctx, work, secret)

	res := PushToBranch(ctx, work, "origin", "main") // no policy
	if !res.Pushed {
		t.Fatalf("off-mode push must proceed: %+v", res)
	}
	res = PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{Mode: ScanOff})
	if res.Kind != "" {
		t.Fatalf("explicit off must not refuse: %+v", res)
	}

	got := SecretScanStatsSnapshot()
	if got.Runs != 0 || got.Findings != 0 || got.ShadowReports != 0 || got.Refusals != 0 {
		t.Errorf("off mode must record nothing, got %+v", got)
	}
}

// TestSecretScanFixedClock — with the clock frozen, last_run_at (and,
// after an enforce refusal, last_refusal_at) are exactly the fixed time.
func TestSecretScanFixedClock(t *testing.T) {
	withSecretScanCountersReset(t)
	fixed := time.Date(2026, 11, 1, 12, 34, 56, 0, time.UTC)
	prev := secretScanNow
	secretScanNow = func() time.Time { return fixed }
	t.Cleanup(func() { secretScanNow = prev })

	ctx := context.Background()
	_, work := testRepo(t, ctx)
	secret := fakeScanSecret()
	commitSecretFile(t, ctx, work, secret)

	PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{Mode: ScanShadow})
	if got := SecretScanStatsSnapshot().LastRunAt; !got.Equal(fixed) {
		t.Errorf("last_run_at = %s, want %s", got.UTC(), fixed)
	}

	// Second commit (a second file, so git has a diff) so the enforce
	// scan below has unpushed content.
	_ = os.WriteFile(filepath.Join(work, "env2"), []byte("GITHUB_TOKEN=\""+secret+"\"\n"), 0o644)
	mustGit(t, ctx, work, "add", "env2")
	mustGit(t, ctx, work, "commit", "-m", "second accidental credential")
	PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{Mode: ScanEnforce})
	if got := SecretScanStatsSnapshot().LastRefusalAt; !got.Equal(fixed) {
		t.Errorf("last_refusal_at = %s, want %s", got.UTC(), fixed)
	}
}
