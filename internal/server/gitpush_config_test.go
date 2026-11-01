// git.push_on_complete / git.push_remote REST coverage: PUT a value, GET it
// back, and confirm invalid policies never clobber the stored value.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func gitPushServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		cfg:     &config.Config{},
		cfgPath: t.TempDir() + "/config.yaml",
	}
}

func gitPushPut(t *testing.T, s *Server, patch map[string]any) int {
	t.Helper()
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, req)
	return rr.Code
}

func gitPushGet(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d %s", rr.Code, rr.Body.String())
	}
	var v map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode GET /api/config: %v", err)
	}
	git, _ := v["git"].(map[string]any)
	if git == nil {
		t.Fatalf("GET /api/config response has no git section: %v", v)
	}
	return git
}

func TestGitPushOnCompletePutGetRoundTrip(t *testing.T) {
	s := gitPushServer(t)
	if code := gitPushPut(t, s, map[string]any{"git.push_on_complete": "default_branch"}); code != http.StatusOK {
		t.Fatalf("PUT git.push_on_complete: HTTP %d", code)
	}
	if got := gitPushGet(t, s)["push_on_complete"]; got != "default_branch" {
		t.Errorf("after PUT default_branch, GET push_on_complete = %v, want default_branch", got)
	}
	if got := gitPushGet(t, s)["push_remote"]; got != "origin" {
		t.Errorf("GET push_remote = %v, want origin", got)
	}
}

func TestGitPushOnCompleteInvalidIgnored(t *testing.T) {
	s := gitPushServer(t)
	if code := gitPushPut(t, s, map[string]any{"git.push_on_complete": "never"}); code != http.StatusOK {
		t.Fatalf("PUT never: HTTP %d", code)
	}
	// An invalid policy must be ignored without changing the stored value.
	// (Case-insensitive + trimmed: the valid values themselves still apply.)
	if code := gitPushPut(t, s, map[string]any{"git.push_on_complete": "sometimes"}); code != http.StatusOK {
		// Whether the daemon reports or ignores it, the stored value below
		// is what matters.
		t.Logf("invalid PUT returned HTTP %d", code)
	}
	if got := gitPushGet(t, s)["push_on_complete"]; got != "never" {
		t.Errorf("invalid value clobbered stored value: push_on_complete = %v, want never", got)
	}
	if code := gitPushPut(t, s, map[string]any{"git.push_on_complete": "  DEFAULT_BRANCH "}); code != http.StatusOK {
		t.Fatalf("PUT trimmed/upper: HTTP %d", code)
	}
	if got := gitPushGet(t, s)["push_on_complete"]; got != "default_branch" {
		t.Errorf("trimmed/upper value should apply: push_on_complete = %v, want default_branch", got)
	}
}

func TestGitPushRemoteRoundTrip(t *testing.T) {
	s := gitPushServer(t)
	if code := gitPushPut(t, s, map[string]any{"git.push_remote": "upstream"}); code != http.StatusOK {
		t.Fatalf("PUT git.push_remote: HTTP %d", code)
	}
	if got := gitPushGet(t, s)["push_remote"]; got != "upstream" {
		t.Errorf("after PUT upstream, GET push_remote = %v, want upstream", got)
	}
	// Clearing the remote resets the effective default to origin.
	if code := gitPushPut(t, s, map[string]any{"git.push_remote": ""}); code != http.StatusOK {
		t.Fatalf("PUT git.push_remote clear: HTTP %d", code)
	}
	if got := gitPushGet(t, s)["push_remote"]; got != "origin" {
		t.Errorf("after clear, GET push_remote = %v, want origin", got)
	}
}
