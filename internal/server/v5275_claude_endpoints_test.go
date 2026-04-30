// v5.27.5 — tests for the static claude-options endpoints
// (handleClaudeModels / handleClaudeEfforts / handleClaudePermissionModes).
// All three are static lists per operator decision 2026-04-29
// (BL206 frozen — no Anthropic /v1/models query at runtime).

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleClaudeModels_Shape(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/llm/claude/models", nil)
	rr := httptest.NewRecorder()
	s.handleClaudeModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["source"] != "hardcoded" {
		t.Errorf("source=%v want hardcoded", got["source"])
	}
	if _, ok := got["aliases"].([]interface{}); !ok {
		t.Errorf("aliases missing or wrong type")
	}
	if _, ok := got["full_names"].([]interface{}); !ok {
		t.Errorf("full_names missing or wrong type")
	}
	// At least the headline aliases must be present.
	aliases := got["aliases"].([]interface{})
	wantAlias := map[string]bool{"opus": false, "sonnet": false, "haiku": false}
	for _, a := range aliases {
		row := a.(map[string]interface{})
		if v, ok := row["value"].(string); ok {
			wantAlias[v] = true
		}
	}
	for k, present := range wantAlias {
		if !present {
			t.Errorf("missing alias %q in /api/llm/claude/models response", k)
		}
	}
}

func TestHandleClaudeModels_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/llm/claude/models", nil)
	rr := httptest.NewRecorder()
	s.handleClaudeModels(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
}

func TestHandleClaudeEfforts_Shape(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/llm/claude/efforts", nil)
	rr := httptest.NewRecorder()
	s.handleClaudeEfforts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	levels := got["levels"].([]interface{})
	want := map[string]bool{"low": false, "medium": false, "high": false, "xhigh": false, "max": false}
	for _, l := range levels {
		row := l.(map[string]interface{})
		if v, ok := row["value"].(string); ok {
			want[v] = true
		}
	}
	for k, present := range want {
		if !present {
			t.Errorf("missing effort level %q", k)
		}
	}
}

func TestHandleClaudePermissionModes_IncludesPlan(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/llm/claude/permission_modes", nil)
	rr := httptest.NewRecorder()
	s.handleClaudePermissionModes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	modes := got["modes"].([]interface{})
	hasPlan := false
	for _, m := range modes {
		row := m.(map[string]interface{})
		if v, ok := row["value"].(string); ok && v == "plan" {
			hasPlan = true
		}
	}
	if !hasPlan {
		t.Errorf("permission_modes endpoint must list `plan` — that's the whole point of v5.27.5")
	}
}
