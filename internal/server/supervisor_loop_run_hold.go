// The daemon's side of the hold on the supervisor loop jobs that start a merge
// or a deploy (autonomous/supervisor_loop_run_hold.go): their refusals go to
// the audit log with the same action as a refused run request.

package server

import (
	"context"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Who is named on a refusal nobody asked for by hand.
const (
	supervisorLoopAuditActor = "supervisor-loop"
	daemonStartAuditActor    = "daemon"
)

// What started the refused run, in the audit details.
const (
	refusedManualRun   = "manual"
	refusedDaemonRun   = "daemon"
	refusedDaemonStart = "daemon_start"
)

// The loop jobs the hold covers, and the audit subject of each.
var supervisorHeldJobSubjects = map[string]string{
	"deploy_after_ship":        deployRunSubject,
	"run_merge_until_conflict": mergeRunSubject,
}

// runHoldDetails says in an audit entry which switch or pause held a run.
func runHoldDetails(hold autonomous.SupervisorRunHold, trigger string) map[string]any {
	d := map[string]any{"trigger": trigger}
	if hold.Setting != "" {
		d["setting"] = hold.Setting
	}
	if p := hold.Pause; p != nil {
		d["held_by"] = "operator_pause"
		d["pause_scope"] = string(p.Scope)
		d["pause_project"] = p.Project
		d["pause_reason"] = p.Reason
		d["paused_by"] = p.By
	}
	return d
}

// auditSupervisorRunRefusals makes h write each refusal to the audit log. The
// log is read at write time.
func (s *Server) auditSupervisorRunRefusals(h *autonomous.SupervisorLoopHooks) {
	h.RunRefused = func(ctx context.Context, hold autonomous.SupervisorRunHold, byHand bool) {
		actor, d := supervisorLoopAuditActor, runHoldDetails(hold, refusedDaemonRun)
		if byHand {
			actor, d = s.auditActor(ctx), runHoldDetails(hold, refusedManualRun)
			d["route"] = "/api/supervisor-loop/jobs/" + hold.Job + "/run"
		}
		d["job"] = hold.Job
		s.writeRunRefusal(actor, supervisorHeldJobSubjects[hold.Job], d)
	}
}

// AuditSupervisorRunRefusals makes the loop hooks write each refusal to this
// server's audit log. A nil server records nothing; the hold still applies.
func (s *HTTPServer) AuditSupervisorRunRefusals(h *autonomous.SupervisorLoopHooks) {
	if s != nil && s.api != nil {
		s.api.auditSupervisorRunRefusals(h)
	}
}
