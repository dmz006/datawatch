// cli_generated_artefacts.go — `datawatch autonomous generated-artefacts`,
// `change-record` and `generated`: read and set the generated-artefact
// switches, read an Automaton's change record, preview or regenerate one
// generated artefact. All of them call the daemon's REST routes.
package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// generatedArtefactConfigKey is the dotted config key of one switch.
func generatedArtefactConfigKey(name string) string {
	return "autonomous.generated_artefacts." + name
}

// parseOnOff turns on/off (and true/false) into a bool.
func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("expected on or off, got %q", s)
}

func newAutonomousGeneratedArtefactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generated-artefacts [get|set <key> <on|off>]",
		Short: "Show or change the switches for the artefacts an Automaton generates",
		Long: "Without arguments, or with get, show the effective generated-artefact switches and the kinds. " +
			"With set <key> <on|off>, switch one artefact on or off (the key is a snake_case name such as gate_report; " +
			"it is saved as autonomous.generated_artefacts.<key> in the daemon config).",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || (len(args) == 1 && args[0] == "get") {
				return daemonJSON(http.MethodGet, "/api/autonomous/generated-artefacts", nil)
			}
			if args[0] != "set" || len(args) != 3 {
				return fmt.Errorf("usage: generated-artefacts [get|set <key> <on|off>]")
			}
			on, err := parseOnOff(args[2])
			if err != nil {
				return fmt.Errorf("usage: generated-artefacts set <key> <on|off>: %w", err)
			}
			return daemonJSON(http.MethodPut, "/api/config", map[string]any{generatedArtefactConfigKey(args[1]): on})
		},
	}
	return cmd
}

func newAutonomousChangeRecordCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "change-record <prd-id>",
		Short: "Show the change record of an Automaton",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodGet, "/api/autonomous/prds/"+url.PathEscape(args[0])+"/change-record", nil)
		},
	}
}

func newAutonomousGeneratedCmd() *cobra.Command {
	var regenerate bool
	cmd := &cobra.Command{
		Use:   "generated <prd-id> <kind> [--regenerate]",
		Short: "Preview a generated artefact of an Automaton, or regenerate it",
		Long: "Show the preview of one generated artefact (kind is one of the kinds listed by generated-artefacts). " +
			"With --regenerate, write the artefact again into the project.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			p := "/api/autonomous/prds/" + url.PathEscape(args[0]) + "/generated/" + url.PathEscape(args[1])
			if regenerate {
				return daemonJSON(http.MethodPost, p+"/regenerate", map[string]any{})
			}
			return daemonJSON(http.MethodGet, p, nil)
		},
	}
	cmd.Flags().BoolVar(&regenerate, "regenerate", false, "write the artefact again instead of previewing it")
	return cmd
}
