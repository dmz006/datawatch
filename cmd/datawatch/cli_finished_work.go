// datawatch autonomous finished-work — the finished work that is waiting to
// land (owner and open question per row), and `finished-work run sweep|audit`
// to run one bounded sweep pass or the weekly audit now. The read changes
// nothing; an existing weekly audit schedule can call run audit. --json prints
// the raw response untouched.

package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func newAutonomousFinishedWorkCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "finished-work",
		Short: "Show finished work that is waiting to land",
		Long: "Show the Automata that finished while their branch still holds commits that " +
			"are not on the default branch, or whose code tasks are all done while docs or " +
			"gate tasks remain, or whose left-alone date came: the cause, the branch, how " +
			"long it has waited, the owner and the open question. A read: nothing is " +
			"changed. Setting: autonomous.finished_work (on, shadow or off).\n\n" +
			"--json prints the raw response untouched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/finished-work", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			d, err := autonomous.ParseFinishedWorkDigest(body)
			if err != nil {
				return err
			}
			return autonomous.RenderFinishedWorkDigest(cmd.OutOrStdout(), d)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.AddCommand(newAutonomousFinishedWorkRunCmd())
	return c
}

func newAutonomousFinishedWorkRunCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "run sweep|audit",
		Short: "Run one finished-work sweep pass or the audit now",
		Long: "sweep makes one bounded pass (at most 100 branch comparisons): finished work " +
			"older than a day gets an owner and one open question. audit reads every Automaton, " +
			"cancelled and branchless ones included, and writes the result as a decision record " +
			"(once per week). Neither merges, touches a session or changes an Automaton status; " +
			"with autonomous.finished_work=shadow they record findings only.\n\n" +
			"A weekly audit as a datawatch schedule: datawatch schedule add (command: " +
			"datawatch autonomous finished-work run audit).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run := strings.ToLower(strings.TrimSpace(args[0]))
			if run != "sweep" && run != "audit" {
				return fmt.Errorf("run must be sweep or audit, got %q", args[0])
			}
			body, err := daemonJSONBody(http.MethodPost, "/api/autonomous/finished-work/run", map[string]any{"run": run})
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			return autonomous.RenderFinishedWorkRun(cmd.OutOrStdout(), run, body)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
