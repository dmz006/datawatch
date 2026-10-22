package autonomous

// Live, opt-in smoke for the Automation control operations (pause, resume,
// cancel with its required in-flight choice, insert_member with the one-urgent
// cap, approve_insert). It drives a SANDBOX daemon over its REST API, in the same
// way as TestAutomationForkJoinLiveSmoke in automation_live_smoke_test.go, and
// shares that file's client helpers.
//
// It is skipped unless DW_LIVE_AUTOMATION_CONTROL_SMOKE is set, so `go test ./...`
// in CI skips it. The variable has no default: its value is the base URL of the
// sandbox daemon the operator stood up, so the test can never reach production by
// accident. Environment:
//
//	DW_LIVE_AUTOMATION_CONTROL_SMOKE=https://host:port  base URL of the sandbox daemon
//	DW_BASE_TOKEN=...                                   bearer token, if the daemon has one
//	DW_BASE_INSECURE=0                                  verify TLS (default: skip verify)
//	DW_LIVE_AUTOMATION_PROJECT_DIR=...                  project_dir for the smoke PRDs (optional)
//
// Run it against a sandbox daemon the operator started:
//
//	DW_LIVE_AUTOMATION_CONTROL_SMOKE=https://127.0.0.1:18443 \
//	  go test -count=1 -timeout 10m -run TestAutomationControlLiveSmoke -v ./internal/autonomous/
//
// The test only calls the REST API of that daemon. It never touches the terminal
// multiplexer or any process on the host, and every cancel it sends carries
// cancel_in_flight=false, so the session-end path is never taken. The member
// PRDs are drafts with placeholder specs that nothing runs.
//
// The cancel_in_flight=true path is not exercised: it ends the member session of
// every in-flight member, and a sandbox smoke has no safe way to make a member
// in flight. That path is left to the Manager-level tests.
//
// Cleanup cancels (cancel_in_flight=false) and then deletes each Automation the
// store allows; draft smoke PRDs are left in place and logged.

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

const controlSmokeEnv = "DW_LIVE_AUTOMATION_CONTROL_SMOKE"

// controlSmokeAutomation is the subset of an Automation record this smoke reads.
type controlSmokeAutomation struct {
	ID             string          `json:"id"`
	Version        int64           `json:"version"`
	Status         string          `json:"status"`
	PendingInserts []PendingInsert `json:"pending_inserts"`
}

func controlSmokeDecode(t *testing.T, step string, out []byte) controlSmokeAutomation {
	t.Helper()
	var a controlSmokeAutomation
	if err := json.Unmarshal(out, &a); err != nil {
		t.Errorf("%s: decode automation: %v body=%s", step, err, out)
	}
	return a
}

// controlSmokeGet reads the current record of an Automation.
func controlSmokeGet(t *testing.T, c *smokeClient, id string) controlSmokeAutomation {
	t.Helper()
	return controlSmokeDecode(t, "get "+id, c.must(t, http.MethodGet, "/api/automations/"+id, nil, http.StatusOK))
}

// controlSmokeOp POSTs one control operation at the Automation's current
// version, merging extra into the body. Any status outside want is a t.Errorf.
// It returns the status and the decoded record (zero on a non-200).
func controlSmokeOp(t *testing.T, c *smokeClient, step, id, op string, extra map[string]any, want ...int) (int, controlSmokeAutomation) {
	t.Helper()
	body := map[string]any{
		"actor": "operator", "reason": "BL431 live control smoke",
		"expected_version": controlSmokeGet(t, c, id).Version,
	}
	for k, v := range extra {
		body[k] = v
	}
	status, out := c.do(t, http.MethodPost, "/api/automations/"+id+"/"+op, body)
	t.Logf("%s: POST /api/automations/%s/%s -> %d %s", step, id, op, status, strings.TrimSpace(string(out)))
	ok := false
	for _, w := range want {
		ok = ok || status == w
	}
	if !ok {
		t.Errorf("%s: %s returned %d, want one of %v body=%s", step, op, status, want, out)
	}
	if status != http.StatusOK {
		return status, controlSmokeAutomation{}
	}
	return status, controlSmokeDecode(t, step, out)
}

// controlSmokeDraft creates a draft Automation with two PRD members and one
// outgoing edge (m1 -> m2), registers it for cleanup, and returns it with the
// member PRD IDs.
func controlSmokeDraft(t *testing.T, c *smokeClient, name, projectDir string, created, prdIDs *[]string) controlSmokeAutomation {
	t.Helper()
	p1 := smokeCreatePRD(t, c, name+" m1", projectDir)
	p2 := smokeCreatePRD(t, c, name+" m2", projectDir)
	*prdIDs = append(*prdIDs, p1, p2)
	a := smokeCreate(t, c, smokeAutomationBody(name, "m1", []any{
		map[string]any{"id": "m1", "prd_id": p1, "on_complete": "m2"},
		map[string]any{"id": "m2", "prd_id": p2},
	}))
	*created = append(*created, a.ID)
	t.Logf("create %s: id=%s version=%d status=%s prds=[%s %s]", name, a.ID, a.Version, a.Status, p1, p2)
	return controlSmokeAutomation{ID: a.ID, Version: a.Version, Status: a.Status}
}

func TestAutomationControlLiveSmoke(t *testing.T) {
	base := strings.TrimRight(os.Getenv(controlSmokeEnv), "/")
	if base == "" {
		t.Skip("set DW_LIVE_AUTOMATION_CONTROL_SMOKE=https://host:port of a sandbox daemon to run the automation control live smoke")
	}
	c := newSmokeClient(base, os.Getenv("DW_BASE_TOKEN"))
	projectDir := os.Getenv("DW_LIVE_AUTOMATION_PROJECT_DIR")

	var created, prdIDs []string
	t.Cleanup(func() {
		var leftover []string
		for i := len(created) - 1; i >= 0; i-- {
			id := created[i]
			status, out := c.do(t, http.MethodDelete, "/api/automations/"+id, nil)
			if status == http.StatusConflict {
				// Still activated: cancel without touching in-flight members, then delete.
				cs, cout := c.do(t, http.MethodPost, "/api/automations/"+id+"/cancel", map[string]any{
					"actor": "operator", "reason": "BL431 live control smoke cleanup",
					"expected_version": controlSmokeGet(t, c, id).Version, "cancel_in_flight": false,
				})
				t.Logf("cleanup cancel %s -> %d %s", id, cs, strings.TrimSpace(string(cout)))
				status, out = c.do(t, http.MethodDelete, "/api/automations/"+id, nil)
			}
			if status != http.StatusNoContent {
				t.Logf("cleanup delete %s -> %d %s", id, status, strings.TrimSpace(string(out)))
				leftover = append(leftover, id)
			}
		}
		t.Logf("deleted %d of %d smoke automations; left on the sandbox: %v", len(created)-len(leftover), len(created), leftover)
		t.Logf("draft smoke PRDs left for operator review: %v", prdIDs)
	})

	// (1) + (2) A draft Automation with 2 members and one outgoing edge, then activate.
	main := controlSmokeDraft(t, c, "control smoke main", projectDir, &created, &prdIDs)
	if main.Status != "draft" {
		t.Errorf("create: status %q, want draft", main.Status)
	}
	controlSmokeOp(t, c, "activate", main.ID, "activate", nil, http.StatusOK)
	cur := controlSmokeGet(t, c, main.ID)
	t.Logf("after activate: status=%s version=%d", cur.Status, cur.Version)

	// (3) pause -> paused, resume -> active. A draft member PRD may block the
	// Automation on activation; a blocked Automation cannot be paused (409), which
	// is logged and then the pause/resume pair is skipped for it.
	if cur.Status == "active" {
		if _, a := controlSmokeOp(t, c, "pause", main.ID, "pause", nil, http.StatusOK); a.Status != "paused" {
			t.Errorf("pause: status %q, want paused", a.Status)
		}
		if _, a := controlSmokeOp(t, c, "resume", main.ID, "resume", nil, http.StatusOK); a.Status != "active" && a.Status != "blocked" {
			// resume hands the Automation to the coordinator, which may block it again.
			t.Errorf("resume: status %q, want active", a.Status)
		}
	} else {
		controlSmokeOp(t, c, "pause (blocked automation)", main.ID, "pause", nil, http.StatusConflict)
		t.Logf("automation is %s after activation (placeholder member PRDs are drafts); pause/resume transition not reachable on this sandbox", cur.Status)
	}

	// (4) cancel on a separate throwaway Automation: the in-flight choice has no default.
	cancelA := controlSmokeDraft(t, c, "control smoke cancel", projectDir, &created, &prdIDs)
	controlSmokeOp(t, c, "activate cancel target", cancelA.ID, "activate", nil, http.StatusOK)
	controlSmokeOp(t, c, "cancel without cancel_in_flight", cancelA.ID, "cancel", nil, http.StatusBadRequest)
	if got := controlSmokeGet(t, c, cancelA.ID); got.Status == "cancelled" {
		t.Errorf("cancel without a choice cancelled the automation")
	}
	_, cancelled := controlSmokeOp(t, c, "cancel with cancel_in_flight=false", cancelA.ID, "cancel",
		map[string]any{"cancel_in_flight": false}, http.StatusOK)
	if cancelled.Status != "cancelled" {
		t.Errorf("cancel response status %q, want cancelled", cancelled.Status)
	}
	if got := controlSmokeGet(t, c, cancelA.ID); got.Status != "cancelled" {
		t.Errorf("GET after cancel: status %q, want cancelled", got.Status)
	}

	// (5) insert_member into a throwaway Automation: an insert into an activated
	// Automation is held pending. Urgent twice -> second is a 409; approve the first.
	insA := controlSmokeDraft(t, c, "control smoke insert", projectDir, &created, &prdIDs)
	controlSmokeOp(t, c, "activate insert target", insA.ID, "activate", nil, http.StatusOK)
	u1 := smokeCreatePRD(t, c, "urgent one", projectDir)
	u2 := smokeCreatePRD(t, c, "urgent two", projectDir)
	prdIDs = append(prdIDs, u1, u2)

	controlSmokeOp(t, c, "insert without reason", insA.ID, "insert_member",
		map[string]any{"reason": "", "member": map[string]any{"prd_id": u1}, "urgent": true}, http.StatusBadRequest)
	_, first := controlSmokeOp(t, c, "insert urgent #1", insA.ID, "insert_member",
		map[string]any{"member": map[string]any{"prd_id": u1, "name": "urgent one"}, "urgent": true}, http.StatusOK)
	controlSmokeOp(t, c, "insert urgent #2 (cap)", insA.ID, "insert_member",
		map[string]any{"member": map[string]any{"prd_id": u2, "name": "urgent two"}, "urgent": true}, http.StatusConflict)

	var pendingID string
	for _, p := range first.PendingInserts {
		if p.PRDID == u1 {
			pendingID = p.ID
		}
	}
	if pendingID == "" {
		t.Logf("urgent insert #1 is not pending (landed directly or status %s); approve_insert skipped", first.Status)
		return
	}
	// approve_insert of an urgent insert needs exactly one active member to follow;
	// a blocked sandbox Automation answers 409 for that, which is accepted.
	controlSmokeOp(t, c, "approve_insert #1", insA.ID, "approve_insert",
		map[string]any{"member_id": pendingID}, http.StatusOK, http.StatusConflict)
}
