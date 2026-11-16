package autonomous

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// reapFakeGit is a scripted MergeGitRunner: each (dir, args) key maps to a
// scripted stdout, or a scripted failure, and every call is recorded in order
// so tests can assert the backup-ref order against the branch delete.
type reapFakeGit struct {
	out   map[string]string
	fail  map[string]bool
	calls []string
}

func (f *reapFakeGit) key(dir string, args []string) string {
	return dir + " | " + strings.Join(args, " ")
}

func (f *reapFakeGit) Run(_ context.Context, dir string, args ...string) (string, error) {
	k := f.key(dir, args)
	f.calls = append(f.calls, k)
	if f.fail[k] {
		return "", fmt.Errorf("scripted failure: %s", k)
	}
	if v, ok := f.out[k]; ok {
		return v, nil
	}
	return "", fmt.Errorf("unscripted git: %s", k) // like a git failure
}

// callIndex returns the position of the first recorded call containing sub,
// or -1.
func (f *reapFakeGit) callIndex(sub string) int {
	for i, c := range f.calls {
		if strings.Contains(c, sub) {
			return i
		}
	}
	return -1
}

const reapRepo = "/repo"

var reapNow = time.Unix(1760000000, 0).UTC()

func reapTestPRD() *PRD {
	return &PRD{
		ID:         "prd-1",
		ProjectDir: "/wt/prd-1",
		Git:        GitWorkflow{Branch: "automaton/prd-1"},
		Story: []Story{
			{Tasks: []Task{{ID: "t1", WorktreeBranch: "automaton/prd-1--tasks-fix-t1"}}},
		},
	}
}

// baseGit scripts the default-branch answer both passes need.
func baseGit() *reapFakeGit {
	return &reapFakeGit{
		out: map[string]string{
			reapRepo + " | symbolic-ref refs/remotes/origin/HEAD": "refs/remotes/origin/main",
		},
	}
}

func TestReapContainedBranchReaped(t *testing.T) {
	const tb = "automaton/prd-1--tasks-fix-t1"
	const ref = "refs/datawatch/reap-backup/automaton-prd-1--tasks-fix-t1/1760000000"
	git := baseGit()
	git.out = mergeReapOut(git.out, map[string]string{
		reapRepo + " | rev-parse -q --verify refs/heads/automaton/prd-1": "",
		reapRepo + " | cherry main automaton/prd-1":                      "-aaa", // parent fully on main: landed
		reapRepo + " | rev-parse -q --verify refs/heads/" + tb:           "",
		reapRepo + " | cherry automaton/prd-1 " + tb:                     "-bbb\n-ccc",
		reapRepo + " | update-ref " + ref + " " + tb:                     "",
		reapRepo + " | branch -D " + tb:                                  "",
	})

	rep, err := ReapPass(context.Background(), git, reapRepo, []*PRD{reapTestPRD()}, reapNow)
	if err != nil {
		t.Fatalf("ReapPass: %v", err)
	}
	if len(rep.Reaped) != 1 {
		t.Fatalf("Reaped = %d entries %+v, want 1", len(rep.Reaped), rep.Reaped)
	}
	e := rep.Reaped[0]
	if e.Branch != tb || e.AutomatonID != "prd-1" || e.BackupRef != ref || e.ContainedIn != "automaton/prd-1" {
		t.Fatalf("reaped entry = %+v", e)
	}
	if e.Reason == "" || !strings.Contains(e.Reason, "on automaton/prd-1") {
		t.Fatalf("reaped reason = %q", e.Reason)
	}
	if len(rep.Waiting) != 0 {
		t.Fatalf("Waiting = %+v, want none", rep.Waiting)
	}
}

func TestReapBackupWrittenBeforeDelete(t *testing.T) {
	const tb = "automaton/prd-1--tasks-fix-t1"
	const ref = "refs/datawatch/reap-backup/automaton-prd-1--tasks-fix-t1/1760000000"
	git := baseGit()
	git.out = mergeReapOut(git.out, map[string]string{
		reapRepo + " | rev-parse -q --verify refs/heads/automaton/prd-1": "",
		reapRepo + " | cherry main automaton/prd-1":                      "-aaa",
		reapRepo + " | rev-parse -q --verify refs/heads/" + tb:           "",
		reapRepo + " | cherry automaton/prd-1 " + tb:                     "-bbb",
		reapRepo + " | update-ref " + ref + " " + tb:                     "",
		reapRepo + " | branch -D " + tb:                                  "",
	})
	if _, err := ReapPass(context.Background(), git, reapRepo, []*PRD{reapTestPRD()}, reapNow); err != nil {
		t.Fatalf("ReapPass: %v", err)
	}
	backup, del := git.callIndex("update-ref "+ref+" "+tb), git.callIndex("branch -D "+tb)
	if backup == -1 || del == -1 {
		t.Fatalf("calls = %v: backup or delete missing", git.calls)
	}
	if backup > del {
		t.Fatalf("backup ref written after branch delete: %v", git.calls)
	}
}

func TestReapUncontainedBranchKept(t *testing.T) {
	const tb = "automaton/prd-1--tasks-fix-t1"
	git := baseGit()
	git.out = mergeReapOut(git.out, map[string]string{
		reapRepo + " | rev-parse -q --verify refs/heads/automaton/prd-1": "",
		reapRepo + " | cherry main automaton/prd-1":                      "-aaa", // parent landed
		reapRepo + " | rev-parse -q --verify refs/heads/" + tb:           "",
		reapRepo + " | cherry automaton/prd-1 " + tb:                     "+bbb", // commit not on parent
		reapRepo + " | cherry main " + tb:                                "+bbb\n+ccc",
	})
	rep, err := ReapPass(context.Background(), git, reapRepo, []*PRD{reapTestPRD()}, reapNow)
	if err != nil {
		t.Fatalf("ReapPass: %v", err)
	}
	if len(rep.Reaped) != 0 {
		t.Fatalf("Reaped = %+v, want none", rep.Reaped)
	}
	if len(rep.Waiting) != 1 {
		t.Fatalf("Waiting = %+v, want 1", rep.Waiting)
	}
	e := rep.Waiting[0]
	if e.Branch != tb || e.BackupRef != "" {
		t.Fatalf("waiting entry = %+v", e)
	}
	if !strings.Contains(e.Reason, "commit(s)") || !strings.Contains(e.Reason, tb) {
		t.Fatalf("waiting reason = %q", e.Reason)
	}
	if git.callIndex("update-ref") != -1 || git.callIndex("branch -D") != -1 {
		t.Fatalf("an uncontained branch was touched: %v", git.calls)
	}
}

func TestReapPendingParentKeepsChildren(t *testing.T) {
	const tb = "automaton/prd-1--tasks-fix-t1"
	git := baseGit()
	git.out = mergeReapOut(git.out, map[string]string{
		reapRepo + " | rev-parse -q --verify refs/heads/automaton/prd-1": "",
		reapRepo + " | cherry main automaton/prd-1":                      "+aaa", // parent still unlanded
		reapRepo + " | rev-parse -q --verify refs/heads/" + tb:           "",
		// must not even be asked to compare the child:
		reapRepo + " | cherry automaton/prd-1 " + tb: "-bbb",
	})
	rep, err := ReapPass(context.Background(), git, reapRepo, []*PRD{reapTestPRD()}, reapNow)
	if err != nil {
		t.Fatalf("ReapPass: %v", err)
	}
	if len(rep.Reaped) != 0 {
		t.Fatalf("Reaped = %+v, want none", rep.Reaped)
	}
	if len(rep.Waiting) != 1 {
		t.Fatalf("Waiting = %+v, want 1", rep.Waiting)
	}
	e := rep.Waiting[0]
	if !strings.Contains(e.Reason, "pending") {
		t.Fatalf("waiting reason = %q, want it to name the pending landing state", e.Reason)
	}
	if git.callIndex("update-ref") != -1 || git.callIndex("branch -D") != -1 {
		t.Fatalf("a pending parent's children were touched: %v", git.calls)
	}
}

func TestReapDroppedParentReapsOnBase(t *testing.T) {
	const tb = "automaton/prd-1--tasks-fix-t1"
	const ref = "refs/datawatch/reap-backup/automaton-prd-1--tasks-fix-t1/1760000000"
	git := baseGit()
	git.fail = map[string]bool{
		reapRepo + " | rev-parse -q --verify refs/heads/automaton/prd-1": true, // work branch gone
	}
	git.out = mergeReapOut(git.out, map[string]string{
		reapRepo + " | rev-parse -q --verify refs/heads/" + tb: "",
		reapRepo + " | cherry main " + tb:                      "-bbb", // contained in main
		reapRepo + " | update-ref " + ref + " " + tb:           "",
		reapRepo + " | branch -D " + tb:                        "",
	})
	rep, err := ReapPass(context.Background(), git, reapRepo, []*PRD{reapTestPRD()}, reapNow)
	if err != nil {
		t.Fatalf("ReapPass: %v", err)
	}
	if len(rep.Reaped) != 1 || rep.Reaped[0].ContainedIn != "main" {
		t.Fatalf("Reaped = %+v, want 1 reaped contained in main", rep.Reaped)
	}
}

func TestReapMergeInProgressIsANoop(t *testing.T) {
	git := baseGit()
	git.out = mergeReapOut(git.out, map[string]string{
		"/wt/prd-1 | rev-parse -q --verify MERGE_HEAD": "abc123", // a merge is mid-flight
	})
	rep, err := ReapPass(context.Background(), git, reapRepo, []*PRD{reapTestPRD()}, reapNow)
	if err != nil {
		t.Fatalf("ReapPass: %v", err)
	}
	if len(rep.Reaped) != 0 {
		t.Fatalf("Reaped = %+v, want none while a merge is in progress", rep.Reaped)
	}
	if len(rep.Waiting) != 1 {
		t.Fatalf("Waiting = %+v, want 1", rep.Waiting)
	}
	if !strings.Contains(rep.Waiting[0].Reason, "merge") {
		t.Fatalf("waiting reason = %q, want it to name the in-progress merge", rep.Waiting[0].Reason)
	}
	// Early: the base branch was never even resolved, and nothing was
	// backed up or deleted.
	if git.callIndex("symbolic-ref") != -1 || git.callIndex("update-ref") != -1 || git.callIndex("branch -D") != -1 {
		t.Fatalf("a merge-in-progress pass still ran git: %v", git.calls)
	}
}

func TestBranchBackupRestoreNewest(t *testing.T) {
	const tb = "automaton/prd-1--tasks-fix-t1"
	git := baseGit()
	git.out = mergeReapOut(git.out, map[string]string{
		reapRepo + " | for-each-ref --format=%(refname) refs/datawatch/reap-backup/automaton-prd-1--tasks-fix-t1/": "refs/datawatch/reap-backup/automaton-prd-1--tasks-fix-t1/1759000000\nrefs/datawatch/reap-backup/automaton-prd-1--tasks-fix-t1/1760000000\n",
		reapRepo + " | rev-parse refs/datawatch/reap-backup/automaton-prd-1--tasks-fix-t1/1760000000":              "deadbeef",
		reapRepo + " | branch " + tb + " deadbeef":                                                                 "",
	})
	sha, err := RestoreReaped(context.Background(), git, reapRepo, tb)
	if err != nil {
		t.Fatalf("RestoreReaped: %v", err)
	}
	if sha != "deadbeef" {
		t.Fatalf("sha = %q, want deadbeef", sha)
	}
	if !strings.HasSuffix(git.calls[len(git.calls)-1], "branch "+tb+" deadbeef") {
		t.Fatalf("last call = %q, want the branch created from the newest backup", git.calls)
	}
}

func TestBranchBackupRestoreMissingErrors(t *testing.T) {
	const tb = "automaton/prd-1--tasks-fix-t1"
	git := baseGit()
	git.out = mergeReapOut(git.out, map[string]string{
		reapRepo + " | for-each-ref --format=%(refname) refs/datawatch/reap-backup/automaton-prd-1--tasks-fix-t1/": "",
	})
	if _, err := RestoreReaped(context.Background(), git, reapRepo, tb); err == nil {
		t.Fatal("RestoreReaped succeeded with no backup ref, want an error")
	} else if !strings.Contains(err.Error(), "no backup ref") {
		t.Fatalf("error = %q, want it to say no backup ref", err)
	}
	if git.callIndex("branch "+tb) != -1 {
		t.Fatalf("a branch was created from nothing: %v", git.calls)
	}
}

func TestReapBackupExpiry(t *testing.T) {
	const old = "refs/datawatch/reap-backup/automaton-x/1759000000" // ~11.6 days before reapNow
	const fresh = "refs/datawatch/reap-backup/automaton-y/1759900000"
	const weird = "refs/datawatch/reap-backup/automaton-z/not-a-ts"
	git := baseGit()
	git.out = mergeReapOut(git.out, map[string]string{
		reapRepo + " | for-each-ref --format=%(refname) refs/datawatch/reap-backup/": old + "\n" + fresh + "\n" + weird + "\n",
		reapRepo + " | update-ref -d " + old:                                         "",
	})
	removed, err := ExpireBackups(context.Background(), git, reapRepo, reapNow, 10)
	if err != nil {
		t.Fatalf("ExpireBackups: %v", err)
	}
	if len(removed) != 1 || removed[0] != old {
		t.Fatalf("removed = %v, want only the old ref", removed)
	}
	if git.callIndex("update-ref -d "+old) == -1 {
		t.Fatalf("old ref not deleted: %v", git.calls)
	}
	if git.callIndex("update-ref -d "+fresh) != -1 || git.callIndex("update-ref -d "+weird) != -1 {
		t.Fatalf("a fresh or unparseable ref was deleted: %v", git.calls)
	}
}

func TestReapBackupExpiryDefaultDays(t *testing.T) {
	// With days <= 0 the default (30) applies: a 40-day-old ref goes, a
	// 20-day-old one stays.
	const old = "refs/datawatch/reap-backup/automaton-x/1756566400" // ~40 days before reapNow
	const keep = "refs/datawatch/reap-backup/automaton-y/1758272000"
	git := baseGit()
	git.out = mergeReapOut(git.out, map[string]string{
		reapRepo + " | for-each-ref --format=%(refname) refs/datawatch/reap-backup/": old + "\n" + keep + "\n",
		reapRepo + " | update-ref -d " + old:                                         "",
	})
	removed, err := ExpireBackups(context.Background(), git, reapRepo, reapNow, 0)
	if err != nil {
		t.Fatalf("ExpireBackups: %v", err)
	}
	if len(removed) != 1 || removed[0] != old {
		t.Fatalf("removed = %v, want only the 40-day-old ref under the 30-day default", removed)
	}
}

// mergeReapOut returns a fresh map holding both.
func mergeReapOut(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
