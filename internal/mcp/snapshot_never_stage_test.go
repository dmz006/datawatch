package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// snapshot_never_stage — the autonomous_config_set tool schema must declare
// an array-of-strings property, the tool must forward it to the REST write,
// and an explicit [] must be sent as [] (present-versus-absent), not
// elided, so the operator can clear the list back to "no protected
// patterns". The autonomous_config_get description must name it.

func TestAutonomousConfigSet_SchemaDeclaresSnapshotNeverStageArray(t *testing.T) {
	s := &Server{}
	var tool struct {
		InputSchema struct {
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
				Items       struct {
					Type string `json:"type"`
				} `json:"items"`
			} `json:"properties"`
		} `json:"inputSchema"`
	}
	b, err := json.Marshal(s.toolAutonomousConfigSet())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &tool); err != nil {
		t.Fatal(err)
	}
	p, ok := tool.InputSchema.Properties["snapshot_never_stage"]
	if !ok {
		t.Fatalf("autonomous_config_set schema lacks snapshot_never_stage property: %s", b)
	}
	if p.Type != "array" {
		t.Fatalf("snapshot_never_stage schema type = %q, want array", p.Type)
	}
	if p.Items.Type != "string" {
		t.Fatalf("snapshot_never_stage items type = %q, want string", p.Items.Type)
	}
}

func TestAutonomousConfigGet_DescriptionNamesSnapshotNeverStage(t *testing.T) {
	s := &Server{}
	b, err := json.Marshal(s.toolAutonomousConfigGet())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "snapshot_never_stage") {
		t.Fatal("autonomous_config_get description lacks snapshot_never_stage")
	}
}

func TestAutonomousConfigSet_ForwardsSnapshotNeverStageToREST(t *testing.T) {
	var putBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/config" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		putBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	// A value list is forwarded as-is.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"snapshot_never_stage": []any{".env", "node_modules/"},
	})); err != nil {
		t.Fatalf("handleAutonomousConfigSet: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(putBody, &got); err != nil {
		t.Fatalf("decode PUT body: %v (%s)", err, putBody)
	}
	var arr []string
	if err := json.Unmarshal(got["snapshot_never_stage"], &arr); err != nil {
		t.Fatalf("PUT body lacks snapshot_never_stage: %s", putBody)
	}
	if len(arr) != 2 || arr[0] != ".env" || arr[1] != "node_modules/" {
		t.Fatalf("forwarded snapshot_never_stage = %v", arr)
	}

	// An explicit empty list must be sent as [] (present), not elided —
	// that is how the operator clears the list back to "no protected
	// patterns".
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"snapshot_never_stage": []any{},
	})); err != nil {
		t.Fatalf("handleAutonomousConfigSet (empty): %v", err)
	}
	if !strings.Contains(string(putBody), `"snapshot_never_stage":[]`) {
		t.Fatalf("explicit [] not forwarded verbatim; PUT body = %s", putBody)
	}

	// Absent argument: the key must not appear in the PUT body at all.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"auto_fix_retries": 2,
	})); err != nil {
		t.Fatalf("handleAutonomousConfigSet (absent): %v", err)
	}
	if strings.Contains(string(putBody), "snapshot_never_stage") {
		t.Fatalf("absent snapshot_never_stage leaked into PUT body: %s", putBody)
	}
}
