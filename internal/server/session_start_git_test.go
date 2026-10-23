// handleStartSession must forward the request's `git` field into
// session.StartOptions.Git. The manager applies opts.Git by setting
// sess.Git (manager.Start), so asserting the started session's Git flag
// verifies the exact StartOptions value seen by the manager.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

func TestHandleStartSession_GitFlagPassthrough(t *testing.T) {
	for _, tc := range []struct {
		name    string
		git     bool
		omit    bool
		wantGit bool
	}{
		{name: "explicit true", git: true, wantGit: true},
		{name: "explicit false", git: false, omit: false, wantGit: false},
		{name: "omitted", omit: true, wantGit: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newLLMServer(t)

			payload := map[string]any{"task": "hello", "project_dir": t.TempDir()}
			if !tc.omit {
				payload["git"] = tc.git
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
			w := httptest.NewRecorder()
			srv.handleStartSession(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("handleStartSession: status %d body: %s", w.Code, w.Body.String())
			}
			cleanupStartedSession(t, srv, w)

			var resp struct {
				FullID string `json:"full_id"`
				ID     string `json:"id"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			sessID := resp.FullID
			if sessID == "" {
				sessID = resp.ID
			}
			if sessID == "" {
				t.Fatal("response had no session id")
			}

			var started *session.Session
			for _, s := range srv.manager.ListSessions() {
				if s.FullID == sessID || s.ID == sessID {
					started = s
					break
				}
			}
			if started == nil {
				t.Fatalf("started session %q not found in manager", sessID)
			}
			// The manager sets sess.Git only when StartOptions.Git is true,
			// so this reflects the opts value seen by the manager.
			if started.Git != tc.wantGit {
				t.Fatalf("StartOptions.Git as applied to session = %v, want %v", started.Git, tc.wantGit)
			}
		})
	}
}

// TestHandleStartSession_GitDefaultConfigFallback verifies that an omitted
// `git` field falls back to the session.git_default config value, while an
// explicit request value (true or false) always wins.
func TestHandleStartSession_GitDefaultConfigFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cfgDefault bool
		payloadGit any // omitempty when nil
		wantGit    bool
	}{
		{name: "omitted with git_default true", cfgDefault: true, payloadGit: nil, wantGit: true},
		{name: "omitted with git_default false", cfgDefault: false, payloadGit: nil, wantGit: false},
		{name: "explicit true beats git_default false", cfgDefault: false, payloadGit: true, wantGit: true},
		{name: "explicit false beats git_default true", cfgDefault: true, payloadGit: false, wantGit: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newLLMServer(t)
			srv.cfg = &config.Config{Session: config.SessionConfig{GitDefault: tc.cfgDefault}}

			payload := map[string]any{"task": "hello", "project_dir": t.TempDir()}
			if tc.payloadGit != nil {
				payload["git"] = tc.payloadGit
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(body))
			w := httptest.NewRecorder()
			srv.handleStartSession(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("handleStartSession: status %d body: %s", w.Code, w.Body.String())
			}
			cleanupStartedSession(t, srv, w)

			var resp struct {
				FullID string `json:"full_id"`
				ID     string `json:"id"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &resp)
			sessID := resp.FullID
			if sessID == "" {
				sessID = resp.ID
			}
			if sessID == "" {
				t.Fatal("response had no session id")
			}

			var started *session.Session
			for _, s := range srv.manager.ListSessions() {
				if s.FullID == sessID || s.ID == sessID {
					started = s
					break
				}
			}
			if started == nil {
				t.Fatalf("started session %q not found in manager", sessID)
			}
			if started.Git != tc.wantGit {
				t.Fatalf("session.Git = %v, want %v (cfg git_default=%v, payload git=%v)",
					started.Git, tc.wantGit, tc.cfgDefault, tc.payloadGit)
			}
		})
	}
}
