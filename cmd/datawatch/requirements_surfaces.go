// Requirements surfaces CLI (REST parity):
//
//	datawatch automation requirements-report [--format text|json]
//	datawatch automation model-consent get
//	datawatch automation model-consent set --model <name> [--deny <name> ...] [--publisher <p> ...] | --clear
//	datawatch compute node versions <name>
//
// The requirements-report text is rendered by the autonomous package, the
// same renderer the REST route, the MCP tool and the comm verb use;
// --format json prints the REST body untouched.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// datawatch automation requirements-report [--format text|json]
func newAutomationRequirementsReportCmd() *cobra.Command {
	var format string
	c := &cobra.Command{
		Use:   "requirements-report",
		Short: "Show the automation backend requirements report (Ollama minimums, node versions, backends, missing)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f := strings.ToLower(strings.TrimSpace(format))
			if f != "text" && f != "json" {
				return fmt.Errorf("--format must be text or json, got %q", format)
			}
			body, err := progressGet("/api/automation/requirements-report", url.Values{"format": {"json"}})
			if err != nil {
				return err
			}
			if f == "json" {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var rep autonomous.RequirementsReport
			if err := json.Unmarshal(body, &rep); err != nil {
				return fmt.Errorf("decode report: %w", err)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), rep.Text())
			return err
		},
	}
	c.Flags().StringVar(&format, "format", "text", "output format: text or json")
	return c
}

// datawatch automation model-consent get|set ...
func newAutomationModelConsentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model-consent",
		Short: "Model-pull consent policy (get / set)",
		Long: `Model-pull consent is the pull gate for the model registry: which models
(or which publishers) may be pulled, and what the rule in words is.

  get   Show the current policy (models with allowed/refused state and notes,
        the publisher allow-list, and the decision rule).
  set   Replace the complete policy. Repeated flags compose the whole list:
        --model <name> allows a model, --deny <name> records a refusal with a
        note, --publisher <name> allow-lists a publisher. --clear wipes the
        policy. The set replaces what is stored, it does not append.`,
	}
	cmd.AddCommand(newAutomationModelConsentGetCmd())
	cmd.AddCommand(newAutomationModelConsentSetCmd())
	return cmd
}

func newAutomationModelConsentGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the model-pull consent policy",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet("/api/automation/model-consent", nil)
			if err != nil {
				return err
			}
			var view consentCLIView
			if err := json.Unmarshal(body, &view); err != nil {
				// Not the expected shape; print the raw body.
				_, werr := cmd.OutOrStdout().Write(body)
				return werr
			}
			out := renderConsentCLIView(view)
			_, err = fmt.Fprint(cmd.OutOrStdout(), out)
			return err
		},
	}
}

type consentCLIEntry struct {
	Name    string `json:"name"`
	Allowed bool   `json:"allowed"`
	Note    string `json:"note"`
}

type consentCLIView struct {
	Models       []consentCLIEntry `json:"models"`
	Publishers   []string          `json:"publishers"`
	DecisionRule string            `json:"decision_rule"`
}

func renderConsentCLIView(view consentCLIView) string {
	var sb strings.Builder
	sb.WriteString("Model pull consent\n")
	sb.WriteString("  rule: " + view.DecisionRule + "\n")
	if len(view.Models) == 0 {
		sb.WriteString("  models: (none listed)\n")
	} else {
		sb.WriteString("  models:\n")
		for _, m := range view.Models {
			state := "allowed"
			if !m.Allowed {
				state = "refused"
			}
			line := "    " + m.Name + " " + state
			if m.Note != "" {
				line += " — " + m.Note
			}
			sb.WriteString(line + "\n")
		}
	}
	if len(view.Publishers) == 0 {
		sb.WriteString("  publishers: (none allow-listed)\n")
	} else {
		sb.WriteString("  publishers: " + strings.Join(view.Publishers, ", ") + "\n")
	}
	return sb.String()
}

func nonEmptyList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

func newAutomationModelConsentSetCmd() *cobra.Command {
	var (
		models     []string
		denied     []string
		publishers []string
		clear      bool
	)
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Replace the model-pull consent policy (the flags compose the complete list)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			models = nonEmptyList(models)
			denied = nonEmptyList(denied)
			publishers = nonEmptyList(publishers)
			if clear {
				if len(models)+len(denied)+len(publishers) > 0 {
					return fmt.Errorf("--clear cannot be combined with --model, --deny or --publisher")
				}
			} else if len(models)+len(denied)+len(publishers) == 0 {
				return fmt.Errorf("give at least one of --model, --deny or --publisher (or --clear)")
			}
			type entry struct {
				Name    string `json:"name"`
				Allowed bool   `json:"allowed"`
				Note    string `json:"note,omitempty"`
			}
			list := []any{}
			seen := map[string]bool{}
			add := func(name string, allowed bool, note string) error {
				key := strings.ToLower(name)
				if seen[key] {
					return fmt.Errorf("model name listed twice: %s", name)
				}
				seen[key] = true
				e := entry{Name: name, Allowed: allowed}
				if note != "" {
					e.Note = note
				}
				list = append(list, e)
				return nil
			}
			for _, m := range models {
				if err := add(m, true, ""); err != nil {
					return err
				}
			}
			for _, d := range denied {
				if err := add(d, false, "denied by operator"); err != nil {
					return err
				}
			}
			return daemonJSON(http.MethodPut, "/api/automation/model-consent",
				map[string]any{"models": list, "publishers": publishers})
		},
	}
	cmd.Flags().StringArrayVar(&models, "model", nil, "allow this model (repeatable; compose the complete list)")
	cmd.Flags().StringArrayVar(&denied, "deny", nil, "record this model as refused with a note (repeatable)")
	cmd.Flags().StringArrayVar(&publishers, "publisher", nil, "allow-list this publisher (repeatable)")
	cmd.Flags().BoolVar(&clear, "clear", false, "wipe the policy (no models, no publishers)")
	return cmd
}
