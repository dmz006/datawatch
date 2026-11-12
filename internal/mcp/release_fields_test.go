package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestReleaseFieldsMCP_SetReleaseSizeProxies(t *testing.T) {
	var path, method string
	var got map[string]string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"id":"p1","release_size":"minor"}`))
	})
	res, err := s.handleAutonomousPRDSetReleaseSize(context.Background(), callReq(map[string]any{"id": "p1", "release_size": "minor"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/prds/p1/set_release_size" || got["release_size"] != "minor" {
		t.Fatalf("proxied %s %s body=%v", method, path, got)
	}

	// An empty size is forwarded so the daemon clears the setting.
	if _, err := s.handleAutonomousPRDSetReleaseSize(context.Background(), callReq(map[string]any{"id": "p1", "release_size": ""})); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["release_size"]; !ok || v != "" {
		t.Fatalf("empty release_size must be sent to clear: %v", got)
	}
}

func TestReleaseFieldsMCP_RefusalIsAToolError(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown release size \"huge\"", http.StatusBadRequest)
	})
	res, err := s.handleAutonomousPRDSetReleaseSize(context.Background(), callReq(map[string]any{"id": "p1", "release_size": "huge"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("a 400 from the daemon must be a tool error: err=%v res=%+v", err, res)
	}
}

func TestReleaseFieldsMCP_SetBreakingChangeProxies(t *testing.T) {
	var path string
	var got map[string]any
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		got = nil
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"id":"p1","breaking_change":true}`))
	})
	res, err := s.handleAutonomousPRDSetBreakingChange(context.Background(), callReq(map[string]any{"id": "p1", "breaking_change": true}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if path != "/api/autonomous/prds/p1/set_breaking_change" || got["breaking_change"] != true {
		t.Fatalf("proxied %s body=%v", path, got)
	}
}

func TestReleaseFieldsMCP_BreakingChangeMustBeBoolean(t *testing.T) {
	called := false
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	})
	for _, args := range []map[string]any{
		{"id": "p1"},
		{"id": "p1", "breaking_change": "yes"},
	} {
		res, err := s.handleAutonomousPRDSetBreakingChange(context.Background(), callReq(args))
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("args %v must be a tool error: err=%v res=%+v", args, err, res)
		}
	}
	if called {
		t.Fatal("an invalid breaking_change must not reach the daemon")
	}
}

func TestReleaseFieldsMCP_ToolShapeAndCaps(t *testing.T) {
	s := &Server{}
	size := s.toolAutonomousPRDSetReleaseSize()
	if size.Name != "autonomous_prd_set_release_size" {
		t.Fatalf("name = %q", size.Name)
	}
	if req := strings.Join(size.InputSchema.Required, ","); !strings.Contains(req, "release_size") || !strings.Contains(req, "id") {
		t.Fatalf("id and release_size must be required, got %q", req)
	}
	brk := s.toolAutonomousPRDSetBreakingChange()
	if brk.Name != "autonomous_prd_set_breaking_change" {
		t.Fatalf("name = %q", brk.Name)
	}
	if req := strings.Join(brk.InputSchema.Required, ","); !strings.Contains(req, "breaking_change") {
		t.Fatalf("breaking_change must be required, got %q", req)
	}
	for _, name := range []string{"autonomous_prd_set_release_size", "autonomous_prd_set_breaking_change"} {
		if _, ok := federation.RequiredCapForMCPTool(name); !ok {
			t.Errorf("%s has no federation capability", name)
		}
	}
}
