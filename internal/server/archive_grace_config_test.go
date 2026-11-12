package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// archiveGraceGet reads autonomous.archive_grace_minutes from GET /api/config.
func archiveGraceGet(t *testing.T, s *Server) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(getAutonomousConfigMap(t, s)["archive_grace_minutes"], &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestArchiveGraceConfigRoundTrip(t *testing.T) {
	const key = "autonomous.archive_grace_minutes"
	s, _ := newLoopServer(t)
	if g := archiveGraceGet(t, s); g != float64(60) {
		t.Fatalf("GET default = %v, want 60", g)
	}
	for _, v := range []int{0, 90, 15} {
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{key: v})
		if g := archiveGraceGet(t, s); g != float64(v) {
			t.Errorf("PUT %d, GET %v", v, g)
		}
	}
}

func TestArchiveGraceConfigRefusesBadValues(t *testing.T) {
	const key = "autonomous.archive_grace_minutes"
	for _, bad := range []any{-1, 1.5, "abc"} {
		s, _ := newLoopServer(t)
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{key: 45})
		b, _ := json.Marshal(map[string]any{key: bad})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%v: want 400, got %d", bad, rr.Code)
			continue
		}
		if !strings.Contains(rr.Body.String(), "archive_grace_minutes") {
			t.Errorf("%v: error does not name the key: %q", bad, rr.Body.String())
		}
		if g := archiveGraceGet(t, s); g != float64(45) {
			t.Errorf("%v: refused PUT changed the value to %v", bad, g)
		}
	}
}
