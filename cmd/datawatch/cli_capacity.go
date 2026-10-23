// datawatch capacity [--json] — show the capacity admission ledger: pools,
// limits, holders, queued tasks with their reasons, running holdings,
// back-off flags and budgets.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
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
	c.AddCommand(newCapacityAdmissionCmd())
	return c
}

// newCapacityAdmissionCmd is `datawatch capacity admission`: ask the daemon
// whether a start would be admitted now, without starting anything.
func newCapacityAdmissionCmd() *cobra.Command {
	var asJSON bool
	var backend, node, model, role, priority string
	c := &cobra.Command{
		Use:   "admission",
		Short: "Ask whether a start would be admitted now, queued or moved elsewhere (changes nothing)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			for k, v := range map[string]string{"backend": backend, "node": node, "model": model, "role": role, "priority": priority} {
				if v != "" {
					q.Set(k, v)
				}
			}
			if backend == "" {
				return fmt.Errorf("--backend is required")
			}
			req, err := http.NewRequest(http.MethodGet, daemonURL()+"/api/capacity/admission?"+q.Encode(), nil)
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
			return renderAdmission(cmd.OutOrStdout(), body, asJSON)
		},
	}
	c.Flags().StringVar(&backend, "backend", "", "backend or named LLM to start (required)")
	c.Flags().StringVar(&node, "node", "", "compute node the start would use")
	c.Flags().StringVar(&model, "model", "", "model wanted")
	c.Flags().StringVar(&role, "role", "", "worker (default), verifier, planner or interactive")
	c.Flags().StringVar(&priority, "priority", "", "integer queue priority (default 0)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON decision")
	return c
}

func renderAdmission(w io.Writer, body []byte, asJSON bool) error {
	if asJSON {
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			return err
		}
		out, _ := json.MarshalIndent(v, "", "  ")
		_, err := fmt.Fprintln(w, string(out))
		return err
	}
	text, err := capacity.FormatDecisionJSON(body)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, text)
	return err
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
	text, err := autonomous.FormatCapacityText(body)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, text)
	return err
}
