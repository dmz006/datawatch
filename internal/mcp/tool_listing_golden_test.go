// Golden check of the unconditionally registered MCP tool names. The golden
// file records the tool listing before any dependency update so a later
// change in the listing shows up as a diff. Set DW_UPDATE_TOOL_LISTING=1 to
// rewrite the golden file instead of comparing.
package mcp

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

const registeredToolNamesGolden = "testdata/registered_tool_names.golden"

func TestRegisteredToolListingMatchesGolden(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	allowTestTemp(mgr)
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()

	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected a non-empty tool catalog from a real New() server")
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	got := strings.Join(names, "\n") + "\n"

	if os.Getenv("DW_UPDATE_TOOL_LISTING") == "1" {
		if err := os.WriteFile(registeredToolNamesGolden, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("rewrote %s with %d names", registeredToolNamesGolden, len(names))
		return
	}

	raw, err := os.ReadFile(registeredToolNamesGolden)
	if err != nil {
		t.Fatalf("read golden (run with DW_UPDATE_TOOL_LISTING=1 to create it): %v", err)
	}
	want := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	have := names
	wantSet := map[string]bool{}
	for _, n := range want {
		wantSet[n] = true
	}
	haveSet := map[string]bool{}
	for _, n := range have {
		haveSet[n] = true
	}
	for _, n := range have {
		if !wantSet[n] {
			t.Errorf("tool %q is registered but missing from the golden file", n)
		}
	}
	for _, n := range want {
		if !haveSet[n] {
			t.Errorf("tool %q is in the golden file but no longer registered", n)
		}
	}
	if got != string(raw) && !t.Failed() {
		t.Errorf("tool listing differs from golden in order or formatting")
	}
}
