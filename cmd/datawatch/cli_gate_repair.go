// datawatch autonomous gate-repair status [--json]
// datawatch autonomous gate-repair run [--automaton ID] [--task ID] [--json]
//
// Read the gate repair engine, and run one gate repair pass (or repair one
// failed task gate). The text is the one renderer the comm channels use;
// --json prints the REST body untouched. Nothing here changes the engine
// mode or the repair budget.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// gateRepairRunResponse is the POST /api/autonomous/gate-repair/run body:
// the run's outcomes and the engine's fresh status.
type gateRepairRunResponse struct {
	Runs   []autonomous.GateRepairOutcome `json:"runs"`
	Status autonomous.GateRepairStatus    `json:"status"`
}

// gateRepairPost POSTs one loopback-style request to the daemon and returns
// the body untouched (the progressGet twin for the gate repair run).
func gateRepairPost(path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(http.MethodPost, daemonURL()+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(out))
	}
	return out, nil
}

// newAutonomousGateRepairCmd is the `autonomous gate-repair` surface: the
// engine read (status) and the run verb (one pass, or one failed gate).
func newAutonomousGateRepairCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "gate-repair",
		Short: "Gate repair engine: the status read, and a repair pass (or one failed gate)",
		Long: `Gate repair engine over the daemon:

  gate-repair status          the mode, the repair records and the spent budget
  gate-repair run             one pass across the daemon's failed task gates
  gate-repair run --automaton ID --task ID
                              repair one failed task gate

Nothing changes the engine mode or the repair budget.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Usage() // bare use: the two subcommands
			}
			return fmt.Errorf("unknown subcommand %q (want status or run)", strings.Join(args, " "))
		},
	}
	parent.AddCommand(newAutonomousGateRepairStatusCmd(), newAutonomousGateRepairRunCmd())
	return parent
}

// newAutonomousGateRepairStatusCmd is `gate-repair status`.
func newAutonomousGateRepairStatusCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Show the gate repair engine's status (read only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/gate-repair", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err := cmd.OutOrStdout().Write(body)
				return err
			}
			var s autonomous.GateRepairStatus
			if err := json.Unmarshal(body, &s); err != nil {
				return fmt.Errorf("unexpected daemon reply: %w", err)
			}
			var buf strings.Builder
			autonomous.RenderGateRepairs(&buf, &s)
			_, err = cmd.OutOrStdout().Write([]byte(buf.String()))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the REST body as JSON")
	return c
}

// newAutonomousGateRepairRunCmd is `gate-repair run`.
func newAutonomousGateRepairRunCmd() *cobra.Command {
	var asJSON bool
	var automaton, task string
	c := &cobra.Command{
		Use:   "run",
		Short: "Run a gate repair pass (or repair one failed task gate)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			automaton = strings.TrimSpace(automaton)
			task = strings.TrimSpace(task)
			if (automaton == "") != (task == "") {
				return fmt.Errorf("pass both --automaton and --task to repair one gate, or neither to run a pass")
			}
			var body any
			if automaton != "" {
				body = map[string]string{"automaton_id": automaton, "task_id": task}
			}
			out, err := gateRepairPost("/api/autonomous/gate-repair/run", body)
			if err != nil {
				return err
			}
			if asJSON {
				_, err := cmd.OutOrStdout().Write(out)
				return err
			}
			var resp gateRepairRunResponse
			if err := json.Unmarshal(out, &resp); err != nil {
				return fmt.Errorf("unexpected daemon reply: %w", err)
			}
			var buf strings.Builder
			autonomous.RenderGateRepairRun(&buf, resp.Runs)
			buf.WriteString("\n")
			autonomous.RenderGateRepairs(&buf, &resp.Status)
			_, err = cmd.OutOrStdout().Write([]byte(buf.String()))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the REST body as JSON")
	c.Flags().StringVar(&automaton, "automaton", "", "repair this Automaton's failed gate (with --task)")
	c.Flags().StringVar(&task, "task", "", "repair this Automaton's failed task gate (with --automaton)")
	return c
}
