// The operator surface of the task sub-branch reaper: a read-only report
// (which task sub-branches would be reaped or held, and which backup refs
// exist), an explicit run, and a restore of one reaped branch from its
// newest backup ref. The merge read gate style applies to all three: the
// base repo must be configured (503 when it is not), the write paths ask
// for the autonomous write cap.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// reapReader is the read side of the reaper the surface needs.
type reapReader interface {
	ReapPreview(ctx context.Context) (autonomous.ReapReport, []autonomous.ReapBackup, error)
}

// reapRunner is the write side of the reaper the surface needs.
type reapRunner interface {
	reapReader
	ReapRun(ctx context.Context) (autonomous.ReapReport, error)
	ReapRestore(ctx context.Context, branch string) (string, string, error)
}

// reapBranchReader gates the two reap routes the same way mergeReadGate
// gates the merge reads: the method, the installed manager, the federation
// cap and the interface the handler needs. It writes its refusal itself and
// returns the manager only when the handler may proceed.
func (s *Server) reapReadGate(w http.ResponseWriter, r *http.Request, method string, path string) (reapReader, bool) {
	if r.Method != method {
		w.Header().Set("Allow", method)
		http.Error(w, "method not allowed; "+path+" takes "+method, http.StatusMethodNotAllowed)
		return nil, false
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous manager not installed", http.StatusServiceUnavailable)
		return nil, false
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return nil, false
	}
	m, ok := s.autonomousMgr.(reapReader)
	if !ok {
		http.Error(w, "the autonomous manager does not expose the branch reaper", http.StatusServiceUnavailable)
		return nil, false
	}
	return m, true
}

// handleAutonomousBranchReap answers
// GET /api/autonomous/branch-reap: the read-only preview — the task
// sub-branches that would be reaped (with the backup ref an earlier pass
// left, when one exists) and those that would be held, plus every backup
// ref under refs/datawatch/reap-backup/ and the retention in days. It
// writes nothing and deletes no branch.
func (s *Server) handleAutonomousBranchReap(w http.ResponseWriter, r *http.Request) {
	m, ok := s.reapReadGate(w, r, http.MethodGet, "/api/autonomous/branch-reap")
	if !ok {
		return
	}
	rep, backups, err := m.ReapPreview(r.Context())
	if err != nil {
		if errors.Is(err, autonomous.ErrMergeNoBaseRepo) {
			http.Error(w, "the base repo is not configured; the branch reaper has nothing to work on", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	days := 0
	if s.cfg != nil {
		days = s.cfg.Autonomous.EffectiveReapedBranchBackupDays()
	}
	writeJSONOK(w, autonomous.ReapSurfaceReport{
		ReapReport: rep,
		Backups:    backups,
		BackupDays: days,
		ComputedAt: time.Now(),
	})
}

// handleAutonomousBranchReapRun answers
// POST /api/autonomous/branch-reap/run: perform one reap pass. An explicit
// operator action: the branches listed in the preview are backed up first
// and then deleted. Requires the autonomous write cap (federation peers
// must ask for a grant). When a pass is already running or the base repo is
// not configured it answers 409/503.
func (s *Server) handleAutonomousBranchReapRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed; /api/autonomous/branch-reap/run takes POST", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous manager not installed", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "could not read the body", http.StatusBadRequest)
		return
	}
	if len(body) > 0 {
		http.Error(w, "run takes no body; the preview (GET) already computed it", http.StatusBadRequest)
		return
	}
	m, ok := s.autonomousMgr.(reapRunner)
	if !ok {
		http.Error(w, "the autonomous manager does not expose the branch reaper", http.StatusServiceUnavailable)
		return
	}
	rep, err := m.ReapRun(r.Context())
	if err != nil {
		switch {
		case errors.Is(err, autonomous.ErrReapBusy):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, autonomous.ErrMergeNoBaseRepo):
			http.Error(w, "the base repo is not configured; the branch reaper has nothing to work on", http.StatusServiceUnavailable)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	writeJSONOK(w, rep)
}

// reapRestoreRequest is the body of the restore call: the branch to bring
// back from its newest backup ref.
type reapRestoreRequest struct {
	Branch string `json:"branch"`
}

// handleAutonomousBranchReapRestore answers
// POST /api/autonomous/branch-reap/restore: recreate one reaped branch from
// its newest backup ref (refs/datawatch/reap-backup/<branch>/<unix-ts>).
// Requires the autonomous write cap. A branch that was never reaped (no
// backup ref) answers 404.
func (s *Server) handleAutonomousBranchReapRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed; /api/autonomous/branch-reap/restore takes POST", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous manager not installed", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "could not read the body", http.StatusBadRequest)
		return
	}
	if len(body) == 0 {
		http.Error(w, "give the branch as {\"branch\": \"...\"}", http.StatusBadRequest)
		return
	}
	var req reapRestoreRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "the body must be {\"branch\": \"...\"}", http.StatusBadRequest)
		return
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		http.Error(w, "give the branch to restore as {\"branch\": \"...\"}", http.StatusBadRequest)
		return
	}
	m, ok := s.autonomousMgr.(reapRunner)
	if !ok {
		http.Error(w, "the autonomous manager does not expose the branch reaper", http.StatusServiceUnavailable)
		return
	}
	ref, sha, err := m.ReapRestore(r.Context(), branch)
	if err != nil {
		switch {
		case errors.Is(err, autonomous.ErrBadRestoreBranch):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, autonomous.ErrNoReapBackup):
			http.Error(w, "no backup ref for that branch: "+err.Error(), http.StatusNotFound)
		case errors.Is(err, autonomous.ErrMergeNoBaseRepo):
			http.Error(w, "the base repo is not configured; the branch reaper has nothing to work on", http.StatusServiceUnavailable)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	writeJSONOK(w, autonomous.ReapRestoreResult{Branch: branch, BackupRef: ref, SHA: sha})
}
