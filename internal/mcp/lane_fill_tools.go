package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The lane fill status tool is read-only and proxies GET /api/autonomous/lane-fill.
// The lane preference tool proxies POST /api/autonomous/prds/{id}/set_lane_preference.

func (s *Server) toolAutonomousLaneFillStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_lane_fill_status",
		mcpsdk.WithDescription("Read the lane fill status: each GPU and Claude lane with what it is running or waiting on, whether it is idle, the newest decisions the planner made (newest first), and any idle-lane alerts. A lane's waiting_on_verifier flag with its wait_reason text means its Automaton has running work but every piece is blocked waiting for a verifier slot, so the lane is occupied without advancing; a Claude lane the verifier-wait rule counts available carries the state waiting_verifier. A decision made on such a lane keeps the same wait_reason as its basis. Read-only; same object as GET /api/autonomous/lane-fill. limit sets how many decisions come back (a whole number from 1 to 1000, default 50)."),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Optional — how many decisions to return, a whole number from 1 to 1000 (default 50)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousLaneFillStatus(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	// limit arrives as a number, or as text from a client that quotes it; the
	// REST route decides whether it is valid.
	switch v := req.GetArguments()["limit"].(type) {
	case string:
		if v != "" {
			q.Set("limit", v)
		}
	case float64:
		q.Set("limit", strconv.FormatFloat(v, 'f', -1, 64))
	}
	out, err := s.proxyGet("/api/autonomous/lane-fill", q)
	if err != nil {
		return toolError("autonomous_lane_fill_status", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) toolAutonomousPRDSetLanePreference() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_prd_set_lane_preference",
		mcpsdk.WithDescription("Set which lane an Automaton may take: gpu (a GPU lane), claude (a Claude lane) or any. An empty lane_preference clears the setting, which means any. Proxies POST /api/autonomous/prds/{id}/set_lane_preference."),
		mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("Automaton ID")),
		mcpsdk.WithString("lane_preference", mcpsdk.Required(), mcpsdk.Enum("gpu", "claude", "any", ""), mcpsdk.Description("gpu, claude or any; empty means any")),
	)
}

func (s *Server) handleAutonomousPRDSetLanePreference(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	id := req.GetString("id", "")
	body, _ := json.Marshal(map[string]string{"lane_preference": req.GetString("lane_preference", "")})
	out, err := s.proxyJSON(http.MethodPost, "/api/autonomous/prds/"+url.PathEscape(id)+"/set_lane_preference", body)
	return llmSetterResult(out, err)
}

// laneFillConfigArgs maps the autonomous_config_set lane_fill_* arguments into
// the nested lane_fill object of the autonomous config body. Numbers are
// presence-aware so an explicit 0 is sent; text is sent only when given.
func laneFillConfigArgs(req mcpsdk.CallToolRequest) map[string]any {
	lane := map[string]any{}
	args := req.GetArguments()
	if args == nil {
		return lane
	}
	for _, k := range []string{"gpu_lanes", "claude_lanes", "idle_alert_minutes", "decompose_ahead_min", "poll_seconds"} {
		if _, ok := args["lane_fill_"+k]; ok {
			lane[k] = req.GetFloat("lane_fill_"+k, 0)
		}
	}
	for _, k := range []string{"mode", "gpu_backend", "gpu_model", "claude_backend"} {
		if v := req.GetString("lane_fill_"+k, ""); v != "" {
			lane[k] = v
		}
	}
	return lane
}

// LaneFillToolNames lists the lane fill tools, for the parity test.
func LaneFillToolNames() []string {
	s := &Server{}
	return []string{s.toolAutonomousLaneFillStatus().Name, s.toolAutonomousPRDSetLanePreference().Name}
}
