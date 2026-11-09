package mcp

// Planning restart policy argument for autonomous_config_set. The policy
// (autonomous.planning_restart_policy) lives in the daemon config, so the
// tool sends it on PUT /api/config. It is validated here before any request
// is made, with the same three values the daemon accepts.

import (
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
)

// planningRestartPolicyPatch returns the daemon-config patch for the
// planning_restart_policy argument, or nil when the caller did not pass it.
// A present argument must be one of reattach, restart or draft; anything
// else is refused with the valid values named, so nothing is sent.
func planningRestartPolicyPatch(req mcpsdk.CallToolRequest) (map[string]any, error) {
	raw, ok := req.GetArguments()["planning_restart_policy"]
	if !ok {
		return nil, nil
	}
	str, isString := raw.(string)
	if !isString || !config.ValidPlanningRestartPolicy(str) {
		return nil, fmt.Errorf(`planning_restart_policy must be "reattach", "restart" or "draft" (got %v)`, raw)
	}
	return map[string]any{"autonomous.planning_restart_policy": config.NormalizePlanningRestartPolicy(str)}, nil
}
