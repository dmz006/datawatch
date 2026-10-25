package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// tagAPI is the optional AutonomousAPI extension that manages tags.
type tagAPI interface {
	ListTags(includeSub bool) []any
	CreateTag(name, parentID, color, description string) (any, error)
	UpdateTag(id string, name, parentID, color, description *string) (any, error)
	DeleteTag(id string) error
	SetPRDTags(prdID string, tagIDs []string) (any, error)
	ListPRDsByTag(tagID string, includeSub bool) ([]any, error)
	// RequireTag and AutomatonTagIDs back the session tag registry and the
	// inherited-tag lookup (session_tags.go).
	RequireTag(id string) error
	AutomatonTagIDs(prdID string) ([]string, bool)
}

func (s *Server) tagAPI() (tagAPI, bool) {
	t, ok := s.autonomousMgr.(tagAPI)
	return t, ok
}

// queryBool reads a boolean query parameter, falling back to def when absent
// or unparseable.
func queryBool(r *http.Request, key string, def bool) bool {
	switch strings.ToLower(r.URL.Query().Get(key)) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	}
	return def
}

// writeTagError maps a tag error to its HTTP status. Refusals (children,
// assigned Automata, cycle) are 409; an assigned-Automata refusal carries
// the count in the body.
func writeTagError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	body := map[string]any{"error": err.Error()}
	switch {
	case errors.Is(err, autonomous.ErrTagNotFound):
		status = http.StatusNotFound
	case errors.Is(err, autonomous.ErrTagInUse):
		status = http.StatusConflict
		var inUse *autonomous.TagInUseError
		if errors.As(err, &inUse) {
			body["count"] = inUse.Count
		}
	case errors.Is(err, autonomous.ErrTagHasChildren), errors.Is(err, autonomous.ErrTagCycle):
		status = http.StatusConflict
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// handleAutonomousTags manages the tag registry.
//
//	GET    /api/autonomous/tags[?include_sub=]  — list tags with Automata counts
//	POST   /api/autonomous/tags                 — create {name,parent_id?,color?,description?}
//	PATCH  /api/autonomous/tags/{id}            — rename/recolor/reparent/describe
//	DELETE /api/autonomous/tags/{id}            — delete; 409 on children or assigned Automata
func (s *Server) handleAutonomousTags(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	api, ok := s.tagAPI()
	if !ok {
		http.Error(w, "tags not supported", http.StatusNotImplemented)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/autonomous/tags"), "/")
	if id == "" {
		switch r.Method {
		case http.MethodGet:
			if !s.fedCap(w, r, federation.CapAutonomousRead) {
				return
			}
			writeJSONOK(w, map[string]any{"tags": api.ListTags(queryBool(r, "include_sub", false))})
		case http.MethodPost:
			if !s.fedCap(w, r, federation.CapAutonomousWrite) {
				return
			}
			var req struct {
				Name        string `json:"name"`
				ParentID    string `json:"parent_id"`
				Color       string `json:"color"`
				Description string `json:"description"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			t, err := api.CreateTag(req.Name, req.ParentID, req.Color, req.Description)
			if err != nil {
				writeTagError(w, err)
				return
			}
			s.audit(r.Context(), "create", "automaton_tag", req.Name, nil)
			writeJSONOK(w, t)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	switch r.Method {
	case http.MethodPatch:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		var req struct {
			Name        *string `json:"name"`
			ParentID    *string `json:"parent_id"`
			Color       *string `json:"color"`
			Description *string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		t, err := api.UpdateTag(id, req.Name, req.ParentID, req.Color, req.Description)
		if err != nil {
			writeTagError(w, err)
			return
		}
		s.audit(r.Context(), "update", "automaton_tag", id, nil)
		writeJSONOK(w, t)
	case http.MethodDelete:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
		if err := api.DeleteTag(id); err != nil {
			writeTagError(w, err)
			return
		}
		s.audit(r.Context(), "delete", "automaton_tag", id, nil)
		writeJSONOK(w, map[string]any{"deleted": id})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
