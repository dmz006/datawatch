package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
)

const checksCLISample = `{"prd_id":"p1","policy":{"pre_start_verifier":"on","block_when_missing":true,"plan_rules":"on","after_done":"shadow","registry_readme":"on"},
"findings":[{"prd_id":"p1","at":"2026-01-02T00:00:00Z","source":"after_done_checklist","text":"checklist found one"}]}`

const shipCLISample = `{"policy":{"push":"off","tag":"on","ci_watch":"shadow","tag_format":"v{version}","remote":"origin","ci_workflows":["release.yaml"]},"findings":[]}`

func TestChecksCLI_RendersPolicyAndFindings(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, checksCLISample)
	out, err := runLaneFillCmd(t, newAutonomousChecksCmd(), []string{"--prd", "p1", "--project-dir", "/srv/x"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/checks" || d.query != "prd_id=p1&project_dir=%2Fsrv%2Fx" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	for _, want := range []string{"after_done=shadow", "block_when_missing=true", "checklist found one"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %q", want, out)
		}
	}
}

func TestShipCLI_RendersPolicyAndEmptyFindings(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, shipCLISample)
	out, err := runLaneFillCmd(t, newAutonomousShipCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/ship" || d.query != "" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	for _, want := range []string{"push=off", "ci_watch=shadow", "ci_workflows=release.yaml", "no findings recorded"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %q", want, out)
		}
	}
}

func TestChecksShipCLI_JSONAndErrors(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, shipCLISample)
	out, err := runLaneFillCmd(t, newAutonomousShipCmd(), []string{"--json"})
	if err != nil || strings.TrimSpace(out) != shipCLISample {
		t.Fatalf("--json: %q %v", out, err)
	}
	newLaneFillCLIDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	if _, err := runLaneFillCmd(t, newAutonomousChecksCmd(), nil); err == nil {
		t.Fatal("a 503 must surface as an error")
	}
}

func TestChecksShipCLI_RegisteredUnderAutonomous(t *testing.T) {
	have := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		have[c.Name()] = true
	}
	if !have["checks"] || !have["ship"] {
		t.Fatalf("checks/ship not registered: %v", have["checks"])
	}
}

// TestChecksShipParity pins the checks and ship reads on all four surfaces:
// REST route, MCP tool (with the read capability), CLI subcommand and comm verb.
func TestChecksShipParity(t *testing.T) {
	want := map[string][3]string{
		"checks": {"/api/autonomous/checks", "checks_status", "checks"},
		"ship":   {"/api/autonomous/ship", "ship_status", "ship"},
	}
	ops := server.ChecksShipRESTOperations()
	if len(ops) != len(want) {
		t.Fatalf("%d REST operations, want %d", len(ops), len(want))
	}
	cli := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		cli[c.Name()] = true
	}
	mcpTools, comm := toSet(mcp.ChecksShipToolNames()), toSet(router.ChecksShipCommVerbs())
	for _, op := range ops {
		w := want[op.Name]
		if op.Method != http.MethodGet || op.Path != w[0] {
			t.Errorf("%s: REST = %s %s", op.Name, op.Method, op.Path)
		}
		if !mcpTools[w[1]] || federation.MCPToolCap[w[1]] != federation.CapAutonomousRead {
			t.Errorf("%s: MCP tool %s missing or wrong capability", op.Name, w[1])
		}
		if !cli[w[2]] || !comm[op.Name] || router.Parse("autonomous "+op.Name).Type != router.CmdAutonomous {
			t.Errorf("%s: CLI or comm verb missing", op.Name)
		}
	}
}
