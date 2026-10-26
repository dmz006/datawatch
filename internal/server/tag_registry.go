package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/tagregistry"
)

// The shared tag registry routes. A daemon that keeps the registry, or stands
// alone, applies a tag write to its own registry. A peer forwards the write to
// the keeper and adopts the keeper's answer, so a conflict the keeper decided is
// relayed to the caller. The keeper serves its snapshot and applies the writes
// peers forward. Every route here is guarded by a federation capability.

// tagRegistryStatus is the registry object of GET /api/autonomous/tags: the
// role, keeper, staleness and sync state of this daemon's copy, and its merge
// notes. A daemon with no registry wired reports itself as standalone.
func (s *Server) tagRegistryStatus() map[string]any {
	if s.tagRegistry == nil {
		return map[string]any{
			"role":               tagregistry.RoleStandalone,
			"keeper":             "",
			"stale":              false,
			"last_synced_at":     nil,
			"revision":           int64(0),
			"pending_sync_count": 0,
			"notes":              []tagregistry.Note{},
			"last_error":         "",
		}
	}
	v := s.tagRegistry.View()
	pending := 0
	for _, t := range v.Tags {
		if t.PendingSync {
			pending++
		}
	}
	notes := v.Notes
	if notes == nil {
		notes = []tagregistry.Note{}
	}
	var synced any
	if !v.LastSyncedAt.IsZero() {
		synced = v.LastSyncedAt
	}
	return map[string]any{
		"role":               v.Role,
		"keeper":             v.Keeper,
		"stale":              v.Stale,
		"last_synced_at":     synced,
		"revision":           v.Revision,
		"pending_sync_count": pending,
		"notes":              notes,
		"last_error":         v.LastError,
	}
}

// SetTagRegistry wires the shared tag registry. startSync starts the sync loop;
// the daemon calls it once at boot, and again after a config change makes this
// daemon a peer. Nil startSync starts nothing.
func (s *Server) SetTagRegistry(r *tagregistry.Registry, startSync func()) {
	s.tagRegistry = r
	s.tagRegistryStart = startSync
}

// applyTagRegistrySettings reconciles the registry with the live
// autonomous.tags.registry_keeper and registry_sync_seconds, so a change made
// through PUT /api/config takes effect without a restart.
func (s *Server) applyTagRegistrySettings() {
	if s.tagRegistry == nil || s.cfg == nil {
		return
	}
	eff := s.cfg.Autonomous.Tags.Effective()
	every := time.Duration(eff.RegistrySyncSeconds) * time.Second
	if err := s.tagRegistry.Reconfigure(eff.RegistryKeeper, every); err != nil {
		fmt.Printf("[tags] registry settings not applied: %v\n", err)
		return
	}
	if s.tagRegistryStart != nil {
		s.tagRegistryStart()
	}
}

// emitTagChange sends one tag_update frame for a registry change, so open
// clients refetch their counts. An empty kind sends nothing.
func (s *Server) emitTagChange(kind, tagID string) {
	if kind == "" || s.hub == nil {
		return
	}
	s.hub.BroadcastTagUpdate(autonomous.TagEvent{Kind: kind, TagID: tagID})
}

// tagEventKindFor maps a registry write kind to its tag event kind. A description
// change sends no event, as the local description write never did.
func tagEventKindFor(kind tagregistry.WriteKind) string {
	switch kind {
	case tagregistry.WriteCreate:
		return autonomous.TagEventCreated
	case tagregistry.WriteRename:
		return autonomous.TagEventRenamed
	case tagregistry.WriteReparent:
		return autonomous.TagEventReparented
	case tagregistry.WriteRecolor:
		return autonomous.TagEventRecolored
	case tagregistry.WriteDelete:
		return autonomous.TagEventDeleted
	}
	return ""
}

// registryWriteView is a tag as the write routes return it. The conflict fields
// are set when the keeper kept its own value for a field the caller edited.
type registryWriteView struct {
	tagregistry.Tag
	Conflict      bool   `json:"conflict,omitempty"`
	ConflictField string `json:"conflict_field,omitempty"`
	Revision      int64  `json:"revision"`
}

// registryWriteResponse builds the reply for a write that returned tag t.
func registryWriteResponse(t tagregistry.Tag, res tagregistry.WriteResult) registryWriteView {
	return registryWriteView{Tag: t, Conflict: res.Conflict, ConflictField: res.ConflictField, Revision: res.Revision}
}

// registryTagByID returns the tag with id from the registry's current copy.
func (s *Server) registryTagByID(id string) (tagregistry.Tag, bool) {
	for _, t := range s.tagRegistry.View().Tags {
		if t.ID == id {
			return t, true
		}
	}
	return tagregistry.Tag{}, false
}

// registryCreateTag is the POST /api/autonomous/tags write when a registry is
// wired. A peer forwards the create to the keeper, or keeps it pending when the
// keeper is unreachable.
func (s *Server) registryCreateTag(w http.ResponseWriter, r *http.Request, name, parentID, color, description string) {
	res, err := s.tagRegistry.Create(r.Context(), tagregistry.CreateInput{
		Name: name, ParentID: parentID, Color: color, Description: description,
	})
	if err != nil {
		writeTagError(w, err)
		return
	}
	s.audit(r.Context(), "create", "automaton_tag", name, nil)
	s.emitTagChange(autonomous.TagEventCreated, res.Tag.ID)
	writeJSONOK(w, registryWriteResponse(res.Tag, res))
}

// tagPatchRequest is the PATCH /api/autonomous/tags/{id} body. A nil field is
// left as it is.
type tagPatchRequest struct {
	Name        *string `json:"name"`
	ParentID    *string `json:"parent_id"`
	Color       *string `json:"color"`
	Description *string `json:"description"`
}

// registryPatchTag is the PATCH write when a registry is wired. The fields are
// written in the order the local update used: parent, name, color, description.
// A refused parent change therefore leaves every field as it was. A later
// refusal leaves the earlier fields applied, and the error says which write
// was refused.
func (s *Server) registryPatchTag(w http.ResponseWriter, r *http.Request, id string, req tagPatchRequest) {
	cur, ok := s.registryTagByID(id)
	if !ok {
		writeTagError(w, fmt.Errorf("%w: %q", autonomous.ErrTagNotFound, id))
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeTagError(w, fmt.Errorf("%w: name is required", autonomous.ErrTagInvalid))
		return
	}
	ctx := r.Context()
	var conflict tagregistry.WriteResult
	var events []string
	step := func(kind string, call func() (tagregistry.WriteResult, error)) error {
		res, err := call()
		if err != nil {
			return err
		}
		if res.Conflict {
			conflict = res
		}
		events = append(events, kind)
		return nil
	}
	var err error
	if req.ParentID != nil && *req.ParentID != cur.ParentID {
		err = step(autonomous.TagEventReparented, func() (tagregistry.WriteResult, error) {
			return s.tagRegistry.Reparent(ctx, id, *req.ParentID)
		})
	}
	if err == nil && req.Name != nil {
		err = step(autonomous.TagEventRenamed, func() (tagregistry.WriteResult, error) {
			return s.tagRegistry.Rename(ctx, id, *req.Name)
		})
	}
	if err == nil && req.Color != nil {
		err = step(autonomous.TagEventRecolored, func() (tagregistry.WriteResult, error) {
			return s.tagRegistry.Recolor(ctx, id, *req.Color)
		})
	}
	if err == nil && req.Description != nil {
		err = step("", func() (tagregistry.WriteResult, error) {
			return s.tagRegistry.Describe(ctx, id, *req.Description)
		})
	}
	if err != nil {
		writeTagError(w, err)
		return
	}
	s.audit(ctx, "update", "automaton_tag", id, nil)
	for _, kind := range events {
		s.emitTagChange(kind, id)
	}
	t, _ := s.registryTagByID(id)
	writeJSONOK(w, registryWriteResponse(t, conflict))
}

// registryDeleteTag is the DELETE write when a registry is wired.
func (s *Server) registryDeleteTag(w http.ResponseWriter, r *http.Request, id string) {
	if _, err := s.tagRegistry.Delete(r.Context(), id); err != nil {
		writeTagError(w, err)
		return
	}
	s.audit(r.Context(), "delete", "automaton_tag", id, nil)
	s.emitTagChange(autonomous.TagEventDeleted, id)
	writeJSONOK(w, map[string]any{"deleted": id})
}

// handleTagRegistrySnapshot serves the keeper's registry to its peers.
//
//	GET /api/autonomous/tags/registry  → {revision, tags, notes}
//
// A peer daemon refuses with 409: only the keeper and a standalone daemon hold
// the registry.
func (s *Server) handleTagRegistrySnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.tagRegistry == nil {
		http.Error(w, "tag registry not wired", http.StatusServiceUnavailable)
		return
	}
	snap, err := s.tagRegistry.KeeperSnapshot()
	if err != nil {
		writeTagError(w, err)
		return
	}
	writeJSONOK(w, snap)
}

// handleTagRegistryWrite applies one write a peer forwarded to this daemon.
//
//	POST /api/autonomous/tags/registry/write  body: tagregistry.Write  → tagregistry.WriteResult
//
// The reply carries the keeper's value, the revision and any conflict, so the
// peer can relay them. A daemon that is not the keeper refuses with 409.
func (s *Server) handleTagRegistryWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	if s.tagRegistry == nil {
		http.Error(w, "tag registry not wired", http.StatusServiceUnavailable)
		return
	}
	var wr tagregistry.Write
	if err := json.NewDecoder(r.Body).Decode(&wr); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	res, err := s.tagRegistry.ApplyPeerWrite(wr)
	if err != nil {
		writeTagError(w, err)
		return
	}
	s.audit(r.Context(), forwardedAuditAction(wr.Kind), "automaton_tag", wr.TagID,
		map[string]any{"forwarded": true, "edit": string(wr.Kind)})
	s.emitTagChange(tagEventKindFor(wr.Kind), res.Tag.ID)
	writeJSONOK(w, res)
}

// forwardedAuditAction is the audit action for a forwarded write, named as the
// local write is.
func forwardedAuditAction(kind tagregistry.WriteKind) string {
	switch kind {
	case tagregistry.WriteCreate:
		return "create"
	case tagregistry.WriteDelete:
		return "delete"
	}
	return "update"
}

// handleTagRegistryNotes dismisses a merge or removal note on this daemon.
//
//	POST /api/autonomous/tags/registry/notes/{id}/dismiss  → {dismissed}
//
// A dismissed note stays dismissed when the keeper's snapshot is applied again.
func (s *Server) handleTagRegistryNotes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/autonomous/tags/registry/notes/")
	id, action, ok := strings.Cut(rest, "/")
	if !ok || id == "" || action != "dismiss" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	if s.tagRegistry == nil {
		http.Error(w, "tag registry not wired", http.StatusServiceUnavailable)
		return
	}
	if err := s.tagRegistry.DismissNote(id); err != nil {
		writeTagError(w, err)
		return
	}
	s.audit(r.Context(), "dismiss", "automaton_tag_note", id, nil)
	writeJSONOK(w, map[string]any{"dismissed": id})
}
