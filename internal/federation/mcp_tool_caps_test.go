package federation_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

// TestMCPToolCap_EveryRegisteredToolHasAnEntry is Design A3's structural
// regression test (mirrors internal/server's A2 audit test): parses every
// mcpSrv.AddTool(s.toolX(), ...) registration in internal/mcp/server.go,
// resolves each toolX() function to its registered tool name, and fails if
// that name has no entry in federation.MCPToolCap — a future MCP tool
// landing with no capability assigned (defaulting to "nobody but admin can
// call it" until someone notices) is a safety failure, not silence.
func TestMCPToolCap_EveryRegisteredToolHasAnEntry(t *testing.T) {
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
	if len(matches) < 300 {
		t.Fatalf("expected at least 300 AddTool registrations, found %d — regex may be stale", len(matches))
	}

	var missing []string
	seen := map[string]bool{}
	for _, m := range matches {
		name, ok := toolFuncToName[m[1]]
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		if _, ok := federation.RequiredCapForMCPTool(name); !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("Design A3 regression: %d registered MCP tool(s) have no entry in federation.MCPToolCap "+
			"(they will be treated as admin-only until classified): %v", len(missing), missing)
	}
	if len(seen) < 300 {
		t.Fatalf("only resolved %d tool names from %d AddTool registrations — tool-name extraction may be broken", len(seen), len(matches))
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
