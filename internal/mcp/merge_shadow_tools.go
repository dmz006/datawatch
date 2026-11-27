package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// mergeShadowPath is the REST read endpoint autonomous_merge_shadow_status
// proxies.
const mergeShadowPath = "/api/autonomous/merge/shadow"

func (s *Server) toolAutonomousMergeShadowStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_merge_shadow_status",
		mcpsdk.WithDescription("Read the shadow merge state. turn_on is the turn-on report for the merge repository: the run of shadowed hand merges that matched against the target, seven results per merge, the work eligible and excluded now with why, the next push, what merging would do outside a run and what is still to apply (turn_on.lines is the same as text). Also, per project, the stored shadow merge plans, the stored comparisons, the matched and different comparison counts per release size (patch, minor, major, none), and the agreement streak against the configured autonomous.merge_shadow.agreement_streak threshold. Read-only; the same data as GET /api/autonomous/merge/shadow. project limits the answer to one project (default: every project)."),
		mcpsdk.WithString("project", mcpsdk.Description("Optional — only this project's entry; default is every project")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousMergeShadowStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_merge_shadow_status"
	out, err := s.proxyGet(mergeShadowPath, nil)
	if err != nil {
		return toolError(op, err), nil
	}
	project, _ := req.GetArguments()["project"].(string)
	project = strings.TrimSpace(project)
	if project == "" {
		return mcpsdk.NewToolResultText(string(out)), nil
	}
	var body struct {
		Projects []json.RawMessage `json:"projects"`
		TurnOn   json.RawMessage   `json:"turn_on"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return toolError(op, fmt.Errorf("decode response: %w", err)), nil
	}
	kept := []json.RawMessage{}
	for _, raw := range body.Projects {
		var p struct {
			Project string `json:"project"`
		}
		if json.Unmarshal(raw, &p) == nil && p.Project == project {
			kept = append(kept, raw)
		}
	}
	filtered := map[string]any{"projects": kept}
	var turnOn struct {
		Project string `json:"project"`
	}
	if len(body.TurnOn) > 0 && json.Unmarshal(body.TurnOn, &turnOn) == nil && turnOn.Project == project {
		filtered["turn_on"] = body.TurnOn
	}
	b, _ := json.Marshal(filtered)
	return mcpsdk.NewToolResultText(string(b)), nil
}

// MergeShadowToolNames lists the merge shadow tools, for the parity test.
// The tool is a read of the daemon's shadow plans and comparison counts.
func MergeShadowToolNames() []string {
	return []string{"autonomous_merge_shadow_status"}
}
