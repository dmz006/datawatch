// Housekeeping action-list REST surface:
//
//   GET  /api/autonomous/unlanded-work                       — archived Automaton branches with work not on the default branch
//   POST /api/autonomous/unlanded-work/keep                  — leave a branch alone
//   POST /api/autonomous/unlanded-work/archive-branch        — move a branch under the archive refs
//   POST /api/autonomous/unlanded-work/delete-worktree       — back up, then delete a worktree and branch (confirm required)
//   GET  /api/autonomous/uncommitted-work/diff?worktree=<p>  — what a commit would record
//   POST /api/autonomous/uncommitted-work/commit             — commit a worktree's uncommitted files
//   POST /api/autonomous/cleanup/fix-permissions             — restore owner write permission on stuck folders
//
// An off or empty state (no base repository, nothing unlanded) reads as an
// empty list, never an error.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// unlandedWorkOps is the Manager surface these routes need; asserted so
// minimal test doubles keep compiling.
type unlandedWorkOps interface {
	UnlandedWork(ctx context.Context) ([]autonomous.UnlandedBranch, error)
	KeepUnlanded(ctx context.Context, branch, note string) error
	ArchiveUnlanded(ctx context.Context, branch string) (string, error)
	DeleteUnlandedWorktree(ctx context.Context, branch string, confirm bool) (autonomous.DeleteWorktreeResult, error)
	UncommittedWorkDiff(ctx context.Context, worktree string) (string, error)
	CommitUncommittedWork(ctx context.Context, worktree, message string) (string, error)
	FixPermissions(paths []string) (int, error)
}

// housekeepingGate checks the method, that Automata are enabled, the
// federation capability and the Manager surface. It returns nil when the
// request was answered.
func (s *Server) housekeepingGate(w http.ResponseWriter, r *http.Request, method, capName string) unlandedWorkOps {
	if r.Method != method {
		w.Header().Set("Allow", method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return nil
	}
	if !s.fedCap(w, r, capName) {
		return nil
	}
	ops, ok := s.autonomousMgr.(unlandedWorkOps)
	if !ok {
		http.Error(w, "Housekeeping actions unavailable", http.StatusServiceUnavailable)
		return nil
	}
	return ops
}

func decodeHousekeepingBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if len(body) == 0 {
		return true
	}
	if err := json.Unmarshal(body, v); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// housekeepingError maps an action error to a status.
func housekeepingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, autonomous.ErrUnlandedNotListed):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, autonomous.ErrNoBaseRepo),
		errors.Is(err, autonomous.ErrUncommittedEmptyMessage),
		errors.Is(err, autonomous.ErrUncommittedOutsideBase),
		errors.Is(err, autonomous.ErrUncommittedNothing):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusConflict)
	}
}

// handleUnlandedWork serves GET /api/autonomous/unlanded-work.
func (s *Server) handleUnlandedWork(w http.ResponseWriter, r *http.Request) {
	ops := s.housekeepingGate(w, r, http.MethodGet, federation.CapAutonomousRead)
	if ops == nil {
		return
	}
	list, err := ops.UnlandedWork(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []autonomous.UnlandedBranch{}
	}
	writeJSONOK(w, map[string]any{"branches": list})
}

// handleUnlandedAction serves the three POST actions on a listed branch.
func (s *Server) handleUnlandedAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := s.housekeepingGate(w, r, http.MethodPost, federation.CapAutonomousWrite)
		if ops == nil {
			return
		}
		var req struct {
			Branch  string `json:"branch"`
			Note    string `json:"note"`
			Confirm bool   `json:"confirm"`
		}
		if !decodeHousekeepingBody(w, r, &req) {
			return
		}
		if req.Branch == "" {
			http.Error(w, "branch is required", http.StatusBadRequest)
			return
		}
		switch action {
		case "keep":
			if err := ops.KeepUnlanded(r.Context(), req.Branch, req.Note); err != nil {
				housekeepingError(w, err)
				return
			}
			writeJSONOK(w, map[string]any{"branch": req.Branch, "kept": true})
		case "archive":
			ref, err := ops.ArchiveUnlanded(r.Context(), req.Branch)
			if err != nil {
				housekeepingError(w, err)
				return
			}
			writeJSONOK(w, map[string]any{"branch": req.Branch, "archive_ref": ref})
		default:
			if !req.Confirm {
				http.Error(w, "confirm is required to delete a worktree", http.StatusBadRequest)
				return
			}
			res, err := ops.DeleteUnlandedWorktree(r.Context(), req.Branch, true)
			if err != nil {
				housekeepingError(w, err)
				return
			}
			writeJSONOK(w, res)
		}
	}
}

// handleUncommittedDiff serves GET /api/autonomous/uncommitted-work/diff.
func (s *Server) handleUncommittedDiff(w http.ResponseWriter, r *http.Request) {
	ops := s.housekeepingGate(w, r, http.MethodGet, federation.CapAutonomousRead)
	if ops == nil {
		return
	}
	wt := r.URL.Query().Get("worktree")
	if wt == "" {
		http.Error(w, "worktree is required", http.StatusBadRequest)
		return
	}
	diff, err := ops.UncommittedWorkDiff(r.Context(), wt)
	if err != nil {
		housekeepingError(w, err)
		return
	}
	writeJSONOK(w, map[string]any{"worktree": wt, "diff": diff, "clean": diff == ""})
}

// handleUncommittedCommit serves POST /api/autonomous/uncommitted-work/commit.
func (s *Server) handleUncommittedCommit(w http.ResponseWriter, r *http.Request) {
	ops := s.housekeepingGate(w, r, http.MethodPost, federation.CapAutonomousWrite)
	if ops == nil {
		return
	}
	var req struct {
		Worktree string `json:"worktree"`
		Message  string `json:"message"`
	}
	if !decodeHousekeepingBody(w, r, &req) {
		return
	}
	if req.Worktree == "" {
		http.Error(w, "worktree is required", http.StatusBadRequest)
		return
	}
	sha, err := ops.CommitUncommittedWork(r.Context(), req.Worktree, req.Message)
	if err != nil {
		housekeepingError(w, err)
		return
	}
	writeJSONOK(w, map[string]any{"worktree": req.Worktree, "commit": sha})
}

// handleFixPermissions serves POST /api/autonomous/cleanup/fix-permissions.
func (s *Server) handleFixPermissions(w http.ResponseWriter, r *http.Request) {
	ops := s.housekeepingGate(w, r, http.MethodPost, federation.CapAutonomousWrite)
	if ops == nil {
		return
	}
	var req struct {
		Paths []string `json:"paths"`
	}
	if !decodeHousekeepingBody(w, r, &req) {
		return
	}
	n, err := ops.FixPermissions(req.Paths)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSONOK(w, map[string]any{"changed": n})
}
