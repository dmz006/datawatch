package federation_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

// registeredMCPToolNames is the shared enumeration source for the coverage
// tests in this file: it parses internal/mcp, finds each tool function
// (func (s *Server) toolX()) returning a NewTool("name", ...) or
// NewToolWithRawSchema("name", ...) literal, and resolves the names
// registered by the mcpSrv.AddTool(s.toolX(), ...) calls in server.go. It
// returns the deduplicated registered tool names and the number of AddTool
// registrations found.
func registeredMCPToolNames(t *testing.T) (names []string, addToolCount int) {
	t.Helper()
	const mcpDir = "../mcp"
	entries, err := os.ReadDir(mcpDir)
	if err != nil {
		t.Fatalf("read %s: %v", mcpDir, err)
	}

	toolFuncToName := map[string]string{}
	toolDeclRe := regexp.MustCompile(`^func \(s \*Server\) (tool\w+)\(\)`)
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) < 3 || name[len(name)-3:] != ".go" {
			continue
		}
		f, err := parser.ParseFile(fset, mcpDir+"/"+name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		src, _ := os.ReadFile(mcpDir + "/" + name)
		lines := splitLines(string(src))
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			line := lines[fset.Position(fn.Pos()).Line-1]
			m := toolDeclRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			// Find the first NewTool("name", ...) / NewToolWithRawSchema("name", ...)
			// call inside the function body.
			var toolName string
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if toolName != "" {
					return false
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "NewTool" && sel.Sel.Name != "NewToolWithRawSchema") {
					return true
				}
				if len(call.Args) == 0 {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok {
					return true
				}
				toolName = lit.Value
				return false
			})
			if toolName != "" {
				// Strip surrounding quotes from the Go string literal.
				toolFuncToName[m[1]] = toolName[1 : len(toolName)-1]
			}
		}
	}

	addToolRe := regexp.MustCompile(`mcpSrv\.AddTool\(s\.(tool\w+)\(\),`)
	src, err := os.ReadFile(mcpDir + "/server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	matches := addToolRe.FindAllStringSubmatch(string(src), -1)

	seen := map[string]bool{}
	for _, m := range matches {
		name, ok := toolFuncToName[m[1]]
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, len(matches)
}

// TestMCPToolCap_EveryRegisteredToolHasAnEntry is Design A3's structural
// regression test (mirrors internal/server's A2 audit test): parses every
// mcpSrv.AddTool(s.toolX(), ...) registration in internal/mcp/server.go,
// resolves each toolX() function to its registered tool name, and fails if
// that name has no entry in federation.MCPToolCap — a future MCP tool
// landing with no capability assigned (defaulting to "nobody but admin can
// call it" until someone notices) is a safety failure, not silence.
func TestMCPToolCap_EveryRegisteredToolHasAnEntry(t *testing.T) {
	names, addToolCount := registeredMCPToolNames(t)
	if addToolCount < 300 {
		t.Fatalf("expected at least 300 AddTool registrations, found %d — regex may be stale", addToolCount)
	}
	if len(names) < 300 {
		t.Fatalf("only resolved %d tool names from %d AddTool registrations — tool-name extraction may be broken", len(names), addToolCount)
	}
	var missing []string
	for _, name := range names {
		if _, ok := federation.RequiredCapForMCPTool(name); !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("Design A3 regression: %d registered MCP tool(s) have no entry in federation.MCPToolCap "+
			"(they will be treated as admin-only until classified): %v", len(missing), missing)
	}
}

// TestIsConfigWriteTool_Classification pins both sides of the BL412
// self-config classification (option C,
// docs/plans/2026-10-29-bl412-self-config-gate-proposal.md): the daemon
// settings writers and the component-registry writers classify as config
// writes, while the excluded surfaces (secrets, identity, daemon control,
// session/autonomous state) are explicitly classified non-config-write —
// never left on neither side.
func TestIsConfigWriteTool_Classification(t *testing.T) {
	configWrite := []string{
		"config_set",
		"detection_config_set", "dns_channel_config_set", "proxy_config_set",
		"channel_routing_config_set", "council_config_set", "council_personas_set",
		"orchestrator_config_set", "autonomous_config_set", "autonomous_scan_config_set",
		"observer_config_set", "smoke_forward_config_set", "burn_guard_config_set",
		"compute_pin_settings",
		"usage_backoff_set", "usage_extra_credit_set", "usage_remaining_work_set",
		"llm_add", "llm_update", "llm_delete", "llm_disable", "llm_enable",
		"llm_add_model", "llm_remove_model", "llm_refresh_models",
		"compute_node_add", "compute_node_update", "compute_node_delete",
		"compute_node_attach_observer", "compute_node_detach_observer",
		"compute_node_pull_model", "compute_node_remove_model",
		"observer_peer_register", "observer_peer_delete",
		"server_add", "server_delete", "server_update",
		"websearch_provider_add", "websearch_provider_update",
		"websearch_provider_delete", "websearch_provider_disable",
	}
	for _, name := range configWrite {
		if !federation.IsConfigWriteTool(name) {
			t.Errorf("IsConfigWriteTool(%q) = false, want true", name)
		}
		if federation.IsNonConfigWriteTool(name) {
			t.Errorf("%q must sit in exactly one of the two lists", name)
		}
	}

	// Explicit non-config side: readers of the same surfaces, the excluded
	// surfaces, and autonomous state.
	nonConfigWrite := []string{
		"llm_list", "llm_get",
		"get_config",
		"observer_config_get",
		"compute_node_get",
		"secret_get", "secret_set", "secrets_vault_status",
		"set_identity", "update_identity", "configure_identity",
		"restore_identity", "reset_identity",
		"restart_daemon", "reload",
		"agent_spawn", "autonomous_prd_create",
	}
	for _, name := range nonConfigWrite {
		if federation.IsConfigWriteTool(name) {
			t.Errorf("IsConfigWriteTool(%q) = true, want false (excluded surface)", name)
		}
		if !federation.IsNonConfigWriteTool(name) {
			t.Errorf("IsNonConfigWriteTool(%q) = false — must be explicitly listed non-config-write", name)
		}
	}

	// Unknown tool names never classify as config writes.
	for _, name := range []string{"", "not_a_registered_tool", "LLM_ADD"} {
		if federation.IsConfigWriteTool(name) {
			t.Errorf("IsConfigWriteTool(%q) = true, want false for unknown tool names", name)
		}
	}
}

// TestIsConfigWriteTool_EveryRegisteredToolIsClassified fails the build
// when a registered MCP tool lands in neither configWriteTools nor
// nonConfigWriteTools, so a new registration can't be forgotten on both
// sides of the self-config classification (BL412 option C). It reuses the
// same enumeration source as TestMCPToolCap_EveryRegisteredToolHasAnEntry.
func TestIsConfigWriteTool_EveryRegisteredToolIsClassified(t *testing.T) {
	names, addToolCount := registeredMCPToolNames(t)
	if addToolCount < 300 {
		t.Fatalf("expected at least 300 AddTool registrations, found %d — regex may be stale", addToolCount)
	}
	var unclassified []string
	for _, name := range names {
		if federation.IsConfigWriteTool(name) || federation.IsNonConfigWriteTool(name) {
			continue
		}
		unclassified = append(unclassified, name)
	}
	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Fatalf("every registered MCP tool must be classified as a config write or explicitly listed non-config-write; %d unclassified: %v\n"+
			"Add each new tool to exactly one of the lists under IsConfigWriteTool in mcp_tool_caps.go.",
			len(unclassified), unclassified)
	}
}

// TestMCPToolCaps pins the GH#204 result-panel MCP tool → capability
// mapping, which the per-tool coverage tests only check structurally
// ("has an entry") but not semantically ("the RIGHT entry"):
//   - post_result          → sessions:result_write — deliberately NOT in any
//     built-in group (it is minted explicitly into per-session tokens only,
//     the same way every other write-class capability is least-privilege
//     by default).
//   - schedule_results_list→ autonomous:read — granted to the read-only and
//     session-default groups, so peers that could read schedules before can
//     read the panel with no new grant.
func TestMCPToolCaps_ResultPanelTools(t *testing.T) {
	cap, ok := federation.RequiredCapForMCPTool("post_result")
	if !ok {
		t.Fatal("MCPToolCap[\"post_result\"] missing")
	}
	if cap != federation.CapSessionsResultWrite {
		t.Errorf("post_result mapped to %q, want %q", cap, federation.CapSessionsResultWrite)
	}
	for _, gname := range []string{"full-control", "session-default", "read-only", "monitor", "session-operator", "autonomous-operator"} {
		g, ok := federation.BuiltinGroups[gname]
		if !ok {
			continue
		}
		if federation.Check(federation.Resolve(g.Caps, nil), federation.CapSessionsResultWrite) {
			t.Errorf("federation.CapSessionsResultWrite must not be in built-in group %q (mint it per-session explicitly)", gname)
		}
	}

	cap, ok = federation.RequiredCapForMCPTool("schedule_results_list")
	if !ok {
		t.Fatal("MCPToolCap[\"schedule_results_list\"] missing")
	}
	if cap != federation.CapAutonomousRead {
		t.Errorf("schedule_results_list mapped to %q, want %q", cap, federation.CapAutonomousRead)
	}
	// Read-only peers (read-only / session-default) must be able to read the panel.
	for _, gname := range []string{"read-only", "session-default"} {
		g, ok := federation.BuiltinGroups[gname]
		if !ok {
			continue
		}
		if !federation.Check(federation.Resolve(g.Caps, nil), federation.CapAutonomousRead) {
			t.Errorf("%q should be able to call schedule_results_list but lacks federation.CapAutonomousRead", gname)
		}
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
