package server

// Finished-Automaton archive REST surface.
//
//	GET  /api/autonomous/prds/{id}/archive  the archive of one Automaton, read on ask
//	POST /api/autonomous/archive            {dry_run: bool} (dry run is the default)
//
// The GET reads the archive file of that one Automaton and nothing else. The
// POST reports what an archive run would do (dry run, writes nothing) or, with
// dry_run false, takes a verified backup and archives every eligible finished
// Automaton. Neither route touches a session.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// automatonArchiveAPI is the optional manager surface the archive routes need.
type automatonArchiveAPI interface {
	ReadAutomatonArchive(id string) (*autonomous.PRDArchive, error)
	PlanAutomatonArchive() autonomous.ArchivePlan
	RunAutomatonArchive() (autonomous.ArchiveReport, error)
}

func (s *Server) archiveAPI(w http.ResponseWriter) (automatonArchiveAPI, bool) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return nil, false
	}
	a, ok := s.autonomousMgr.(automatonArchiveAPI)
	if !ok {
		http.Error(w, "archive is not available", http.StatusServiceUnavailable)
		return nil, false
	}
	return a, true
}

// handlePRDArchive serves GET /api/autonomous/prds/{id}/archive.
func (s *Server) handlePRDArchive(w http.ResponseWriter, r *http.Request) {
	a, ok := s.archiveAPI(w)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		id = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/autonomous/prds/"), "/archive")
	}
	arc, err := a.ReadAutomatonArchive(id)
	switch {
	case errors.Is(err, autonomous.ErrNotArchived):
		http.Error(w, "automaton is not archived", http.StatusNotFound)
		return
	case err != nil && strings.Contains(err.Error(), "not found"):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, arc)
}

// handleAutonomousArchive serves POST /api/autonomous/archive.
func (s *Server) handleAutonomousArchive(w http.ResponseWriter, r *http.Request) {
	a, ok := s.archiveAPI(w)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	var req struct {
		DryRun *bool `json:"dry_run"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	dryRun := req.DryRun == nil || *req.DryRun // dry run unless told otherwise
	if dryRun {
		writeJSONOK(w, a.PlanAutomatonArchive())
		return
	}
	report, err := a.RunAutomatonArchive()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	s.audit(r.Context(), "automaton_archive", "autonomous", "",
		map[string]any{"archived": len(report.Archived), "failed": len(report.Failed), "backup_dir": report.BackupDir})
	writeJSONOK(w, report)
}
