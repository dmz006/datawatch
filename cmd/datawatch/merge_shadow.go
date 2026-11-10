// datawatch autonomous merge-shadow status|compare
//
// status prints the matched and different comparison counts per release size
// from the daemon's merge shadow read endpoint. compare prints the list of
// differences found for one Automaton.
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const (
	mergeShadowAPI = "/api/autonomous/merge/shadow"
	// mergeShadowNoComparisonLine is the one line printed when no comparison exists.
	mergeShadowNoComparisonLine = "no merge shadow comparison exists yet"
)

type mergeShadowCLIView struct {
	Projects []struct {
		Project     string `json:"project"`
		Comparisons []struct {
			AutomatonID string `json:"automaton_id"`
			ReleaseSize string `json:"release_size"`
			Matched     int    `json:"matched"`
			Different   int    `json:"different"`
			NotCompared int    `json:"not_compared"`
			Differences []struct {
				Field    string `json:"field"`
				Planned  string `json:"planned"`
				Landed   string `json:"landed"`
				Severity string `json:"severity"`
			} `json:"differences"`
		} `json:"comparisons"`
		Counts map[string]struct {
			Matched   int `json:"matched"`
			Different int `json:"different"`
		} `json:"counts"`
	} `json:"projects"`
}

func fetchMergeShadow() (*mergeShadowCLIView, error) {
	out, err := mergePipelineSend("GET", mergeShadowAPI, nil, nil)
	if err != nil {
		return nil, err
	}
	var v mergeShadowCLIView
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("decode merge shadow response: %w", err)
	}
	return &v, nil
}

func newAutonomousMergeShadowCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "merge-shadow",
		Short: "Show merge shadow comparison counts and per-Automaton differences",
		Long: `The merge shadow compares the merge the daemon planned with what landed.

  status               Matched and different counts per release size
  compare <automaton>  The differences found for one Automaton
`,
	}
	c.AddCommand(newMergeShadowStatusCmd(), newMergeShadowCompareCmd())
	return c
}

func newMergeShadowStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print matched and different counts per release size",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v, err := fetchMergeShadow()
			if err != nil {
				return err
			}
			total := 0
			matched := map[string]int{}
			different := map[string]int{}
			for _, p := range v.Projects {
				total += len(p.Comparisons)
				for sz, n := range p.Counts {
					matched[sz] += n.Matched
					different[sz] += n.Different
				}
			}
			if total == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), mergeShadowNoComparisonLine)
				return nil
			}
			for _, sz := range []string{"patch", "minor", "major", "none"} {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: matched %d, different %d\n", sz, matched[sz], different[sz])
			}
			return nil
		},
	}
}

func newMergeShadowCompareCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "compare <automaton-id>",
		Short: "Print the difference list for one Automaton",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			v, err := fetchMergeShadow()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			found := false
			for _, p := range v.Projects {
				for _, c := range p.Comparisons {
					if c.AutomatonID != id {
						continue
					}
					found = true
					_, _ = fmt.Fprintf(w, "Automaton %s (%s): matched %d, different %d, not compared %d\n",
						id, c.ReleaseSize, c.Matched, c.Different, c.NotCompared)
					if len(c.Differences) == 0 {
						_, _ = fmt.Fprintln(w, "no differences")
					}
					for _, d := range c.Differences {
						_, _ = fmt.Fprintf(w, "  %s [%s]: planned %q, landed %q\n", d.Field, d.Severity, d.Planned, d.Landed)
					}
				}
			}
			if !found {
				_, _ = fmt.Fprintf(w, "no merge shadow comparison exists for Automaton %s\n", id)
			}
			return nil
		},
	}
}
