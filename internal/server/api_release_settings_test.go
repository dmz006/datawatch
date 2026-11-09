// REST round-trip for the release settings: push_and_tag, release_path and
// git.release_ci_window_minutes.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func releaseSettingsServer(t *testing.T) *Server {
	t.Helper()
	return &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
}

func releaseSettingsGet(t *testing.T, s *Server) (autonomous, git map[string]any) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d", rr.Code)
	}
	var got struct {
		Autonomous map[string]any `json:"autonomous"`
		Git        map[string]any `json:"git"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.Autonomous, got.Git
}

func releaseSettingsPut(s *Server, body map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	return rr
}

func TestReleaseSettings_PutReadBack(t *testing.T) {
	s := releaseSettingsServer(t)
	auto, git := releaseSettingsGet(t, s)
	if auto["release_path"] != "direct_push" || auto["push_and_tag"] != false {
		t.Fatalf("defaults: %v", auto)
	}
	if _, ok := git["release_ci_window_minutes"]; !ok {
		t.Fatalf("git lacks release_ci_window_minutes: %v", git)
	}

	for _, body := range []map[string]any{
		{"autonomous.push_and_tag": true},
		{"autonomous.release_path": "pull_request"},
		{"git.release_ci_window_minutes": 90},
	} {
		if rr := releaseSettingsPut(s, body); rr.Code != http.StatusOK {
			t.Fatalf("PUT %v: %d %s", body, rr.Code, rr.Body.String())
		}
	}
	auto, git = releaseSettingsGet(t, s)
	if auto["push_and_tag"] != true {
		t.Errorf("push_and_tag: %v", auto["push_and_tag"])
	}
	if auto["release_path"] != "pull_request" {
		t.Errorf("release_path: %v", auto["release_path"])
	}
	if git["release_ci_window_minutes"] != float64(90) {
		t.Errorf("release_ci_window_minutes: %v", git["release_ci_window_minutes"])
	}
}

func TestReleaseSettings_InvalidRefused(t *testing.T) {
	s := releaseSettingsServer(t)
	rr := releaseSettingsPut(s, map[string]any{"autonomous.release_path": "force_push"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code %d", rr.Code)
	}
	want := config.ValidateReleasePath("force_push").Error()
	if !strings.Contains(rr.Body.String(), want) {
		t.Fatalf("body %q lacks %q", rr.Body.String(), want)
	}
	if auto, _ := releaseSettingsGet(t, s); auto["release_path"] != "direct_push" {
		t.Fatalf("refused PUT changed value: %v", auto["release_path"])
	}
	if rr := releaseSettingsPut(s, map[string]any{"git.release_ci_window_minutes": 0}); rr.Code != http.StatusBadRequest {
		t.Fatalf("zero window code %d", rr.Code)
	}
}
