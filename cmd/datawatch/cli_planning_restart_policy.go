package main

// Client-side handling of planning_restart_policy for `autonomous config-set`.
// The policy is a daemon-config key, so the CLI takes it out of the JSON body
// and sends it to PUT /api/config. An invalid value is refused here, before any
// request is made.

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/config"
)

// takePlanningRestartPolicy removes planning_restart_policy from a config-set
// body. It returns the daemon-config patch that carries the value, or nil when
// the body does not mention the key. A present value must be reattach, restart
// or draft; anything else is an error and nothing is sent.
func takePlanningRestartPolicy(body map[string]any) (map[string]any, error) {
	raw, ok := body["planning_restart_policy"]
	if !ok {
		return nil, nil
	}
	delete(body, "planning_restart_policy")
	str, isString := raw.(string)
	if !isString || !config.ValidPlanningRestartPolicy(str) {
		return nil, fmt.Errorf(`planning_restart_policy must be "reattach", "restart" or "draft" (got %v)`, raw)
	}
	return map[string]any{"autonomous.planning_restart_policy": config.NormalizePlanningRestartPolicy(str)}, nil
}
