package server

import (
	"encoding/json"
	"errors"
	"fmt"
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
		"placement":             placementForDisplay(e.Placement),
	}
}

// placementForDisplay renders the effective placement block for
// GET /api/config. enabled and template are shown per axis, so the operator
// sees what a daemon with no placement config of its own would derive.
func placementForDisplay(p config.PlacementConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled": p.Enabled,
		"axes": map[string]interface{}{
			"compute":    axisForDisplay(p.Compute),
			"peer":       axisForDisplay(p.Peer),
			"backend":    axisForDisplay(p.Backend),
			"lane":       axisForDisplay(p.Lane),
			"role":       axisForDisplay(p.Role),
			"project":    axisForDisplay(p.Project),
			"automaton":  axisForDisplay(p.Automaton),
			"automation": axisForDisplay(p.Automation),
			"stage":      axisForDisplay(p.Stage),
			"kind":       axisForDisplay(p.Kind),
		},
		"custom": customAxesForDisplay(p.Custom),
	}
}

func axisForDisplay(a config.PlacementAxis) map[string]interface{} {
	return map[string]interface{}{"enabled": a.Enabled, "template": a.Template}
}

func customAxesForDisplay(cs []config.PlacementCustomAxis) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]interface{}{
			"name": c.Name, "field": c.Field, "template": c.Template, "enabled": c.Enabled,
		})
	}
	return out
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
	// RequireTag, TagSystem and AutomatonTagIDs back the session tag
	// registry and the inherited-tag lookup (session_tags.go).
	RequireTag(id string) error
	TagSystem(id string) (bool, error)
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

// tagListFilters reads the optional system and axis filters of the tag
// listing (see handleAutonomousTags). system is nil when the parameter is
// absent, so filtering only happens on request; axis is "" without the
// parameter. A present but unrecognised system value is a refusal, and an
// axis name outside the axis rule (lowercase letters, digits, hyphens) is
// one too.
func tagListFilters(r *http.Request) (system *bool, axis string, err error) {
	switch strings.ToLower(r.URL.Query().Get("system")) {
	case "":
		// No filter.
	case "1", "true", "yes":
		v := true
		system = &v
	case "0", "false", "no":
		v := false
		system = &v
	default:
		return nil, "", fmt.Errorf("system must be true or false")
	}
	axis = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("axis")))
	if axis != "" && !axisNameOK(axis) {
		return nil, "", fmt.Errorf("axis must be lowercase letters, digits and hyphens")
	}
	return system, axis, nil
}

// axisNameOK is the placement axis name rule (see
// config.placementCustomNameOK): lowercase letters, digits and hyphens.
func axisNameOK(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

// tagIdentity reads the name and system-ness of one tag element of a tag
// listing, whichever of the two list shapes it is.
func tagIdentity(tag any) (name string, system bool, ok bool) {
	switch t := tag.(type) {
	case autonomous.TagView:
		return t.Name, t.System, true
	case autonomous.TagCountView:
		return t.Name, t.System, true
	}
	return "", false, false
}

// tagMatchesAxis reports whether a tag name belongs to the axis: the bare
// axis name, or an axis-derived name "<axis>:<value>".
func tagMatchesAxis(name, axis string) bool {
	return name == axis || strings.HasPrefix(name, axis+":")
}

// filterTagList keeps the tags matching the listing filters (see
// tagListFilters). An element in neither shape of a tag listing is returned
// untouched, so the filter degrades to a pass-through rather than dropping
// data.
func filterTagList(tags []any, system *bool, axis string) []any {
	if system == nil && axis == "" {
		return tags
	}
	out := make([]any, 0, len(tags))
	for _, tag := range tags {
		name, isSystem, ok := tagIdentity(tag)
		if !ok {
			out = append(out, tag)
			continue
		}
		if system != nil && isSystem != *system {
			continue
		}
		if axis != "" && !tagMatchesAxis(name, axis) {
			continue
		}
		out = append(out, tag)
	}
	return out
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
		errors.Is(err, autonomous.ErrTagSystem), errors.Is(err, tagregistry.ErrNotKeeper), errors.Is(err, tagregistry.ErrTagSystem):
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
//
// system=false keeps the operator tags only and system=true the system tags
// only; axis=<axis> keeps the tags of one placement axis: any tag starting
// with "<axis>:" plus the bare axis name when it exists. Both filters may
// be combined and each is optional — omitting both leaves the listing
// unchanged.
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
			system, axis, filterErr := tagListFilters(r)
			if filterErr != nil {
				http.Error(w, filterErr.Error(), http.StatusBadRequest)
				return
			}
			registry := s.tagRegistryStatus()
			if queryBool(r, "counts", false) {
				tags, untagged := api.ListTagsWithCounts(includeSub)
				writeJSONOK(w, map[string]any{"tags": filterTagList(tags, system, axis), "untagged": untagged, "registry": registry})
				return
			}
			writeJSONOK(w, map[string]any{"tags": filterTagList(api.ListTags(includeSub), system, axis), "registry": registry})
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

// tagSweepAPI is the sweep surface the tag-sweep handler asserts the
// autonomous manager against; it keeps the method out of the
// AutonomousAPI interface so the existing server-side fakes stay untouched.
type tagSweepAPI interface {
	SweepTags(dryRun bool) autonomous.SweepResult
}

// handleTagSweep applies the in-force tag settings (autonomous.tags.
// placement — the master switch, the per-axis on/off and template, the
// custom axes) to everything that already exists under them: one pass over
// every session and record the Automata own, re-deriving each one's system
// tags from its stored facts and writing the changed ones back.
//
//	POST /api/autonomous/tags/sweep[?dry_run=true]
//
// It answers with a per-kind tally (sessions, automata, automations,
// tasks, templates, decision records, held questions): examined, changed,
// unchanged, failed. dry_run (query or body) reports the same tally without
// writing a single tag. The operator's own tags and the inherited tags are
// a separate field the sweep never reads or writes; a second pass changes
// nothing; turning an axis off removes that axis's tag and turning it on
// adds it. The CLI's `autonomous tag-sweep` and the dashboard's sweep
// button both land here.
func (s *Server) handleTagSweep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sweep, ok := s.autonomousMgr.(tagSweepAPI)
	if !ok {
		http.Error(w, "tag sweep not supported", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	// The query flag is primary (the CLI and the dashboard use it); the
	// dry_run body field is accepted on top for clients that post JSON.
	dryRun := queryBool(r, "dry_run", false)
	if r.ContentLength > 0 {
		var body struct {
			DryRun *bool `json:"dry_run"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.DryRun != nil {
			dryRun = *body.DryRun
		}
	}
	mode := "run"
	if dryRun {
		mode = "dry-run"
	}
	s.audit(r.Context(), "sweep", "autonomous_tags", mode, nil)
	writeJSONOK(w, sweep.SweepTags(dryRun))
}
