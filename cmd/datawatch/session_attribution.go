package main

import (
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/spendledger"
)

// sessionAttr is the owner stamped onto a session at start: the Automaton,
// the task (empty when the launcher cannot know it) and the spend role. The
// session record stores it as given, so spend is never inferred later.
type sessionAttr struct {
	AutomatonID string
	TaskID      string
	Role        string
}

// apply writes the attribution into a /api/sessions/start request body.
func (a sessionAttr) apply(body map[string]any) {
	if a.AutomatonID != "" {
		body["automaton_id"] = a.AutomatonID
	}
	if a.TaskID != "" {
		body["task_id"] = a.TaskID
	}
	if a.Role != "" {
		body["spend_role"] = a.Role
	}
}

// workerAttr attributes a worker task session (first run, retry or resume).
func workerAttr(req autonomouspkg.SpawnRequest) sessionAttr {
	return sessionAttr{AutomatonID: req.PRDID, TaskID: req.TaskID, Role: spendledger.RoleWorker}
}

// decomposeAttr attributes a decompose session: the Automaton is known, the
// task is not.
func decomposeAttr(prdID string) sessionAttr {
	return sessionAttr{AutomatonID: prdID, Role: spendledger.RoleDecompose}
}

// verifyAttr attributes a task verification session.
func verifyAttr(prdID, taskID string) sessionAttr {
	return sessionAttr{AutomatonID: prdID, TaskID: taskID, Role: spendledger.RoleVerification}
}

// guardrailAttr attributes a judge session by what it judges. A task-level
// guardrail carries the task id; the whole-Automaton rules review and plan
// review carry none and take the rules-review role; story-level and CI-watch
// judgments have no task and use the guardrail role.
func guardrailAttr(inv autonomouspkg.GuardrailInvocation) sessionAttr {
	a := sessionAttr{AutomatonID: inv.PRDID, Role: spendledger.RoleGuardrail}
	switch inv.Level {
	case "task":
		a.TaskID = inv.UnitID
	case "prd", "plan":
		if inv.Guardrail == "rules-review" || inv.Guardrail == planReviewGuardrailName {
			a.Role = spendledger.RoleRulesReview
		}
	}
	return a
}
