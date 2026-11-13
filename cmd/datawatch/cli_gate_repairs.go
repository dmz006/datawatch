// datawatch autonomous gate-repairs — the gate repair engine read over the
// CLI: the mode and the engine's records, newest first, each saying what was
// found, what was done and how to undo it. --json prints the body untouched.
// The engine mode changes only through the existing config set paths.

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func newAutonomousGateRepairsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "gate-repairs",
		Short: "Read the gate repair engine: the mode and its repair records, newest first",
		Long: "Show what the gate repair engine has found and done on failed gates: the mode in " +
			"effect (off, report or on), its records newest first — for each failed gate the " +
			"failure class found, what it would do or did, and how to undo it — and how much " +
			"repair budget has been spent. A read: nothing is repaired, no alert is raised and " +
			"no mode changes. Change the mode with config set, key autonomous.gate_repair.mode.\n\n" +
			"--json prints the raw response untouched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/gate-repairs", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var parsed autonomous.GateRepairStatus
			if err := json.Unmarshal(body, &parsed); err != nil {
				return err
			}
			buf := &strings.Builder{}
			autonomous.RenderGateRepairs(buf, &parsed)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
