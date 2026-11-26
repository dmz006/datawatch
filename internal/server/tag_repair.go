package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// maxTagRepairBody bounds the saved copies a repair request carries.
const maxTagRepairBody = 4 << 20

// tagRepairAPI is the repair surface the handler asserts the autonomous
// manager against.
type tagRepairAPI interface {
	RepairTags(copies []*autonomous.Tag, dryRun bool) (autonomous.TagRepairReport, error)
}

// handleTagRepair makes good the tag IDs that have no record.
//
//	POST /api/autonomous/tags/repair  body: {dry_run, copies}
//
// It is a dry run unless dry_run is false, in the body or as ?dry_run=false; a
// dry_run in the body wins over the query. copies are tag records from saved
// copies. The reply is the autonomous.TagRepairReport.
func (s *Server) handleTagRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	repair, ok := s.autonomousMgr.(tagRepairAPI)
	if !ok {
		http.Error(w, "tag repair not supported", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	var req struct {
		DryRun *bool             `json:"dry_run"`
		Copies []*autonomous.Tag `json:"copies"`
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTagRepairBody)).Decode(&req)
	if err != nil && !errors.Is(err, io.EOF) {
		// An unread dry_run must not turn into a write.
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	dryRun := queryBool(r, "dry_run", true)
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	report, err := repair.RepairTags(req.Copies, dryRun)
	if err != nil {
		http.Error(w, fmt.Sprintf("tag repair stopped part way: %v; run it again after the cause is fixed", err), http.StatusInternalServerError)
		return
	}
	mode := "run"
	if dryRun {
		mode = "dry-run"
	}
	s.audit(r.Context(), "repair", "autonomous_tags", mode, map[string]any{
		"missing": report.Missing, "restore": report.Restore, "merge": report.Merge, "unresolved": report.Unresolved,
	})
	writeJSONOK(w, report)
}
