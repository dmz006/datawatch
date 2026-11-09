package router

// Comm-channel refusal for autonomous.planning_restart_policy. The configure
// verb sends the key to the daemon config; this check stops a bad value before
// it is sent and names the values the daemon accepts.

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/config"
)

// planningRestartPolicyRefusal returns the reply text for a configure request
// that names autonomous.planning_restart_policy with a value the daemon does
// not accept. It returns "" when the request may go on (any other key, or a
// valid value).
func planningRestartPolicyRefusal(key, value string) string {
	if key != "autonomous.planning_restart_policy" || config.ValidPlanningRestartPolicy(value) {
		return ""
	}
	return fmt.Sprintf("Configure refused: autonomous.planning_restart_policy must be reattach, restart or draft (got %q).", value)
}
