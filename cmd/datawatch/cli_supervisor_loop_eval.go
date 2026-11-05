// datawatch supervisor-loop eval — compare models on a model job's eval cases:
//
//	datawatch supervisor-loop eval --job <name> --model <id> [--model <id>...] [--cases <name>] [--format json]

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/supervisorloop"
	"github.com/spf13/cobra"
)

func newSupervisorLoopEvalCmd() *cobra.Command {
	var job, casesDir, format string
	var models []string
	c := &cobra.Command{
		Use:   "eval",
		Short: "Compare models on a supervisor loop model job's eval cases",
		Long: "Run a model job's eval cases (draft_spec, classify_ci_failure or pick_backend) " +
			"against each --model and print, per model: cases, passed, validator failures, " +
			"retries, median and maximum duration and escalations. If no listed model is " +
			"reachable the command fails naming each model; nothing is invented.\n\n" +
			`--cases is a relative directory holding <job>/*.json cases; --format json prints the raw response.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if job == "" {
				return errors.New("--job is required")
			}
			if len(models) == 0 {
				return errors.New("at least one --model is required")
			}
			switch format {
			case "", "text", "json":
			default:
				return fmt.Errorf("unknown --format %q (use \"json\" or \"text\")", format)
			}
			body := map[string]any{"job": job, "models": models}
			if casesDir != "" {
				body["cases_dir"] = casesDir
			}
			raw, err := daemonJSONBody("POST", "/api/supervisor-loop/eval", body)
			if err != nil {
				return err
			}
			if format == "json" {
				_, err = cmd.OutOrStdout().Write(raw)
				return err
			}
			var rep supervisorloop.EvalReport
			if err := json.Unmarshal(raw, &rep); err != nil {
				return err
			}
			buf := &strings.Builder{}
			supervisorloop.RenderEval(buf, &rep)
			_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
			return err
		},
	}
	c.Flags().StringVar(&job, "job", "", `the model job to evaluate: "draft_spec", "classify_ci_failure" or "pick_backend"`)
	c.Flags().StringArrayVar(&models, "model", nil, "a model id to compare (repeatable)")
	c.Flags().StringVar(&casesDir, "cases", "", "name of a folder under the bundled cases folder, holding <job>/*.json cases (default: the bundled cases)")
	c.Flags().StringVar(&format, "format", "", `output format: "json" for the raw response, "text" (default)`)
	return c
}
