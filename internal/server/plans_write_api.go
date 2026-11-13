// plans_write_api.go — the plans write API: create a plan, reorder plans and
// the Automata of a plan, attach and detach an Automaton, an Automation or a
// finding.
//
//	POST /api/plans                        create a plan (autonomous:write)
//	PATCH /api/plans/{ref}                 rename a plan and/or edit its goal
//	PUT  /api/plans/order                  set the plan priority order
//	PUT  /api/plans/{ref}/automata/order   set the order of a plan's Automata
//	POST /api/plans/{ref}/attach           attach an Automaton, Automation or finding
//	POST /api/plans/{ref}/detach           detach one of them
//
// Every route reuses the plan-record store methods; none of them rewrites a
// record itself. A reference is validated before the store is called: an
// unknown plan, Automaton, Automation or finding answers 404 and a malformed
// list answers 400. A link is never replaced: attaching something that is
// linked or attached elsewhere answers 409 and names the holder, and the
// caller detaches first.
//
// The Automata reorder answers with the plan's run view. The next marker in
// it skips blocked and held Automata, so the caller sees the effect of the new
// order on what runs next.
//
// autonomous.plan_records.enabled false answers every route with 404. A
// request only reads and writes record files; it never starts, signals or
// restarts a process.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

// planWriteGate runs the checks every write route shares: the method, plan
// records on, the write capability and the store present. It answers the
// failure and returns false.
func (s *Server) planWriteGate(w http.ResponseWriter, r *http.Request, method string) bool {
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return false
	}
	if !s.cfg.Autonomous.PlanRecords.EffectiveEnabled() {
		http.Error(w, "plan records are turned off (autonomous.plan_records.enabled)", http.StatusNotFound)
		return false
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		planRecordsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return false
	}
	if s.planRecords == nil {
		planRecordsError(w, http.StatusServiceUnavailable, "plan records are not available")
		return false
	}
	return true
}

// writePlanWriteError answers a failure of a link or order store call. A link
// conflict is 409 and names where the item is linked now.
func writePlanWriteError(w http.ResponseWriter, err error, what string) {
	var lc *planrecords.ErrLinkConflict
	if errors.As(err, &lc) {
		writeJSONError(w, http.StatusConflict, map[string]any{
			"error": fmt.Sprintf("%s %q is already linked to %s; detach it first", lc.Kind, lc.ID, lc.Holder),
			"kind":  lc.Kind, "id": lc.ID, "holder": lc.Holder,
		})
		return
	}
	writePlanRecordStoreError(w, err, what)
}

// planItemResolver says whether an id is a known Automaton or Automation and
// what its title and state are. A test replaces it with planItemSource.
func (s *Server) planItemResolver() func(string) (planrecords.MigrateRef, bool) {
	if s.planItemSource != nil {
		return s.planItemSource
	}
	return s.planMigrateResolver()
}

// checkPlanItem looks an Automaton or Automation up. It answers the failure
// itself and returns false: 404 for an unknown id, 503 when the Automata
// manager is not available to check it.
func (s *Server) checkPlanItem(w http.ResponseWriter, kind, id string) (planrecords.LinkInfo, bool) {
	resolve := s.planItemResolver()
	if resolve == nil {
		planRecordsError(w, http.StatusServiceUnavailable, "the Automata manager is not available, so the "+kind+" cannot be checked")
		return planrecords.LinkInfo{}, false
	}
	ref, ok := resolve(id)
	if !ok || ref.Kind != kind {
		planRecordsError(w, http.StatusNotFound, fmt.Sprintf("no %s matches %q", kind, id))
		return planrecords.LinkInfo{}, false
	}
	return planrecords.LinkInfo{Title: ref.Title, State: ref.State}, true
}

// planCreateBody is the create body.
type planCreateBody struct {
	Project string            `json:"project"`
	Title   string            `json:"title"`
	Goal    string            `json:"goal"`
	State   planrecords.State `json:"state"`
}

// handlePlanCreate serves POST /api/plans.
func (s *Server) handlePlanCreate(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPost) {
		return
	}
	var body planCreateBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		planRecordsError(w, http.StatusBadRequest, "title is required")
		return
	}
	if !s.checkPlanRecordProject(w, body.Project) {
		return
	}
	if body.State != "" && !planrecords.ValidState(planrecords.KindPlan, body.State) {
		planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("state %q is not valid for a plan; use one of %s",
			body.State, planStateList(planrecords.KindPlan)))
		return
	}
	rec, err := s.planRecords.Create(planrecords.Record{
		Kind: planrecords.KindPlan, Project: strings.TrimSpace(body.Project), Title: body.Title,
		Goal: strings.TrimSpace(body.Goal), State: body.State, Actor: s.auditActor(r.Context()),
	})
	if err != nil {
		writePlanWriteError(w, err, "create the plan")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"plan": newPlanRecordView(rec)})
}

// planEditBody is the rename and goal edit body. A field that is left out is
// left alone; goal may be an empty string, which clears the goal.
type planEditBody struct {
	Title *string `json:"title"`
	Goal  *string `json:"goal"`
}

// handlePlanEdit serves PATCH /api/plans/{ref}: rename a plan and/or set its
// goal. Both use the store methods, so the rules are the store's: a title is
// one line of at most 100 characters (400 otherwise) and must not be used by
// another record in the project (409 naming that record, like create). The
// request is checked as a whole before anything is written, so a bad goal
// never leaves a rename behind.
func (s *Server) handlePlanEdit(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPatch) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	var body planEditBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	if body.Title == nil && body.Goal == nil {
		planRecordsError(w, http.StatusBadRequest, "nothing to change: give title, goal or both")
		return
	}
	if body.Title != nil {
		if err := planrecords.ValidatePlanTitle(*body.Title); err != nil {
			writePlanWriteError(w, err, "rename the plan")
			return
		}
	}
	if body.Goal != nil && utf8.RuneCountInString(strings.TrimSpace(*body.Goal)) > planrecords.MaxGoalRunes {
		planRecordsError(w, http.StatusBadRequest, fmt.Sprintf("goal is longer than %d characters", planrecords.MaxGoalRunes))
		return
	}
	actor := s.auditActor(r.Context())
	var rec planrecords.Record
	var err error
	if body.Title != nil {
		if rec, err = s.planRecords.RenamePlan(ref, *body.Title, actor); err != nil {
			writePlanWriteError(w, err, "rename the plan")
			return
		}
		ref = rec.ID
	}
	if body.Goal != nil {
		if rec, err = s.planRecords.EditPlanGoal(ref, *body.Goal, actor); err != nil {
			writePlanWriteError(w, err, "edit the plan goal")
			return
		}
	}
	writeJSONOK(w, map[string]any{"plan": newPlanRecordView(rec), "goal": rec.Goal, "title_set_by_hand": rec.TitleSetByHand})
}

// planOrderBody is the plans reorder body.
type planOrderBody struct {
	Plans []string `json:"plans"`
}

// planOrderRow is one plan of a reorder answer.
type planOrderRow struct {
	Title string `json:"title"`
	ID    string `json:"id"`
	Order int    `json:"order"`
}

// handlePlansOrder serves PUT /api/plans/order.
func (s *Server) handlePlansOrder(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPut) {
		return
	}
	var body planOrderBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	if len(body.Plans) == 0 {
		planRecordsError(w, http.StatusBadRequest, "plans is required: list the plans in the order they should have")
		return
	}
	refs := make([]string, 0, len(body.Plans))
	for _, ref := range body.Plans {
		refs = append(refs, normalizePlanRef(ref))
	}
	if err := s.planRecords.ReorderPlans(refs); err != nil {
		writePlanWriteError(w, err, "reorder the plans")
		return
	}
	sums := s.planRecords.Plans("")
	rows := make([]planOrderRow, 0, len(sums))
	for _, sm := range sums {
		rows = append(rows, planOrderRow{Title: sm.Title, ID: sm.ID, Order: sm.Order})
	}
	writeJSONOK(w, map[string]any{"plans": rows})
}

// planAutomataOrderBody is the Automata reorder body.
type planAutomataOrderBody struct {
	Automata []string `json:"automata"`
}

// planRunView is the run view of a plan: its Automata in run order and the
// next marker, which skips blocked and held Automata.
func (s *Server) planRunView(ref string) (map[string]any, error) {
	d, page, total, err := s.planRecords.PlanAutomataPage(ref, planrecords.Page{Limit: planrecords.MaxLimit})
	if err != nil {
		return nil, err
	}
	rows := make([]planAutomatonRow, 0, len(page))
	for _, a := range page {
		rows = append(rows, planAutomatonRow{
			Title: a.Title, ID: a.ID, State: a.State, Run: a.Run, Position: a.Position,
			CompletedAt: a.CompletedAt,
			Skipped:     a.Run == planrecords.RunBlocked || a.Run == planrecords.RunHeld,
			IsNext:      d.Next != nil && d.Next.ID == a.ID,
		})
	}
	var next *planNextMarker
	if d.Next != nil {
		next = &planNextMarker{AutomatonID: d.Next.ID, Title: d.Next.Title}
	}
	return map[string]any{
		"plan":     map[string]any{"title": d.Summary.Title, "id": d.Summary.ID, "state": string(d.Summary.State)},
		"progress": newPlanProgress(d.Done, len(d.Automata)),
		"next":     next,
		"automata": rows,
		"page":     map[string]int{"total": total},
	}, nil
}

// handlePlanAutomataOrder serves PUT /api/plans/{ref}/automata/order.
func (s *Server) handlePlanAutomataOrder(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPut) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	var body planAutomataOrderBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	if len(body.Automata) == 0 {
		planRecordsError(w, http.StatusBadRequest, "automata is required: list every Automaton of the plan in the order they should have")
		return
	}
	rec, err := s.planRecords.ReorderAutomata(ref, body.Automata)
	if err != nil {
		writePlanWriteError(w, err, "reorder the Automata")
		return
	}
	view, err := s.planRunView(rec.ID)
	if err != nil {
		writePlanWriteError(w, err, "read the plan")
		return
	}
	writeJSONOK(w, view)
}

// planLinkBody is the attach and detach body. Kind is automaton, automation or
// finding; ID is the Automaton or Automation id, or the finding reference.
type planLinkBody struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (b planLinkBody) kindOK() bool {
	switch b.Kind {
	case "automaton", "automation", "finding":
		return true
	}
	return false
}

// handlePlanAttach serves POST /api/plans/{ref}/attach.
func (s *Server) handlePlanAttach(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPost) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	var body planLinkBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	body.Kind, body.ID = strings.ToLower(strings.TrimSpace(body.Kind)), strings.TrimSpace(body.ID)
	if !body.kindOK() {
		planRecordsError(w, http.StatusBadRequest, "kind must be automaton, automation or finding")
		return
	}
	if body.ID == "" {
		planRecordsError(w, http.StatusBadRequest, "id is required")
		return
	}
	plan, err := s.planRecords.Resolve(ref, "")
	if err == nil && plan.Kind != planrecords.KindPlan {
		err = planrecords.ErrNotFound
	}
	if err != nil {
		writePlanWriteError(w, err, "read the plan")
		return
	}
	var rec planrecords.Record
	switch body.Kind {
	case "automaton", "automation":
		info, ok := s.checkPlanItem(w, body.Kind, body.ID)
		if !ok {
			return
		}
		if body.Kind == "automaton" {
			rec, err = s.planRecords.AttachAutomaton(plan.ID, body.ID, info)
		} else {
			rec, err = s.planRecords.AttachAutomation(plan.ID, body.ID, info)
		}
	default:
		rec, err = s.planRecords.AttachFinding(normalizePlanRef(body.ID), planrecords.Target{Kind: planrecords.AttachPlan, Ref: plan.ID})
	}
	if err != nil {
		writePlanWriteError(w, err, "attach")
		return
	}
	if body.Kind == "finding" {
		writeJSONOK(w, map[string]any{"attached": body.Kind, "plan": plan.ID, "finding": newPlanRecordView(rec)})
		return
	}
	writeJSONOK(w, map[string]any{"attached": body.Kind, "id": body.ID, "plan": newPlanRecordView(rec)})
}

// handlePlanDetach serves POST /api/plans/{ref}/detach. The item must be
// linked to the plan named in the path; one linked to another plan is a 409
// that names the holder, so a detach never reaches into a different plan.
func (s *Server) handlePlanDetach(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPost) {
		return
	}
	ref, ok := s.planRefOf(w, r)
	if !ok {
		return
	}
	var body planLinkBody
	if !decodePlanRecordJSON(w, r, &body) {
		return
	}
	body.Kind, body.ID = strings.ToLower(strings.TrimSpace(body.Kind)), strings.TrimSpace(body.ID)
	if !body.kindOK() {
		planRecordsError(w, http.StatusBadRequest, "kind must be automaton, automation or finding")
		return
	}
	if body.ID == "" {
		planRecordsError(w, http.StatusBadRequest, "id is required")
		return
	}
	plan, err := s.planRecords.Resolve(ref, "")
	if err == nil && plan.Kind != planrecords.KindPlan {
		err = planrecords.ErrNotFound
	}
	if err != nil {
		writePlanWriteError(w, err, "read the plan")
		return
	}
	var rec planrecords.Record
	switch body.Kind {
	case "automaton", "automation":
		var holder planrecords.PlanRef
		var linked bool
		if body.Kind == "automaton" {
			holder, linked = s.planRecords.PlanOf(body.ID)
		} else {
			holder, linked = s.planRecords.PlanOfAutomation(body.ID)
		}
		if linked && holder.UID != plan.UID {
			writePlanWriteError(w, &planrecords.ErrLinkConflict{Kind: body.Kind, ID: body.ID, Holder: holder.ID}, "detach")
			return
		}
		if body.Kind == "automaton" {
			rec, err = s.planRecords.DetachAutomaton(body.ID)
		} else {
			rec, err = s.planRecords.DetachAutomation(body.ID)
		}
	default:
		fin, ferr := s.planRecords.Resolve(normalizePlanRef(body.ID), "")
		if ferr != nil {
			writePlanWriteError(w, ferr, "read the finding")
			return
		}
		if fin.AttachKind == planrecords.AttachPlan && fin.AttachRef != plan.ID {
			writePlanWriteError(w, &planrecords.ErrLinkConflict{Kind: "finding", ID: fin.ID, Holder: "plan:" + fin.AttachRef}, "detach")
			return
		}
		rec, err = s.planRecords.DetachFinding(fin.ID)
	}
	if err != nil {
		writePlanWriteError(w, err, "detach")
		return
	}
	if body.Kind == "finding" {
		writeJSONOK(w, map[string]any{"detached": body.Kind, "plan": plan.ID, "finding": newPlanRecordView(rec)})
		return
	}
	writeJSONOK(w, map[string]any{"detached": body.Kind, "id": body.ID, "plan": newPlanRecordView(rec)})
}
