package mcp

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The infrastructure retry tool is read-only and proxies
// GET /api/autonomous/infra-retries. The retry policy itself is set through
// the existing config tools (config_set with autonomous.infra_retry.* keys, or
// the infra_retry_* arguments of autonomous_config_set).

func (s *Server) toolAutonomousInfraRetries() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_infra_retries",
		mcpsdk.WithDescription("Read the infrastructure retry state: the retry policy in effect and the tasks of Automata that were retried, or are held, because the machinery around them failed (a spawn error, a killed session, a daemon restart or a storage write) rather than because of their own work. The most recently retried task comes first. Read-only; same object as GET /api/autonomous/infra-retries. automaton limits the list to one Automaton; limit sets how many tasks come back (a whole number from 1 to 1000, default 50)."),
		mcpsdk.WithString("automaton", mcpsdk.Description("Optional — an Automaton ID; only its tasks are listed")),
		mcpsdk.WithNumber("limit", mcpsdk.Description("Optional — how many tasks to return, a whole number from 1 to 1000 (default 50)")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleAutonomousInfraRetries(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	args := req.GetArguments()
	if v, ok := args["automaton"].(string); ok && strings.TrimSpace(v) != "" {
		q.Set("automaton", strings.TrimSpace(v))
	}
	// limit arrives as a number, or as text from a client that quotes it; the
	// REST route decides whether it is valid.
	switch v := args["limit"].(type) {
	case string:
		if v != "" {
			q.Set("limit", v)
		}
	case float64:
		q.Set("limit", strconv.FormatFloat(v, 'f', -1, 64))
	}
	out, err := s.proxyGet("/api/autonomous/infra-retries", q)
	if err != nil {
		return toolError("autonomous_infra_retries", err), nil
	}
	return textOK(string(out)), nil
}

// infraRetryConfigOptions are the autonomous_config_set arguments for the
// autonomous.infra_retry block. They are sent as dotted keys on PUT /api/config,
// where the daemon checks each value and names the valid ones in a refusal.
func infraRetryConfigOptions() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithBoolean("infra_retry_enabled", mcpsdk.Description("autonomous.infra_retry.enabled — retry a task automatically when the machinery around it failed (default true)")),
		mcpsdk.WithNumber("infra_retry_max_tries", mcpsdk.Description("autonomous.infra_retry.max_tries — tries a task gets before the exhausted action applies; a whole number from 1 to 1000 (default 3)")),
		mcpsdk.WithArray("infra_retry_backoff_seconds", mcpsdk.Description("autonomous.infra_retry.backoff_seconds — delay in seconds before each retry (default [60,300,540]); the last entry repeats when there are more tries than entries; an empty list means retry with no delay. The value replaces the whole list."), mcpsdk.WithNumberItems()),
		mcpsdk.WithArray("infra_retry_categories", mcpsdk.Description("autonomous.infra_retry.categories — failure classes treated as transient (default all four; an empty list retries none). The daemon names the valid classes in its refusal. The value replaces the whole list."), mcpsdk.WithStringItems()),
		mcpsdk.WithString("infra_retry_exhausted_action", mcpsdk.Enum("hold", "count"), mcpsdk.Description("autonomous.infra_retry.exhausted_action — what happens when the tries are spent: hold (the default) holds the task for the operator, count books it as an ordinary failure")),
		mcpsdk.WithBoolean("infra_retry_retry_dependents", mcpsdk.Description("autonomous.infra_retry.retry_dependents — also retry tasks that were blocked behind the failed one (default true)")),
		mcpsdk.WithBoolean("infra_retry_raise_question", mcpsdk.Description("autonomous.infra_retry.raise_question — raise an operator question when the tries are spent (default true)")),
	}
}

// infraRetryConfigArgs maps the infra_retry_* arguments of
// autonomous_config_set into PUT /api/config entries keyed
// autonomous.infra_retry.<name>. Every argument is presence-aware, so an
// explicit false, 0 or empty list is sent as given.
func infraRetryConfigArgs(req mcpsdk.CallToolRequest) map[string]any {
	out := map[string]any{}
	args := req.GetArguments()
	for _, k := range []string{"enabled", "max_tries", "backoff_seconds", "categories", "exhausted_action", "retry_dependents", "raise_question"} {
		if v, ok := args["infra_retry_"+k]; ok {
			out["autonomous.infra_retry."+k] = v
		}
	}
	return out
}
