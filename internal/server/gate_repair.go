// Gate repair surface: GET /api/autonomous/gate-repair and
// POST /api/autonomous/gate-repair/run.
//
// The GET shows the gate repair engine exactly as the existing plural read
// view does — the mode in effect (off, report or on), the bounded records (for
// each failed gate the failure class found, what it would do or did, how to
// undo it, and the before/after script texts of a file edit) and the spent
// repair budget keys (one "automaton/task/<class>" key per class already spent)
// — returned as the native GateRepairStatus, so both endpoints agree on shape.
// Read-only: reading it runs no repair, raises no alert and cannot change the
// mode; the mode changes only through the existing config set paths
// (autonomous.gate_repair.mode). A federated peer needs the autonomous read
// capability (federation.CapAutonomousRead).
//
// The POST runs the engine now: with both automaton_id and task_id it resolves
// exactly that gate; with neither it runs one pass across every failed task
// gate on the daemon; with exactly one it is refused. It returns the run
// outcome(s) plus a fresh GateRepairStatus so a caller can see what the pass
// recorded. A federated peer needs the autonomous run capability
// (federation.CapAutonomousRun).
//
// These are the singular "gate repair" surfaces; they coexist with the existing
// plural gate-repairs read (internal/server/gate_repairs.go).
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// gateRepairRunInvoker is the narrow run surface the POST route uses. The
// Manager implements it; tests provide a spy.
type gateRepairRunInvoker interface {
	RunGateRepair(ctx context.Context, automatonID, taskID string) autonomous.GateRepairOutcome
	RunGateRepairPass(ctx context.Context) []autonomous.GateRepairOutcome
}

// gateRepairOutcomeView is one gate repair outcome as the run route returns it
// (before/after carry the script texts of a file edit, when made). The
// autonomous.GateRepairOutcome type carries no json tags, so this view fixes
// the field names to snake_case.
type gateRepairOutcomeView struct {
	Mode    string `json:"mode"`
	Class   string `json:"class"`
	Would   string `json:"would,omitempty"`
	Did     string `json:"did,omitempty"`
	Done    string `json:"done,omitempty"`
	Before  string `json:"before,omitempty"`
	After   string `json:"after,omitempty"`
	Held    string `json:"held,omitempty"`
	Err     string `json:"err,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Subject string `json:"subject,omitempty"`
	Found   string `json:"found,omitempty"`
}

// gateRepairRunResponse is the POST body: the outcome(s) plus a fresh native
// status read (GateRepairStatus: mode, records and budget).
type gateRepairRunResponse struct {
	Runs   []gateRepairOutcomeView     `json:"runs"`
	Status autonomous.GateRepairStatus `json:"status"`
}

func gateRepairOutcomeViewFrom(o autonomous.GateRepairOutcome) gateRepairOutcomeView {
	return gateRepairOutcomeView{
		Mode: o.Mode, Class: string(o.Class), Would: o.Would, Did: o.Did,
		Done: o.Done, Before: o.Before, After: o.After, Held: o.Held,
		Err: o.Err, Kind: o.Kind, Subject: o.Subject, Found: o.Found,
	}
}

// handleGateRepair serves GET /api/autonomous/gate-repair: the native
// GateRepairStatus, identical in shape to the plural GET /api/autonomous/gate-repairs.
func (s *Server) handleGateRepair(w http.ResponseWriter, r *http.Request) {
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
	gr, ok := s.autonomousMgr.(gateRepairsReader)
	if !ok {
		http.Error(w, "gate repair unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSONOK(w, gr.GateRepairerStatus())
}

// handleGateRepairRun serves POST /api/autonomous/gate-repair/run: one failed
// gate (both automaton_id and task_id) or one pass (neither); exactly one is
// refused. It returns the outcome(s) and a fresh status.
func (s *Server) handleGateRepairRun(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRun) {
		return
	}
	runner, ok := s.autonomousMgr.(gateRepairRunInvoker)
	if !ok {
		http.Error(w, "gate repair unavailable", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		AutomatonID string `json:"automaton_id"`
		TaskID      string `json:"task_id"`
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request: unreadable body", http.StatusBadRequest)
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	body.AutomatonID = strings.TrimSpace(body.AutomatonID)
	body.TaskID = strings.TrimSpace(body.TaskID)

	ctx := r.Context()
	var outcomes []autonomous.GateRepairOutcome
	switch {
	case body.AutomatonID != "" && body.TaskID != "":
		outcomes = []autonomous.GateRepairOutcome{runner.RunGateRepair(ctx, body.AutomatonID, body.TaskID)}
	case body.AutomatonID == "" && body.TaskID == "":
		outcomes = runner.RunGateRepairPass(ctx)
	default:
		http.Error(w, "bad request: pass both automaton_id and task_id to run one gate, or neither to run a pass", http.StatusBadRequest)
		return
	}
	if outcomes == nil {
		outcomes = []autonomous.GateRepairOutcome{}
	}
	runs := make([]gateRepairOutcomeView, 0, len(outcomes))
	for _, o := range outcomes {
		runs = append(runs, gateRepairOutcomeViewFrom(o))
	}
	resp := gateRepairRunResponse{Runs: runs}
	if st, ok := s.autonomousMgr.(gateRepairsReader); ok {
		resp.Status = st.GateRepairerStatus()
	}
	writeJSONOK(w, resp)
}
