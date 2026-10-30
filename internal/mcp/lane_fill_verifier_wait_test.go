package mcp

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The lane fill body as the daemon sends it, with a waiting lane and a
// decision that rests on the wait.
const verifierWaitStatusBody = `{
	"mode":"on",
	"lanes":[
		{"id":"claude-1","type":"claude","state":"waiting_verifier",
		 "occupant":{"prd_id":"wa","title":"waiting work","backend":"claude-code",
		 "waiting_on_verifier":true,"wait_reason":"waiting for a capacity slot to finish verification"},
		 "waiting_on_verifier":true,"wait_reason":"waiting for a capacity slot to finish verification"},
		{"id":"claude-2","type":"claude","state":"occupied",
		 "occupant":{"prd_id":"wk","title":"working work","backend":"claude-code"}}
	],
	"decisions":[
		{"lane_id":"claude-1","action":"start_prd","wait_reason":"waiting for a capacity slot to finish verification"},
		{"lane_id":"claude-2","action":"idle","repeat_count":1}
	]
}`

// TestVerifierWaitMCPOutput: autonomous_lane_fill_status passes the daemon's
// body through untouched, so a waiting lane arrives with waiting_on_verifier
// and wait_reason and the decision on that lane with wait_reason.
func TestVerifierWaitMCPOutput(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/lane-fill" {
			t.Errorf("proxied %s %s, want GET /api/autonomous/lane-fill", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(verifierWaitStatusBody))
	})
	res, err := s.handleAutonomousLaneFillStatus(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	text := automationText(t, res)
	for _, want := range []string{
		`"waiting_on_verifier":true`,
		`"wait_reason":"waiting for a capacity slot to finish verification"`,
		`"state":"waiting_verifier"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("MCP output lacks %s: %s", want, text)
		}
	}
}

// TestVerifierWaitDescription: the tool description says, in plain words,
// what waiting_verifier and waiting_on_verifier mean, and it never uses the
// tracker word PRD or any tracker ID.
func TestVerifierWaitDescription(t *testing.T) {
	desc := (&Server{}).toolAutonomousLaneFillStatus().Description
	for _, name := range []string{"waiting_verifier", "waiting_on_verifier", "wait_reason"} {
		if !strings.Contains(desc, name) {
			t.Errorf("description does not name %q: %s", name, desc)
		}
	}
	if strings.Contains(desc, "PRD") {
		t.Errorf("description must not use the tracker word: %s", desc)
	}
}

// TestVerifierWaitOpenAPINames: both OpenAPI copies keep the lane fill field
// names in step with the tool output: the lane and the lane occupant carry
// waiting_on_verifier and wait_reason, the decision records carry
// wait_reason and the lane state enumeration names waiting_verifier.
func TestVerifierWaitOpenAPINames(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	for _, rel := range []string{"internal/server/web/openapi.yaml", "docs/api/openapi.yaml"} {
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
