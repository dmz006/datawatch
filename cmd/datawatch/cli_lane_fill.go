// datawatch autonomous lane-fill [--limit N] [--json]
// datawatch autonomous prd-set-lane-preference <id> <gpu|claude|any>
//
// Lane fill status and the per-Automaton lane preference. The lane summary is
// rendered by the same function the comm channel uses; --json prints the REST
// body untouched.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// laneFillLimitMax is the largest decision count GET /api/autonomous/lane-fill accepts.
const laneFillLimitMax = 1000

func newAutonomousLaneFillCmd() *cobra.Command {
	var limit int
	var asJSON bool
	c := &cobra.Command{
		Use:   "lane-fill",
		Short: "Show lane fill: each lane, the queued eligible counts and the newest decisions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if cmd.Flags().Changed("limit") {
				if limit < 1 || limit > laneFillLimitMax {
					return fmt.Errorf("--limit must be a whole number from 1 to %d", laneFillLimitMax)
				}
				q.Set("limit", strconv.Itoa(limit))
			}
			// progressGet is the plain GET helper for the autonomous read routes.
			body, err := progressGet("/api/autonomous/lane-fill", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			st, err := autonomous.ParseLaneFillStatus(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), autonomous.RenderLaneFillText(st, 0))
			return err
		},
	}
	c.Flags().IntVar(&limit, "limit", 0, "how many decisions to read, 1 to 1000 (default 50)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// parseLanePreferenceArg checks a lane preference typed on the command line:
// gpu, claude or any. Anything else is refused before anything is sent.
func parseLanePreferenceArg(arg string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(arg))
	switch p {
	case autonomous.LanePreferenceGPU, autonomous.LanePreferenceClaude, autonomous.LanePreferenceAny:
		return p, nil
	}
	return "", fmt.Errorf("unknown lane preference %q: use gpu, claude or any", strings.TrimSpace(arg))
}

func newAutonomousPRDSetLanePreferenceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prd-set-lane-preference <id> <gpu|claude|any>",
		Short: "Set which lane an Automaton may take: gpu, claude or any",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			pref, err := parseLanePreferenceArg(args[1])
			if err != nil {
				return err
			}
			body, _ := json.Marshal(map[string]string{"lane_preference": pref})
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_lane_preference", body)
		},
	}
}
