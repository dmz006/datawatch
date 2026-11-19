package autonomous

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Remote reap tests script the same MergeGitRunner fake the local reap tests
// use: every git call is keyed by (dir, args) and any unscripted call fails,
// so a test cannot silently delete through an unscripted push.
//
// A fully merged, old-enough branch scripts to:
//
//	symbolic-ref refs/remotes/origin/HEAD            -> refs/remotes/origin/main
//	rev-parse --verify refs/heads/<b>                -> tip
//	rev-list --count main..<b>                       -> 2
//	cherry main <b>                                  -> "-sh1\n-sh2" (all on main)
//	log main..<b> --format=%s                        -> subjects
//	log main --format=%s                             -> subjects
//	merge-base main <b>                              -> tip (tip is on main)
//
// and, beyond the proof:
//
//	log -1 --format=%H %cI refs/heads/<b>            -> "tip 2026-06-01T00:00:00Z"
//	ls-remote --heads origin <b>                     -> "tip refs/heads/<b>"
//
// The pass instant is reapNow (from branch_reap_test.go, same package);
// 2026-06-01 is 30+ whole days before it, inside the 30-day window check.
const remoteReapTip = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

func remoteReapGit(branch string) *reapFakeGit {
	g := baseGit() // scripts symbolic-ref refs/remotes/origin/HEAD -> refs/remotes/origin/main
	g.out = mergeReapOut(g.out, map[string]string{
		reapRepo + " | rev-parse --verify refs/heads/" + branch:     remoteReapTip,
		reapRepo + " | rev-list --count main.." + branch:            "2",
		reapRepo + " | cherry main " + branch:                       "-sh1\n-sh2",
		reapRepo + " | log main.." + branch + " --format=%s":        "add the thing\nfix the thing",
		reapRepo + " | log main --format=%s":                        "add the thing\nfix the thing\nolder base work",
		reapRepo + " | merge-base main " + branch:                   remoteReapTip,
		reapRepo + " | log -1 --format=%H %cI refs/heads/" + branch: remoteReapTip + " 2025-08-01T00:00:00Z",
		reapRepo + " | ls-remote --heads origin " + branch:          remoteReapTip + " refs/heads/" + branch,
	})
	return g
}

func TestRemoteReapMergedAgedBranchDeleted(t *testing.T) {
	const b = "automaton/prd-1"
	g := remoteReapGit(b)
	ref := "refs/datawatch/remote-reap-backup/automaton-prd-1/" + remoteReapTs()
	g.out[reapRepo+" | for-each-ref --format=%(refname) refs/datawatch/remote-reap-backup/automaton-prd-1/"] = ""
	g.out[reapRepo+" | update-ref "+ref+" "+remoteReapTip] = ""
	g.out[reapRepo+" | rev-parse --verify "+ref] = remoteReapTip
	g.out[reapRepo+" | push --delete origin "+b] = ""

	rep, err := RemoteReapPass(context.Background(), g, reapRepo, []*PRD{reapTestPRD()}, reapNow, 30, true)
	if err != nil {
		t.Fatalf("RemoteReapPass: %v", err)
	}
	if rep.ReportOnly {
		t.Fatal("ReportOnly = true, want false for an enabled pass")
	}
	if len(rep.Reaped) != 1 {
		t.Fatalf("Reaped = %+v, want 1 entry", rep.Reaped)
	}
	e := rep.Reaped[0]
	if e.Branch != "automaton/prd-1" || e.AutomatonID != "prd-1" || e.BackupRef != ref {
		t.Fatalf("reaped entry = %+v", e)
	}
	if len(rep.Held) != 0 {
		t.Fatalf("Held = %+v, want none", rep.Held)
	}
	if i := g.callIndex("push --delete origin " + b); i < 0 {
		t.Fatalf("the enabled pass never ran git push --delete; calls: %v", g.calls)
	}
}

func TestRemoteReapBackupWrittenBeforeDelete(t *testing.T) {
	const b = "automaton/prd-1"
	g := remoteReapGit(b)
	ref := "refs/datawatch/remote-reap-backup/automaton-prd-1/" + remoteReapTs()
	g.out[reapRepo+" | for-each-ref --format=%(refname) refs/datawatch/remote-reap-backup/automaton-prd-1/"] = ""
	g.out[reapRepo+" | update-ref "+ref+" "+remoteReapTip] = ""
	g.out[reapRepo+" | rev-parse --verify "+ref] = remoteReapTip
	g.out[reapRepo+" | push --delete origin "+b] = ""

	if _, err := RemoteReapPass(context.Background(), g, reapRepo, []*PRD{reapTestPRD()}, reapNow, 30, true); err != nil {
		t.Fatalf("RemoteReapPass: %v", err)
	}
	upd := g.callIndex("update-ref " + ref + " " + remoteReapTip)
	del := g.callIndex("push --delete origin " + b)
	if upd < 0 || del < 0 {
		t.Fatalf("expected both backup and delete calls; calls: %v", g.calls)
	}
	if upd > del {
		t.Fatalf("backup write (index %d) is after the branch delete (index %d); the delete must come after the backup", upd, del)
	}
}

func TestRemoteReapReportOnlyDeletesNothing(t *testing.T) {
	const b = "automaton/prd-1"
	g := remoteReapGit(b)
	// The report side looks up an existing backup ref; none exists.
	g.out[reapRepo+" | for-each-ref --format=%(refname) refs/datawatch/remote-reap-backup/automaton-prd-1/"] = ""

	rep, err := RemoteReapPreview(context.Background(), g, reapRepo, []*PRD{reapTestPRD()}, reapNow, 30)
	if err != nil {
		t.Fatalf("RemoteReapPreview: %v", err)
	}
	if !rep.ReportOnly {
		t.Fatal("ReportOnly = false, want true for the report side")
	}
	if len(rep.Reaped) != 1 {
		t.Fatalf("Reaped = %+v, want the one eligible entry reported", rep.Reaped)
	}
	if e := rep.Reaped[0]; e.Reason == "" || !strings.Contains(e.Reason, "would be reaped") {
		t.Fatalf("report entry reason = %q, want the 'would be reaped' outcome", e.Reason)
	}
	for _, c := range g.calls {
		if strings.Contains(c, "push --delete") || strings.Contains(c, "update-ref") {
			t.Fatalf("the report side issued a mutating git call: %s (calls: %v)", c, g.calls)
		}
	}
}

func TestRemoteReapNotMergedBranchNeverDeleted(t *testing.T) {
	const b = "automaton/prd-1"
	g := remoteReapGit(b)
	// One patch not on main: the proof is not merged.
	g.out[reapRepo+" | cherry main "+b] = "+sh1\n-sh2"

	rep, err := RemoteReapPass(context.Background(), g, reapRepo, []*PRD{reapTestPRD()}, reapNow, 30, true)
	if err != nil {
		t.Fatalf("RemoteReapPass: %v", err)
	}
	if len(rep.Held) != 1 {
		t.Fatalf("Held = %+v, want the one not-merged entry held", rep.Held)
	}
	if e := rep.Held[0]; e.Reason == "" || !strings.Contains(e.Reason, "not fully merged") {
		t.Fatalf("held reason = %q, want the not-merged explanation", e.Reason)
	}
	if len(rep.Reaped) != 0 {
		t.Fatalf("Reaped = %+v, want none", rep.Reaped)
	}
	for _, c := range g.calls {
		if strings.Contains(c, "push --delete") || strings.Contains(c, "update-ref") {
			t.Fatalf("a not-merged branch triggered a mutating git call: %s (calls: %v)", c, g.calls)
		}
	}
}

func TestRemoteReapTooNewBranchHeldBeforeAnyDelete(t *testing.T) {
	const b = "automaton/prd-1"
	g := remoteReapGit(b)
	// A tip a day before the pass instant: merged but not 30 days old.
	g.out[reapRepo+" | log -1 --format=%H %cI refs/heads/"+b] =
		remoteReapTip + " " + reapNow.Add(-24*time.Hour).UTC().Format(time.RFC3339)

	rep, err := RemoteReapPass(context.Background(), g, reapRepo, []*PRD{reapTestPRD()}, reapNow, 30, true)
	if err != nil {
		t.Fatalf("RemoteReapPass: %v", err)
	}
	if len(rep.Held) != 1 {
		t.Fatalf("Held = %+v, want the one too-new entry held", rep.Held)
	}
	if len(rep.Reaped) != 0 {
		t.Fatalf("Reaped = %+v, want none", rep.Reaped)
	}
	for _, c := range g.calls {
		if strings.Contains(c, "push --delete") || strings.Contains(c, "update-ref") {
			t.Fatalf("a too-new branch triggered a mutating git call: %s (calls: %v)", c, g.calls)
		}
	}
}

func TestRemoteReapBackupFailureAbortsDelete(t *testing.T) {
	const b = "automaton/prd-1"
	g := remoteReapGit(b)
	ref := "refs/datawatch/remote-reap-backup/automaton-prd-1/" + remoteReapTs()
	g.out[reapRepo+" | for-each-ref --format=%(refname) refs/datawatch/remote-reap-backup/automaton-prd-1/"] = ""
	g.fail = map[string]bool{}
	g.fail[reapRepo+" | update-ref "+ref+" "+remoteReapTip] = true

	rep, err := RemoteReapPass(context.Background(), g, reapRepo, []*PRD{reapTestPRD()}, reapNow, 30, true)
	if err != nil {
		t.Fatalf("RemoteReapPass: %v", err)
	}
	if len(rep.Held) != 1 {
		t.Fatalf("Held = %+v, want the one failed-backup entry held", rep.Held)
	}
	if !strings.Contains(rep.Held[0].Reason, "backup ref could not be") {
		t.Fatalf("held reason = %q, want the backup-failure explanation", rep.Held[0].Reason)
	}
	if i := g.callIndex("push --delete origin " + b); i >= 0 {
		t.Fatalf("the delete ran despite the backup failure (call index %d); calls: %v", i, g.calls)
	}
}

func TestRemoteReapBackupVerifyFailureAbortsDelete(t *testing.T) {
	const b = "automaton/prd-1"
	g := remoteReapGit(b)
	ref := "refs/datawatch/remote-reap-backup/automaton-prd-1/" + remoteReapTs()
	g.out[reapRepo+" | for-each-ref --format=%(refname) refs/datawatch/remote-reap-backup/automaton-prd-1/"] = ""
	g.out[reapRepo+" | update-ref "+ref+" "+remoteReapTip] = ""
	// The backup ref does not point at the tip: the delete must abort.
	g.out[reapRepo+" | rev-parse --verify "+ref] = "ffffffffffffffffffffffffffffffffffffffff"

	rep, err := RemoteReapPass(context.Background(), g, reapRepo, []*PRD{reapTestPRD()}, reapNow, 30, true)
	if err != nil {
		t.Fatalf("RemoteReapPass: %v", err)
	}
	if len(rep.Held) != 1 {
		t.Fatalf("Held = %+v, want the one unverified-backup entry held", rep.Held)
	}
	if len(rep.Reaped) != 0 {
		t.Fatalf("Reaped = %+v, want none", rep.Reaped)
	}
	if i := g.callIndex("push --delete origin " + b); i >= 0 {
		t.Fatalf("the delete ran despite the backup verify failure; calls: %v", g.calls)
	}
}

func TestRemoteReapNoRemoteRefHeldBeforeAnyDelete(t *testing.T) {
	const b = "automaton/prd-1"
	g := remoteReapGit(b)
	// origin no longer carries the branch: nothing to delete.
	g.out[reapRepo+" | ls-remote --heads origin "+b] = ""

	rep, err := RemoteReapPass(context.Background(), g, reapRepo, []*PRD{reapTestPRD()}, reapNow, 30, true)
	if err != nil {
		t.Fatalf("RemoteReapPass: %v", err)
	}
	if len(rep.Held) != 1 {
		t.Fatalf("Held = %+v, want the one missing-remote branch held", rep.Held)
	}
	if len(rep.Reaped) != 0 {
		t.Fatalf("Reaped = %+v, want none", rep.Reaped)
	}
	for _, c := range g.calls {
		if strings.Contains(c, "push --delete") || strings.Contains(c, "update-ref") {
			t.Fatalf("a branch with no remote ref triggered a mutating git call: %s (calls: %v)", c, g.calls)
		}
	}
}

// remoteReapTs is the unix seconds of reapNow: the enabled pass stamps its
// backup ref with the pass instant, so tests predict the exact ref name.
func remoteReapTs() string {
	return strconv.FormatInt(reapNow.Unix(), 10)
}

func TestRemoteReapManagerWiring(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = repo
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The manager defaults read disabled + 30 days, like the local reaper.
	enabled, retention := m.remoteReapSettings()
	if enabled || retention != config.RemoteReapRetentionDaysDefault {
		t.Fatalf("default remote reap settings = (enabled=%v, retention=%d), want (false, %d)", enabled, retention, config.RemoteReapRetentionDaysDefault)
	}

	// The daemon can flip the live settings; the defaults come back when cleared.
	m.SetRemoteReapSettings(func() (bool, int) { return true, 7 })
	if enabled, retention := m.remoteReapSettings(); !enabled || retention != 7 {
		t.Fatalf("wired settings = (%v, %d), want (true, 7)", enabled, retention)
	}
	m.SetRemoteReapSettings(nil)
	if enabled, retention := m.remoteReapSettings(); enabled || retention != config.RemoteReapRetentionDaysDefault {
		t.Fatalf("cleared settings = (%v, %d), want the defaults", enabled, retention)
	}

	// With no stored Automata the pass is an empty report in report mode.
	rep, err := m.RemoteReapRun(context.Background())
	if err != nil {
		t.Fatalf("RemoteReapRun: %v", err)
	}
	if !rep.ReportOnly || len(rep.Reaped) != 0 || len(rep.Held) != 0 {
		t.Fatalf("run = %+v, want an empty report-only report", rep)
	}
}

func TestRemoteReapManagerNoBaseRepoIsTypedError(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = m.RemoteReapPreview(context.Background())
	if err != ErrMergeNoBaseRepo {
		t.Fatalf("want ErrMergeNoBaseRepo, got %v", err)
	}
	if _, err := m.RemoteReapRun(context.Background()); err != ErrMergeNoBaseRepo {
		t.Fatalf("RemoteReapRun: want ErrMergeNoBaseRepo, got %v", err)
	}
}

func TestRemoteReapManagerBusyIsTypedError(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	remoteReapRunning.Lock()
	defer remoteReapRunning.Unlock()
	if _, err := m.RemoteReapRun(context.Background()); err != ErrRemoteReapBusy {
		t.Fatalf("want ErrRemoteReapBusy, got %v", err)
	}
}
