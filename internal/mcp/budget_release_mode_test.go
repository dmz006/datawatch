package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestMCPReleaseModeWriteTagsMCPSurface checks that a release approval mode set
// through the MCP config write goes to PUT /api/config tagged surface=mcp, so
// the daemon records the change with the mcp surface and a decision record.
func TestMCPReleaseModeWriteTagsMCPSurface(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotBody map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	key := "autonomous.merge_pipeline.release_approval.patch"
	if _, err := s.putPlanTierAllocation(map[string]any{key: "manual"}); err != nil {
		t.Fatalf("putPlanTierAllocation: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/config" {
		t.Errorf("request = %s %s, want PUT /api/config", gotMethod, gotPath)
	}
	if gotQuery != "surface=mcp" {
		t.Errorf("query = %q, want surface=mcp", gotQuery)
	}
	if gotBody[key] != "manual" {
		t.Errorf("body[%s] = %v, want manual", key, gotBody[key])
	}
}
