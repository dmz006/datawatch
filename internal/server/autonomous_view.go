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

// writeAutonomousResultOverride is the LLM-override-write variant of
// writeAutonomousResult: when override is non-nil (a call accepted in queued
// mode, override_queued_tasks enabled at a locked Automaton status), the
// reply carries it in override — in the full view as a top-level sibling of
// the PRD's own fields, in the compact view as the override key of the write
// view — while a nil override keeps the exact bare-PRD / write-view replies
// every other write uses.
func (s *Server) writeAutonomousResultOverride(w http.ResponseWriter, r *http.Request, prd any, override any, taskID, storyID, message string) {
	if override == nil {
		s.writeAutonomousResult(w, r, prd, taskID, storyID, message)
		return
	}
	switch view := r.URL.Query().Get("view"); view {
	case "", "full":
		p, err := toPRD(prd)
		if err != nil {
			http.Error(w, "full view: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// The PRD's own top-level fields come through the embedded struct; the
		// queued-mode override rides along as a top-level sibling, not a wrapper.
		writeJSONOK(w, struct {
			*autonomous.PRD
			Override any `json:"override"`
		}{PRD: p, Override: override})
	case "compact":
		p, err := toPRD(prd)
		if err != nil {
			http.Error(w, "compact view: "+err.Error(), http.StatusInternalServerError)
			return
		}
		v := autonomous.BuildWriteView(p, taskID, storyID, message)
		v.Override = override
		writeJSONOK(w, v)
	default:
		http.Error(w, "invalid view: accepted values are full, compact", http.StatusBadRequest)
	}
}

// writeAutonomousPlanningResult writes the PRD-level set_llm reply for a call
// that carried a planning field (decomposition_profile / decomposition_model):
// the structured planning-requeue outcome rides along as a top-level sibling
// of the PRD's own fields in the full view and as the planning key of the
// write view in compact mode. planning is always non-nil here (the handler
// routes only when a requeue result exists), so every other write keeps its
// exact legacy bare-PRD / write-view reply.
func (s *Server) writeAutonomousPlanningResult(w http.ResponseWriter, r *http.Request, prd any, planning any, taskID, storyID, message string) {
	switch view := r.URL.Query().Get("view"); view {
	case "", "full":
		if isNilPRD(prd) {
			writeJSONOK(w, struct {
				Planning any `json:"planning"`
			}{Planning: planning})
			return
		}
		p, err := toPRD(prd)
		if err != nil {
			http.Error(w, "full view: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, struct {
			*autonomous.PRD
			Planning any `json:"planning"`
		}{PRD: p, Planning: planning})
	case "compact":
		p, err := toPRD(prd)
		if err != nil {
			http.Error(w, "compact view: "+err.Error(), http.StatusInternalServerError)
			return
		}
		v := autonomous.BuildWriteView(p, taskID, storyID, message)
		v.Planning = planning
		writeJSONOK(w, v)
	default:
		http.Error(w, "invalid view: accepted values are full, compact", http.StatusBadRequest)
	}
}

// writeAutonomousCancelResult writes the reply for the DELETE cancel route.
// view=compact returns the write view for prd and carries killed, the number
// of sessions the cancel attempted to end, in killed_sessions, and killedLive,
// how many of those were still live when the cancel began, in killed_live_sessions. Any other view keeps
// the full reply that the caller built in full, so its shape does not change.
func (s *Server) writeAutonomousCancelResult(w http.ResponseWriter, r *http.Request, prd any, killed, killedLive int, full any) {
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
	view.KilledLiveSessions = killedLive
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

// inheritedTagReader is the optional AutonomousAPI extension that reads the
// inherited_tags field of the summary views: the union of the system tags
// (placement tags) carried by the Automaton's sessions. autonomous.API
// implements it; unwired managers (tests) leave the field absent.
type inheritedTagReader interface {
	InheritedTagsForPRD(prdID string) []string
}

// inheritedTagSnapshotter is the optional AutonomousAPI extension that gives a
// per-call lookup of the inherited tags, built from one snapshot of the
// sessions. List handlers use it so they read the sessions once per call.
type inheritedTagSnapshotter interface {
	InheritedTagsSnapshot() func(prdID string) []string
}

// inheritedTagFiller returns a function that fills a summary view's
// inherited_tags. It takes the snapshot once, when called; the returned
// function is cheap per Automaton. It falls back to the per-Automaton reader
// when the manager has no snapshot, and does nothing when it has no reader.
func (s *Server) inheritedTagFiller() func(view *autonomous.SummaryView) {
	if sn, ok := s.autonomousMgr.(inheritedTagSnapshotter); ok {
		lookup := sn.InheritedTagsSnapshot()
		return func(view *autonomous.SummaryView) { view.InheritedTags = lookup(view.ID) }
	}
	return s.fillInheritedTags
}

// fillInheritedTags sets the view's read-only inherited_tags field — the
// union of the system tags carried by the Automaton's sessions — when the
// manager provides the reader. The stored PRD record is never changed.
func (s *Server) fillInheritedTags(view *autonomous.SummaryView) {
	if ir, ok := s.autonomousMgr.(inheritedTagReader); ok {
		view.InheritedTags = ir.InheritedTagsForPRD(view.ID)
	}
}

// withInheritedTags adds the read-only inherited_tags field to the
// full-detail record response: the union of the system tags (placement
// tags) carried by the Automaton's sessions. The stored PRD record itself
// never carries the field; it is added to the response copy only, and
// omitted when the manager has no reader or no inherited tags.
func (s *Server) withInheritedTags(rec any, prdID string) any {
	ir, ok := s.autonomousMgr.(inheritedTagReader)
	if !ok {
		return rec
	}
	tags := ir.InheritedTagsForPRD(prdID)
	if len(tags) == 0 {
		return rec
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return rec
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return rec
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return rec
	}
	m["inherited_tags"] = b
	return m
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
// is), summary writes its summary view, and tasks writes its task listing
// narrowed to tagSel (a task is kept when its own system tags satisfy the
// selection; an empty selection keeps every task). The stored record is never
// changed: the cap works on a copy. The tasks listing and the full record
// carry model_request on every task that has a request-watch observation.
func (s *Server) writePRDDetail(w http.ResponseWriter, prd any, detail string, textCap int, tagSel autonomous.ObjectTagSelection) {
	id := prdIDOf(prd)
	switch detail {
	case "summary", "tasks":
		p, err := toPRD(prd)
		if err != nil {
			http.Error(w, "detail view: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if detail == "summary" {
			view := autonomous.BuildSummaryView(p)
			view.Project = autonomous.ProjectAliasOfPRD(p, s.projectAliasMap())
			view.PlanningFailure = s.planningFailureOf(p)
			s.fillInheritedTags(&view)
			writeJSONOK(w, s.withProjectLink(s.withAutomatonCost(view, id), id))
			return
		}
		view := autonomous.BuildTasksView(autonomous.CapPRDText(p, textCap))
		view = autonomous.FilterTasksView(view, tagSel)
		view.Project = autonomous.ProjectAliasOfPRD(p, s.projectAliasMap())
		view.PlanningFailure = s.planningFailureOf(p)
		s.fillInheritedTags(&view.SummaryView)
		s.attachTaskObservations(&view)
		writeJSONOK(w, s.withAutomatonCost(view, id))
	default:
		if textCap <= 0 {
			writeJSONOK(w, s.withProjectLink(s.withBranchRefresh(s.withAutomatonCost(s.withInheritedTags(s.withPlanningFailure(s.withPRDProject(s.prdWithObservations(prd, id), prd), prd), id), id)), id))
			return
		}
		p, err := toPRD(prd)
		if err != nil {
			http.Error(w, "detail view: "+err.Error(), http.StatusInternalServerError)
			return
		}
		capped := autonomous.CapPRDText(p, textCap)
		writeJSONOK(w, s.withProjectLink(s.withBranchRefresh(s.withAutomatonCost(s.withInheritedTags(s.withPlanningFailure(s.withPRDProject(s.prdWithObservations(capped, capped.ID), capped), capped), capped.ID), capped.ID)), capped.ID))
	}
}

// planningFailureViewer is the optional autonomous-manager surface that
// reads the planning failure view of an Automaton. AutonomousAPI fakes that
// lack it simply show no planning failure.
type planningFailureViewer interface {
	PlanningFailureView(prd *autonomous.PRD) *autonomous.PlanningFailure
}

// planningFailureOf returns the planning failure of prd, or nil.
func (s *Server) planningFailureOf(prd *autonomous.PRD) *autonomous.PlanningFailure {
	if prd == nil || s.autonomousMgr == nil {
		return nil
	}
	v, ok := s.autonomousMgr.(planningFailureViewer)
	if !ok {
		return nil
	}
	return v.PlanningFailureView(prd)
}

// withPlanningFailure adds the planning_failure object to a full Automaton
// body. The key is omitted when the Automaton has no planning failure.
func (s *Server) withPlanningFailure(payload any, prd any) any {
	p, err := toPRD(prd)
	if err != nil || p == nil {
		return payload
	}
	pf := s.planningFailureOf(p)
	if pf == nil {
		return payload
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return payload
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil || doc == nil {
		return payload
	}
	doc["planning_failure"] = pf
	return doc
}

// prdIDOf returns the id of an Automaton record, or "" when it has none.
func prdIDOf(prd any) string {
	if p, ok := prd.(*autonomous.PRD); ok && p != nil {
		return p.ID
	}
	p, err := toPRD(prd)
	if err != nil || p == nil {
		return ""
	}
	return p.ID
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
