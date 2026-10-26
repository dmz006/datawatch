package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// sessionTagBridge lets the session manager validate session tags and read
// the tags of an owning Automaton, both through the autonomous tag API. It
// reads the autonomous manager on every call, so it stays correct if that
// manager is wired after the session manager.
type sessionTagBridge struct{ s *Server }

// RequireTag implements session.TagRegistry. With no autonomous tag API
// wired, every tag is unknown.
func (b sessionTagBridge) RequireTag(id string) error {
	api, ok := b.s.tagAPI()
	if !ok {
		return fmt.Errorf("%w: %q", autonomous.ErrTagNotFound, id)
	}
	return api.RequireTag(id)
}

// AutomatonTagIDs implements session.TagOwnerLookup.
func (b sessionTagBridge) AutomatonTagIDs(prdID string) ([]string, bool) {
	api, ok := b.s.tagAPI()
	if !ok {
		return nil, false
	}
	return api.AutomatonTagIDs(prdID)
}

// sessionTagCounterSetter is implemented by the autonomous API so the server
// can hand it the session manager as its session tag counter.
type sessionTagCounterSetter interface {
	SetSessionTagCounter(c autonomous.SessionTagCounter)
}

// wireSessionTags connects the session manager to the autonomous tag
// registry. The session side validates and resolves tags through the bridge;
// the autonomous side counts session-carried tags for its delete-in-use check.
func (s *Server) wireSessionTags() {
	if s.manager == nil {
		return
	}
	bridge := sessionTagBridge{s}
	s.manager.SetTagRegistry(bridge)
	s.manager.SetTagOwnerLookup(bridge)
	if c, ok := s.autonomousMgr.(sessionTagCounterSetter); ok {
		c.SetSessionTagCounter(s.manager)
	}
}

// sessionTagView is a session's JSON with the read-time inherited_tags
// added. The embedded tags field is the session's own tags. inherited_tags
// comes from the owning Automaton and is never persisted.
type sessionTagView struct {
	*session.Session
	InheritedTags []string `json:"inherited_tags"`
}

// sessionView wraps one session for a list or detail response.
func (s *Server) sessionView(sess *session.Session) sessionTagView {
	return sessionTagView{Session: sess, InheritedTags: s.manager.InheritedTags(sess)}
}

// sessionViews wraps every session of a list in sessionView.
func (s *Server) sessionViews(sessions []*session.Session) []sessionTagView {
	out := make([]sessionTagView, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, s.sessionView(sess))
	}
	return out
}

// handleSessionTags replaces a session's own tags.
//
//	POST /api/sessions/tags  {"id":"<session>","tag_ids":["<tag>",...]}
//
// id is the full or short session ID. A tag ID the registry does not know
// is refused with 404 and nothing is written; duplicates are dropped, first
// occurrence wins. The response is the updated session with inherited_tags.
// Requires the sessions write capability.
func (s *Server) handleSessionTags(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID     string   `json:"id"`
		TagIDs []string `json:"tag_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	sess, err := s.manager.SetSessionTags(req.ID, req.TagIDs)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeTagError(w, err)
		return
	}
	s.audit(r.Context(), "set_tags", "session", sess.FullID, map[string]any{"tag_ids": sess.Tags})
	s.broadcastSessionsAsync()
	if s.hub != nil {
		s.hub.BroadcastTagUpdate(autonomous.TagEvent{Kind: autonomous.TagEventSessionAssigned, SessionID: sess.FullID})
	}
	writeJSONOK(w, s.sessionView(sess))
}
