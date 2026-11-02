package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newAutomationSetEffortCeilingCmd builds "automation set-effort-ceiling <id>":
// it posts the per-task-kind effort ceiling to the Automation's
// set_effort_ceiling control endpoint. The daemon validates the kinds and values.
func newAutomationSetEffortCeilingCmd() *cobra.Command {
	var f automationControlFlags
	var clear bool
	cmd := &cobra.Command{
		Use:   "set-effort-ceiling <id> [kind=ceiling ...]",
		Short: "Set or clear the effort ceiling of an Automation",
		Long: `Set the effort ceiling of an Automation as kind=ceiling pairs. Kinds are docs,
parity, live_check and final_gate; ceilings are low, medium, high and
cheaper_backend. A ceiling only lowers a task's effort or moves it to the
cheaper backend; it never raises one. --clear (or no pairs) removes the ceiling.
The daemon validates the values. --expected-version must match the Automation's
current version or the daemon refuses with HTTP 409.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ceiling := map[string]string{}
			if !clear {
				for _, tok := range args[1:] {
					k, v, ok := strings.Cut(tok, "=")
					if !ok || k == "" || v == "" {
						return fmt.Errorf("%q: expected kind=ceiling, for example docs=low", tok)
					}
					ceiling[k] = v
				}
			} else if len(args) > 1 {
				return fmt.Errorf("--clear cannot be combined with kind=ceiling pairs")
			}
			body := f.body(cmd)
			body["effort_ceiling"] = ceiling
			return runAutomationControl(args[0], "set-effort-ceiling", body)
		},
	}
	f.register(cmd)
	cmd.Flags().BoolVar(&clear, "clear", false, "Remove the effort ceiling")
	return cmd
}
