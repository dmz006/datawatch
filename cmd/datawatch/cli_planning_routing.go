// datawatch autonomous planning-routing {get,set,test,scorecard} — the
// planning routing config, a no-LLM routing test and the counts-only
// scorecard over the CLI. Each calls the REST routes under
// /api/autonomous/planning-routing and renders the same text the comm verbs
// show (internal/autonomous/planning_routing_text.go).

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const planningRoutingAPI = "/api/autonomous/planning-routing"

// newAutonomousPlanningRoutingCmd is the `datawatch autonomous
// planning-routing` group.
func newAutonomousPlanningRoutingCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "planning-routing",
		Short: "Planning routing: config, routing test and scorecard",
		Long: `Planning routing picks the planner for an Automaton decompose per rule.
It is off by default.

Subcommands:
  get         Show the config and its validation result
  set         Update enabled, shadow, rules, ladders or classes (a left-out field keeps its value)
  test        Route one input and show the decision; no LLM is called
  scorecard   Counts-only scorecard and gate report over a window`,
	}
	c.AddCommand(
		newPlanningRoutingGetCmd(),
		newPlanningRoutingSetCmd(),
		newPlanningRoutingTestCmd(),
		newPlanningRoutingScorecardCmd(),
	)
	return c
}

func newPlanningRoutingGetCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "get",
		Short: "Show the planning routing config and its validation result",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet(planningRoutingAPI, nil)
			if err != nil {
				return err
			}
			return printPlanningRouting(cmd, body, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func printPlanningRouting(cmd *cobra.Command, body []byte, asJSON bool) error {
	if asJSON {
		_, err := cmd.OutOrStdout().Write(body)
		return err
	}
	v, err := autonomous.ParsePlanningRoutingView(body)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), autonomous.RenderPlanningRoutingView(v))
	return err
}

func newPlanningRoutingSetCmd() *cobra.Command {
	var enabled, shadow bool
	var rules, ladders, classes, rulesFile string
	var asJSON bool
	c := &cobra.Command{
		Use:   "set",
		Short: "Update the planning routing config",
		Long: `Update the planning routing config. A field you leave out keeps its current
value. The result is validated first; a bad config is rejected and nothing is applied.

  datawatch autonomous planning-routing set --enabled=true --shadow=true
  datawatch autonomous planning-routing set --rules-file rules.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cur, err := progressGet(planningRoutingAPI, nil)
			if err != nil {
				return err
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(cur, &body); err != nil {
				return err
			}
			delete(body, "validation")
			if cmd.Flags().Changed("enabled") {
				body["enabled"], _ = json.Marshal(enabled)
			}
			if cmd.Flags().Changed("shadow") {
				body["shadow"], _ = json.Marshal(shadow)
			}
			if rulesFile != "" {
				b, err := os.ReadFile(rulesFile) // #nosec G304 -- operator-supplied path
				if err != nil {
					return err
				}
				rules = string(b)
			}
			for k, v := range map[string]string{"rules": rules, "ladders": ladders, "classes": classes} {
				if v == "" {
					continue
				}
				if !json.Valid([]byte(v)) {
					return fmt.Errorf("%s: invalid JSON", k)
				}
				body[k] = json.RawMessage(v)
			}
			raw, _ := json.Marshal(body)
			out, err := mergePipelineSend(http.MethodPut, planningRoutingAPI, nil, raw)
			if err != nil {
				return err
			}
			return printPlanningRouting(cmd, out, asJSON)
		},
	}
	c.Flags().BoolVar(&enabled, "enabled", false, "turn planning routing on or off")
	c.Flags().BoolVar(&shadow, "shadow", false, "record the routed decision only; keep the planner unchanged")
	c.Flags().StringVar(&rules, "rules", "", "JSON array of rules (replaces the current rules)")
	c.Flags().StringVar(&rulesFile, "rules-file", "", "file holding the JSON array of rules")
	c.Flags().StringVar(&ladders, "ladders", "", "JSON object of ladders by name")
	c.Flags().StringVar(&classes, "classes", "", "JSON object of risk classes by name")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newPlanningRoutingTestCmd() *cobra.Command {
	var kind, project string
	var tags, files []string
	var specBytes int
	var asJSON bool
	c := &cobra.Command{
		Use:   "test",
		Short: "Route one input under the current config; no LLM is called",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tags == nil {
				tags = []string{}
			}
			if files == nil {
				files = []string{}
			}
			raw, _ := json.Marshal(map[string]any{
				"kind": kind, "tags": tags, "spec_bytes": specBytes, "files": files, "project": project,
			})
			out, err := mergePipelineSend(http.MethodPost, planningRoutingAPI+"/test", nil, raw)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(out)
				return err
			}
			d, err := autonomous.ParsePlanningRouteDecisionView(out)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), autonomous.RenderPlanningRouteDecisionView(d))
			return err
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "routing input kind, e.g. feature or bug")
	c.Flags().StringSliceVar(&tags, "tags", nil, "tags (comma-separated)")
	c.Flags().IntVar(&specBytes, "spec-bytes", 0, "spec size in bytes")
	c.Flags().StringSliceVar(&files, "files", nil, "repo-relative paths the tasks touch (comma-separated)")
	c.Flags().StringVar(&project, "project", "", "project identifier")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newPlanningRoutingScorecardCmd() *cobra.Command {
	var window string
	var asJSON bool
	c := &cobra.Command{
		Use:   "scorecard",
		Short: "Counts-only planning scorecard and gate report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if strings.TrimSpace(window) != "" {
				q.Set("window", strings.TrimSpace(window))
			}
			body, err := progressGet(planningRoutingAPI+"/scorecard", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			report, err := autonomous.ParsePlanningScorecardReport(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), report)
			return err
		},
	}
	c.Flags().StringVar(&window, "window", "", "window such as 36h or 7d (default 7d; 0 = all records)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
