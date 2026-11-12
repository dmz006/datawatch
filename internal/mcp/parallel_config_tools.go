package mcp

import (
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// parallelConfigOptions are the autonomous_config_set arguments for the
// autonomous.parallel block. They are sent as dotted keys on PUT /api/config,
// where the daemon checks each value and names the valid ones in a refusal.
func parallelConfigOptions() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithBoolean("parallel_read_only_tasks", mcpsdk.Description("autonomous.parallel.read_only_tasks — tasks flagged read_only skip the tree lock (default true).")),
		mcpsdk.WithBoolean("parallel_worktree_per_task", mcpsdk.Description("autonomous.parallel.worktree_per_task — each parallel task runs in its own worktree and branch, and the branch is merged back into the PRD branch in dependency order (default true).")),
		mcpsdk.WithString("parallel_merge_conflict_policy", mcpsdk.Enum("additive_then_model", "block", "triage"), mcpsdk.Description("autonomous.parallel.merge_conflict_policy — what happens when a task branch conflicts on merge: additive_then_model (keep both sides of additive changes, then ask the model; the default), block (hold the task), triage (raise it for an operator decision).")),
		mcpsdk.WithString("parallel_merge_conflict_model", mcpsdk.Description("autonomous.parallel.merge_conflict_model — the model that resolves a merge conflict; empty (the default) uses the strongest configured planning model.")),
		mcpsdk.WithNumber("parallel_merge_retry_seconds", mcpsdk.Min(float64(0)), mcpsdk.Description("autonomous.parallel.merge_retry_seconds — whole seconds between timed merge retry passes (default 60; 0 turns the timer off; negative is refused).")),
	}
}

// parallelConfigArgs maps the parallel_* arguments of autonomous_config_set
// into PUT /api/config entries keyed autonomous.parallel.<name>. Every
// argument is presence-aware, so an explicit false or empty string is sent
// as given.
func parallelConfigArgs(req mcpsdk.CallToolRequest) map[string]any {
	out := map[string]any{}
	args := req.GetArguments()
	for _, k := range []string{"read_only_tasks", "worktree_per_task", "merge_conflict_policy", "merge_conflict_model", "merge_retry_seconds"} {
		if v, ok := args["parallel_"+k]; ok {
			out["autonomous.parallel."+k] = v
		}
	}
	return out
}
