package mcp

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const mcpDocsBaselineFile = "testdata/mcp-docs-undocumented-baseline.txt"

var backtickTokenRe = regexp.MustCompile("`([^`\n]+)`")

func readNameLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestMCPToolsDocumentedInMCPMd requires every registered MCP tool to appear
// as a backticked token in docs/mcp.md. Tools not yet documented are listed
// in the baseline file, which may only shrink.
func TestMCPToolsDocumentedInMCPMd(t *testing.T) {
	registered := readNameLines(t, registeredToolNamesGolden)
	doc, err := os.ReadFile("../../docs/mcp.md")
	if err != nil {
		t.Fatalf("read docs/mcp.md: %v", err)
	}
	documented := map[string]bool{}
	for _, m := range backtickTokenRe.FindAllStringSubmatch(string(doc), -1) {
		documented[m[1]] = true
	}
	var undocumented []string
	regSet := map[string]bool{}
	for _, n := range registered {
		regSet[n] = true
		if !documented[n] {
			undocumented = append(undocumented, n)
		}
	}
	sort.Strings(undocumented)

	if os.Getenv("DW_UPDATE_MCP_DOCS_BASELINE") == "1" {
		body := strings.Join(undocumented, "\n") + "\n"
		if err := os.WriteFile(mcpDocsBaselineFile, []byte(body), 0o644); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
		return
	}

	baseline := map[string]bool{}
	for _, n := range readNameLines(t, mcpDocsBaselineFile) {
		baseline[n] = true
	}
	for _, n := range undocumented {
		if !baseline[n] {
			t.Errorf("MCP tool %q is registered but not documented: add a row for `%s` to docs/mcp.md", n, n)
		}
	}
	for n := range baseline {
		switch {
		case !regSet[n]:
			t.Errorf("baseline entry %q is no longer registered: remove it from %s", n, mcpDocsBaselineFile)
		case documented[n]:
			t.Errorf("baseline entry %q is now documented in docs/mcp.md: remove it from %s", n, mcpDocsBaselineFile)
		}
	}
}
