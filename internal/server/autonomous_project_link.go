package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// projectLinkAPI is the optional part of the Automaton manager that the
// project link routes need. autonomous.API implements it; a manager without it
// answers 501, so AutonomousAPI test doubles need no change.
type projectLinkAPI interface {
	SetPRDProject(prdID, project string) (*autonomous.PRD, error)
	EffectiveProject(prdID string) (project, source string)
	ProjectLinkBackfill(apply bool) autonomous.ProjectLinkBackfillReport
	LatestProjectLinkBackfill() (autonomous.ProjectLinkBackfillReport, bool)
}

// handleSetPRDProject serves POST /api/autonomous/prds/{id}/set_project with
// {"project":"<name>"}; an empty name clears the link. The caller has already
// checked the method and the federation capability. It edits the Automaton
// record only and never touches a session.
func (s *Server) handleSetPRDProject(w http.ResponseWriter, r *http.Request, id string) {
	pl, ok := s.autonomousMgr.(projectLinkAPI)
	if !ok {
		http.Error(w, "project link not supported", http.StatusNotImplemented)
		return
	}
	var req struct {
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	updated, err := pl.SetPRDProject(id, req.Project)
	if err != nil {
		if _, exists := s.autonomousMgr.GetPRD(id); !exists {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r.Context(), "set_project", "automaton", id, map[string]any{"project": strings.TrimSpace(req.Project)})
	s.writeAutonomousResult(w, r, s.withProjectLink(updated, id), "", "", "")
}

// withProjectLink adds the read-only effective project and its source to an
// Automaton read view: project is the project the Automaton belongs to and
// project_source says how it was found (link, automation, parent, directory
// or none). The stored record is never changed; the fields are added to the
// response copy only. A manager without the resolver leaves the view as is.
func (s *Server) withProjectLink(rec any, prdID string) any {
	pl, ok := s.autonomousMgr.(projectLinkAPI)
	if !ok || isNilPRD(rec) {
		return rec
	}
	project, source := pl.EffectiveProject(prdID)
	raw, err := json.Marshal(rec)
	if err != nil {
		return rec
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return rec
	}
	pb, _ := json.Marshal(project)
	sb, _ := json.Marshal(source)
	m["project"] = pb
	m["project_source"] = sb
	return m
}

// handleAutonomousProjectLinkBackfill serves GET (latest report) and POST
// {"apply":true|false} (run one pass; false is a dry run) on
// /api/autonomous/project-link-backfill. The pass reads and writes Automaton
// records only.
func (s *Server) handleAutonomousProjectLinkBackfill(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous manager not available", http.StatusServiceUnavailable)
		return
	}
	pl, ok := s.autonomousMgr.(projectLinkAPI)
	if !ok {
		http.Error(w, "project link backfill not supported", http.StatusNotImplemented)
		return
	}
	if r.Method == http.MethodGet {
		rep, ok := pl.LatestProjectLinkBackfill()
		if !ok {
			http.Error(w, "no project link backfill report yet", http.StatusNotFound)
			return
		}
		writeJSONOK(w, rep)
		return
	}
	var req struct {
		Apply bool `json:"apply"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	rep := pl.ProjectLinkBackfill(req.Apply)
	s.audit(r.Context(), "project_link_backfill", "autonomous", "", map[string]any{"apply": req.Apply, "counts": rep.Counts})
	writeJSONOK(w, rep)
}
