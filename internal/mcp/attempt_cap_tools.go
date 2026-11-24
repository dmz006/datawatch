// Attempt cap MCP surface: the autonomous.attempt_cap.* arguments of
// autonomous_config_set. The block holds the daemon-owned total attempt cap and
// its back-off; the per-task count is part of every task the Automaton tools
// return (attempts_total), and the operator's force reset is
// autonomous_prd_reset_task with force.
package mcp

import (
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// attemptCapConfigOptions are the autonomous_config_set arguments for the
// autonomous.attempt_cap block. They are sent as dotted keys on PUT /api/config,
// where the daemon checks each value and names the valid ones in a refusal.
func attemptCapConfigOptions() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithBoolean("attempt_cap_enabled", mcpsdk.Description("autonomous.attempt_cap.enabled — count every attempt a task is started for, across cancels, resets and restarts, and hold the task when the cap is reached (default true)")),
		mcpsdk.WithNumber("attempt_cap_max_total", mcpsdk.Description("autonomous.attempt_cap.max_total — attempts a task may be started for in all; a whole number from 1 to 1000 (default 6). At the cap the task is held and only the operator's force reset starts it again. A project can set its own cap in its policy block.")),
		mcpsdk.WithNumber("attempt_cap_free_attempts", mcpsdk.Description("autonomous.attempt_cap.free_attempts — attempts that start with no wait; a whole number from 1 to 1000 (default 3)")),
		mcpsdk.WithArray("attempt_cap_backoff_minutes", mcpsdk.Description("autonomous.attempt_cap.backoff_minutes — minutes to wait before each attempt after the free ones (default [5,15,45]); the last entry repeats; an empty list means no wait. The value replaces the whole list."), mcpsdk.WithNumberItems()),
	}
}

// attemptCapConfigArgs maps the attempt_cap_* arguments of autonomous_config_set
// into PUT /api/config entries keyed autonomous.attempt_cap.<name>. Every
// argument is presence-aware, so an explicit false, 0 or empty list is sent as
// given.
func attemptCapConfigArgs(req mcpsdk.CallToolRequest) map[string]any {
	out := map[string]any{}
	args := req.GetArguments()
	for _, k := range []string{"enabled", "max_total", "free_attempts", "backoff_minutes"} {
		if v, ok := args["attempt_cap_"+k]; ok {
			out["autonomous.attempt_cap."+k] = v
		}
	}
	return out
}
