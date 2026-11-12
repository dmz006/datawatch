// datawatch autonomous security-failure — the security failure reports
// read over the CLI. The body of GET /api/autonomous/security-failures is
// rendered as a table (repository, ref, workflow, run, status, dependency,
// advisory, recommendation, action) by the renderer the comm verb shares;
// --json prints the body untouched. A read: nothing is written.

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newAutonomousSecurityFailureCmd is the `datawatch autonomous
// security-failure` subcommand: the stored security failure reports,
// newest first.
func newAutonomousSecurityFailureCmd() *cobra.Command {
	var repo, status string
	var asJSON bool
	c := &cobra.Command{
		Use:   "security-failure",
		Short: "Read the security failure reports of failed security runs",
		Long: "Show the security failure reports the coordinator stored for failed " +
			"security runs, newest first, as a table (repository, ref, workflow, run, " +
			"status, dependency, advisory, recommendation, action). Without --status " +
			"only the open reports are listed — runs whose watch status is neither " +
			"green nor superseded. A read: nothing is written, no Automaton is " +
			"stopped or resumed and no run is rerun.\n\n" +
			"Filters (all optional, combinable): --repo for one repository's reports, " +
			"--status for one watch state (green, failing, pending, hung, rerunning, " +
			"superseded).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status = strings.ToLower(strings.TrimSpace(status))
			q := url.Values{}
			if repo != "" {
				q.Set("repo", repo)
			}
			if status != "" {
				q.Set("status", status)
			}
			body, err := progressGet("/api/autonomous/security-failures", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var entries []autonomous.SecurityFailureListEntry
			if err := json.Unmarshal(body, &entries); err != nil {
				return err
			}
			if status == "" {
				entries = autonomous.FilterOpenSecurityFailures(entries)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderSecurityFailureList(entries))
			return err
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "only this repository path's reports (e.g. dmz006/datawatch)")
	c.Flags().StringVar(&status, "status", "", "only this watch state: green, failing, pending, hung, rerunning or superseded")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
