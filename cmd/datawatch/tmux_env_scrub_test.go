package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// TestTmuxEnvScrub_SpawnSites confirms scrub_tmux_env is requested for pi,
// pi-prompt and claude-code launches from the executor, verifier and
// guardrail spawn sites.
func TestTmuxEnvScrub_SpawnSites(t *testing.T) {
	backends := []string{"pi", "pi-prompt", "claude-code"}

	for _, backend := range backends {
		t.Run("executor/"+backend, func(t *testing.T) {
			body := buildAutonomousSessionBody(autonomouspkg.SpawnRequest{Title: "t", Backend: backend}, "spec", "")
			if body["scrub_tmux_env"] != true {
				t.Fatalf("scrub_tmux_env = %v, want true", body["scrub_tmux_env"])
			}
		})
	}

	// Verifier and guardrail sessions are spawned through the one-shot
	// judgment helper; capture the start request it posts.
	for _, site := range []string{"verifier", "guardrail"} {
		for _, backend := range backends {
			t.Run(site+"/"+backend, func(t *testing.T) {
				var got map[string]any
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(b, &got)
					http.Error(w, "stop", http.StatusInternalServerError)
				}))
				defer srv.Close()
				old := serverURL
				serverURL = srv.URL
				defer func() { serverURL = old }()

				cfg := &config.Config{DataDir: t.TempDir()}
				name := site + "-scrub-test"
				_, _, err := runOneShotJSONSessionWithReader(cfg, "prd1", t.TempDir(), backend, "m", "",
					name, "task", t.TempDir()+"/out.json", "out.json", 5*time.Second,
					func(string, string, string, string) (string, bool) { return "", false })
				if err == nil || !strings.Contains(err.Error(), "stop") {
					t.Fatalf("expected the stub server error, got %v", err)
				}
				if got["scrub_tmux_env"] != true || got["backend"] != backend {
					t.Fatalf("start body = %v", got)
				}
			})
		}
	}
}
