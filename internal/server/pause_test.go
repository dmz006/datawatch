package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/pause"
)

func pauseServer(t *testing.T) (*Server, *pause.Store, *audit.Log) {
	t.Helper()
	ps, err := pause.Open(filepath.Join(t.TempDir(), "pause.json"), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s := &Server{cfg: &config.Config{Projects: map[string]config.ProjectConfigEntry{"web": {Dir: "/work/web"}}}, auditLog: al}
	s.SetPause(ps)
	return s, ps, al
}

func pauseDo(s *Server, method, path, body string, sessionCaps []string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if sessionCaps != nil {
		req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, sessionCaps))
	}
	rr := httptest.NewRecorder()
	switch {
	case strings.HasSuffix(path, "/resume"):
		s.handlePauseResume(rr, req)
	default:
		s.handlePause(rr, req)
	}
	return rr
}

func pauseSnap(t *testing.T, rr *httptest.ResponseRecorder) pauseResponse {
	t.Helper()
	var out pauseResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return out
}

func TestPauseREST_NotWiredIs503(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	if rr := pauseDo(s, http.MethodGet, "/api/pause", "", nil); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestPauseREST_PauseThenStatusThenResume(t *testing.T) {
	s, ps, _ := pauseServer(t)
	if rr := pauseDo(s, http.MethodGet, "/api/pause", "", nil); rr.Code != 200 || pauseSnap(t, rr).Paused {
		t.Fatalf("fresh status = %d %s", rr.Code, rr.Body.String())
	}
	rr := pauseDo(s, http.MethodPost, "/api/pause", `{"scope":"global","reason":"release freeze","actor":"cli"}`, nil)
	if rr.Code != 200 {
		t.Fatalf("pause = %d %s", rr.Code, rr.Body.String())
	}
	got := pauseSnap(t, rr)
	if !got.Paused || got.Global == nil || got.Global.Reason != "release freeze" || got.Global.By != "cli" {
		t.Fatalf("pause response = %+v", got)
	}
	if _, blocked := ps.Blocked("/anything"); !blocked {
		t.Fatal("the store must now block")
	}
	rr = pauseDo(s, http.MethodPost, "/api/pause/resume", `{"scope":"global","actor":"web","note":"all clear"}`, nil)
	if rr.Code != 200 || pauseSnap(t, rr).Paused {
		t.Fatalf("resume = %d %s", rr.Code, rr.Body.String())
	}
}

func TestPauseREST_ReasonAndScopeValidated(t *testing.T) {
	s, _, _ := pauseServer(t)
	for _, body := range []string{`{"scope":"global"}`, `{"scope":"project","reason":"x"}`, `{"scope":"planet","reason":"x"}`, `not json`} {
		if rr := pauseDo(s, http.MethodPost, "/api/pause", body, nil); rr.Code != http.StatusBadRequest {
			t.Errorf("body %s => %d, want 400", body, rr.Code)
		}
	}
}

func TestPauseREST_ProjectNameResolvesToDirectory(t *testing.T) {
	s, ps, _ := pauseServer(t)
	rr := pauseDo(s, http.MethodPost, "/api/pause", `{"scope":"project","project":"web","reason":"review"}`, nil)
	if rr.Code != 200 {
		t.Fatalf("pause = %d %s", rr.Code, rr.Body.String())
	}
	if _, blocked := ps.Blocked("/work/web"); !blocked {
		t.Fatal("a registered project name must pause its directory")
	}
	if _, blocked := ps.Blocked("/work/api"); blocked {
		t.Fatal("another project must stay free")
	}
	// A raw directory is accepted too.
	if rr := pauseDo(s, http.MethodPost, "/api/pause", `{"scope":"project","project":"/work/api","reason":"x"}`, nil); rr.Code != 200 {
		t.Fatalf("raw dir pause = %d %s", rr.Code, rr.Body.String())
	}
}

func TestPauseREST_SessionCallerCannotResume(t *testing.T) {
	s, ps, al := pauseServer(t)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	// Even a session token that was granted pause:write is refused: the daemon
	// refuses the resume itself, no model has to cooperate.
	rr := pauseDo(s, http.MethodPost, "/api/pause/resume", `{"scope":"global","actor":"operator"}`, []string{"pause:write", "pause:read"})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("session resume = %d %s, want 403", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "only the operator can resume") {
		t.Fatalf("refusal must give a clear reason: %s", rr.Body.String())
	}
	if _, blocked := ps.Blocked("/x"); !blocked {
		t.Fatal("the pause must still hold")
	}
	rr = pauseDo(s, http.MethodPost, "/api/pause/resume", `{"all":true}`, []string{"pause:write"})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("session resume-all = %d", rr.Code)
	}
	entries, _ := al.Read(audit.QueryFilter{Action: "pause_resume_refused"})
	if len(entries) == 0 {
		t.Fatal("a refused resume must be audited")
	}
	if !strings.HasPrefix(entries[0].Actor, "session") {
		t.Fatalf("refused actor = %q, want a session actor", entries[0].Actor)
	}
}

func TestPauseREST_SessionWithoutCapCannotEvenPause(t *testing.T) {
	s, ps, _ := pauseServer(t)
	rr := pauseDo(s, http.MethodPost, "/api/pause", `{"scope":"global","reason":"x"}`, []string{"sessions:list"})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("pause without pause:write = %d", rr.Code)
	}
	if _, blocked := ps.Blocked("/x"); blocked {
		t.Fatal("nothing may be paused")
	}
}

func TestPauseREST_BodyActorCannotImpersonateAWorker(t *testing.T) {
	s, ps, _ := pauseServer(t)
	_, _ = ps.Pause(pause.ScopeGlobal, "", "freeze", "operator")
	rr := pauseDo(s, http.MethodPost, "/api/pause/resume", `{"scope":"global","actor":"doer"}`, nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("a worker actor on the operator token = %d, want 403", rr.Code)
	}
}

func TestPauseREST_ResumeAllClearsEverything(t *testing.T) {
	s, ps, _ := pauseServer(t)
	_, _ = ps.Pause(pause.ScopeProject, "/a", "x", "operator")
	_, _ = ps.Pause(pause.ScopeGlobal, "", "y", "operator")
	rr := pauseDo(s, http.MethodPost, "/api/pause/resume", `{"all":true,"actor":"cli"}`, nil)
	if rr.Code != 200 {
		t.Fatalf("resume all = %d %s", rr.Code, rr.Body.String())
	}
	if got := pauseSnap(t, rr); got.Paused || got.Cleared != 2 {
		t.Fatalf("resume-all response = %+v", got)
	}
}

func TestPauseREST_AuditHasWhoWhyWhen(t *testing.T) {
	s, _, al := pauseServer(t)
	pauseDo(s, http.MethodPost, "/api/pause", `{"scope":"global","reason":"freeze","actor":"cli"}`, nil)
	pauseDo(s, http.MethodPost, "/api/pause/resume", `{"scope":"global","actor":"web","note":"ok"}`, nil)
	pe, _ := al.Read(audit.QueryFilter{Action: "pause_set"})
	re, _ := al.Read(audit.QueryFilter{Action: "pause_resumed"})
	if len(pe) != 1 || pe[0].Actor != "cli" || pe[0].Details["reason"] != "freeze" || pe[0].Timestamp.IsZero() {
		t.Fatalf("pause audit = %+v", pe)
	}
	if len(re) != 1 || re[0].Actor != "web" || re[0].Details["note"] != "ok" {
		t.Fatalf("resume audit = %+v", re)
	}
}

// fakePeer records the pause calls it receives and answers its own state.
type fakePeer struct {
	mu    sync.Mutex
	calls []string
	state pauseResponse
}

func (f *fakePeer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		body := new(strings.Builder)
		buf := make([]byte, 512)
		for {
			n, err := r.Body.Read(buf)
			body.Write(buf[:n])
			if err != nil {
				break
			}
		}
		f.calls = append(f.calls, r.Method+" "+r.URL.Path+" "+body.String())
		if r.Header.Get("Authorization") != "Bearer peer-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(f.state)
	})
}

func TestPauseREST_PauseIsSentToPeersAndEachReportsItsOwnState(t *testing.T) {
	s, _, _ := pauseServer(t)
	fp := &fakePeer{state: pauseResponse{Paused: true, Global: &pause.Entry{Scope: pause.ScopeGlobal, Reason: "freeze", By: "peer:main"}}}
	ts := httptest.NewServer(fp.handler())
	defer ts.Close()
	s.cfg.Servers = []config.RemoteServerConfig{{Name: "laptop", URL: ts.URL, Token: "peer-token", Enabled: true}}

	rr := pauseDo(s, http.MethodPost, "/api/pause", `{"scope":"global","reason":"freeze","all_peers":true}`, nil)
	if rr.Code != 200 {
		t.Fatalf("pause = %d %s", rr.Code, rr.Body.String())
	}
	got := pauseSnap(t, rr)
	pr, ok := got.Peers["laptop"]
	if !ok || !pr.OK || pr.State == nil || !pr.State.Paused {
		t.Fatalf("peer result = %+v", got.Peers)
	}
	fp.mu.Lock()
	if len(fp.calls) != 1 || !strings.HasPrefix(fp.calls[0], "POST /api/pause ") || !strings.Contains(fp.calls[0], `"reason":"freeze"`) {
		fp.mu.Unlock()
		t.Fatalf("peer calls = %v", fp.calls)
	}
	fp.mu.Unlock()

	// A status read with peers=1 shows each peer's own state.
	rr = pauseDo(s, http.MethodGet, "/api/pause?peers=1", "", nil)
	got = pauseSnap(t, rr)
	if p := got.Peers["laptop"]; !p.OK || p.State == nil || p.State.Global == nil || p.State.Global.Reason != "freeze" {
		t.Fatalf("peer state = %+v", got.Peers)
	}
	if !got.Paused || got.Global == nil || got.Global.By != "operator" {
		t.Fatalf("this daemon reports its own state next to the peers': %+v", got)
	}
}

func TestPauseREST_UnreachablePeerIsReportedNotFatal(t *testing.T) {
	s, ps, _ := pauseServer(t)
	s.cfg.Servers = []config.RemoteServerConfig{{Name: "gone", URL: "http://127.0.0.1:1", Token: "t", Enabled: true}}
	rr := pauseDo(s, http.MethodPost, "/api/pause", `{"scope":"global","reason":"x","all_peers":true}`, nil)
	if rr.Code != 200 {
		t.Fatalf("pause = %d %s", rr.Code, rr.Body.String())
	}
	got := pauseSnap(t, rr)
	if p := got.Peers["gone"]; p.OK || p.Error == "" {
		t.Fatalf("an unreachable peer must carry an error: %+v", got.Peers)
	}
	if _, blocked := ps.Blocked("/x"); !blocked {
		t.Fatal("the local pause must apply even when a peer is down")
	}
}
