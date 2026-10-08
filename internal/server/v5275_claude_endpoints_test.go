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

// v9.0.0 major-release model-alias refresh (AGENT.md's major-release rule)
// found full_names stuck at "claude-opus-5"/"claude-sonnet-5" — missing
// the "-5-5" minor-version suffix for the current Opus 5.5/Sonnet 5.5
// models. Pins the corrected values so the next major release's refresh
// has something concrete to diff against, instead of re-discovering the
// drift from scratch.
func TestHandleClaudeModels_FullNamesCurrentAsOfV9(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/llm/claude/models", nil)
	rr := httptest.NewRecorder()
	s.handleClaudeModels(rr, req)

	var got map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fullNames := got["full_names"].([]interface{})
	values := map[string]bool{}
	for _, fn := range fullNames {
		row := fn.(map[string]interface{})
		if v, ok := row["value"].(string); ok {
			values[v] = true
		}
	}
	want := []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-4-5-20251001", "claude-fable-5-1"}
	for _, w := range want {
		if !values[w] {
			t.Errorf("full_names missing %q (got: %v)", w, values)
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
