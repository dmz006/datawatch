// plans_migrate.go — POST /api/plans/migrate: build plan records from the
// project arcs and the registry rows.
//
//	POST /api/plans/migrate   body {"apply": bool}   (autonomous:write)
//
// The default is a dry run: the answer lists what would be created and
// linked and nothing is written. {"apply": true} writes. A plan or link that
// already exists is never changed, so running it twice is safe. No arc, row
// or config key is deleted. The apply is audit-logged through the plan-record
// audit hook. autonomous.plan_records.enabled false answers 404. A request
// only reads and writes record files; it never starts or signals a process.

package server

import (
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

const plansMigratePath = "/api/plans/migrate"

// planMigrateBody is the request body.
type planMigrateBody struct {
	Apply bool `json:"apply"`
}

// planMigrateArcs lists every configured project arc with the title and goal
// of its Automation.
func (s *Server) planMigrateArcs() []planrecords.MigrateArc {
	var out []planrecords.MigrateArc
	for name, entry := range s.cfg.Projects {
		for _, arc := range entry.OrderedArcs() {
			a := planrecords.MigrateArc{
				Project: name, AutomationID: arc.AutomationID, Priority: arc.Priority, State: arc.State,
			}
			if s.autonomousMgr != nil {
				if auto, ok := s.autonomousMgr.GetAutomation(arc.AutomationID); ok {
					if auto := rawAutomationOf(auto); auto != nil {
						a.Title, a.Goal = strings.TrimSpace(auto.Name), strings.TrimSpace(auto.Description)
					}
				}
			}
			out = append(out, a)
		}
	}
	return out
}

// planMigrateResolver says whether an id is a known Automaton or Automation.
func (s *Server) planMigrateResolver() func(string) (planrecords.MigrateRef, bool) {
	if s.autonomousMgr == nil {
		return nil
	}
	return func(id string) (planrecords.MigrateRef, bool) {
		if auto, ok := s.autonomousMgr.GetAutomation(id); ok {
			if a := rawAutomationOf(auto); a != nil {
				return planrecords.MigrateRef{Kind: "automation", Title: a.Name, State: string(a.Status)}, true
			}
		}
		if raw, ok := s.autonomousMgr.GetPRD(id); ok {
			if p, err := toPRD(raw); err == nil && p != nil {
				return planrecords.MigrateRef{Kind: "automaton", Title: p.Title, State: string(p.Status)}, true
			}
		}
		return planrecords.MigrateRef{}, false
	}
}

// runPlanMigration runs the migration with the server's arcs, resolver and
// audit hook. Every surface that migrates goes through this.
func (s *Server) runPlanMigration(apply bool, actor string) (planrecords.MigrateReport, error) {
	return planrecords.Migrate(planrecords.MigrateOptions{
		Apply: apply, Store: s.planRecords, Arcs: s.planMigrateArcs(),
		Resolve: s.planMigrateResolver(), Audit: PlanRecordsAuditFunc(s.auditLog), Actor: actor,
	})
}

func (s *Server) handlePlansMigrate(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Autonomous.PlanRecords.EffectiveEnabled() {
		http.Error(w, "plan records are turned off (autonomous.plan_records.enabled)", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		planRecordsError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	if s.planRecords == nil {
		planRecordsError(w, http.StatusServiceUnavailable, "plan records are not available")
		return
	}
	var body planMigrateBody
	if r.ContentLength != 0 && !decodePlanRecordJSON(w, r, &body) {
		return
	}
	rep, err := s.runPlanMigration(body.Apply, s.auditActor(r.Context()))
	if err != nil {
		planRecordsError(w, http.StatusInternalServerError, "cannot migrate: "+err.Error())
		return
	}
	note := "Preview only: nothing was written. Send {\"apply\": true} to write."
	if body.Apply {
		note = "Migration finished. Run it again any time; plans and links already there are left alone."
	}
	writeJSONOK(w, map[string]any{"note": note, "counts": rep.Counts(), "result": rep})
}
