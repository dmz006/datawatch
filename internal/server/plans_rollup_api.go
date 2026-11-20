// plans_rollup_api.go — the plan status roll-up over REST.
//
//	GET  /api/plans/rollup?project=&dry_run=true   what the roll-up would change (autonomous:read)
//	POST /api/plans/rollup                         apply it (autonomous:write)
//
// The roll-up derives each plan's status from the states last reported for the
// Automata and Automations linked to it (planrecords.DerivePlanStatus). Both
// routes reuse the store's ReconcilePlans; neither works the status out itself.
//
// GET only ever previews: it never writes. Asking it for dry_run=false is
// refused with 400 so a read cannot change a plan by mistake; use POST to
// apply. POST applies by default; the body {"project": "...", "dry_run": true}
// (or ?dry_run=true) previews instead, so a caller can run the same request
// both ways. A plan whose status was set by hand is never changed: it is
// listed with set_by_hand_kept true and the status the roll-up would have
// written.
//
// The project is optional; empty rolls up every project. A project that is
// given must be a registered alias (400 otherwise).
//
// autonomous.plan_records.enabled false answers every route with 404. A
// request only reads and writes record files; it never starts, signals or
// restarts a process.

package server

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/planrecords"
)

const plansRollupPath = "/api/plans/rollup"

// planRollupBody is the POST body. Both fields are optional.
type planRollupBody struct {
	Project string `json:"project"`
	DryRun  *bool  `json:"dry_run"`
}

// planRollupView is the answer of both routes.
type planRollupView struct {
	Project string                     `json:"project,omitempty"`
	DryRun  bool                       `json:"dry_run"`
	Applied bool                       `json:"applied"`
	Changes []planrecords.RollupChange `json:"changes"`
	// Changed counts the plans written (or, on a preview, that would be);
	// Kept counts the plans whose hand-set status was left alone.
	Changed int `json:"changed"`
	Kept    int `json:"kept"`
}

// planRollupProject checks an optional project alias. It answers the failure
// and returns false.
func (s *Server) planRollupProject(w http.ResponseWriter, project string) bool {
	if project == "" {
		return true
	}
	if err := autonomous.ValidateProjectAlias(project, s.projectAliasMap()); err != nil {
		planRecordsError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

// parseDryRun reads a dry_run value. An empty value is def.
func parseDryRun(raw string, def bool) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("dry_run must be true or false")
	}
	return b, nil
}

func (s *Server) planRollupAnswer(w http.ResponseWriter, project string, dry bool) {
	changes, err := s.planRecords.ReconcilePlans(project, dry)
	if err != nil {
		writePlanRecordStoreError(w, err, "roll up the plans")
		return
	}
	if changes == nil {
		changes = []planrecords.RollupChange{}
	}
	view := planRollupView{Project: project, DryRun: dry, Applied: !dry, Changes: changes}
	for _, c := range changes {
		if c.SetByHandKept {
			view.Kept++
		} else {
			view.Changed++
		}
	}
	writeJSONOK(w, view)
}

// handlePlansRollupGet serves GET /api/plans/rollup. It only previews.
func (s *Server) handlePlansRollupGet(w http.ResponseWriter, r *http.Request) {
	if !s.planReadGate(w, r) {
		return
	}
	q := r.URL.Query()
	dry, err := parseDryRun(q.Get("dry_run"), true)
	if err != nil {
		planRecordsError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !dry {
		planRecordsError(w, http.StatusBadRequest, "GET only previews; use POST /api/plans/rollup to apply the roll-up")
		return
	}
	project := strings.TrimSpace(q.Get("project"))
	if !s.planRollupProject(w, project) {
		return
	}
	s.planRollupAnswer(w, project, true)
}

// handlePlansRollupPost serves POST /api/plans/rollup. It applies unless the
// body or the query asks for a dry run.
func (s *Server) handlePlansRollupPost(w http.ResponseWriter, r *http.Request) {
	if !s.planWriteGate(w, r, http.MethodPost) {
		return
	}
	var body planRollupBody
	// An empty body is allowed: it rolls every project up.
	if !decodePlanRollupBody(w, r, &body) {
		return
	}
	q := r.URL.Query()
	dry := false
	if body.DryRun != nil {
		dry = *body.DryRun
	}
	if v := q.Get("dry_run"); v != "" {
		d, err := parseDryRun(v, dry)
		if err != nil {
			planRecordsError(w, http.StatusBadRequest, err.Error())
			return
		}
		dry = d
	}
	project := strings.TrimSpace(body.Project)
	if project == "" {
		project = strings.TrimSpace(q.Get("project"))
	}
	if !s.planRollupProject(w, project) {
		return
	}
	s.planRollupAnswer(w, project, dry)
}

// decodePlanRollupBody reads the optional body. An empty body (EOF) is fine.
func decodePlanRollupBody(w http.ResponseWriter, r *http.Request, v *planRollupBody) bool {
	rd := http.MaxBytesReader(w, r.Body, maxPlanRecordBodyBytes)
	buf, err := io.ReadAll(rd)
	if err != nil {
		planRecordsError(w, http.StatusRequestEntityTooLarge, "request body too large (cap 256 KiB)")
		return false
	}
	if strings.TrimSpace(string(buf)) == "" {
		return true
	}
	r.Body = io.NopCloser(strings.NewReader(string(buf)))
	return decodePlanRecordJSON(w, r, v)
}
