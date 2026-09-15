// BL385 Phase 2 — REST handler tests for POST /api/memory/scopes/save
// and POST /api/memory/scopes/delete.
//
// TS-1:  POST save with missing scope → 400
// TS-2:  POST save with missing content → 400
// TS-3:  POST save for prd-shared scope → 200, returns id + scope
// TS-4:  POST save for story-shared scope → 200, stored with story/ role
// TS-5:  POST save then GET recall with prd_id → entry visible
// TS-6:  POST save, then POST delete → 200, entry gone
// TS-7:  POST delete with missing memory_id → 400
// TS-8:  GET recall with prd_id — prd-shared layer included
// TS-9:  GET recall without prd_id — prd-shared layer absent
// TS-10: GET recall with story_id — story-shared layer included

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/memory"
)

// scopeTestBackend is a minimal in-memory Backend for Phase 2 scope tests.
// Reuses fakeDiscussionBackend's stub methods but with ListRecent support.
type scopeTestBackend struct {
	fakeDiscussionBackend
}

func (b *scopeTestBackend) ListRecent(projectDir string, n int) ([]memory.Memory, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []memory.Memory
	for _, m := range b.entries {
		if m.ProjectDir == projectDir {
			out = append(out, m)
			if len(out) >= n {
				break
			}
		}
	}
	return out, nil
}

var _ memory.Backend = (*scopeTestBackend)(nil)

func newScopesTestServer(t *testing.T) (*Server, *scopeTestBackend) {
	t.Helper()
	b := &scopeTestBackend{}
	s := &Server{memoryBackend: b}
	return s, b
}

func scopesSaveRequest(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/scopes/save", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.memoryScopesSave(rr, req)
	return rr
}

func scopesDeleteRequest(t *testing.T, s *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/scopes/delete", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.memoryScopesDelete(rr, req)
	return rr
}

// TS-1: missing scope → 400.
func TestBL385_ScopeSave_MissingScope(t *testing.T) {
	s, _ := newScopesTestServer(t)
	rr := scopesSaveRequest(t, s, map[string]any{
		"scope":   map[string]any{},
		"content": "hello",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TS-2: missing content → 400.
func TestBL385_ScopeSave_MissingContent(t *testing.T) {
	s, _ := newScopesTestServer(t)
	rr := scopesSaveRequest(t, s, map[string]any{
		"scope": map[string]any{"scope": "prd-shared", "project": "proj1", "prd_id": "prd-abc"},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TS-3: save prd-shared → 200, response carries id and scope.
func TestBL385_ScopeSave_PRDShared_Returns200(t *testing.T) {
	s, b := newScopesTestServer(t)
	rr := scopesSaveRequest(t, s, map[string]any{
		"scope":   map[string]any{"scope": "prd-shared", "project": "proj1", "prd_id": "prd-abc"},
		"content": "learned about auth flow",
		"summary": "auth flow",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] == nil {
		t.Error("expected id in response")
	}
	// Verify entry stored under role "prd/prd-abc"
	if len(b.entries) != 1 || b.entries[0].Role != "prd/prd-abc" {
		t.Errorf("expected 1 entry with role prd/prd-abc, got %+v", b.entries)
	}
}

// TS-4: save story-shared → stored under role "story/story-xyz".
func TestBL385_ScopeSave_StoryShared_CorrectRole(t *testing.T) {
	s, b := newScopesTestServer(t)
	rr := scopesSaveRequest(t, s, map[string]any{
		"scope":   map[string]any{"scope": "story-shared", "project": "proj1", "story_id": "story-xyz"},
		"content": "story insight",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if len(b.entries) != 1 || b.entries[0].Role != "story/story-xyz" {
		t.Errorf("expected role story/story-xyz, got %+v", b.entries)
	}
}

// TS-5: POST save then GET recall with prd_id — entry visible.
func TestBL385_ScopeSaveRecall_EntryVisible(t *testing.T) {
	s, _ := newScopesTestServer(t)
	// Save an entry into prd-shared scope.
	rr := scopesSaveRequest(t, s, map[string]any{
		"scope":   map[string]any{"scope": "prd-shared", "project": "proj1", "prd_id": "prd-abc"},
		"content": "prd memory",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("save: expected 200, got %d", rr.Code)
	}
	// Recall — with prd_id the prd-shared layer should be included.
	req := httptest.NewRequest(http.MethodGet, "/api/memory/scopes/recall?project=proj1&prd_id=prd-abc", nil)
	rr2 := httptest.NewRecorder()
	s.memoryRecall(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("recall: expected 200, got %d", rr2.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	count, _ := resp["count"].(float64)
	if count == 0 {
		t.Error("expected at least one result from recall after save")
	}
}

// TS-6: POST save, then POST delete → entry gone.
func TestBL385_ScopeDelete_RemovesEntry(t *testing.T) {
	s, b := newScopesTestServer(t)
	rr := scopesSaveRequest(t, s, map[string]any{
		"scope":   map[string]any{"scope": "prd-shared", "project": "proj1", "prd_id": "prd-abc"},
		"content": "to be deleted",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("save: %d", rr.Code)
	}
	var saveResp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &saveResp); err != nil {
		t.Fatalf("decode save resp: %v", err)
	}
	memID := int64(saveResp["id"].(float64))

	rr2 := scopesDeleteRequest(t, s, map[string]any{
		"scope":     map[string]any{"scope": "prd-shared", "project": "proj1", "prd_id": "prd-abc"},
		"memory_id": memID,
	})
	if rr2.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
	if len(b.entries) != 0 {
		t.Errorf("expected 0 entries after delete, got %d", len(b.entries))
	}
}

// TS-7: POST delete with missing memory_id → 400.
func TestBL385_ScopeDelete_MissingMemoryID(t *testing.T) {
	s, _ := newScopesTestServer(t)
	rr := scopesDeleteRequest(t, s, map[string]any{
		"scope": map[string]any{"scope": "prd-shared", "project": "proj1"},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TS-8: GET recall with prd_id — prd-shared scope present in results.
func TestBL385_RecallWithPRDID_IncludesPRDLayer(t *testing.T) {
	s, b := newScopesTestServer(t)
	// Plant a prd-scoped memory directly via the backend.
	_, _ = b.Save("proj1", "prd learning", "", "prd/prd-abc", "", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/memory/scopes/recall?project=proj1&prd_id=prd-abc", nil)
	rr := httptest.NewRecorder()
	s.memoryRecall(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	results, _ := resp["results"].([]any)
	found := false
	for _, r := range results {
		rm, _ := r.(map[string]any)
		if rm["scope"] == "prd-shared" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected prd-shared scope in recall results when prd_id provided; got %v", resp)
	}
}

// TS-9: GET recall without prd_id — prd-shared absent from results.
func TestBL385_RecallWithoutPRDID_ExcludesPRDLayer(t *testing.T) {
	s, b := newScopesTestServer(t)
	_, _ = b.Save("proj1", "prd learning", "", "prd/prd-abc", "", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/memory/scopes/recall?project=proj1", nil)
	rr := httptest.NewRecorder()
	s.memoryRecall(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	results, _ := resp["results"].([]any)
	for _, r := range results {
		rm, _ := r.(map[string]any)
		if rm["scope"] == "prd-shared" {
			t.Errorf("prd-shared scope should be absent when prd_id not provided")
		}
	}
}

// TS-10: GET recall with story_id — story-shared scope present.
func TestBL385_RecallWithStoryID_IncludesStoryLayer(t *testing.T) {
	s, b := newScopesTestServer(t)
	_, _ = b.Save("proj1", "story learning", "", "story/story-xyz", "", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/memory/scopes/recall?project=proj1&story_id=story-xyz", nil)
	rr := httptest.NewRecorder()
	s.memoryRecall(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	results, _ := resp["results"].([]any)
	found := false
	for _, r := range results {
		rm, _ := r.(map[string]any)
		if rm["scope"] == "story-shared" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected story-shared scope in recall results when story_id provided; got %v", resp)
	}
}
