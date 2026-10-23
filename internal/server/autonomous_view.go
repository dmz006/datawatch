package server

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// writeAutonomousResult writes the reply for a write to prd. The autonomous
// API hands back any-typed values, and test doubles may stand in with any
// JSON-serializable shape, so prd is taken as any. The view query parameter
// picks the shape: empty or "full" returns prd as it is, and "compact" returns
// the small WriteView summary. Any other value is rejected.
func (s *Server) writeAutonomousResult(w http.ResponseWriter, r *http.Request, prd any, taskID, storyID, message string) {
	// A nil reply is written as it is, as before the compact view existed,
	// so a write whose PRD lookup came back empty keeps its 200 response.
	if isNilPRD(prd) {
		writeJSONOK(w, prd)
		return
	}
	switch view := r.URL.Query().Get("view"); view {
	case "", "full":
		writeJSONOK(w, prd)
	case "compact":
		p, err := toPRD(prd)
		if err != nil {
			http.Error(w, "compact view: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, autonomous.BuildWriteView(p, taskID, storyID, message))
	default:
		http.Error(w, "invalid view: accepted values are full, compact", http.StatusBadRequest)
	}
}

// writeAutonomousCancelResult writes the reply for the DELETE cancel route.
// view=compact returns the write view for prd and carries killed, the number
// of sessions the cancel stopped, in killed_sessions. Any other view keeps
// the full reply that the caller built in full, so its shape does not change.
func (s *Server) writeAutonomousCancelResult(w http.ResponseWriter, r *http.Request, prd any, killed int, full any) {
	if r.URL.Query().Get("view") != "compact" || isNilPRD(prd) {
		writeJSONOK(w, full)
		return
	}
	p, err := toPRD(prd)
	if err != nil {
		http.Error(w, "compact view: "+err.Error(), http.StatusInternalServerError)
		return
	}
	view := autonomous.BuildWriteView(p, "", "", "")
	view.KilledSessions = killed
	writeJSONOK(w, view)
}

// parsePRDDetail reads the detail query parameter of the PRD GET route. An
// absent value means "full". It returns the level and true for full, summary
// and tasks, and false for any other value.
func parsePRDDetail(r *http.Request) (string, bool) {
	switch detail := r.URL.Query().Get("detail"); detail {
	case "":
		return "full", true
	case "full", "summary", "tasks":
		return detail, true
	default:
		return "", false
	}
}

// prdDetailCap returns the per-field byte cap for the default PRD detail
// reply: 0 (no cap) when the request carries full=1, otherwise the
// autonomous.detail_text_cap_bytes setting.
func (s *Server) prdDetailCap(r *http.Request) int {
	if queryBool(r, "full", false) {
		return 0
	}
	configured := 0
	if s.cfg != nil {
		configured = s.cfg.Autonomous.DetailTextCapBytes
	}
	return autonomous.ResolveDetailTextCap(configured)
}

// writePRDDetail writes prd at the given detail level. full writes prd with
// its long text fields cut to textCap bytes each (textCap 0 writes it as it
// is), summary writes its summary view, and tasks writes its task listing.
// The stored record is never changed: the cap works on a copy.
func writePRDDetail(w http.ResponseWriter, prd any, detail string, textCap int) {
	switch detail {
	case "summary", "tasks":
		p, err := toPRD(prd)
		if err != nil {
			http.Error(w, "detail view: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if detail == "summary" {
			writeJSONOK(w, autonomous.BuildSummaryView(p))
			return
		}
		writeJSONOK(w, autonomous.BuildTasksView(autonomous.CapPRDText(p, textCap)))
	default:
		if textCap <= 0 {
			writeJSONOK(w, prd)
			return
		}
		p, err := toPRD(prd)
		if err != nil {
			http.Error(w, "detail view: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, autonomous.CapPRDText(p, textCap))
	}
}

// planCoverageAckner is the optional autonomous-manager surface for
// acknowledging a failing plan-requirement coverage report. It is an
// optional interface (like the ReportPairingWarnings reader in
// handleAutonomousConfig) so AutonomousAPI fakes in server tests don't
// need to implement it — handlers that hit a fake get a clean
// 501 "not supported" instead of a compile error.
type planCoverageAckner interface {
	AcknowledgePlanCoverage(prdID, actor, note string) (*autonomous.PRD, error)
}

// isNilPRD reports whether v is nil, or a nil *autonomous.PRD.
func isNilPRD(v any) bool {
	if v == nil {
		return true
	}
	p, ok := v.(*autonomous.PRD)
	return ok && p == nil
}

// toPRD returns v as a PRD for the compact view. A *autonomous.PRD is used as
// it is. Any other value takes a JSON round trip, so a test double's shape
// still gets a summary.
func toPRD(v any) (*autonomous.PRD, error) {
	if p, ok := v.(*autonomous.PRD); ok {
		return p, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var p autonomous.PRD
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
