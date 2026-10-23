package main

import (
	"sort"
	"testing"

	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
)

// parityNames maps a REST operation to its counterpart name on each verb
// surface. An operation missing from a map uses its own name.
type parityNames struct{ mcp, cli, comm string }

var parityMap = map[string]parityNames{
	"list":                   {"automation_list", "list", "list"},
	"get":                    {"automation_get", "get", "get"},
	"create":                 {"automation_create", "create", "create"},
	"update":                 {"automation_update", "update", "update"},
	"delete":                 {"automation_delete", "delete", "delete"},
	"activate":               {"automation_activate", "activate", "activate"},
	"pause":                  {"automation_pause", "pause", "pause"},
	"resume":                 {"automation_resume", "resume", "resume"},
	"cancel":                 {"automation_cancel", "cancel", "cancel"},
	"skip_member":            {"automation_skip_member", "skip", "skip"},
	"insert_member":          {"automation_insert_member", "insert", "insert"},
	"remove_member":          {"automation_remove_member", "remove", "remove"},
	"reorder_edges":          {"automation_reorder_edges", "edges", "edges"},
	"approve_insert":         {"automation_approve_insert", "approve", "approve"},
	"set_llm":                {"automation_set_llm", "set-llm", "set-llm"},
	"set_verification_llm":   {"automation_set_verification_llm", "set-verification-llm", "set-verification-llm"},
	"set_budget":             {"automation_set_budget", "set-budget", "set-budget"},
	"set_decision_assistant": {"automation_set_decision_assistant", "set-decision-assistant", "set-decision-assistant"},
	"status":                 {"autonomous_status", "status", "status"},
	"import-graph":           {"automation_import_graph", "import-graph", "import-graph"},
}

// commExempt lists the operations kept off the comm channel by design (see the
// Parity surface table in the design doc), with the reason.
var commExempt = map[string]string{
	"create": "a member list does not fit a chat message; create goes through REST, MCP and CLI",
	"update": "a member list does not fit a chat message; update goes through REST, MCP and the web form",
	"delete": "destructive and unrecoverable; not offered from a chat message",
}

// knownParityGaps lists every missing counterpart that is accepted for now, as
// "<operation>/<surface>": "<reason>". The test fails for a missing
// counterpart that is not listed, and for a listed entry that is no longer a
// gap.
var knownParityGaps = map[string]string{}

func toSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func TestAutomationParity(t *testing.T) {
	mcpTools := toSet(mcp.AutomationToolNames())
	commVerbs := toSet(router.AutomationCommVerbs())
	cliVerbs := map[string]bool{}
	for _, c := range newAutomationCmd().Commands() {
		cliVerbs[c.Name()] = true
	}

	seen := map[string]bool{}
	check := func(op, surface string, present bool) {
		key := op + "/" + surface
		seen[key] = true
		reason, known := knownParityGaps[key]
		switch {
		case !present && !known:
			t.Errorf("REST operation %q has no %s counterpart (add it, or list %q in knownParityGaps with a reason)", op, surface, key)
		case present && known:
			t.Errorf("%q is listed in knownParityGaps (%s) but the %s counterpart for REST operation %q exists; remove the entry", key, reason, surface, op)
		}
	}

	for _, op := range server.AutomationRESTOperations() {
		names, ok := parityMap[op.Name]
		if !ok {
			t.Errorf("REST operation %q has no entry in parityMap; map it to its MCP tool, CLI verb and comm verb", op.Name)
			continue
		}
		check(op.Name, "mcp", mcpTools[names.mcp])
		check(op.Name, "cli", cliVerbs[names.cli])
		if _, exempt := commExempt[op.Name]; exempt {
			if commVerbs[names.comm] {
				t.Errorf("REST operation %q is exempt from the comm channel (%s) but has a comm verb %q", op.Name, commExempt[op.Name], names.comm)
			}
			continue
		}
		check(op.Name, "comm", commVerbs[names.comm])
	}

	var stale []string
	for key := range knownParityGaps {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("knownParityGaps entry %q does not match any REST operation and surface", key)
	}
}
