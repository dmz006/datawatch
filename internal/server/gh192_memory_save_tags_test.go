// GH#192 D78a — POST /api/memory/save with an optional "tags" field.
// Uses the fakeMemAPI + newTestServer helpers already defined in
// health_test.go.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleMemorySave_WithTags(t *testing.T) {
	mem := &fakeMemAPI{rememberID: 42}
	s := newTestServer(t, mem, nil)

	body, _ := json.Marshal(map[string]string{"content": "a memory", "tags": "work,bug"})
	req := httptest.NewRequest(http.MethodPost, "/api/memory/save", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleMemorySave(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] != float64(42) {
		t.Errorf("id = %v, want 42", resp["id"])
	}
	if resp["tags_applied"] != true {
		t.Errorf("tags_applied = %v, want true", resp["tags_applied"])
	}
}

func TestHandleMemorySave_NoTags_SkipsSetTagsEntirely(t *testing.T) {
	mem := &fakeMemAPI{rememberID: 1, tagsErr: true} // would fail if ever called
	s := newTestServer(t, mem, nil)

	body, _ := json.Marshal(map[string]string{"content": "a memory"})
	req := httptest.NewRequest(http.MethodPost, "/api/memory/save", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleMemorySave(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
}

// The whole point of making this best-effort (same as pinning): an
// unsupported-backend SetTags error must not fail the save -- the
// memory itself was already created successfully by Remember.
func TestHandleMemorySave_TagsErrorDoesNotFailTheSave(t *testing.T) {
	mem := &fakeMemAPI{rememberID: 7, tagsErr: true}
	s := newTestServer(t, mem, nil)

	body, _ := json.Marshal(map[string]string{"content": "a memory", "tags": "urgent"})
	req := httptest.NewRequest(http.MethodPost, "/api/memory/save", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleMemorySave(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when the backend doesn't support tags (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] != float64(7) {
		t.Errorf("id = %v, want 7 -- the memory must still be reported as saved", resp["id"])
	}
	if resp["tags_applied"] != false {
		t.Errorf("tags_applied = %v, want false -- caller should be able to tell tags weren't applied", resp["tags_applied"])
	}
}

func TestHandleMemorySave_MissingContent(t *testing.T) {
	mem := &fakeMemAPI{}
	s := newTestServer(t, mem, nil)

	body, _ := json.Marshal(map[string]string{"tags": "work"})
	req := httptest.NewRequest(http.MethodPost, "/api/memory/save", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleMemorySave(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
