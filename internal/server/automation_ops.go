package server

import "net/http"

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
	{"status", http.MethodGet},
	{"import-graph", http.MethodPost},
}

// automationNonControlOps are the operations that are not sub-paths of
// /api/automations/{id}/ handled by handleAutomationControl.
var automationNonControlOps = map[string]bool{
	"list": true, "get": true, "create": true, "update": true, "delete": true,
	"activate": true, "status": true, "import-graph": true,
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
