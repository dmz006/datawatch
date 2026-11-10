package router

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestCommReleaseModeConfigureTagsCommSurface checks that a release approval
// mode set from chat is sent to PUT /api/config tagged surface=comm, with the
// key and the trimmed value, so the daemon records it as a comm change.
func TestCommReleaseModeConfigureTagsCommSurface(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	_, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port: %v", err)
	}

	r := &Router{webPort: port}
	key := "autonomous.merge_pipeline.projects.demo.release_approval.major"
	if err := r.configureMergePipelineReleaseMode(key, "  ask "); err != nil {
		t.Fatalf("configureMergePipelineReleaseMode: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/config" {
		t.Errorf("request = %s %s, want PUT /api/config", gotMethod, gotPath)
	}
	if gotQuery != "surface=comm" {
		t.Errorf("query = %q, want surface=comm", gotQuery)
	}
	if gotBody[key] != "ask" {
		t.Errorf("body[%s] = %v, want the trimmed value ask", key, gotBody[key])
	}
}
