package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/tagregistry"
)

// tagSettingLookup reads a tag setting for the tag routes from the live config.
// It answers only the attention statuses key, the one the routes need.
func (s *Server) tagSettingLookup(key string) ([]string, bool) {
	if key == autonomous.AttentionStatusesKey && s.cfg != nil {
		return s.cfg.Autonomous.Tags.Effective().AttentionStatuses, true
	}
	return nil, false
}

// tagSettingsForDisplay renders the effective autonomous.tags block for
// GET /api/config, so an unset value shows its default rather than a blank.
func tagSettingsForDisplay(t config.TagsConfig) map[string]interface{} {
	e := t.Effective()
	return map[string]interface{}{
		"default_view":          e.DefaultView,
		"attention_statuses":    e.AttentionStatuses,
		"default_match_mode":    e.DefaultMatchMode,
		"direct_counts_visible": e.DirectCountsVisible,
		"registry_keeper":       e.RegistryKeeper,
		"registry_sync_seconds": e.RegistrySyncSeconds,
	}
}

// firstTagSettingError returns the first refusal among the autonomous.tags keys
// of a PUT /api/config patch, checked in key order so the reported error is
// stable. It returns nil when every tag key is valid or none is present.
func firstTagSettingError(patch map[string]interface{}) error {
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := config.ValidateTagSetting(k, patch[k]); err != nil {
			return err
		}
	}
	return nil
}

// tagAPI is the optional AutonomousAPI extension that manages tags.
type tagAPI interface {
	ListTags(includeSub bool) []any
	ListTagsWithCounts(includeSub bool) ([]any, autonomous.UntaggedCounts)
	CreateTag(name, parentID, color, description string) (any, error)
	UpdateTag(id string, name, parentID, color, description *string) (any, error)
	DeleteTag(id string) error
	SetPRDTags(prdID string, tagIDs []string) (any, error)
	SetPRDTagsAndPrimary(prdID string, tagIDs []string, primary *string) (any, error)
	ListPRDsByTag(tagID string, includeSub bool) ([]any, error)
	AttentionAutomata(statuses []string) []any
	// RequireTag and AutomatonTagIDs back the session tag registry and the
	// inherited-tag lookup (session_tags.go).
	RequireTag(id string) error
	AutomatonTagIDs(prdID string) ([]string, bool)
}

func (s *Server) tagAPI() (tagAPI, bool) {
	t, ok := s.autonomousMgr.(tagAPI)
	return t, ok
}

// BroadcastTagUpdate sends a tag_update frame to every connected client. The
// frame carries the id-only TagEvent, so a client refetches the counts it
// shows. The autonomous Manager's tag callback (main.go) and the session tags
// route call it.
func (h *Hub) BroadcastTagUpdate(ev autonomous.TagEvent) {
	h.Broadcast(MsgTagUpdate, ev)
}

// BroadcastTagUpdate is the HTTPServer entry point for tag_update frames. A
// server without a hub drops the frame.
func (s *HTTPServer) BroadcastTagUpdate(ev autonomous.TagEvent) {
	if s.hub == nil {
		return
	}
	s.hub.BroadcastTagUpdate(ev)
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
// assigned Automata, cycle, a peer that is not the keeper) are 409; an
// assigned-Automata refusal carries the count in the body. A keeper that cannot
// be reached is 503. The body names the refusal kind (tagregistry.ErrorKind)
// when the error has one, so a peer maps a relayed refusal back to its sentinel.
func writeTagError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	body := map[string]any{"error": err.Error()}
	if kind := tagregistry.ErrorKind(err); kind != "" {
		body["kind"] = kind
	}
	switch {
	case errors.Is(err, autonomous.ErrTagNotFound), errors.Is(err, tagregistry.ErrNoteNotFound):
		status = http.StatusNotFound
	case errors.Is(err, autonomous.ErrTagInUse):
		status = http.StatusConflict
		var inUse *autonomous.TagInUseError
		if errors.As(err, &inUse) {
			body["count"] = inUse.Count
		}
	case errors.Is(err, autonomous.ErrTagHasChildren), errors.Is(err, autonomous.ErrTagCycle),
		errors.Is(err, tagregistry.ErrNotKeeper):
		status = http.StatusConflict
	case errors.Is(err, tagregistry.ErrKeeperUnreachable):
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// handleAutonomousAttention lists the Automata that need the operator's
// attention.
//
//	GET /api/autonomous/attention  → {statuses, items}
//
// statuses are the attention statuses in force. items are the Automata whose
// status is one of them, plus every Automaton with a task held for operator
// review, each as its summary view (blocked_reason included).
func (s *Server) handleAutonomousAttention(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	api, ok := s.tagAPI()
	if !ok {
		http.Error(w, "tags not supported", http.StatusNotImplemented)
		return
	}
	statuses := autonomous.ResolveAttentionStatuses(s.autonomousSettings)
	writeJSONOK(w, map[string]any{"statuses": statuses, "items": api.AttentionAutomata(statuses)})
}

// handleAutonomousTags manages the tag registry.
//
//	GET    /api/autonomous/tags[?include_sub=&counts=]  — list tags with Automata counts
//	POST   /api/autonomous/tags                         — create {name,parent_id?,color?,description?}
//	PATCH  /api/autonomous/tags/{id}                    — rename/recolor/reparent/describe
//	DELETE /api/autonomous/tags/{id}                    — delete; 409 on children or assigned Automata
//
// counts=true adds subtree_count, direct_count, session_subtree_count and
// session_direct_count to each tag, and an untagged {count, session_count}
// object to the reply. The count field keeps its include_sub meaning.
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
			includeSub := queryBool(r, "include_sub", false)
			registry := s.tagRegistryStatus()
			if queryBool(r, "counts", false) {
				tags, untagged := api.ListTagsWithCounts(includeSub)
				writeJSONOK(w, map[string]any{"tags": tags, "untagged": untagged, "registry": registry})
				return
			}
			writeJSONOK(w, map[string]any{"tags": api.ListTags(includeSub), "registry": registry})
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
			if s.tagRegistry != nil {
				s.registryCreateTag(w, r, req.Name, req.ParentID, req.Color, req.Description)
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
		var req tagPatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if s.tagRegistry != nil {
			s.registryPatchTag(w, r, id, req)
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
		if s.tagRegistry != nil {
			s.registryDeleteTag(w, r, id)
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
