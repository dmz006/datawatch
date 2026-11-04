package main

import (
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

// traceQuery builds the query string of a trace request from the flag
// values; unset flags are left out so the daemon's defaults apply.
func traceQuery(kv map[string]string, errorsOnly bool, limit, loopThreshold int) string {
	q := url.Values{}
	for k, v := range kv {
		if v != "" {
			q.Set(k, v)
		}
	}
	if errorsOnly {
		q.Set("errors", "true")
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if loopThreshold > 0 {
		q.Set("loop_threshold", strconv.Itoa(loopThreshold))
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// newTraceCmd is `datawatch trace session|automaton|rollup|coverage`, the
// CLI face of GET /api/trace/*. The tracing.* settings are read and changed
// with `datawatch config`.
func newTraceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Agent trace: session and Automaton traces, roll-ups, coverage",
		Long: "Reads the trace the daemon records of what agents did: tool calls, results, model\n" +
			"requests, state changes, capability and permission decisions, loop warnings and\n" +
			"errors. Event text is redacted when it is stored. Tracing is set with the\n" +
			"tracing.* settings (mode, arg_cap_bytes, retention_days, backends).",
	}
	cmd.AddCommand(newTraceEventsCmd("session", "Trace of one session", "/api/trace/session/"))
	cmd.AddCommand(newTraceEventsCmd("automaton", "Trace of one Automaton (all its sessions)", "/api/trace/automaton/"))
	cmd.AddCommand(newTraceRollupCmd(), newTraceCoverageCmd())
	return cmd
}

func newTraceEventsCmd(verb, short, base string) *cobra.Command {
	var tool, backend, kind string
	var errorsOnly bool
	var limit, loop int
	c := &cobra.Command{
		Use:   verb + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := base + url.PathEscape(args[0]) + traceQuery(map[string]string{"tool": tool, "backend": backend, "kind": kind}, errorsOnly, limit, loop)
			return daemonGet(path)
		},
	}
	c.Flags().StringVar(&tool, "tool", "", "Keep events with this tool name")
	c.Flags().StringVar(&backend, "backend", "", "Keep events from this backend")
	c.Flags().StringVar(&kind, "kind", "", "Keep one event kind (tool_call, tool_result, model_request, state_change, capability_check, permission_decision, loop_warning, error)")
	c.Flags().BoolVar(&errorsOnly, "errors", false, "Keep only error events and failed outcomes")
	c.Flags().IntVar(&limit, "limit", 0, "Keep only the last N events (0 = all)")
	c.Flags().IntVar(&loop, "loop-threshold", 0, "Repeat count that raises a loop warning in the roll-up (default 5)")
	return c
}

func newTraceRollupCmd() *cobra.Command {
	var dimension, key string
	var loop int
	c := &cobra.Command{
		Use:   "rollup",
		Short: "Roll up the trace along session, task, story, automaton, agent or backend",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return daemonGet("/api/trace/rollup" + traceQuery(map[string]string{"dimension": dimension, "key": key}, false, 0, loop))
		},
	}
	c.Flags().StringVar(&dimension, "dimension", "", "session (default), task, story, automaton, agent or backend")
	c.Flags().StringVar(&key, "key", "", "Keep only this group")
	c.Flags().IntVar(&loop, "loop-threshold", 0, "Repeat count that raises a loop warning (default 5)")
	return c
}

func newTraceCoverageCmd() *cobra.Command {
	var backend string
	c := &cobra.Command{
		Use:   "coverage",
		Short: "How much of each session's backend activity the trace captures",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return daemonGet("/api/trace/coverage" + traceQuery(map[string]string{"backend": backend}, false, 0, 0))
		},
	}
	c.Flags().StringVar(&backend, "backend", "", "Only this backend")
	return c
}
