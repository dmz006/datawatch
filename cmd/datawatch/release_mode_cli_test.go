package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestConfigSetReleaseModeTagsCLISurface checks that `datawatch config set` of a
// release approval mode sends PUT /api/config tagged surface=cli, so the daemon
// records the change with the cli surface and a decision record.
func TestConfigSetReleaseModeTagsCLISurface(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })

	key := "autonomous.merge_pipeline.release_approval.minor"
	if err := runConfigSet(&config.Config{}, key, "manual"); err != nil {
		t.Fatalf("runConfigSet: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/config" {
		t.Errorf("request = %s %s, want PUT /api/config", gotMethod, gotPath)
	}
	if gotQuery != "surface=cli" {
		t.Errorf("query = %q, want surface=cli", gotQuery)
	}
	if gotBody[key] != "manual" {
		t.Errorf("body[%s] = %v, want manual", key, gotBody[key])
	}
}
