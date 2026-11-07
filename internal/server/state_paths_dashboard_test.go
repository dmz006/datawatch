package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plantHomeDecoy writes a sentinel file at path under fakeHome and returns
// the written bytes so the caller can assert the file is untouched later.
func plantHomeDecoy(t *testing.T, fakeHome, relPath string) []byte {
	t.Helper()
	decoy := []byte(`{"cards":[{"id":"decoy-card","cs":1,"rs":1,"system":true}]}`)
	if relPath == "channel_routing.json" {
		decoy = []byte(`{"rules":[{"channel_pattern":"decoy-*","peer_name":"decoy-peer"}]}`)
	}
	target := filepath.Join(fakeHome, relPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir decoy dir: %v", err)
	}
	if err := os.WriteFile(target, decoy, 0o600); err != nil {
		t.Fatalf("write decoy: %v", err)
	}
	return decoy
}

// assertDecoyUntouched fails the test if the decoy file under fakeHome is
// missing or has changed from the original bytes.
func assertDecoyUntouched(t *testing.T, fakeHome, relPath string, original []byte) {
	t.Helper()
	after, err := os.ReadFile(filepath.Join(fakeHome, relPath))
	if err != nil {
		t.Fatalf("decoy file missing: %v", err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("decoy changed: got %s, want %s", after, original)
	}
}

// TestStatePathDashboardLayoutUnderDataDir proves the dashboard layout file
// lives under the configured data directory and never under HOME, even when
// a decoy dashboard-layout.json already sits in HOME.
func TestStatePathDashboardLayoutUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	decoy := plantHomeDecoy(t, fakeHome, filepath.Join(".datawatch", "dashboard-layout.json"))

	s := &Server{token: "admin-token", dataDir: dataDir}

	// GET: the layout is served and seeded from the data directory.
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/layout", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.handleDashboardLayout(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "decoy-card") {
		t.Fatalf("GET body contains decoy content: %s", rr.Body.String())
	}

	// PUT: a stored layout must land under the data directory.
	req = httptest.NewRequest(http.MethodPut, "/api/dashboard/layout", strings.NewReader(`{"cards":[]}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	rr = httptest.NewRecorder()
	s.handleDashboardLayout(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	storedPath := filepath.Join(dataDir, "dashboard-layout.json")
	stored, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatalf("stored layout missing under data dir %q: %v", storedPath, err)
	}
	if strings.Contains(string(stored), "decoy-card") {
		t.Fatalf("stored layout under data dir contains decoy content: %s", stored)
	}

	assertDecoyUntouched(t, fakeHome, filepath.Join(".datawatch", "dashboard-layout.json"), decoy)
}

// TestStatePathChannelRoutingUnderDataDir proves GET /api/channel/routing
// reads channel_routing.json from the configured data directory, not from a
// decoy already present in HOME.
func TestStatePathChannelRoutingUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	decoy := plantHomeDecoy(t, fakeHome, filepath.Join(".datawatch", "channel_routing.json"))

	s := &Server{token: "admin-token", dataDir: dataDir}

	req := httptest.NewRequest(http.MethodGet, "/api/channel/routing", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	rr := httptest.NewRecorder()
	s.handleChannelRouting(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "decoy-peer") || strings.Contains(rr.Body.String(), "decoy-*") {
		t.Fatalf("GET body contains decoy content: %s", rr.Body.String())
	}

	assertDecoyUntouched(t, fakeHome, filepath.Join(".datawatch", "channel_routing.json"), decoy)
}
