// Operator pause REST surface.
//
//	GET  /api/pause          the pause state; ?peers=1 adds every federation peer's own state
//	POST /api/pause          pause new work { scope: global|project, project?, reason, actor?, peers?, all_peers? }
//	POST /api/pause/resume   clear a pause { scope, project? } or { all: true }, plus { actor?, note?, peers?, all_peers? }
//
// A pause stops new work from being admitted (new Automaton starts, restarts,
// new worker admissions, lane fill) without stopping schedules, the daemon or
// the autonomous manager; work already running finishes. Reading needs
// pause:read, setting or clearing needs pause:write. Clearing is refused to a
// session-scoped credential even when it holds pause:write, and to any actor
// that names a worker, watcher, supervisor or model, so a model can never
// resume what the operator paused. Each daemon keeps its own state; a pause
// can be sent to peers, and each peer reports its own.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/pause"
)

// SetPause wires the operator pause service exposed at /api/pause and routes
// its audit events into the operator audit log.
func (s *Server) SetPause(p *pause.Store) {
	s.pauseStore = p
	if p != nil {
		p.SetAudit(s.auditPauseEvent)
	}
}

// SetPause wires the operator pause service on the HTTP server.
func (s *HTTPServer) SetPause(p *pause.Store) {
	if s.api != nil {
		s.api.SetPause(p)
	}
}

// auditPauseEvent writes one pause, resume or refused resume to the audit log.
func (s *Server) auditPauseEvent(e pause.Event) {
	if s.auditLog == nil {
		return
	}
	action := audit.ActionPauseSet
	noteKey := "reason"
	switch e.Action {
	case pause.ActionResume:
		action, noteKey = audit.ActionPauseResumed, "note"
	case pause.ActionResumeRefused:
		action, noteKey = audit.ActionPauseResumeRefused, "note"
	}
	subject := string(e.Scope)
	if e.Scope == pause.ScopeProject {
		subject = e.Project
	}
	if subject == "" {
		subject = "all"
	}
	_ = s.auditLog.Write(audit.Entry{
		Timestamp: e.At, Actor: e.Actor, Action: action, Subject: subject,
		Details: map[string]any{
			"scope": string(e.Scope), "project": e.Project, noteKey: e.Reason,
			"resource_type": "pause", "resource_id": subject,
		},
	})
}

// pauseRequest is the POST body of both endpoints.
type pauseRequest struct {
	Scope    string   `json:"scope"`
	Project  string   `json:"project,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Note     string   `json:"note,omitempty"`
	Actor    string   `json:"actor,omitempty"`
	All      bool     `json:"all,omitempty"`
	Peers    []string `json:"peers,omitempty"`
	AllPeers bool     `json:"all_peers,omitempty"`
}

// pausePeerResult is what one peer answered.
type pausePeerResult struct {
	OK    bool           `json:"ok"`
	Error string         `json:"error,omitempty"`
	State *pauseResponse `json:"state,omitempty"`
}

// pauseResponse is the state this daemon reports.
type pauseResponse struct {
	Paused   bool                       `json:"paused"`
	Global   *pause.Entry               `json:"global,omitempty"`
	Projects []pause.Entry              `json:"projects,omitempty"`
	Cleared  int                        `json:"cleared,omitempty"`
	Peers    map[string]pausePeerResult `json:"peers,omitempty"`
}

func (s *Server) pauseState() pauseResponse {
	snap := s.pauseStore.Snapshot()
	return pauseResponse{Paused: snap.Paused, Global: snap.Global, Projects: snap.Projects}
}

// pauseActor derives who is calling. A session-scoped credential is always a
// session, whatever the body says; a federation peer is named; only the admin
// token may label itself with a surface name (cli, web, channel:signal).
func (s *Server) pauseActor(ctx context.Context, claimed string) string {
	if sessionCapsFromContext(ctx) != nil {
		if s.sessionTokens != nil {
			if tok := callerTokenFromContext(ctx); tok != "" {
				if id := s.sessionTokens.SessionIDForToken(tok); id != "" {
					return "session:" + id
				}
			}
		}
		return "session:unknown"
	}
	if peer := peerFromContext(ctx); peer != nil {
		return "peer:" + peer.Name
	}
	if a := strings.TrimSpace(claimed); a != "" {
		return a
	}
	return "operator"
}

// resolvePauseProject turns a registered project name into its directory; a
// value that is not a registered name is taken as a directory.
func (s *Server) resolvePauseProject(p string) string {
	p = strings.TrimSpace(p)
	if s.cfg != nil {
		if e, ok := s.cfg.Projects[p]; ok && strings.TrimSpace(e.Dir) != "" {
			return e.Dir
		}
	}
	return p
}

func pauseWriteErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pause.ErrNotOperator):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, pause.ErrReasonRequired), errors.Is(err, pause.ErrProjectRequired), errors.Is(err, pause.ErrBadScope):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handlePause serves GET and POST /api/pause.
func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapPauseRead) {
			return
		}
		if s.pauseStore == nil {
			http.Error(w, "pause is not available", http.StatusServiceUnavailable)
			return
		}
		out := s.pauseState()
		if v := r.URL.Query().Get("peers"); v == "1" || v == "true" {
			out.Peers = s.pausePeerCall(r.Context(), s.federationTargets(), http.MethodGet, "/api/pause", nil)
		}
		writeJSONOK(w, out)
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapPauseWrite) {
			return
		}
		if s.pauseStore == nil {
			http.Error(w, "pause is not available", http.StatusServiceUnavailable)
			return
		}
		var req pauseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		actor := s.pauseActor(r.Context(), req.Actor)
		if _, err := s.pauseStore.Pause(pause.Scope(req.Scope), s.resolvePauseProject(req.Project), req.Reason, actor); err != nil {
			pauseWriteErr(w, err)
			return
		}
		out := s.pauseState()
		out.Peers = s.pauseSendToPeers(r.Context(), req, "/api/pause", pauseRequest{
			Scope: req.Scope, Project: s.resolvePauseProject(req.Project), Reason: req.Reason,
		})
		writeJSONOK(w, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePauseResume serves POST /api/pause/resume.
func (s *Server) handlePauseResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapPauseWrite) {
		return
	}
	if s.pauseStore == nil {
		http.Error(w, "pause is not available", http.StatusServiceUnavailable)
		return
	}
	var req pauseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	actor := s.pauseActor(r.Context(), req.Actor)
	var cleared int
	if req.All {
		n, err := s.pauseStore.ResumeAll(actor, req.Note)
		if err != nil {
			pauseWriteErr(w, err)
			return
		}
		cleared = n
	} else {
		ok, err := s.pauseStore.Resume(pause.Scope(req.Scope), s.resolvePauseProject(req.Project), actor, req.Note)
		if err != nil {
			pauseWriteErr(w, err)
			return
		}
		if ok {
			cleared = 1
		}
	}
	out := s.pauseState()
	out.Cleared = cleared
	out.Peers = s.pauseSendToPeers(r.Context(), req, "/api/pause/resume", pauseRequest{
		Scope: req.Scope, Project: s.resolvePauseProject(req.Project), Note: req.Note, All: req.All,
	})
	writeJSONOK(w, out)
}

// pauseSendToPeers forwards a pause or resume to the peers the request names
// (all enabled peers, or the listed ones). It returns nil when it names none.
func (s *Server) pauseSendToPeers(ctx context.Context, req pauseRequest, path string, body pauseRequest) map[string]pausePeerResult {
	if !req.AllPeers && len(req.Peers) == 0 {
		return nil
	}
	targets := s.federationTargets()
	if !req.AllPeers {
		want := map[string]bool{}
		for _, n := range req.Peers {
			want[n] = true
		}
		var picked []federationTarget
		for _, t := range targets {
			if want[t.name] {
				picked = append(picked, t)
				delete(want, t.name)
			}
		}
		out := s.pausePeerCall(ctx, picked, http.MethodPost, path, &body)
		for n := range want {
			out[n] = pausePeerResult{Error: "no such federation peer"}
		}
		return out
	}
	return s.pausePeerCall(ctx, targets, http.MethodPost, path, &body)
}

// pausePeerCall calls every target in parallel and collects what each said.
// A peer that is down or refuses is reported in its own entry; it never fails
// the local action.
func (s *Server) pausePeerCall(ctx context.Context, targets []federationTarget, method, path string, body *pauseRequest) map[string]pausePeerResult {
	out := map[string]pausePeerResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, t := range targets {
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := s.pauseCallPeer(ctx, t, method, path, body)
			mu.Lock()
			out[t.name] = res
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func (s *Server) pauseCallPeer(ctx context.Context, t federationTarget, method, path string, body *pauseRequest) pausePeerResult {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return pausePeerResult{Error: err.Error()}
		}
		rd = bytes.NewReader(b)
	}
	u := strings.TrimRight(t.url, "/") + path
	if _, err := url.Parse(u); err != nil {
		return pausePeerResult{Error: err.Error()}
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, method, u, rd)
	if err != nil {
		return pausePeerResult{Error: err.Error()}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := s.peerClient(t).Do(req)
	if err != nil {
		return pausePeerResult{Error: err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return pausePeerResult{Error: fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(data)))}
	}
	var st pauseResponse
	if err := json.Unmarshal(data, &st); err != nil {
		return pausePeerResult{Error: "unreadable answer: " + err.Error()}
	}
	st.Peers = nil
	return pausePeerResult{OK: true, State: &st}
}
