// datawatch autonomous task-routing — the task type routing table, its
// profiles, the per-Automaton view and the no-write dry run over the CLI.
// Each subcommand calls the REST routes under /api/autonomous/task-routing
// and renders the same text the chat verb shows
// (internal/autonomous/task_routing_text.go).

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

const taskRoutingAPI = "/api/autonomous/task-routing"

func taskRoutingAutomatonAPI(id, tail string) string {
	return "/api/autonomous/prds/" + url.PathEscape(id) + "/task-routing" + tail
}

func newAutonomousTaskRoutingCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "task-routing",
		Short: "Task routing: which LLM and model each type of task starts with",
		Long: `The task routing table picks the worker and verification LLM for each task
type when the planner decomposes an Automaton. Rows live in named profiles and
one profile is active. An LLM you set on a task, story or Automaton still wins
over a row. Changing the table only affects tasks created afterwards; no
running session is touched.

Subcommands:
  show                 Table of the active profile
  set                  Replace the table from a file, or set one row with flags
  profile list         List profiles and the active one
  profile save         Save a profile (copies the active profile unless --file is given)
  profile activate     Make a profile the active one
  profile delete       Delete a profile
  profile export       Write one profile as JSON (--out file, default stdout)
  profile import       Import an exported profile file
  automaton <id>       Which row decided each task's LLM in an Automaton
  dry-run <id>         What a profile would assign to each task; writes nothing`,
	}
	profile := &cobra.Command{Use: "profile", Short: "Manage task routing profiles"}
	profile.AddCommand(
		newTaskRoutingProfileListCmd(),
		newTaskRoutingProfileSaveCmd(),
		newTaskRoutingProfileActivateCmd(),
		newTaskRoutingProfileDeleteCmd(),
		newTaskRoutingProfileExportCmd(),
		newTaskRoutingProfileImportCmd(),
	)
	c.AddCommand(newTaskRoutingShowCmd(), newTaskRoutingSetCmd(), profile,
		newTaskRoutingAutomatonCmd(), newTaskRoutingDryRunCmd())
	return c
}

// taskRoutingOut prints body untouched with asJSON, otherwise render(body).
func taskRoutingOut(cmd *cobra.Command, body []byte, asJSON bool, render func([]byte) (string, error)) error {
	if asJSON {
		_, err := cmd.OutOrStdout().Write(body)
		return err
	}
	s, err := render(body)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), s)
	return err
}

func newTaskRoutingShowCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Show the routing table of the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet(taskRoutingAPI, nil)
			if err != nil {
				return err
			}
			return taskRoutingOut(cmd, body, asJSON, func(b []byte) (string, error) {
				return autonomous.RenderTaskRoutingTable(b, true)
			})
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// parseTaskRoutingChoice reads "backend/model" (the model may itself contain
// slashes) into a choice.
func parseTaskRoutingChoice(s string) (*config.TaskRoutingChoice, error) {
	backend, model, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok || backend == "" || model == "" {
		return nil, fmt.Errorf("--second-choice must look like backend/model, got %q", s)
	}
	return &config.TaskRoutingChoice{Backend: backend, Model: model}, nil
}

// readTaskRoutingFile reads a JSON or YAML file holding either a whole
// task_routing block or one bare profile (task type -> row).
func readTaskRoutingFile(path string) (config.TaskRoutingConfig, *config.TaskRoutingProfile, error) {
	var zero config.TaskRoutingConfig
	b, err := os.ReadFile(path) // #nosec G304 -- operator-supplied path
	if err != nil {
		return zero, nil, err
	}
	var probe map[string]any
	if err := yaml.Unmarshal(b, &probe); err != nil {
		return zero, nil, fmt.Errorf("%s: %w", path, err)
	}
	_, a := probe["profiles"]
	_, e := probe["enabled"]
	_, p := probe["active_profile"]
	if a || e || p {
		var cfg config.TaskRoutingConfig
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return zero, nil, fmt.Errorf("%s: %w", path, err)
		}
		return cfg, nil, nil
	}
	var prof config.TaskRoutingProfile
	if err := yaml.Unmarshal(b, &prof); err != nil {
		return zero, nil, fmt.Errorf("%s: %w", path, err)
	}
	return zero, &prof, nil
}

func getTaskRoutingConfig() (config.TaskRoutingConfig, error) {
	var cfg config.TaskRoutingConfig
	body, err := progressGet(taskRoutingAPI, nil)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(body, &cfg)
	return cfg, err
}

func putTaskRoutingConfig(cfg config.TaskRoutingConfig) ([]byte, error) {
	raw, _ := json.Marshal(cfg)
	return mergePipelineSend(http.MethodPut, taskRoutingAPI, nil, raw)
}

func newTaskRoutingSetCmd() *cobra.Command {
	var file, typ, wb, wm, vb, vm, effort, second string
	var asJSON bool
	c := &cobra.Command{
		Use:   "set",
		Short: "Replace the routing table from a file, or set one row",
		Long: `Replace the whole table from a JSON or YAML file, or set one row of the
active profile with flags. The result is validated first; an invalid table is
rejected and nothing changes.

  datawatch autonomous task-routing set --file routing.yaml
  datawatch autonomous task-routing set --type docs_set \
      --worker-backend pi-prompt --worker-model ollama/qwen3.8:27b \
      --verify-backend claude-code --verify-model sonnet --effort low \
      --second-choice claude-code/sonnet

The file holds either a whole task_routing block (enabled, active_profile,
profiles) or one profile (task type to row), which replaces the active profile.
Flags left out of a one-row set keep the row's current value.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rowFlags := cmd.Flags().Changed("worker-backend") || cmd.Flags().Changed("worker-model") ||
				cmd.Flags().Changed("verify-backend") || cmd.Flags().Changed("verify-model") ||
				cmd.Flags().Changed("effort") || cmd.Flags().Changed("second-choice")
			if file != "" && (typ != "" || rowFlags) {
				return fmt.Errorf("use either --file or the row flags, not both")
			}
			if file == "" && typ == "" {
				return fmt.Errorf("give --file, or --type with the row flags")
			}
			cur, err := getTaskRoutingConfig()
			if err != nil {
				return err
			}
			next := cur
			if file != "" {
				cfg, prof, err := readTaskRoutingFile(file)
				if err != nil {
					return err
				}
				if prof != nil {
					next = cur.Clone()
					if next.Profiles == nil {
						next.Profiles = map[string]config.TaskRoutingProfile{}
					}
					next.Profiles[next.ActiveProfile] = *prof
				} else {
					next = cfg
				}
			} else {
				if !config.IsTaskRoutingTaskType(typ) {
					return fmt.Errorf("unknown task type %q; known: %s", typ, strings.Join(config.TaskRoutingTaskTypes(), ", "))
				}
				next = cur.Clone()
				prof := next.Profiles[next.ActiveProfile]
				if prof == nil {
					prof = config.TaskRoutingProfile{}
				}
				row := prof[typ]
				if cmd.Flags().Changed("worker-backend") {
					row.WorkerBackend = wb
				}
				if cmd.Flags().Changed("worker-model") {
					row.WorkerModel = wm
				}
				if cmd.Flags().Changed("verify-backend") {
					row.VerificationBackend = vb
				}
				if cmd.Flags().Changed("verify-model") {
					row.VerificationModel = vm
				}
				if cmd.Flags().Changed("effort") {
					row.Effort = effort
				}
				if cmd.Flags().Changed("second-choice") {
					if second == "" || second == "none" {
						row.SecondChoice = nil
					} else if row.SecondChoice, err = parseTaskRoutingChoice(second); err != nil {
						return err
					}
				}
				prof[typ] = row
				next.Profiles[next.ActiveProfile] = prof
			}
			out, err := putTaskRoutingConfig(next)
			if err != nil {
				return err
			}
			return taskRoutingOut(cmd, out, asJSON, func(b []byte) (string, error) {
				return autonomous.RenderTaskRoutingTable(b, true)
			})
		},
	}
	f := c.Flags()
	f.StringVar(&file, "file", "", "JSON or YAML file holding the table or one profile")
	f.StringVar(&typ, "type", "", "task type of the row to set, e.g. docs_set")
	f.StringVar(&wb, "worker-backend", "", "worker backend for the row")
	f.StringVar(&wm, "worker-model", "", "worker model for the row")
	f.StringVar(&vb, "verify-backend", "", "verification backend for the row")
	f.StringVar(&vm, "verify-model", "", "verification model for the row")
	f.StringVar(&effort, "effort", "", "effort for the row")
	f.StringVar(&second, "second-choice", "", "second choice as backend/model, or none to clear")
	f.BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newTaskRoutingProfileListCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List profiles and show the active one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := progressGet(taskRoutingAPI+"/profiles", nil)
			if err != nil {
				return err
			}
			return taskRoutingOut(cmd, body, asJSON, autonomous.RenderTaskRoutingProfiles)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newTaskRoutingProfileSaveCmd() *cobra.Command {
	var file string
	var asJSON bool
	c := &cobra.Command{
		Use:   "save <name>",
		Short: "Save a profile (a copy of the active one unless --file is given)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := map[string]any{"name": args[0]}
			if file != "" {
				_, prof, err := readTaskRoutingFile(file)
				if err != nil {
					return err
				}
				if prof == nil {
					return fmt.Errorf("%s holds a whole table; give a file with one profile (task type to row)", file)
				}
				req["rows"] = prof
			}
			raw, _ := json.Marshal(req)
			out, err := mergePipelineSend(http.MethodPost, taskRoutingAPI+"/profiles", nil, raw)
			if err != nil {
				return err
			}
			return taskRoutingOut(cmd, out, asJSON, func([]byte) (string, error) {
				return fmt.Sprintf("Saved profile %s.", args[0]), nil
			})
		},
	}
	c.Flags().StringVar(&file, "file", "", "JSON or YAML file holding the profile rows")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newTaskRoutingProfileActivateCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "activate <name>",
		Short: "Make a profile the active one (affects tasks created afterwards)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := json.Marshal(map[string]string{"name": args[0]})
			out, err := mergePipelineSend(http.MethodPut, taskRoutingAPI+"/active-profile", nil, raw)
			if err != nil {
				return err
			}
			return taskRoutingOut(cmd, out, asJSON, func([]byte) (string, error) {
				return fmt.Sprintf("Active profile is now %s.", args[0]), nil
			})
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newTaskRoutingProfileDeleteCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := mergePipelineSend(http.MethodDelete, taskRoutingAPI+"/profiles/"+url.PathEscape(args[0]), nil, nil)
			if err != nil {
				return err
			}
			return taskRoutingOut(cmd, out, asJSON, func([]byte) (string, error) {
				return fmt.Sprintf("Deleted profile %s.", args[0]), nil
			})
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newTaskRoutingProfileExportCmd() *cobra.Command {
	var outFile string
	c := &cobra.Command{
		Use:   "export <name>",
		Short: "Write one profile as a JSON document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := progressGet(taskRoutingAPI+"/profiles/"+url.PathEscape(args[0])+"/export", nil)
			if err != nil {
				return err
			}
			if outFile == "" {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			if err := os.WriteFile(outFile, body, 0o600); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Exported profile %s to %s.\n", args[0], outFile)
			return err
		},
	}
	c.Flags().StringVar(&outFile, "out", "", "write to this file instead of stdout")
	return c
}

func newTaskRoutingProfileImportCmd() *cobra.Command {
	var name string
	var overwrite, asJSON bool
	c := &cobra.Command{
		Use:   "import <file>",
		Short: "Import an exported profile file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := os.ReadFile(args[0]) // #nosec G304 -- operator-supplied path
			if err != nil {
				return err
			}
			q := url.Values{}
			if name != "" {
				q.Set("name", name)
			}
			if overwrite {
				q.Set("overwrite", "true")
			}
			out, err := mergePipelineSend(http.MethodPost, taskRoutingAPI+"/profiles/import", q, doc)
			if err != nil {
				return err
			}
			return taskRoutingOut(cmd, out, asJSON, autonomous.RenderTaskRoutingProfiles)
		},
	}
	c.Flags().StringVar(&name, "name", "", "profile name to import as (default: the name in the file)")
	c.Flags().BoolVar(&overwrite, "overwrite", false, "replace a profile that already has this name")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newTaskRoutingAutomatonCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "automaton <id>",
		Short: "Show which row decided each task's LLM in an Automaton",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := progressGet(taskRoutingAutomatonAPI(args[0], ""), nil)
			if err != nil {
				return err
			}
			return taskRoutingOut(cmd, body, asJSON, func(b []byte) (string, error) {
				return autonomous.RenderTaskRoutingAutomaton(b, true)
			})
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newTaskRoutingDryRunCmd() *cobra.Command {
	var profile string
	var asJSON bool
	c := &cobra.Command{
		Use:   "dry-run <id>",
		Short: "Show what a profile would assign to each task; writes nothing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := json.Marshal(map[string]string{"profile": profile})
			out, err := mergePipelineSend(http.MethodPost, taskRoutingAutomatonAPI(args[0], "/dry-run"), nil, raw)
			if err != nil {
				return err
			}
			return taskRoutingOut(cmd, out, asJSON, func(b []byte) (string, error) {
				return autonomous.RenderTaskRoutingDryRun(b, true)
			})
		},
	}
	c.Flags().StringVar(&profile, "profile", "", "profile to try (default: the active profile)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
