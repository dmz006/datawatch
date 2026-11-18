package router

import (
	"net/http"
	"strings"
	"testing"
)

// reapCommSample is a GET /api/autonomous/branch-reap body: one
// task sub-branch a pass would back up and delete (with the backup ref
// where the tip lives and what contained it), one held, one backup ref
// and the configured retention.
const reapCommSample = `{"reaped":[
{"automaton_id":"prd-9","branch":"automaton/prd-9/t1",
 "backup_ref":"refs/datawatch/reap-backup/prd-9/t1","contained_in":"automaton/prd-9",
 "commits":2,"reason":"merged into main"}],
"waiting":[
{"automaton_id":"prd-9","branch":"automaton/prd-9/t2","reason":"commits ahead of main"}],
"backups":[{"branch":"automaton/prd-9/t1","ref":"refs/datawatch/reap-backup/prd-9/t1","sha":"abc123"}],
"backup_days":90,"computed_at":"2026-01-02T09:30:00Z"}`

const reapRestoreCommSample = `{"branch":"automaton/prd-9/t1",
"backup_ref":"refs/datawatch/reap-backup/prd-9/t1","sha":"abc123"}`

func TestReapComm_VerbBranchReapRenders(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, reapCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous branch-reap"), " ")
	if log.path != "/api/autonomous/branch-reap" || log.query != "" {
		t.Errorf("path=%s query=%q; want the branch-reap report route with no filters", log.path, log.query)
	}
	if log.method != http.MethodGet {
		t.Errorf("the report verb must be a read (GET), got %s", log.method)
	}
	if log.body != "" {
		t.Errorf("the report verb must send no body, got %q", log.body)
	}
	for _, want := range []string{
		"Branch reaper preview (the report deletes nothing yet)",
		"Would be reaped:",
		"automaton/prd-9/t1  (Automaton prd-9)  merged into main  [backup refs/datawatch/reap-backup/prd-9/t1]",
		"Held (branch kept):",
		"automaton/prd-9/t2  (Automaton prd-9)  commits ahead of main",
		"Backup refs (retention 90 whole days):",
		"refs/datawatch/reap-backup/prd-9/t1 -> abc123  (branch automaton/prd-9/t1)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks shared render line %q: %q", want, got)
		}
	}
}

func TestReapComm_VerbBranchReapRestorePostsBranch(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, reapRestoreCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous branch-reap-restore automaton/prd-9/t1"), " ")
	if log.path != "/api/autonomous/branch-reap/restore" {
		t.Errorf("path=%s; want the restore route", log.path)
	}
	if log.method != http.MethodPost {
		t.Errorf("restore must POST, got %s", log.method)
	}
	if log.body != `{"branch":"automaton/prd-9/t1"}` {
		t.Errorf("request body = %q; want the one branch name", log.body)
	}
	if !strings.Contains(got, "Branch restored from its newest backup ref") {
		t.Errorf("reply lacks the shared restore render heading: %q", got)
	}
	if !strings.Contains(got, "branch: automaton/prd-9/t1") {
		t.Errorf("reply lacks the restored branch: %q", got)
	}
	if !strings.Contains(got, "backup ref: refs/datawatch/reap-backup/prd-9/t1") {
		t.Errorf("reply lacks the backup ref: %q", got)
	}
	if !strings.Contains(got, "commit: abc123") {
		t.Errorf("reply lacks the commit: %q", got)
	}
}

func TestReapComm_UsageRefusals(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, reapCommSample)
	r := setupPeerRouter(t, port)

	// A branch-reap report with extra arguments is refused with the
	// branch-reap usage (and the restore usage next to it), without a
	// daemon call.
	got := strings.Join(r.HandleTestMessage("autonomous branch-reap now please"), " ")
	if !strings.Contains(got, "usage: autonomous branch-reap") {
		t.Errorf("extra args must be refused with the usage, got: %q", got)
	}
	// A restore without a branch name is refused with the restore usage,
	// without a daemon call.
	got = strings.Join(r.HandleTestMessage("autonomous branch-reap-restore"), " ")
	if !strings.Contains(got, "usage: autonomous branch-reap-restore <branch>") {
		t.Errorf("missing branch must be refused with the usage, got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("the refusals must not reach the daemon, got %d calls", log.calls)
	}

	// The autonomous help lists both branch-reap verbs next to the merge
	// verbs.
	got = strings.Join(r.HandleTestMessage("autonomous reapx"), " ")
	if !strings.Contains(got, "unknown verb reapx") ||
		!strings.Contains(got, "|merge|safe-to-delete|branch-reap|branch-reap-restore <branch>|") {
		t.Errorf("an unknown verb must show the help listing the branch-reap verbs, got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("an unknown verb must not reach the daemon, got %d calls", log.calls)
	}
}
