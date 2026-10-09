// GH#201 Phase 5 — create-alert REST + MCP surface.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
)

func gh201Phase5Server(t *testing.T) *Server {
	t.Helper()
	s := bl90Server(t)
	store, err := alerts.NewStore(t.TempDir() + "/alerts.json")
	if err != nil {
		t.Fatal(err)
	}
	s.alertStore = store
	return s
}

func TestGH201Phase5_CreateAlert_DefaultsToInfoLevel(t *testing.T) {
	s := gh201Phase5Server(t)
	body, _ := json.Marshal(map[string]any{"title": "disk space low", "body": "/data is 95% full"})
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleAlertCreate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var got alerts.Alert
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Level != alerts.LevelInfo {
		t.Errorf("level = %q, want info (default)", got.Level)
	}
	if got.Source != "system" {
		t.Errorf("source = %q, want system", got.Source)
	}
	if got.Title != "disk space low" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestGH201Phase5_CreateAlert_ExplicitLevel(t *testing.T) {
	s := gh201Phase5Server(t)
	body, _ := json.Marshal(map[string]any{"level": "error", "title": "disk full"})
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleAlertCreate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var got alerts.Alert
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Level != alerts.LevelError {
		t.Errorf("level = %q, want error", got.Level)
	}
}

func TestGH201Phase5_CreateAlert_RejectsUnknownLevel(t *testing.T) {
	s := gh201Phase5Server(t)
	body, _ := json.Marshal(map[string]any{"level": "critical", "title": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleAlertCreate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown level, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGH201Phase5_CreateAlert_RequiresTitle(t *testing.T) {
	s := gh201Phase5Server(t)
	body, _ := json.Marshal(map[string]any{"body": "no title here"})
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleAlertCreate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when title is missing, got %d", w.Code)
	}
}

func TestGH201Phase5_CreateAlert_RequiresPostMethod(t *testing.T) {
	s := gh201Phase5Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/alerts/create", nil)
	w := httptest.NewRecorder()
	s.handleAlertCreate(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d", w.Code)
	}
}

func TestGH201Phase5_CreateAlert_Audited(t *testing.T) {
	s := gh201Phase5Server(t)
	al, err := audit.NewAt(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = al.Close() }()
	s.auditLog = al

	body, _ := json.Marshal(map[string]any{"title": "test alert"})
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleAlertCreate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0].Action != "create" || entries[0].Details["resource_type"] != "alert" {
		t.Errorf("entry = %+v, want create/alert", entries[0])
	}
}

// TestGH201Phase5_CreateAlert_CapabilityEnforced confirms the new
// CapAlertsWrite capability is actually checked, and that it was
// deliberately NOT granted to the read-only built-in group (unlike
// CapAlertsRead/List) — an external monitor needs it granted
// explicitly, the same least-privilege default every other new
// write-class capability in this codebase uses.
func TestGH201Phase5_CreateAlert_CapabilityEnforced(t *testing.T) {
	s, _, _ := fedCapFixture(t)
	s.alertStore, _ = alerts.NewStore(t.TempDir() + "/alerts.json")
	body, _ := json.Marshal(map[string]any{"title": "x"})

	capTest(t, s,
		http.MethodPost, "/api/alerts/create", body,
		s.handleAlertCreate,
		http.StatusCreated,    // admin: unrestricted
		http.StatusCreated,    // full-control: has every capability including the new one
		http.StatusForbidden,  // read-only: CapAlertsRead/List only, never granted alerts:write
	)
}
