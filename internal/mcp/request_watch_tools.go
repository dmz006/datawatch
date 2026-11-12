package mcp

import (
	"context"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The request-watch tool is read-only. autonomous_request_watch proxies
// GET /api/autonomous/request-watch: the watch settings in effect and the
// observation of every worker or verifier session whose model request is in
// flight on a compute node. It makes no model call and ends nothing; it only
// reports what the daemon's watch already holds.

func (s *Server) toolAutonomousRequestWatch() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_request_watch",
		mcpsdk.WithDescription("Read the in-flight model request of every watched worker or verifier session whose model runs on a registered compute node, so a hung request can be told from a slow one. The object carries the watch settings in effect (\"enabled\", \"poll_seconds\", \"no_progress_minutes\") and \"observations\" (an empty list when nothing is watched). Each observation names the session (\"session_id\"), the Automaton (\"automaton_id\"), the task (\"task_id\"), the \"role\" (worker or verifier), the \"node\" and the \"model\", the \"state\" (generating, idle, unreachable or unknown before the first sample), when the state last changed (\"last_change\", RFC 3339), \"no_progress_seconds\", \"last_poll\", whether the no-progress threshold is passed (\"over_threshold\"), an \"error\" when the node could not be read, and a plain-text \"summary\". Read-only; the same object as GET /api/autonomous/request-watch. It makes no model call and ends, retries or unloads nothing."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
		mcpsdk.WithString("session", mcpsdk.Description("Optional session ID; narrows the observations to that session.")),
		mcpsdk.WithString("automaton", mcpsdk.Description("Optional Automaton ID; narrows the observations to that Automaton.")),
	)
}

// RequestWatchToolNames lists the request-watch tools, for the parity test.
func RequestWatchToolNames() []string {
	return []string{"autonomous_request_watch"}
}

func (s *Server) handleAutonomousRequestWatch(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if id := req.GetString("session", ""); id != "" {
		q.Set("session", id)
	}
	if id := req.GetString("automaton", ""); id != "" {
		q.Set("automaton", id)
	}
	out, err := s.proxyGet("/api/autonomous/request-watch", q)
	if err != nil {
		return toolError("autonomous_request_watch", err), nil
	}
	return textOK(string(out)), nil
}

// requestWatchConfigKeys are the eleven autonomous.request_watch keys, in the
// order autonomous_config_set declares them. The argument name is
// request_watch_<key>.
var requestWatchConfigKeys = []string{
	"enabled", "poll_seconds", "no_progress_minutes", "observe_workers",
	"observe_verifiers", "use_gpu_signal", "gpu_busy_percent",
	"unreachable_counts", "raise_alert", "raise_question", "remediation_options",
}

// requestWatchConfigOptions are the autonomous_config_set arguments for the
// autonomous.request_watch block. They are sent as dotted keys on PUT
// /api/config, where the daemon checks each value and names the valid ones in
// a refusal.
func requestWatchConfigOptions() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithBoolean("request_watch_enabled", mcpsdk.Description("autonomous.request_watch.enabled — observe the in-flight model request of worker and verifier sessions on compute nodes (default true). Observation only: nothing is ended, retried or unloaded.")),
		mcpsdk.WithNumber("request_watch_poll_seconds", mcpsdk.Description("autonomous.request_watch.poll_seconds — seconds between samples of a watched request; a whole number of 1 or more (default 30)")),
		mcpsdk.WithNumber("request_watch_no_progress_minutes", mcpsdk.Description("autonomous.request_watch.no_progress_minutes — minutes without progress before a request counts as over the threshold; a whole number of 1 or more (default 30)")),
		mcpsdk.WithBoolean("request_watch_observe_workers", mcpsdk.Description("autonomous.request_watch.observe_workers — watch worker sessions (default true)")),
		mcpsdk.WithBoolean("request_watch_observe_verifiers", mcpsdk.Description("autonomous.request_watch.observe_verifiers — watch verifier sessions (default true)")),
		mcpsdk.WithBoolean("request_watch_use_gpu_signal", mcpsdk.Description("autonomous.request_watch.use_gpu_signal — read the node's GPU load to tell a generating request from an idle one (default true)")),
		mcpsdk.WithNumber("request_watch_gpu_busy_percent", mcpsdk.Description("autonomous.request_watch.gpu_busy_percent — GPU utilisation percent at or above which the node counts as generating; a whole number from 1 to 100 (default 10)")),
		mcpsdk.WithBoolean("request_watch_unreachable_counts", mcpsdk.Description("autonomous.request_watch.unreachable_counts — time with an unreachable node counts as no progress (default true)")),
		mcpsdk.WithBoolean("request_watch_raise_alert", mcpsdk.Description("autonomous.request_watch.raise_alert — raise an alert when a request passes the no-progress threshold (default true)")),
		mcpsdk.WithBoolean("request_watch_raise_question", mcpsdk.Description("autonomous.request_watch.raise_question — raise a non-blocking operator question when a request passes the threshold; answering it only records the choice (default true)")),
		mcpsdk.WithAny("request_watch_remediation_options", mcpsdk.Description("autonomous.request_watch.remediation_options — the options the question offers besides keep_waiting, which is always offered and is the default answer: a list or a comma separated string of end_worker, retry_other_backend and unload_reload_model. Unset offers all of them; an empty list offers keep_waiting only. The daemon names the valid options in its refusal. The value replaces the whole list.")),
	}
}

// requestWatchConfigArgs maps the request_watch_* arguments of
// autonomous_config_set into PUT /api/config entries keyed
// autonomous.request_watch.<name>. Every argument is presence-aware, so an
// explicit false, 0 or empty list is sent as given. A comma separated
// remediation_options string is split into a list; an empty string is an
// explicit empty list.
func requestWatchConfigArgs(req mcpsdk.CallToolRequest) map[string]any {
	out := map[string]any{}
	args := req.GetArguments()
	for _, k := range requestWatchConfigKeys {
		v, ok := args["request_watch_"+k]
		if !ok {
			continue
		}
		if k == "remediation_options" {
			v = requestWatchOptionList(v)
		}
		out["autonomous.request_watch."+k] = v
	}
	return out
}

// requestWatchOptionList turns a remediation_options argument, a list or a
// comma separated string, into a list of trimmed ids.
func requestWatchOptionList(v any) any {
	text, ok := v.(string)
	if !ok {
		return v
	}
	list := []string{}
	for _, part := range strings.Split(text, ",") {
		if p := strings.TrimSpace(part); p != "" {
			list = append(list, p)
		}
	}
	return list
}
