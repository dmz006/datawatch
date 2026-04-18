package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

// profileTestServer is a minimal *Server wired with real profile stores
// backed by a tmp dir. Mirrors the pattern in health_test.go.
func profileTestServer(t *testing.T) (*Server, *profile.ProjectStore, *profile.ClusterStore) {
	t.Helper()
	dir := t.TempDir()
	ps, err := profile.NewProjectStore(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}
	cs, err := profile.NewClusterStore(filepath.Join(dir, "clusters.json"))
	if err != nil {
		t.Fatalf("NewClusterStore: %v", err)
	}
	s := &Server{hostname: "testhost"}
	s.SetProjectStore(ps)
	s.SetClusterStore(cs)
	return s, ps, cs
}

func jsonBody(t *testing.T, v interface{}) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf
}

// ── Project handlers ────────────────────────────────────────────────────

func TestProjectProfiles_CreateListDelete(t *testing.T) {
	s, _, _ := profileTestServer(t)

	// List empty
	rr := httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodGet, "/api/profiles/projects", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list empty status=%d", rr.Code)
	}

	// Create
	body := jsonBody(t, &profile.ProjectProfile{
		Name: "foo",
		Git:  profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory: profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	rr = httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodPost, "/api/profiles/projects", body))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}

	// List has one
	rr = httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodGet, "/api/profiles/projects", nil))
	var list struct {
		Profiles []profile.ProjectProfile `json:"profiles"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Profiles) != 1 || list.Profiles[0].Name != "foo" {
		t.Errorf("after create got %v", list.Profiles)
	}

	// Get single
	rr = httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodGet, "/api/profiles/projects/foo", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Delete
	rr = httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodDelete, "/api/profiles/projects/foo", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", rr.Code)
	}

	// 404 on second delete
	rr = httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodDelete, "/api/profiles/projects/foo", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("second delete status=%d, want 404", rr.Code)
	}
}

func TestProjectProfiles_CreateDuplicateReturns409(t *testing.T) {
	s, ps, _ := profileTestServer(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "existing",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})

	body := jsonBody(t, &profile.ProjectProfile{
		Name:      "existing",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	rr := httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodPost, "/api/profiles/projects", body))
	if rr.Code != http.StatusConflict {
		t.Errorf("duplicate create status=%d, want 409", rr.Code)
	}
}

func TestProjectProfiles_InvalidProfileReturns400(t *testing.T) {
	s, _, _ := profileTestServer(t)
	// Invalid: missing agent
	body := jsonBody(t, &profile.ProjectProfile{
		Name: "bad",
		Git:  profile.GitSpec{URL: "https://github.com/x/y"},
	})
	rr := httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodPost, "/api/profiles/projects", body))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid-create status=%d, want 400", rr.Code)
	}
}

func TestProjectProfiles_Update_ForcesNameFromURL(t *testing.T) {
	// Guard against body.Name != URL.Name silently updating the wrong profile.
	s, ps, _ := profileTestServer(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "real",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})

	body := jsonBody(t, &profile.ProjectProfile{
		Name:        "hacker",       // attempt to rename via body
		Description: "via update",
		Git:         profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair:   profile.ImagePair{Agent: "agent-claude"},
		Memory:      profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	rr := httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodPut, "/api/profiles/projects/real", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rr.Code, rr.Body.String())
	}

	// "hacker" must NOT have been created
	if _, err := ps.Get("hacker"); err == nil {
		t.Errorf("body.Name overrode URL name — security issue")
	}
	// "real" got the new description
	updated, err := ps.Get("real")
	if err != nil {
		t.Fatalf("get real: %v", err)
	}
	if updated.Description != "via update" {
		t.Errorf("description not updated: %q", updated.Description)
	}
}

func TestProjectProfiles_Smoke(t *testing.T) {
	s, ps, _ := profileTestServer(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "smokable",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude", Sidecar: "lang-go"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})

	rr := httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodPost, "/api/profiles/projects/smokable/smoke", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("smoke pass status=%d, body=%s", rr.Code, rr.Body.String())
	}
	var result profile.SmokeResult
	_ = json.NewDecoder(rr.Body).Decode(&result)
	if !result.Passed() {
		t.Errorf("smoke should pass, errors=%v", result.Errors)
	}

	// Smoke on unknown → 404
	rr = httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodPost, "/api/profiles/projects/unknown/smoke", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("smoke unknown status=%d, want 404", rr.Code)
	}
}

// ── Cluster handlers (shape mirrors Project) ───────────────────────────

func TestClusterProfiles_Roundtrip(t *testing.T) {
	s, _, _ := profileTestServer(t)

	body := jsonBody(t, &profile.ClusterProfile{
		Name:    "test-k8s",
		Kind:    profile.ClusterK8s,
		Context: "testing",
	})
	rr := httptest.NewRecorder()
	s.handleClusterProfiles(rr, httptest.NewRequest(http.MethodPost, "/api/profiles/clusters", body))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleClusterProfiles(rr, httptest.NewRequest(http.MethodGet, "/api/profiles/clusters/test-k8s", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d", rr.Code)
	}

	rr = httptest.NewRecorder()
	s.handleClusterProfiles(rr, httptest.NewRequest(http.MethodPost, "/api/profiles/clusters/test-k8s/smoke", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("smoke status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleClusterProfiles(rr, httptest.NewRequest(http.MethodDelete, "/api/profiles/clusters/test-k8s", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", rr.Code)
	}
}

// ── Store-not-wired → 503 ──────────────────────────────────────────────

func TestProjectProfiles_NoStore_Returns503(t *testing.T) {
	s := &Server{hostname: "h"} // deliberately no stores wired
	rr := httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodGet, "/api/profiles/projects", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("no-store status=%d, want 503", rr.Code)
	}
}

// ── Bad subpath → 404 ──────────────────────────────────────────────────

func TestProjectProfiles_UnknownSubpath_Returns404(t *testing.T) {
	s, ps, _ := profileTestServer(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	rr := httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodGet, "/api/profiles/projects/p/bogus", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown subpath status=%d, want 404", rr.Code)
	}
}

// ── JSON body errors surface as 400, not 500 ───────────────────────────

func TestProjectProfiles_MalformedJSON_Returns400(t *testing.T) {
	s, _, _ := profileTestServer(t)
	rr := httptest.NewRecorder()
	s.handleProjectProfiles(rr, httptest.NewRequest(http.MethodPost, "/api/profiles/projects",
		strings.NewReader("{not json")))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("malformed json status=%d, want 400", rr.Code)
	}
}
