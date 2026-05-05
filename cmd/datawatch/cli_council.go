// BL260 v6.11.0 — CLI subcommands for Council Mode (multi-agent debate).
//
//	datawatch council personas
//	datawatch council run --proposal "..." [--personas a,b] [--mode debate|quick]
//	datawatch council runs [--limit N]
//	datawatch council get-run <id>

package main

import (
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

func newCouncilCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "council",
		Short: "Run Council Mode multi-agent debates (BL260)",
		Long: `Council runs a structured multi-persona debate on a proposal.

Two modes:
  debate (3 rounds)  — for serious decisions
  quick  (1 round)   — for fast perspective checks

Built-in 6 personas: security-skeptic, ux-advocate, perf-hawk,
simplicity-advocate, ops-realist, contrarian. Operators may add more
by dropping YAML files into ~/.datawatch/council/personas/.

v6.11.0 ships the framework with stubbed LLM responses (deterministic
placeholders). Real per-persona inference lands in a v6.11.x follow-up.`,
	}
	cmd.AddCommand(newCouncilPersonasCmd())
	cmd.AddCommand(newCouncilRunCmd())
	cmd.AddCommand(newCouncilRunsCmd())
	cmd.AddCommand(newCouncilGetRunCmd())
	return cmd
}

func newCouncilPersonasCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "personas",
		Short: "List registered Council personas",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/council/personas") },
	}
}

func newCouncilRunCmd() *cobra.Command {
	var proposal, personas, mode string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a Council debate on a proposal",
		RunE: func(_ *cobra.Command, _ []string) error {
			body := map[string]any{"proposal": proposal, "mode": mode}
			if personas != "" {
				parts := []string{}
				for _, p := range strings.Split(personas, ",") {
					if p = strings.TrimSpace(p); p != "" {
						parts = append(parts, p)
					}
				}
				body["personas"] = parts
			}
			return daemonJSON(http.MethodPost, "/api/council/run", body)
		},
	}
	cmd.Flags().StringVar(&proposal, "proposal", "", "the proposal text (required)")
	cmd.Flags().StringVar(&personas, "personas", "", "comma-separated persona names (default: all)")
	cmd.Flags().StringVar(&mode, "mode", "quick", "debate or quick")
	_ = cmd.MarkFlagRequired("proposal")
	return cmd
}

func newCouncilRunsCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List past Council runs",
		RunE: func(_ *cobra.Command, _ []string) error {
			path := "/api/council/runs"
			if limit > 0 {
				path = path + "?limit=" + itoa(limit)
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "max runs to return")
	return cmd
}

func newCouncilGetRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get-run <id>",
		Short: "Fetch one Council run by id",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/council/runs/" + args[0]) },
	}
}
