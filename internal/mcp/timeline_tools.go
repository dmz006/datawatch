package mcp

import (
	"context"
	"net/url"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The timeline tool is read-only and proxies the REST route, so every
// surface returns the same chart object.

func (s *Server) toolAutonomousTimeline() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_timeline",
		mcpsdk.WithDescription("Read the interval chart of the Automata that moved in the last N days: one project at a time, each with its Automata, each with its stages and tasks as bars with their start and end, blocked and waiting intervals, and the projected finish with the reason behind it. Read-only; same object as GET /api/autonomous/timeline. project, automaton, stage, kind (feature, bug, other) and tag each narrow the chart; with automaton set only that Automaton's chart comes back."),
		mcpsdk.WithString("project", mcpsdk.Description("Optional — only the Automata of this project directory")),
		mcpsdk.WithString("automaton", mcpsdk.Description("Optional — only this Automaton, by id or title")),
		mcpsdk.WithString("stage", mcpsdk.Description("Optional — only this stage (story), by id or title")),
		mcpsdk.WithString("kind", mcpsdk.Description("Optional — only Automata of this kind: feature, bug or other")),
		mcpsdk.WithString("tag", mcpsdk.Description("Optional — only the Automata carrying this tag")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousTimeline(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	for _, k := range []string{"project", "automaton", "stage", "kind", "tag"} {
		if v := req.GetString(k, ""); v != "" {
			q.Set(k, v)
		}
	}
	out, err := s.proxyGet("/api/autonomous/timeline", q)
	if err != nil {
		return nil, err
	}
	return textOK(string(out)), nil
}

// TimelineToolNames lists the timeline tools, for the parity test.
func TimelineToolNames() []string {
	s := &Server{}
	return []string{s.toolAutonomousTimeline().Name}
}
