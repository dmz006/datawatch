// BL93/BL94 — REST handler tests for session reconcile + import.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func newReconcileServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	hub := NewHub()
	srv := NewServer(hub, sm, "h", "", nil, nil, "")
	return srv, dir
}

// helper: write a session.json into <dataDir>/sessions/<fullID>/.
func writeOrphanDir(t *testing.T, dataDir, fullID string) {
	t.Helper()
	dir := filepath.Join(dataDir, "sessions", fullID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{
		ID:       fullID[len(fullID)-4:],
		FullID:   fullID,
		State:    session.StateKilled,
		Hostname: "h",
	}
	data, _ := json.MarshalIndent(sess, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "session.json"), data, 0644)
}

func TestHandleSessionReconcile_DryRun(t *testing.T) {
	srv, dir := newReconcileServer(t)
	writeOrphanDir(t, dir, "h-orph1")

	body := bytes.NewBufferString(`{"auto_import":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/reconcile", body)
	rr := httptest.NewRecorder()
	srv.handleSessionReconcile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got session.ReconcileResult
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Orphaned) != 1 || got.Orphaned[0] != "h-orph1" {
		t.Errorf("orphaned: got %v want [h-orph1]", got.Orphaned)
	}
	if len(got.Imported) != 0 {
		t.Errorf("dry-run imported nonzero: %v", got.Imported)
	}
}

func TestHandleSessionReconcile_AutoImport(t *testing.T) {
	srv, dir := newReconcileServer(t)
	writeOrphanDir(t, dir, "h-orph2")

	body := bytes.NewBufferString(`{"auto_import":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/reconcile", body)
	rr := httptest.NewRecorder()
	srv.handleSessionReconcile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got session.ReconcileResult
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got.Imported) != 1 {
		t.Errorf("imported: got %v want 1", got.Imported)
	}
}

func TestHandleSessionReconcile_RejectsGet(t *testing.T) {
	srv, _ := newReconcileServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/reconcile", nil)
	rr := httptest.NewRecorder()
	srv.handleSessionReconcile(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandleSessionImport_HappyPath(t *testing.T) {
	srv, dir := newReconcileServer(t)
	writeOrphanDir(t, dir, "h-imp1")

	body := bytes.NewBufferString(`{"dir":"h-imp1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/import", body)
	rr := httptest.NewRecorder()
	srv.handleSessionImport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Imported bool             `json:"imported"`
		Session  *session.Session `json:"session"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if !got.Imported {
		t.Error("imported flag should be true on first import")
	}
	if got.Session == nil || got.Session.FullID != "h-imp1" {
		t.Errorf("session: got %+v", got.Session)
	}
}

func TestHandleSessionImport_MissingDir(t *testing.T) {
	srv, _ := newReconcileServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/import",
		bytes.NewBufferString(`{}`))
	rr := httptest.NewRecorder()
	srv.handleSessionImport(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}
