package main

import (
	"sort"
	"strings"
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
	"return_to_draft":        {"automation_return_to_draft", "return-to-draft", "return-to-draft"},
	"shipped":                {"autonomous_shipped_record", "shipped", "shipped"},
	"pause":                  {"automation_pause", "pause", "pause"},
	"resume":                 {"automation_resume", "resume", "resume"},
	"cancel":                 {"automation_cancel", "cancel", "cancel"},
	"skip_member":            {"automation_skip_member", "skip", "skip"},
	"insert_member":          {"automation_insert_member", "insert", "insert"},
	"remove_member":          {"automation_remove_member", "remove", "remove"},
	"reorder_edges":          {"automation_reorder_edges", "edges", "edges"},
	"stages/rename":          {"automation_rename_stage", "rename-stage", "rename-stage"},
	"approve_insert":         {"automation_approve_insert", "approve", "approve"},
	"set_llm":                {"automation_set_llm", "set-llm", "set-llm"},
	"set_verification_llm":   {"automation_set_verification_llm", "set-verification-llm", "set-verification-llm"},
	"set_budget":             {"automation_set_budget", "set-budget", "set-budget"},
	"set_effort_ceiling":     {"automation_set_effort_ceiling", "set-effort-ceiling", "set-effort-ceiling"},
	"set_decision_assistant": {"automation_set_decision_assistant", "set-decision-assistant", "set-decision-assistant"},
	"set_max_parallel":       {"automation_set_max_parallel", "set-max-parallel", "set-max-parallel"},
	"set_stage_gate":         {"automation_set_stage_gate", "set-stage-gate", "set-stage-gate"},
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

// TestAutomationLandingParity — landing state parity across the surfaces:
// the landing record rides on the Automaton read (REST GET, the
// `autonomous prd-get` landing line, MCP get output, comm `autonomous
// landing <id>`, and the PWA overview block), and the two operator
// decisions (keep / drop) are each reachable from REST, MCP, CLI and the
// comm channel. Every REST landing operation must have its MCP tool, CLI
// verb and comm verb; the landing record read must be on the comm channel.
func TestAutomationLandingParity(t *testing.T) {
	mcpTools := toSet(mcp.LandingToolNames())
	commVerbs := toSet(router.LandingCommVerbs())
	cliVerbs := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		cliVerbs[c.Name()] = true
	}

	for _, op := range server.LandingRESTOperations() {
		base := strings.ReplaceAll(op.Name, "/", "_") // landing_keep
		action := strings.TrimPrefix(base, "landing_")
		if action != "keep" && action != "drop" {
			t.Fatalf("landing REST operation %q: expected a keep/drop decision op", op.Name)
		}
		want := map[string]string{
			"mcp":  "autonomous_" + base,
			"cli":  "prd-landing-" + action,
			"comm": "landing-" + action,
		}
		for surface, name := range want {
			present := false
			switch surface {
			case "mcp":
				present = mcpTools[name]
			case "cli":
				present = cliVerbs[name]
			case "comm":
				present = commVerbs[name]
			}
			if !present {
				t.Errorf("REST operation %q has no %s counterpart named %q", op.Name, surface, name)
			}
		}
	}

	// The landing record read is on the comm channel as `autonomous landing <id>`.
	if !commVerbs["landing"] {
		t.Errorf(`comm verbs %v: the landing record read ("landing <id>") is missing`, commVerbs)
	}
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
