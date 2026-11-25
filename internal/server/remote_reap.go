// The operator surface of the remote branch reaper: a read-only report
// (which Automaton work branches on origin would be reaped or held, and
// which backup refs exist under the remote reap-backup namespace), an
// explicit run, and a restore of one reaped branch from its newest backup
// ref. The merge read gate style applies to all three: the base repo must
// be configured (503 when it is not), the write paths ask for the
// autonomous write cap. A run deletes nothing while
// autonomous.remote_reap_enabled is false or unset (the default): the same
// evaluation runs and the report says report_only.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// remoteReapReader is the read side of the remote reaper the surface needs.
type remoteReapReader interface {
	RemoteReapPreview(ctx context.Context) (autonomous.RemoteReapSurfaceReport, []autonomous.ReapBackup, error)
}

// remoteReapRunner is the write side of the remote reaper the surface needs.
type remoteReapRunner interface {
	remoteReapReader
	RemoteReapRun(ctx context.Context) (autonomous.RemoteReapReport, error)
	RemoteReapRestore(ctx context.Context, branch string) (autonomous.RemoteReapRestoreResult, error)
}

// remoteReapReadGate gates the remote reap routes the same way reapReadGate
// gates the branch reap reads: the method, the installed manager, the
// federation cap and the interface the handler needs. It writes its refusal
// itself and returns the manager only when the handler may proceed.
func (s *Server) remoteReapReadGate(w http.ResponseWriter, r *http.Request, method string, path string) (remoteReapReader, bool) {
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
	m, ok := s.autonomousMgr.(remoteReapReader)
	if !ok {
		http.Error(w, "the autonomous manager does not expose the remote branch reaper", http.StatusServiceUnavailable)
		return nil, false
	}
	return m, true
}

// handleAutonomousRemoteReap answers
// GET /api/autonomous/remote-reap: the read-only preview — the Automaton
// work branches on origin that would be reaped (fully merged into the
// project's default branch and older than the retention window) and those
// that would be held (with the reason), plus every backup ref under
// refs/datawatch/remote-reap-backup/ and the retention in days. It writes
// nothing and deletes no remote branch.
func (s *Server) handleAutonomousRemoteReap(w http.ResponseWriter, r *http.Request) {
	m, ok := s.remoteReapReadGate(w, r, http.MethodGet, "/api/autonomous/remote-reap")
	if !ok {
		return
	}
	rep, _, err := m.RemoteReapPreview(r.Context())
	if err != nil {
		if errors.Is(err, autonomous.ErrMergeNoBaseRepo) {
			http.Error(w, "the base repo is not configured; the remote branch reaper has nothing to work on", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rep.Backups == nil {
		rep.Backups = []autonomous.ReapBackup{}
	}
	writeJSONOK(w, rep)
}

// handleAutonomousRemoteReapRun answers
// POST /api/autonomous/remote-reap/run: perform one remote reap pass. An
// explicit operator action: each fully merged branch whose tip is older
// than the retention window is backed up first and, only then, deleted
// from origin. While autonomous.remote_reap_enabled is false or unset (the
// default) the same evaluation runs but nothing is deleted and the report
// says report_only. Requires the autonomous write cap (federation peers
// must ask for a grant). When a pass is already running or the base repo is
// not configured it answers 409/503.
func (s *Server) handleAutonomousRemoteReapRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed; /api/autonomous/remote-reap/run takes POST", http.StatusMethodNotAllowed)
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
	m, ok := s.autonomousMgr.(remoteReapRunner)
	if !ok {
		http.Error(w, "the autonomous manager does not expose the remote branch reaper", http.StatusServiceUnavailable)
		return
	}
	rep, err := m.RemoteReapRun(r.Context())
	if err != nil {
		switch {
		case errors.Is(err, autonomous.ErrRemoteReapBusy):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, autonomous.ErrMergeNoBaseRepo):
			http.Error(w, "the base repo is not configured; the remote branch reaper has nothing to work on", http.StatusServiceUnavailable)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if rep.Reaped == nil {
		rep.Reaped = []autonomous.RemoteReapEntry{}
	}
	if rep.Held == nil {
		rep.Held = []autonomous.RemoteReapEntry{}
	}
	writeJSONOK(w, rep)
}

// remoteReapRestoreRequest is the body of the remote reap restore call: the
// branch to bring back from its newest backup ref.
type remoteReapRestoreRequest struct {
	Branch string `json:"branch"`
}

// handleAutonomousRemoteReapRestore answers
// POST /api/autonomous/remote-reap/restore: recreate one reaped branch from
// its newest backup ref
// (refs/datawatch/remote-reap-backup/<branch>/<unix-ts>) in the base repo.
// Requires the autonomous write cap. A branch that was never reaped (no
// backup ref) answers 404.
func (s *Server) handleAutonomousRemoteReapRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed; /api/autonomous/remote-reap/restore takes POST", http.StatusMethodNotAllowed)
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
	var req remoteReapRestoreRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "the body must be {\"branch\": \"...\"}", http.StatusBadRequest)
		return
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		http.Error(w, "give the branch to restore as {\"branch\": \"...\"}", http.StatusBadRequest)
		return
	}
	m, ok := s.autonomousMgr.(remoteReapRunner)
	if !ok {
		http.Error(w, "the autonomous manager does not expose the remote branch reaper", http.StatusServiceUnavailable)
		return
	}
	res, err := m.RemoteReapRestore(r.Context(), branch)
	if err != nil {
		switch {
		case errors.Is(err, autonomous.ErrBadRestoreBranch):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, autonomous.ErrNoRemoteReapBackup):
			http.Error(w, "no backup ref for that branch: "+err.Error(), http.StatusNotFound)
		case errors.Is(err, autonomous.ErrMergeNoBaseRepo):
			http.Error(w, "the base repo is not configured; the remote branch reaper has nothing to work on", http.StatusServiceUnavailable)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	writeJSONOK(w, res)
}
