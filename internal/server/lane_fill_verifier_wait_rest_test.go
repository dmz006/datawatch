package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// The two OpenAPI copies that must keep the lane fill field names in step
// with the REST body, relative to the repository root.
var verifierWaitOpenAPICopies = []string{"internal/server/web/openapi.yaml", "docs/api/openapi.yaml"}

// repoRootOf finds the repository root from this test file's path.
func repoRootOf(t *testing.T, depth int) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(depth)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

const verifierWaitReason = "waiting for a capacity slot to finish verification"

// verifierWaitRESTStatus is a status with one waiting lane, one working
// lane and one decision that rests on the wait plus one that does not.
func verifierWaitRESTStatus() autonomous.LaneFillStatus {
	waiting := &autonomous.LaneOccupant{
		PRDID: "wa", Title: "waiting work", Backend: "claude-code",
		WaitingOnVerifier: true, WaitReason: verifierWaitReason,
	}
	return autonomous.LaneFillStatus{
		Mode:       "on",
		LastPollAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
		Lanes: []autonomous.LaneFillLaneStatus{
			{ID: "claude-1", Type: "claude", State: "waiting_verifier",
				Occupant: waiting, WaitingOnVerifier: true, WaitReason: verifierWaitReason},
			{ID: "claude-2", Type: "claude", State: "occupied",
				Occupant: &autonomous.LaneOccupant{PRDID: "wk", Title: "working work", Backend: "claude-code"}},
		},
		Decisions: []autonomous.LaneFillDecision{
			{Mode: "on", LaneID: "claude-1", LaneType: "claude", Action: "start_prd",
				PRDID: "r1", Title: "ready work", WaitReason: verifierWaitReason},
			{Mode: "on", LaneID: "claude-2", LaneType: "claude", Action: "idle", RepeatCount: 1},
		},
	}
}

// TestVerifierWaitRESTBody: GET /api/autonomous/lane-fill serialises the
// status structs, so a waiting lane carries waiting_on_verifier and
// wait_reason (on the lane and on its occupant) and a decision that rests
// on the wait carries wait_reason; a lane or a decision that does not wait
// carries neither.
func TestVerifierWaitRESTBody(t *testing.T) {
	spy := newLaneSpy()
	spy.status = verifierWaitRESTStatus()
	s := &Server{autonomousMgr: spy}
	rec := getLaneFill(s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET lane-fill: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"waiting_on_verifier":true`) {
		t.Errorf("body lacks an exact \"waiting_on_verifier\":true field: %s", body)
	}
	if !strings.Contains(body, "\"wait_reason\":\""+verifierWaitReason+"\"") {
		t.Errorf("body lacks the wait_reason value: %s", body)
	}

	var st struct {
		Lanes     []map[string]json.RawMessage `json:"lanes"`
		Decisions []map[string]json.RawMessage `json:"decisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || len(st.Lanes) != 2 || len(st.Decisions) != 2 {
		t.Fatal(err)
	}

	waitLane, ok := st.Lanes[0]["waiting_on_verifier"]
	if !ok || string(waitLane) != "true" {
		t.Errorf("waiting lane lacks waiting_on_verifier true, got %q (present %v)", string(waitLane), ok)
	}
	if raw, ok := st.Lanes[0]["wait_reason"]; !ok || strings.Trim(string(raw), `"`) != verifierWaitReason {
		t.Errorf("waiting lane wait_reason = %q (present %v)", string(raw), ok)
	}
	if _, ok := st.Lanes[1]["waiting_on_verifier"]; ok {
		t.Errorf("working lane must not carry waiting_on_verifier: %s", string(st.Lanes[1]["waiting_on_verifier"]))
	}
	if _, ok := st.Lanes[1]["wait_reason"]; ok {
		t.Errorf("working lane must not carry wait_reason")
	}
	if raw, ok := st.Lanes[1]["state"]; !ok || strings.Trim(string(raw), `"`) != "occupied" {
		t.Errorf("working lane state = %q (present %v), want occupied", string(raw), ok)
	}
	if raw, ok := st.Lanes[0]["state"]; !ok || strings.Trim(string(raw), `"`) != "waiting_verifier" {
		t.Errorf("waiting lane state = %q (present %v), want waiting_verifier", string(raw), ok)
	}

	// The occupant of the waiting lane carries the same two fields.
	var raw struct {
		Lanes []json.RawMessage `json:"lanes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || len(raw.Lanes) < 1 {
		t.Fatal(err)
	}
	var lane0 struct {
		Occupant *struct {
			WaitingOnVerifier bool   `json:"waiting_on_verifier"`
			WaitReason        string `json:"wait_reason"`
		} `json:"occupant"`
	}
	if err := json.Unmarshal(raw.Lanes[0], &lane0); err != nil || lane0.Occupant == nil {
		t.Fatalf("decode waiting lane: %v", err)
	}
	if !lane0.Occupant.WaitingOnVerifier || lane0.Occupant.WaitReason != verifierWaitReason {
		t.Errorf("waiting lane occupant: %+v", lane0.Occupant)
	}

	if _, ok := st.Decisions[0]["wait_reason"]; !ok {
		t.Errorf("decision resting on the wait lacks wait_reason: %s", body)
	}
	if _, ok := st.Decisions[1]["wait_reason"]; ok {
		t.Errorf("a decision that does not rest on the wait must not carry wait_reason: %s", body)
	}
}

// TestVerifierWaitOpenAPINames: both OpenAPI copies keep the lane fill field
// names in step with the body: the lane and the lane occupant carry
// waiting_on_verifier and wait_reason, the decision record carries
// wait_reason and the lane state enumeration names waiting_verifier.
func TestVerifierWaitOpenAPINames(t *testing.T) {
	root := repoRootOf(t, 1)
	for _, rel := range verifierWaitOpenAPICopies {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := string(data)
		for _, name := range []string{"waiting_on_verifier", "wait_reason", "waiting_verifier"} {
			if !strings.Contains(text, name) {
				t.Errorf("%s lacks the lane fill field name %q", rel, name)
			}
		}
		if !strings.Contains(text, "waiting_verifier:") {
			t.Errorf("%s never documents the waiting_verifier lane state", rel)
		}
	}
}
