// datawatch autonomous infra-retries [automaton-id] [--limit N] [--json]
//
// Infrastructure retry status: the retry policy in effect and the tasks that
// were retried, or are held, because the machinery around them failed. The
// text is rendered by the same function the comm channel uses; --json prints
// the REST body untouched.

package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// infraRetryLimitMax is the largest task count GET /api/autonomous/infra-retries accepts.
const infraRetryLimitMax = 1000

func newAutonomousInfraRetriesCmd() *cobra.Command {
	var limit int
	var asJSON bool
	c := &cobra.Command{
		Use:   "infra-retries [automaton-id]",
		Short: "Show infrastructure retries: the policy in effect and the tasks retried or held",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			if cmd.Flags().Changed("limit") {
				if limit < 1 || limit > infraRetryLimitMax {
					return fmt.Errorf("--limit must be a whole number from 1 to %d", infraRetryLimitMax)
				}
				q.Set("limit", strconv.Itoa(limit))
			}
			if len(args) == 1 {
				if id := strings.TrimSpace(args[0]); id != "" {
					q.Set("automaton", id)
				}
			}
			body, err := progressGet("/api/autonomous/infra-retries", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			st, err := autonomous.ParseInfraRetryStatus(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderInfraRetryText(st, 0))
			return err
		},
	}
	c.Flags().IntVar(&limit, "limit", 0, "how many tasks to read, 1 to 1000 (default 50)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
