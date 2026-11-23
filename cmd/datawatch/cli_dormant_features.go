// datawatch autonomous dormant-features — the read-only registry of Automata
// settings that are off or in shadow mode: key, state, default, purpose and
// whether the feature was ever exercised. A read: nothing is changed.
// --json prints the raw response untouched.

package main

import (
	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func newAutonomousDormantFeaturesCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "dormant-features",
		Short: "List the Automata settings that are off or in shadow mode",
		Long: "Show the Automata settings whose current value is off or in shadow mode: " +
			"the key, the state, the default, what the feature does and whether it was " +
			"ever exercised. A read: nothing is changed and no Automaton runs. " +
			"Change a setting with config set.\n\n" +
			"--json prints the raw response untouched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/dormant-features", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			parsed, err := autonomous.ParseDormantFeatures(body)
			if err != nil {
				return err
			}
			return autonomous.RenderDormantFeatures(cmd.OutOrStdout(), parsed.Features)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
