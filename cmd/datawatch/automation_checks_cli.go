// datawatch autonomous checks / ship — the automatic checks and the ship
// step over the CLI. Each is a read of the resolved policy and the findings
// recorded so far; the text is rendered by the shared formatter in
// internal/autonomous/checks_ship_status.go, the same wording the comm verbs
// use.

package main

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func newStatusReadCmd(use, short, long, path string, render func([]byte) (string, error)) *cobra.Command {
	var prd, dir string
	var asJSON bool
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if prd != "" {
				q.Set("prd_id", prd)
			}
			if dir != "" {
				q.Set("project_dir", dir)
			}
			body, err := progressGet(path, q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			text, err := render(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		},
	}
	c.Flags().StringVar(&prd, "prd", "", "only the findings of this Automaton")
	c.Flags().StringVar(&dir, "project-dir", "", "resolve the policy for this project directory")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// newAutonomousChecksCmd is `datawatch autonomous checks`.
func newAutonomousChecksCmd() *cobra.Command {
	return newStatusReadCmd("checks",
		"Show the automatic checks' modes and recorded findings",
		"Show the resolved mode of each automatic check (off, shadow or on) and the findings they recorded, newest first. A read: nothing is run or written.",
		"/api/autonomous/checks",
		func(b []byte) (string, error) {
			s, err := autonomous.ParseChecksStatus(b)
			if err != nil {
				return "", err
			}
			return autonomous.RenderChecksStatusText(s), nil
		})
}

// newAutonomousShipCmd is `datawatch autonomous ship`.
func newAutonomousShipCmd() *cobra.Command {
	return newStatusReadCmd("ship",
		"Show the ship step's modes and recorded outcomes",
		"Show the resolved push, tag and CI-watch modes, tag format, remote and CI workflows, and the outcomes recorded for finished Automata, newest first. A read: nothing is pushed, tagged or written.",
		"/api/autonomous/ship",
		func(b []byte) (string, error) {
			s, err := autonomous.ParseShipStatus(b)
			if err != nil {
				return "", err
			}
			return autonomous.RenderShipStatusText(s), nil
		})
}

// newAutonomousRestartInterruptionsCmd is `datawatch autonomous
// restart-interruptions`: what a daemon restart now would interrupt.
func newAutonomousRestartInterruptionsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "restart-interruptions",
		Short: "Show the running tasks and live sessions a restart now would interrupt",
		Long:  "Show the running tasks (executing worker or verifying) and the live sessions, with their ids, that a daemon restart now would interrupt, and the configured restart policy (autonomous.restart_policy). A read: nothing is restarted.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/autonomous/restart-interruptions", nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			r, err := autonomous.ParseRestartInterruption(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RestartInterruptionText(r))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
