package mcp

import (
	"context"
	"net/url"
	"strconv"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The overlap tool is read-only and proxies GET /api/autonomous/overlap,
// the overlaps the search-before-create check finds for a title and spec,
// best match first, beside the threshold and the mode in force.

func (s *Server) toolAutonomousOverlapSearch() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_overlap_search",
		mcpsdk.WithDescription("Search before you create an Automaton: find the Automata, registry rows and unmerged branches that already cover a plan's title and spec, best match first. \"threshold\" is the match threshold in effect and \"mode\" is the search-before-create mode in effect (off, shadow or on); the search runs in every mode. Give a title, a spec, or both. Read-only; the same object as GET /api/autonomous/overlap."),
		mcpsdk.WithString("title",
			mcpsdk.Description("Optional when spec is given — the title of the plan you are about to create.")),
		mcpsdk.WithString("spec",
			mcpsdk.Description("Optional when title is given — the spec of the plan you are about to create.")),
		mcpsdk.WithNumber("limit",
			mcpsdk.Description("Optional — the most warnings to return, from 1 to 50 (default 10).")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// OverlapToolNames lists the overlap tools, for the parity test.
func OverlapToolNames() []string {
	return []string{"autonomous_overlap_search"}
}

func (s *Server) handleAutonomousOverlapSearch(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	for _, key := range []string{"title", "spec"} {
		if v := req.GetString(key, ""); v != "" {
			q.Set(key, v)
		}
	}
	if n := req.GetInt("limit", 0); n > 0 {
		q.Set("limit", strconv.Itoa(n))
	}
	out, err := s.proxyGet("/api/autonomous/overlap", q)
	if err != nil {
		return toolError("autonomous_overlap_search", err), nil
	}
	return textOK(string(out)), nil
}
