package autonomous

// Live, opt-in fork/join smoke for Phase 3a (docs/plans/2026-10-14-automation-
// orchestration-design.md, "Member kinds", "Validation addenda" and the
// "autonomous_status extension"). It drives a SANDBOX daemon over its REST API
// and checks the multi-cursor surface end to end: fork/join and nested member
// shapes round-trip, validation failures are 400s naming the offending member,
// activation starts the fork so the status row carries active_cursors and a
// join wait (or the Automation lands blocked), and a stale expected_version is
// a 409 on update.
//
// It is skipped unless DW_LIVE_AUTOMATION_SMOKE is set, so `go test ./...` in
// CI skips it. Environment:
//
//	DW_LIVE_AUTOMATION_SMOKE=https://host:port  base URL of the sandbox daemon
//	                                            (required; no default, so the
//	                                            test can never reach production
//	                                            by accident)
//	DW_BASE_TOKEN=...                           bearer token, if the daemon has one
//	DW_BASE_INSECURE=0                          verify TLS (default: skip verify,
//	                                            as the other live scripts do)
//	DW_LIVE_AUTOMATION_PROJECT_DIR=...          project_dir for the smoke PRDs
//	                                            (optional; default: the daemon's
//	                                            own project-dir policy)
//
// Run it against a sandbox daemon the operator started:
//
//	DW_LIVE_AUTOMATION_SMOKE=https://127.0.0.1:18443 \
//	  go test -count=1 -timeout 15m -run TestAutomationForkJoinLiveSmoke -v ./internal/autonomous/
//
// The test only calls the REST API. It never touches tmux or any process on the
// host. Everything it creates is an Automation (deleted in t.Cleanup) or a
// draft smoke PRD (left in place and logged, since PRD deletion is out of scope).
//
// The activated fork/join Automation cannot be deleted: the store removes only
// draft, completed and cancelled Automations, and Phase 3a adds no cancel route
// (cancel_automation is a Phase 3b control op). Cleanup therefore deletes every
// Automation the store allows, accepts exactly that 409 for an activated one,
// and logs it as a leftover for the operator to clear from the sandbox.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	smokeEnv        = "DW_LIVE_AUTOMATION_SMOKE"
	smokeWait       = 3 * time.Minute // a waiting join or blocked Automation must appear within this
	smokePollPeriod = 2 * time.Second
)

// smokeClient is a minimal JSON client for the daemon under test.
type smokeClient struct {
	base  string
	token string
	hc    *http.Client
}

func newSmokeClient(base, token string) *smokeClient {
	return &smokeClient{
		base:  base,
		token: token,
		hc: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: os.Getenv("DW_BASE_INSECURE") != "0", // #nosec G402 -- sandbox daemons use self-signed certs
				},
			},
		},
	}
}

// do sends one request and returns the status and raw body. A nil body sends none.
func (c *smokeClient) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s %s body: %v", method, path, err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s body: %v", method, path, err)
	}
	return resp.StatusCode, out
}

// must is do plus a status check; the response body is in the failure message.
func (c *smokeClient) must(t *testing.T, method, path string, body any, want int) []byte {
	t.Helper()
	status, out := c.do(t, method, path, body)
	if status != want {
		t.Fatalf("%s %s: want %d, got %d body=%s", method, path, want, status, out)
	}
	return out
}

// smokeAutomation is the subset of an Automation record the smoke reads back.
type smokeAutomation struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int64  `json:"version"`
	Status  string `json:"status"`
}

// smokeJoinWait mirrors the join_wait object of an automations[] status row.
type smokeJoinWait struct {
	JoinMemberID    string   `json:"join_member_id"`
	Rule            string   `json:"rule"`
	PendingBranches []string `json:"pending_branches"`
	FailedBranches  []string `json:"failed_branches"`
}

// smokeStatusRow mirrors the fields of an automations[] row this smoke checks.
type smokeStatusRow struct {
	ID            string          `json:"id"`
	Status        string          `json:"status"`
	ActiveCursors json.RawMessage `json:"active_cursors"`
	JoinWait      *smokeJoinWait  `json:"join_wait"`
}

// smokeCreatePRD creates one draft PRD to serve as a branch or child member.
// The spec is a placeholder: nothing here runs it.
func smokeCreatePRD(t *testing.T, c *smokeClient, name, projectDir string) string {
	t.Helper()
	status, out := c.do(t, http.MethodPost, "/api/autonomous/prds", map[string]any{
		"spec":        "BL431 live smoke member " + name + ": placeholder spec, never run",
		"project_dir": projectDir,
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create PRD %s: status %d body=%s", name, status, out)
	}
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &p); err != nil || p.ID == "" {
		t.Fatalf("decode PRD %s: %v body=%s", name, err, out)
	}
	return p.ID
}

// smokeAutomationBody wraps members in the create/update envelope.
func smokeAutomationBody(name, entry string, members []any) map[string]any {
	return map[string]any{
		"name": name, "actor": "operator", "reason": "BL431 live smoke",
		"entry_member_id": entry, "members": members,
	}
}

// smokeForkJoinMembers is the wire graph under test: fork f1 starts two branch
// chains (b1, b2, each one PRD member) at once, and join j1 collects both under
// all_must_succeed.
func smokeForkJoinMembers(prdB1, prdB2 string) []any {
	return []any{
		map[string]any{"id": "f1", "kind": "fork", "fork_members": []string{"b1", "b2"}},
		map[string]any{"id": "b1", "prd_id": prdB1},
		map[string]any{"id": "b2", "prd_id": prdB2},
		map[string]any{"id": "j1", "kind": "join", "join_members": []string{"b1", "b2"}, "on_branch_failure": "all_must_succeed"},
	}
}

// smokeMemberOf returns the wire object for member id inside a body built by
// smokeAutomationBody, so a case can break one field of it.
func smokeMemberOf(t *testing.T, body map[string]any, id string) map[string]any {
	t.Helper()
	for _, m := range body["members"].([]any) {
		if mm := m.(map[string]any); mm["id"] == id {
			return mm
		}
	}
	t.Fatalf("body has no member %s", id)
	return nil
}

// smokeCreate POSTs an Automation and expects 201.
func smokeCreate(t *testing.T, c *smokeClient, body map[string]any) smokeAutomation {
	t.Helper()
	out := c.must(t, http.MethodPost, "/api/automations", body, http.StatusCreated)
	var a smokeAutomation
	if err := json.Unmarshal(out, &a); err != nil || a.ID == "" {
		t.Fatalf("decode create: %v body=%s", err, out)
	}
	return a
}

// smokeGet GETs an Automation and expects 200.
func smokeGet(t *testing.T, c *smokeClient, id string) smokeAutomation {
	t.Helper()
	out := c.must(t, http.MethodGet, "/api/automations/"+id, nil, http.StatusOK)
	var a smokeAutomation
	if err := json.Unmarshal(out, &a); err != nil {
		t.Fatalf("decode automation %s: %v body=%s", id, err, out)
	}
	return a
}

// smokeFindRow returns the automations[] row for id on GET /api/autonomous/status,
// with the raw row for logging.
func smokeFindRow(t *testing.T, c *smokeClient, id string) (smokeStatusRow, []byte, bool) {
	t.Helper()
	out := c.must(t, http.MethodGet, "/api/autonomous/status", nil, http.StatusOK)
	var st struct {
		Automations []json.RawMessage `json:"automations"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		t.Fatalf("decode status: %v body=%s", err, out)
	}
	for _, raw := range st.Automations {
		var row smokeStatusRow
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatalf("decode status row: %v row=%s", err, raw)
		}
		if row.ID == id {
			return row, raw, true
		}
	}
	return smokeStatusRow{}, nil, false
}

func TestAutomationForkJoinLiveSmoke(t *testing.T) {
	base := strings.TrimRight(os.Getenv(smokeEnv), "/")
	if base == "" {
		t.Skip("set DW_LIVE_AUTOMATION_SMOKE=https://host:port of a sandbox daemon to run the fork/join live smoke")
	}
	c := newSmokeClient(base, os.Getenv("DW_BASE_TOKEN"))
	projectDir := os.Getenv("DW_LIVE_AUTOMATION_PROJECT_DIR")

	// Automations are deleted in reverse creation order, so a nested parent
	// goes before the child it points at. The cleanup runs whether the smoke
	// passes or fails.
	var created []string
	var prdIDs []string
	t.Cleanup(func() {
		var leftover []string
		for i := len(created) - 1; i >= 0; i-- {
			status, out := c.do(t, http.MethodDelete, "/api/automations/"+created[i], nil)
			switch {
			case status == http.StatusNoContent:
			case status == http.StatusConflict && strings.Contains(string(out), "can be deleted"):
				// Only an activated Automation is refused (active or blocked). No REST
				// route cancels it in Phase 3a, so it stays on the sandbox.
				leftover = append(leftover, created[i])
			default:
				t.Errorf("delete automation %s: status %d body=%s", created[i], status, out)
			}
		}
		t.Logf("deleted %d of %d smoke automations; activated automations left on the sandbox (no cancel route until Phase 3b): %v",
			len(created)-len(leftover), len(created), leftover)
		t.Logf("draft smoke PRDs left for operator review: %v", prdIDs)
	})

	// Member PRDs: two fork/join branches and one child for the nested Automation.
	prdB1 := smokeCreatePRD(t, c, "branch one", projectDir)
	prdB2 := smokeCreatePRD(t, c, "branch two", projectDir)
	prdChild := smokeCreatePRD(t, c, "nested child", projectDir)
	prdIDs = append(prdIDs, prdB1, prdB2, prdChild)

	// (1) The child Automation is created first as a draft, then the nested
	// Automation nests it, and the fork/join Automation runs its two branches.
	child := smokeCreate(t, c, smokeAutomationBody("smoke child", "p1", []any{
		map[string]any{"id": "p1", "prd_id": prdChild},
	}))
	created = append(created, child.ID)

	nested := smokeCreate(t, c, smokeAutomationBody("smoke nested", "n1", []any{
		map[string]any{"id": "n1", "kind": "automation", "automation_id": child.ID},
	}))
	created = append(created, nested.ID)

	fj := smokeCreate(t, c, smokeAutomationBody("smoke fork join", "f1", smokeForkJoinMembers(prdB1, prdB2)))
	created = append(created, fj.ID)
	if fj.Version != 1 || fj.Status != "draft" {
		t.Fatalf("fork/join create: version=%d status=%q, want 1 / draft", fj.Version, fj.Status)
	}

	// (2) Validation: each bad graph is a 400 whose body names the offending
	// member ID, and nothing is written.
	badPairing := smokeAutomationBody("smoke bad pairing", "f1", smokeForkJoinMembers(prdB1, prdB2))
	smokeMemberOf(t, badPairing, "j1")["join_members"] = []string{"b1"}
	if out := c.must(t, http.MethodPost, "/api/automations", badPairing, http.StatusBadRequest); !strings.Contains(string(out), "j1") {
		t.Errorf("bad fork/join pairing 400 does not name j1: %s", out)
	}

	dupNested := smokeAutomationBody("smoke dup nested", "n9", []any{
		map[string]any{"id": "n9", "kind": "automation", "automation_id": child.ID},
	})
	if out := c.must(t, http.MethodPost, "/api/automations", dupNested, http.StatusBadRequest); !strings.Contains(string(out), "n9") {
		t.Errorf("duplicate nested ownership 400 does not name n9: %s", out)
	}

	// (3) Activate the fork/join Automation and poll until the join is waiting
	// or the Automation is blocked. Either is a valid live outcome for draft
	// member PRDs; the status row must carry active_cursors in both cases.
	c.must(t, http.MethodPost, "/api/automations/"+fj.ID+"/activate", map[string]any{
		"actor": "operator", "reason": "BL431 live smoke", "expected_version": fj.Version,
	}, http.StatusOK)

	deadline := time.Now().Add(smokeWait)
	var row smokeStatusRow
	var raw []byte
	for {
		r, rawRow, ok := smokeFindRow(t, c, fj.ID)
		if ok {
			row, raw = r, rawRow
			if r.JoinWait != nil || r.Status == string(AutomationBlocked) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("join neither waiting nor blocked within %s; last row=%s", smokeWait, raw)
		}
		time.Sleep(smokePollPeriod)
	}
	t.Logf("status row after activation: %s", raw)

	if !strings.HasPrefix(string(row.ActiveCursors), "[") {
		t.Errorf("active_cursors = %s, want a JSON array", row.ActiveCursors)
	}
	if w := row.JoinWait; w != nil {
		if w.JoinMemberID != "j1" || w.Rule != "all_must_succeed" {
			t.Errorf("join_wait = %+v, want j1 under all_must_succeed", w)
		}
		seen := map[string]int{}
		for _, b := range append(append([]string{}, w.PendingBranches...), w.FailedBranches...) {
			seen[b]++
		}
		if len(seen) != 2 || seen["b1"] != 1 || seen["b2"] != 1 {
			t.Errorf("join_wait pending=%v failed=%v, want b1 and b2 each exactly once", w.PendingBranches, w.FailedBranches)
		}
	} else {
		t.Logf("join_wait absent; automation is %s (blocked path)", row.Status)
	}

	// (4) Stale version: activation moves the Automation past the version the
	// smoke created, so a PUT still carrying that version is a 409 and writes
	// nothing.
	cur := smokeGet(t, c, fj.ID)
	if cur.Version <= fj.Version {
		t.Fatalf("activation did not bump version: created %d, now %d", fj.Version, cur.Version)
	}
	status, out := c.do(t, http.MethodPut, "/api/automations/"+fj.ID, map[string]any{
		"name": "smoke fork join stale", "actor": "operator", "reason": "stale writer",
		"expected_version": fj.Version, "entry_member_id": "f1", "members": smokeForkJoinMembers(prdB1, prdB2),
	})
	if status != http.StatusConflict {
		t.Fatalf("stale PUT: want 409, got %d body=%s", status, out)
	}
	if !strings.Contains(string(out), "version mismatch") {
		t.Errorf("stale PUT 409 body %q does not say version mismatch", out)
	}
	if after := smokeGet(t, c, fj.ID); after.Name != fj.Name || after.Version != cur.Version {
		t.Errorf("stale PUT wrote through: name=%q version=%d, want %q / %d", after.Name, after.Version, fj.Name, cur.Version)
	}
}
