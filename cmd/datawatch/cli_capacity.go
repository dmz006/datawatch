// datawatch capacity [--json] — show the capacity admission ledger: pools,
// limits, holders and the queue of tasks waiting for capacity.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/capacity"
)

func newCapacityCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "capacity",
		Short: "Show capacity pools, holders and the wait queue for Automata tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req, err := http.NewRequest(http.MethodGet, daemonURL()+"/api/capacity", nil)
			if err != nil {
				return err
			}
			if tok := daemonToken(); tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			resp, err := daemonClient().Do(req)
			if err != nil {
				return fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
			}
			defer resp.Body.Close() //nolint:errcheck
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode/100 != 2 {
				return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			}
			return renderCapacity(cmd.OutOrStdout(), body, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func renderCapacity(w io.Writer, body []byte, asJSON bool) error {
	if asJSON {
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			return err
		}
		out, _ := json.MarshalIndent(v, "", "  ")
		_, err := fmt.Fprintln(w, string(out))
		return err
	}
	var st capacity.Status
	if err := json.Unmarshal(body, &st); err != nil {
		return err
	}
	_, err := fmt.Fprint(w, capacity.FormatText(st))
	return err
}
