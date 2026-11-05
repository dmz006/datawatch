// CLI for the classification judge: probe and calibration status, the publisher
// allowlist and verdict records. They call the REST routes under
// /api/llm/classifier. verification_mode itself is set with
// `datawatch llm add|update --verification-mode`.

package main

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newLLMClassifierCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "classifier",
		Short: "Classification judge: status, publisher allowlist, verdict records",
		Long: `The classification judge scores a closed label set instead of generating
text. status shows each LLM's verification mode, the capability probe status
per node and model, the calibration status and the publisher allowlist.
publishers lists the allowlist; add-publisher adds one and needs --confirm
(it widens which models may judge, and every addition is audited). verdicts
shows an Automaton's verdict records: mode, score vector, route and outcome.
Set an LLM's mode with: datawatch llm update <name> --verification-mode classify.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Verification modes, capability probes, calibration status and the publisher allowlist",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/llm/classifier") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "publishers",
		Short: "List the publisher allowlist",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/llm/classifier/publishers") },
	})
	var confirm bool
	add := &cobra.Command{
		Use:   "add-publisher <publisher> --confirm",
		Short: "Add a model publisher to the allowlist (needs --confirm; audited)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("adding publisher %q widens which models may judge; rerun with --confirm to confirm", args[0])
			}
			return daemonJSON(http.MethodPost, "/api/llm/classifier/publishers",
				map[string]any{"publisher": args[0], "confirm": true, "actor": "operator"})
		},
	}
	add.Flags().BoolVar(&confirm, "confirm", false, "explicit operator confirmation (required)")
	cmd.AddCommand(add)

	var taskID string
	var low bool
	verdicts := &cobra.Command{
		Use:   "verdicts <automaton-id> [--task <task-id>] [--low-confidence]",
		Short: "An Automaton's verdict records: mode, scores, route, outcome",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			q := url.Values{}
			q.Set("automaton", args[0])
			if taskID != "" {
				q.Set("task", taskID)
			}
			if low {
				q.Set("low_confidence", "true")
			}
			return daemonGet("/api/llm/classifier/verdicts?" + q.Encode())
		},
	}
	verdicts.Flags().StringVar(&taskID, "task", "", "narrow to one task")
	verdicts.Flags().BoolVar(&low, "low-confidence", false, "only blocks reached for low confidence; decide each with `datawatch autonomous resolve-review`")
	cmd.AddCommand(verdicts)
	return cmd
}
