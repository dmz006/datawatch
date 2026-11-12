// Defect queue REST endpoints: intake, open batches, split-out and the
// tiny-fix check. The routing, grouping and classification rules live in
// internal/autonomous (defect_intake.go, defect_batch.go, defect_tinyfix.go);
// these handlers decode, validate and delegate.
//
//	POST /api/autonomous/defects/intake            route one defect
//	GET  /api/autonomous/defects/batches           open batches + story->defect map
//	POST /api/autonomous/defects/batches/{id}/split move one story to its own Automaton
//	POST /api/autonomous/defects/tiny-fix/check    would this change be a tiny fix?
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// DefectStoryRef maps one story of an open batch to the defect it fixes.
type DefectStoryRef struct {
	StoryID  string `json:"story_id"`
	DefectID string `json:"defect_id"`
	Title    string `json:"title,omitempty"`
}

// DefectBatchView is one open batch with its story-to-defect mapping.
type DefectBatchView struct {
	ID      string           `json:"id"`
	Project string           `json:"project"`
	Area    string           `json:"area,omitempty"`
	Status  string           `json:"status"`
	Size    int              `json:"size"`
	Stories []DefectStoryRef `json:"stories"`
}

// ErrDefectNotFound is returned by a backend when a batch or story id is unknown.
var ErrDefectNotFound = errors.New("not found")

// DefectQueueBackend is the storage the defect queue endpoints work against.
// It embeds the engine's intake store; the extra methods serve the batch list
// and the split-out. Wired by SetDefectQueue; nil means the queue is not
// available and the endpoints answer 503.
type DefectQueueBackend interface {
	autonomous.DefectIntakeStore
	// OpenBatchViews lists open batches (all projects when project is empty).
	OpenBatchViews(project string) ([]DefectBatchView, error)
	// SplitStory moves storyID out of batch batchID into a new Automaton and
	// returns the new Automaton id. Unknown ids wrap ErrDefectNotFound.
	SplitStory(batchID, storyID string) (string, error)
}

// SetDefectQueue wires the defect queue backend and its decision log.
func (s *Server) SetDefectQueue(b DefectQueueBackend, log autonomous.IntakeLog) {
	s.defectQueue = b
	s.defectLog = log
}

type defectIntakeRequest struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
	Kind        string   `json:"kind"`
	Area        string   `json:"area"`
	Priority    int      `json:"priority"`
	Project     string   `json:"project"`
}

// handleDefectIntake serves POST /api/autonomous/defects/intake.
func (s *Server) handleDefectIntake(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req defectIntakeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Project) == "" {
		http.Error(w, "project is required", http.StatusBadRequest)
		return
	}
	if s.defectQueue == nil || s.cfg == nil {
		http.Error(w, "defect queue unavailable", http.StatusServiceUnavailable)
		return
	}
	q := s.cfg.Autonomous.DefectQueue
	if !q.EffectiveForProject(req.Project).Enabled {
		http.Error(w, "defect queue is disabled for project "+req.Project, http.StatusConflict)
		return
	}
	dec, err := autonomous.Intake(autonomous.IntakeDefect{
		ID: req.ID, Title: req.Title, Description: req.Description, Files: req.Files,
		Kind: req.Kind, Area: req.Area, Priority: req.Priority, Project: req.Project,
	}, autonomous.IntakeState{Store: s.defectQueue, Log: s.defectLog},
		autonomous.IntakeConfig{Queue: q})
	if err != nil {
		http.Error(w, "defect intake failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, dec)
}

// handleDefectBatches serves GET /api/autonomous/defects/batches[?project=].
func (s *Server) handleDefectBatches(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.defectQueue == nil {
		http.Error(w, "defect queue unavailable", http.StatusServiceUnavailable)
		return
	}
	batches, err := s.defectQueue.OpenBatchViews(r.URL.Query().Get("project"))
	if err != nil {
		http.Error(w, "list batches: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if batches == nil {
		batches = []DefectBatchView{}
	}
	for i := range batches {
		if batches[i].Stories == nil {
			batches[i].Stories = []DefectStoryRef{}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": batches, "total": len(batches)})
}

// handleDefectBatchSplit serves POST /api/autonomous/defects/batches/{id}/split.
func (s *Server) handleDefectBatchSplit(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/autonomous/defects/batches/")
	batchID, ok := strings.CutSuffix(rest, "/split")
	if !ok || batchID == "" || strings.Contains(batchID, "/") {
		http.Error(w, "path must be /api/autonomous/defects/batches/{id}/split", http.StatusBadRequest)
		return
	}
	var req struct {
		StoryID string `json:"story_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.StoryID) == "" {
		http.Error(w, "story_id is required", http.StatusBadRequest)
		return
	}
	if s.defectQueue == nil {
		http.Error(w, "defect queue unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := s.defectQueue.SplitStory(batchID, req.StoryID)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, ErrDefectNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, "split failed: "+err.Error(), code)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"automaton_id": id, "batch_id": batchID, "story_id": req.StoryID})
}

// handleDefectTinyFixCheck serves POST /api/autonomous/defects/tiny-fix/check.
// Read-only: it classifies a change and runs the operator-visible re-check
// without creating anything, so it needs only the read capability.
func (s *Server) handleDefectTinyFixCheck(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Files   []string `json:"files"`
		Kind    string   `json:"kind"`
		Project string   `json:"project"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Files) == 0 {
		http.Error(w, "files is required (at least one path)", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Kind) == "" {
		http.Error(w, "kind is required", http.StatusBadRequest)
		return
	}
	if s.cfg == nil {
		http.Error(w, "config unavailable", http.StatusServiceUnavailable)
		return
	}
	q := s.cfg.Autonomous.DefectQueue
	dec := autonomous.ClassifyTinyFix(autonomous.TinyFixChange{Files: req.Files, Kind: req.Kind}, q, req.Project)
	out := map[string]any{"eligible": dec.Eligible, "reason": dec.Reason}
	if dec.Eligible {
		if ref := autonomous.CheckOperatorVisible(req.Files, q, req.Project); ref.Refused {
			out["eligible"] = false
			out["reason"] = ref.Reason
			out["refused_paths"] = ref.Paths
		}
	}
	writeJSON(w, http.StatusOK, out)
}
