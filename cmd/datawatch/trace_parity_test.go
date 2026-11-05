package main

import (
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
)

// The trace views are one set of four reads. Each must exist on every
// surface — REST route (with its OpenAPI entry), MCP tool, CLI subcommand
// and comm verb — and the tracing.* settings must stay reachable through
// the config API. A missing surface fails here.
var traceSurfaces = []struct {
	verb      string // CLI subcommand and comm verb
	mcpTool   string
	routePart string // path as registered on the mux
	openapi   string // path key in openapi.yaml
}{
	{"session", "trace_session", `"/api/trace/session/"`, "/api/trace/session/{id}:"},
	{"automaton", "trace_automaton", `"/api/trace/automaton/"`, "/api/trace/automaton/{id}:"},
	{"rollup", "trace_rollup", `"/api/trace/rollup"`, "/api/trace/rollup:"},
	{"coverage", "trace_coverage", `"/api/trace/coverage"`, "/api/trace/coverage:"},
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile("../../" + rel)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTraceParity_EverySurface(t *testing.T) {
	serverSrc := readRepoFile(t, "internal/server/server.go")
	openapi := readRepoFile(t, "internal/server/web/openapi.yaml")

	mcpSrv := mcp.New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), mcp.Options{Version: "test"})
	listed, err := mcpSrv.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]bool{}
	for _, tl := range listed {
		tools[tl.Name] = true
	}

	cli := map[string]bool{}
	for _, c := range newTraceCmd().Commands() {
		cli[c.Name()] = true
	}
	comm := map[string]bool{}
	for _, v := range router.TraceCommVerbs() {
		comm[v] = true
	}

	for _, s := range traceSurfaces {
		if !strings.Contains(serverSrc, "apiMux.HandleFunc("+s.routePart) {
			t.Errorf("%s: REST route %s is not registered", s.verb, s.routePart)
		}
		if !strings.Contains(openapi, "  "+s.openapi) {
			t.Errorf("%s: openapi.yaml has no entry for %s", s.verb, s.openapi)
		}
		if !tools[s.mcpTool] {
			t.Errorf("%s: MCP tool %s is not registered", s.verb, s.mcpTool)
		}
		if c, ok := federation.RequiredCapForMCPTool(s.mcpTool); !ok || c != federation.CapAnalyticsRead {
			t.Errorf("%s: MCP tool %s federation capability = %q (found %v), want %q", s.verb, s.mcpTool, c, ok, federation.CapAnalyticsRead)
		}
		if !cli[s.verb] {
			t.Errorf("%s: CLI subcommand missing", s.verb)
		}
		if !comm[s.verb] {
			t.Errorf("%s: comm verb missing", s.verb)
		}
	}
	if len(router.TraceCommVerbs()) != len(traceSurfaces) || len(cli) != len(traceSurfaces) {
		t.Errorf("surface verb sets differ: comm=%v cli=%v", router.TraceCommVerbs(), cli)
	}
}

func TestTraceParity_FilterWordsMatchAcrossSurfaces(t *testing.T) {
	for _, verb := range []string{"session", "automaton"} {
		for _, key := range router.TraceCommKeys(verb) {
			flag := strings.ReplaceAll(key, "_", "-")
			var found bool
			for _, c := range newTraceCmd().Commands() {
				if c.Name() == verb && c.Flag(flag) != nil {
					found = true
				}
			}
			if !found {
				t.Errorf("comm key %s of trace %s has no CLI flag --%s", key, verb, flag)
			}
		}
	}
	for verb, flags := range map[string][]string{"rollup": {"dimension", "key", "loop-threshold"}, "coverage": {"backend"}} {
		for _, c := range newTraceCmd().Commands() {
			if c.Name() != verb {
				continue
			}
			for _, f := range flags {
				if c.Flag(f) == nil {
					t.Errorf("trace %s lacks --%s", verb, f)
				}
			}
		}
	}
}

func TestTraceParity_TracingSettingsReachableThroughConfigAPI(t *testing.T) {
	src := readRepoFile(t, "internal/server/api.go")
	for _, key := range []string{`"tracing": s.cfg.Tracing.WithDefaults()`, "validateTracingPatch(s.cfg.Tracing, patch)", `case "tracing.mode", "tracing.arg_cap_bytes", "tracing.retention_days", "tracing.backends"`} {
		if !strings.Contains(src, key) {
			t.Errorf("config API lost its tracing handling: %s", key)
		}
	}
}

// GET /api/trace/stats is the observability read of the trace store. It is
// not one of the four trace views (no CLI subcommand or comm verb: the CLI
// and comm surfaces get the same numbers from the stats command), but the
// Monitoring Rule gives it a REST route, an OpenAPI entry and an MCP tool
// with the analytics read capability like the other subsystem stats reads.
func TestTraceParity_StatsSurfaces(t *testing.T) {
	serverSrc := readRepoFile(t, "internal/server/server.go")
	openapi := readRepoFile(t, "internal/server/web/openapi.yaml")
	if !strings.Contains(serverSrc, `apiMux.HandleFunc("/api/trace/stats"`) {
		t.Error("REST route /api/trace/stats is not registered")
	}
	if !strings.Contains(openapi, "  /api/trace/stats:") {
		t.Error("openapi.yaml has no entry for /api/trace/stats")
	}
	mcpSrv := mcp.New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), mcp.Options{Version: "test"})
	listed, err := mcpSrv.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, tl := range listed {
		found = found || tl.Name == "trace_stats"
	}
	if !found {
		t.Error("MCP tool trace_stats is not registered")
	}
	if c, ok := federation.RequiredCapForMCPTool("trace_stats"); !ok || c != federation.CapAnalyticsRead {
		t.Errorf("trace_stats federation capability = %q (found %v), want %q", c, ok, federation.CapAnalyticsRead)
	}
	if !strings.Contains(readRepoFile(t, "docs/api-mcp-mapping.md"), "| `GET /api/trace/stats` | `trace_stats` |") {
		t.Error("docs/api-mcp-mapping.md does not map GET /api/trace/stats to trace_stats")
	}
}
