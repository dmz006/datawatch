package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
)

// automationGraphImporter is the optional AutonomousAPI extension that converts
// an orchestrator graph (autonomous.API implements it).
type automationGraphImporter interface {
	ImportGraph(src autonomous.ImportGraphSource, dryRun bool) (*autonomous.ImportGraphResult, error)
}

// handleAutomationImportGraph serves POST /api/automations/import-graph/{graph_id}
// with body {"dry_run": bool}. The orchestrator graph is only read. It answers:
//
//	200 the new Automation, or for a dry run the AutomationInput, warnings and
//	    dry_run true (nothing stored, nothing audited)
//	404 unknown graph
//	409 the store refused the new Automation (member ownership conflict)
//	422 the graph cannot be expressed as an Automation, with the reason
//	503 the orchestrator or the automation manager is not available
//
// A real import writes one automation_import_graph audit entry.
func (s *Server) handleAutomationImportGraph(w http.ResponseWriter, r *http.Request, graphID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	imp, ok := s.autonomousMgr.(automationGraphImporter)
	if !ok || s.orchestratorAPI == nil {
		http.Error(w, "graph import is not available (orchestrator disabled)", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		DryRun bool `json:"dry_run"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	g, found := s.orchestratorAPI.GetGraph(graphID)
	if !found {
		http.Error(w, "graph not found", http.StatusNotFound)
		return
	}
	var src autonomous.ImportGraphSource
	raw, err := json.Marshal(g)
	if err == nil {
		err = json.Unmarshal(raw, &src)
	}
	if err != nil {
		http.Error(w, "graph unreadable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if src.ID == "" {
		src.ID = graphID
	}
	res, err := imp.ImportGraph(src, req.DryRun)
	if err != nil {
		if errors.Is(err, autonomous.ErrGraphNotRepresentable) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		code, msg := automationErrStatus(err)
		if strings.Contains(msg, "already belongs to automation") {
			code = http.StatusConflict
		}
		http.Error(w, msg, code)
		return
	}
	if req.DryRun {
		writeJSONOK(w, res)
		return
	}
	id, _ := automationIdentity(res.Automation)
	s.audit(r.Context(), audit.ActionAutomationImportGraph, "automation", id,
		map[string]any{"graph_id": graphID, "automation_id": id, "member_count": len(res.Automation.Members)})
	writeJSONOK(w, res.Automation)
}
