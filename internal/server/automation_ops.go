package server

import (
	"net/http"
	"strings"
)

// AutomationRESTOperation is one operation on the Automation REST surface:
// its name and the HTTP method it answers to.
type AutomationRESTOperation struct {
	Name   string
	Method string
}

// automationRESTOperations is the single list of Automation REST operations.
// The control sub-paths under /api/automations/{id}/ are derived from it, so
// the router and the parity test cannot drift apart.
var automationRESTOperations = []AutomationRESTOperation{
	{"list", http.MethodGet},
	{"get", http.MethodGet},
	{"create", http.MethodPost},
	{"update", http.MethodPut},
	{"delete", http.MethodDelete},
	{"activate", http.MethodPost},
	{"return_to_draft", http.MethodPost},
	{"shipped", http.MethodPost},
	{"stages/rename", http.MethodPost},
	{"pause", http.MethodPost},
	{"resume", http.MethodPost},
	{"cancel", http.MethodPost},
	{"skip_member", http.MethodPost},
	{"insert_member", http.MethodPost},
	{"remove_member", http.MethodPost},
	{"reorder_edges", http.MethodPost},
	{"approve_insert", http.MethodPost},
	{"set_llm", http.MethodPost},
	{"set_verification_llm", http.MethodPost},
	{"set_budget", http.MethodPost},
	{"set_effort_ceiling", http.MethodPost},
	{"set_decision_assistant", http.MethodPost},
	{"set_max_parallel", http.MethodPost},
	{"set_stage_gate", http.MethodPost},
	{"status", http.MethodGet},
	{"import-graph", http.MethodPost},
}

// automationNonControlOps are the operations that are not sub-paths of
// /api/automations/{id}/ handled by handleAutomationControl.
var automationNonControlOps = map[string]bool{
	"list": true, "get": true, "create": true, "update": true, "delete": true,
	"activate":        true,
	"return_to_draft": true, "shipped": true, "status": true, "import-graph": true,
}

// AutomationRESTOperations returns the Automation REST operations.
func AutomationRESTOperations() []AutomationRESTOperation {
	return append([]AutomationRESTOperation(nil), automationRESTOperations...)
}

// automationControlOps lists the POST-only control sub-paths under
// /api/automations/{id}/, derived from automationRESTOperations.
func automationControlOps() []string {
	var ops []string
	for _, op := range automationRESTOperations {
		if !automationNonControlOps[op.Name] {
			ops = append(ops, op.Name)
		}
	}
	return ops
}

// insertMemberTarget picks what an insert_member body names: an Automaton
// (member.prd_id) or a pending item (pending_item_id, either inside member or
// at the top of the body). The two are exclusive; the caller refuses a body
// that carries both, and the manager refuses one that carries neither. Values
// are trimmed.
func insertMemberTarget(prdID, memberItemID, bodyItemID string) (prd, item string) {
	prd = strings.TrimSpace(prdID)
	item = strings.TrimSpace(memberItemID)
	if item == "" {
		item = strings.TrimSpace(bodyItemID)
	}
	return prd, item
}

// insertMemberKindError checks which kind of member an insert body names and
// returns the 400 message when it does not name exactly one: an Automaton
// (prd_id), a pending item (pending_item_id) or a decision wait (question).
// kind is optional and, when set, can only be decision_wait and needs question.
func insertMemberKindError(kind, question, prdID, itemID string) string {
	switch strings.TrimSpace(kind) {
	case "", "decision_wait":
	default:
		return "400: member kind " + strings.TrimSpace(kind) + " cannot be inserted; only a decision_wait member (with question) can be inserted by kind"
	}
	if strings.TrimSpace(kind) == "decision_wait" && question == "" {
		return "400: a decision_wait member needs a question"
	}
	if prdID != "" && itemID != "" {
		return "400: an insert takes either prd_id or pending_item_id, not both"
	}
	if question != "" && (prdID != "" || itemID != "") {
		return "400: an insert takes one of prd_id, pending_item_id or question, not several"
	}
	return ""
}

// auditTextMax bounds free text copied into an audit record, in runes.
const auditTextMax = 160

// truncateAuditText cuts s to auditTextMax runes so a long question never
// bloats an audit record.
func truncateAuditText(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= auditTextMax {
		return string(r)
	}
	return string(r[:auditTextMax])
}
