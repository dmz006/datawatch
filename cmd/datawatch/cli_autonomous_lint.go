// cli_autonomous_lint.go — the `datawatch autonomous lint ...` subcommand:
// the decompose-lint config read, the config-set override surface, the
// per-PRD dry-run and the stored findings read. Every verb proxies the
// daemon's REST endpoints for decompose-lint (GET/PUT
// /api/autonomous/decompose-lint/config, POST (dry-run) and GET
// /api/autonomous/prds/{id}/decompose-lint) with the shared daemon
// helpers from cli_sx_parity.go, and prints the daemon's own reply.
//
// The operator-facing wording for the finding lines is shared with the
// comm channel verb (`autonomous lint ...`) in
// internal/autonomous/decompose_lint_text.go, so both surfaces say the
// same thing.
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/spf13/cobra"
)

// newAutonomousLintCmd assembles the decompose-lint read/config surface:
//
//	config                  the live decompose-lint config: mode, enforce
//	                          retry budget, active rules, disabled rule ids
//	config-set key=value ... set mode / max_retries / disabled_rules,
//	                          preserving the active rule pack
//	dry-run <id>            lint one Automaton's stored plan read-only
//	findings <id>           the decompose-lint record stored for one
//	                          Automaton
//
// Every verb reaches the daemon's REST endpoints for decompose-lint
// (GET/PUT /api/autonomous/decompose-lint/config, POST (dry-run) and GET
// /api/autonomous/prds/{id}/decompose-lint) with the shared daemon
// helpers, so the CLI says what the daemon says.
func newAutonomousLintCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "lint",
		Short: "Decompose-lint config, dry-run, and the findings one automation stored",
		Long: `Decompose lint: the quality check the decomposer runs over each new
decomposition — the rule pack (bare verbs, missing test mention, ...),
enforced in enforce mode with a retry budget, or recorded only in shadow.

config
    Read the live decompose-lint config: the mode, the enforce retry
    budget, the active rule pack and the disabled rule ids.

config-set key=value ...
    Set decompose-lint keys, preserving the rest of the block (notably
    the active rule pack) — the daemon's PUT is the gate and returns the
    new config. Allowed:
        mode            off | shadow | enforce
        max_retries     whole number >= 0 (the enforce retry budget)
        disabled_rules  comma list of rule ids (empty value clears it)

dry-run <id>
    Lint one Automaton's stored plan WITHOUT storing or retrying: which
    rules fire, on which tasks, at the current config.

findings <id>
    The decompose-lint record the executor stored for one Automaton:
    the mode, the attempts and the final findings, one line each.
    When nothing is stored the daemon says so (mode off records nothing).

Every verb proxies the daemon's REST endpoints for decompose-lint and
prints the daemon's own reply; with --json the raw daemon reply is
printed instead of the operator-facing text.
Exit codes: 0 on success, 1 otherwise (bad key=value, bad id shape,
daemon error).`,
	}
	c.AddCommand(
		newAutonomousLintConfigCmd(),
		newAutonomousLintConfigSetCmd(),
		newAutonomousLintDryRunCmd(),
		newAutonomousLintFindingsCmd(),
	)
	return c
}

// autonomousLintDo proxies one daemon REST call for decompose-lint with
// the shared daemon helpers (same URL resolution, auth and error wording
// as every other daemon-facing CLI verb) and returns the raw reply.
func autonomousLintDo(method, path string, body any) (string, error) {
	raw, err := daemonJSONBody(method, path, body)
	if err != nil {
		return "", fmt.Errorf("decompose lint: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// newAutonomousLintConfigCmd — `lint config`: the live decompose-lint
// config (read only).
func newAutonomousLintConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Read the decompose-lint config (mode, retry budget, rules)",
		Long: `Read the live decompose-lint config: the mode, the enforce retry
budget, the active rule pack and the disabled rule ids. Read only —
changes go through config-set.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			body, err := autonomousLintDo("GET", "/api/autonomous/decompose-lint/config", nil)
			if err != nil {
				return err
			}
			if asJSON {
				fmt.Println(body)
				return nil
			}
			prettyPrint([]byte(body))
			return nil
		},
	}
	c.Flags().Bool("json", false, "print the raw daemon reply instead of pretty JSON")
	return c
}

// newAutonomousLintConfigSetCmd — `lint config-set key=value ...`: set
// decompose-lint keys, merging over the live block so the untouched keys
// (notably the active rule pack) survive, the way config-set for the
// judge policy does.
func newAutonomousLintConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config-set key=value ...",
		Short: "Set decompose-lint mode / max_retries / disabled_rules, preserving the active rules",
		Long: `Set decompose-lint config keys, preserving everything the daemon
already carries that the key=value list does not touch (notably the
active rule pack) — the daemon's PUT on
/api/autonomous/decompose-lint/config is the final gate and returns the
new config, which is printed.

Allowed keys:
    mode            off | shadow | enforce
    max_retries     whole number >= 0 (the enforce retry budget)
    disabled_rules  comma list of rule ids (an empty value clears it)

Example:
    datawatch autonomous lint config-set mode=enforce max_retries=2`,
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("expected at least one of mode, max_retries, disabled_rules as key=value")
			}
			overrides, err := autonomous.ParseDecomposeLintOverrides(args)
			if err != nil {
				return fmt.Errorf("decompose lint config-set: %w", err)
			}
			live, err := autonomousLintDo("GET", "/api/autonomous/decompose-lint/config", nil)
			if err != nil {
				return err
			}
			block, err := autonomous.MergeDecomposeLintBlock([]byte(live), overrides)
			if err != nil {
				return fmt.Errorf("decompose lint config-set: %w", err)
			}
			putBody, err := json.Marshal(map[string]any{"decompose_lint": json.RawMessage(block)})
			if err != nil {
				return fmt.Errorf("cannot encode the merged block: %w", err)
			}
			out, err := autonomousLintDo("PUT", "/api/autonomous/decompose-lint/config", putBody)
			if err != nil {
				return err
			}
			prettyPrint([]byte(out))
			return nil
		},
	}
}

// newAutonomousLintDryRunCmd — `lint dry-run <id>`: lint one stored plan
// read-only and print the daemon's report, one finding line each.
func newAutonomousLintDryRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "dry-run <id>",
		Short: "Lint one Automaton's stored plan read-only (nothing is stored or retried)",
		Long: `Lint one Automaton's stored plan WITHOUT storing or retrying:
which rules fire, on which tasks, at the current config. Prints the
daemon's report — one line per finding, in the shared surface wording
[severity] rule_id task: message.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			body, err := autonomousLintDo("POST", "/api/autonomous/prds/"+args[0]+"/decompose-lint", nil)
			if err != nil {
				return err
			}
			if asJSON {
				fmt.Println(body)
				return nil
			}
			var rep autonomous.DecomposeLintReport
			if err := json.Unmarshal([]byte(body), &rep); err != nil {
				return fmt.Errorf("the daemon's dry-run reply is not a decompose-lint report: %w", err)
			}
			fmt.Println(autonomous.RenderDecomposeLintReport(rep))
			return nil
		},
	}
	c.Flags().Bool("json", false, "print the raw daemon reply instead of the finding lines")
	return c
}

// newAutonomousLintFindingsCmd — `lint findings <id>`: the decompose-lint
// record the executor stored for one Automaton.
func newAutonomousLintFindingsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "findings <id>",
		Short: "The decompose-lint findings one Automaton's executor stored",
		Long: `The decompose-lint record the executor stored for one Automaton:
the mode, the attempts and the final findings, one line each
([severity] rule_id task: message). When nothing is stored the daemon
says so — mode off records nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			body, err := autonomousLintDo("GET", "/api/autonomous/prds/"+args[0]+"/decompose-lint", nil)
			if err != nil {
				return err
			}
			if asJSON {
				fmt.Println(body)
				return nil
			}
			var stored struct {
				DecomposeLint *autonomous.DecomposeLintRecord `json:"decompose_lint"`
			}
			if err := json.Unmarshal([]byte(body), &stored); err != nil {
				return fmt.Errorf("the daemon's reply is not a stored decompose-lint record: %w", err)
			}
			fmt.Println(autonomous.RenderDecomposeLintRecord(args[0], stored.DecomposeLint))
			return nil
		},
	}
	c.Flags().Bool("json", false, "print the raw daemon reply instead of the finding lines")
	return c
}
