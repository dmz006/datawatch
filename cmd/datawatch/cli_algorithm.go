// BL258 v6.9.0 — CLI subcommands for Algorithm Mode (7-phase per-session harness).
//
//	datawatch algorithm list
//	datawatch algorithm get <session-id>
//	datawatch algorithm start <session-id>
//	datawatch algorithm advance <session-id> [--output "..."]
//	datawatch algorithm edit <session-id> --output "..."
//	datawatch algorithm abort <session-id>
//	datawatch algorithm reset <session-id>

package main

import (
	"net/http"

	"github.com/spf13/cobra"
)

func newAlgorithmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "algorithm",
		Short: "Drive the 7-phase Algorithm Mode harness on a session (BL258)",
		Long: `Algorithm Mode is the PAI 7-phase decision/execution harness:
Observe → Orient → Decide → Act → Measure → Learn → Improve.

State is operator-driven for v6.9.0 — call advance to close the
current phase and move on. Auto-detection from LLM output is a
follow-up.`,
	}
	cmd.AddCommand(newAlgorithmListCmd())
	cmd.AddCommand(newAlgorithmGetCmd())
	cmd.AddCommand(newAlgorithmStartCmd())
	cmd.AddCommand(newAlgorithmAdvanceCmd())
	cmd.AddCommand(newAlgorithmEditCmd())
	cmd.AddCommand(newAlgorithmAbortCmd())
	cmd.AddCommand(newAlgorithmResetCmd())
	return cmd
}

func newAlgorithmListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every session currently in Algorithm Mode",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/algorithm") },
	}
}
func newAlgorithmGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <session-id>",
		Short: "Read one session's algorithm state",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/algorithm/" + args[0]) },
	}
}
func newAlgorithmStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <session-id>",
		Short: "Register a session in Algorithm Mode (begins at Observe)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/algorithm/"+args[0]+"/start", nil)
		},
	}
}
func newAlgorithmAdvanceCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "advance <session-id>",
		Short: "Close the current phase by recording its output and advance to the next",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/algorithm/"+args[0]+"/advance",
				map[string]any{"output": output})
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "free-form text captured for this phase")
	return cmd
}
func newAlgorithmEditCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "edit <session-id>",
		Short: "Replace the output captured at the most recent phase gate",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/algorithm/"+args[0]+"/edit",
				map[string]any{"output": output})
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "revised phase output (required)")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}
func newAlgorithmAbortCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "abort <session-id>",
		Short: "Terminate the algorithm mid-flight",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/algorithm/"+args[0]+"/abort", nil)
		},
	}
}
func newAlgorithmResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset <session-id>",
		Short: "Discard the session's algorithm state",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/algorithm/"+args[0], nil)
		},
	}
}
