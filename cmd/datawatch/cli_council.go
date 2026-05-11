// BL260 v6.11.0 — CLI subcommands for Council Mode (multi-agent debate).
//
//	datawatch council personas
//	datawatch council run --proposal "..." [--personas a,b] [--mode debate|quick]
//	datawatch council runs [--limit N]
//	datawatch council get-run <id>

package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newCouncilCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "council",
		Short: "Run Council Mode multi-agent debates (BL260)",
		Long: `Council runs a structured multi-persona debate on a proposal.

Two modes:
  debate (3 rounds)  — for serious decisions
  quick  (1 round)   — for fast perspective checks

Built-in 6 personas: security-skeptic, ux-advocate, perf-hawk,
simplicity-advocate, ops-realist, contrarian. Operators may add more
by dropping YAML files into ~/.datawatch/council/personas/.

v6.11.0 ships the framework with stubbed LLM responses (deterministic
placeholders). Real per-persona inference lands in a v6.11.x follow-up.`,
	}
	cmd.AddCommand(newCouncilPersonasCmd())
	cmd.AddCommand(newCouncilRunCmd())
	cmd.AddCommand(newCouncilRunsCmd())
	cmd.AddCommand(newCouncilGetRunCmd())
	cmd.AddCommand(newCouncilCancelCmd()) // v7.0.0 S3
	cmd.AddCommand(newCouncilPersonaWizardCmd())
	cmd.AddCommand(newCouncilConfigCmd())
	return cmd
}

// BL297 v6.22.4 — runtime config knob exposure (closes the Configuration
// Accessibility parity miss admitted in v6.22.3 audit table).
//
//	datawatch council config get
//	datawatch council config set draft-retention-days <N>
func newCouncilConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read or update Council subsystem config (BL297)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Print current Council config (draft_retention_days, ...)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/council/config") },
	})
	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Update one Council config key",
		Long: "Supported keys:\n" +
			"  draft-retention-days <N>   persona-wizard draft GC retention (days; 0 disables)\n" +
			"  llm-ref <name>             LLM registry entry used for debates\n" +
			"  max-parallel <N>           per-round persona concurrency (0 = serial)\n" +
			"  comm-firehose <true|false>  push every persona response to comm channels",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			key, val := args[0], args[1]
			switch key {
			case "draft-retention-days", "draft_retention_days":
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 {
					return fmt.Errorf("draft-retention-days must be a non-negative integer, got %q", val)
				}
				return daemonJSON(http.MethodPatch, "/api/council/config", map[string]any{"draft_retention_days": n})
			case "llm-ref", "llm_ref":
				return daemonJSON(http.MethodPatch, "/api/council/config", map[string]any{"llm_ref": val})
			case "max-parallel", "max_parallel":
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 {
					return fmt.Errorf("max-parallel must be a non-negative integer, got %q", val)
				}
				return daemonJSON(http.MethodPatch, "/api/council/config", map[string]any{"max_parallel": n})
			case "comm-firehose", "comm_firehose":
				b := val == "true" || val == "1" || val == "yes"
				return daemonJSON(http.MethodPatch, "/api/council/config", map[string]any{"comm_firehose": b})
			}
			return fmt.Errorf("unsupported key: %s", key)
		},
	}
	cmd.AddCommand(setCmd)
	return cmd
}

// BL297 v6.22.3 — CLI wizard subcommands.
//
//	datawatch council persona-wizard one-shot --name X --role Y --focus ... [--save]
//	datawatch council persona-wizard list
//	datawatch council persona-wizard delete <id>
//	datawatch council persona-wizard purge
//
// Per Q9 design: CLI gets one-shot with optional save (no interactive
// chat-loop on the CLI). PWA / chat channels host the full wizard.
func newCouncilPersonaWizardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "persona-wizard",
		Short: "Add a Council persona via LLM-assisted one-shot draft (BL297)",
	}
	cmd.AddCommand(newCouncilOneShotCmd())
	cmd.AddCommand(newCouncilDraftsListCmd())
	cmd.AddCommand(newCouncilDraftsDeleteCmd())
	cmd.AddCommand(newCouncilDraftsPurgeCmd())
	return cmd
}

func newCouncilOneShotCmd() *cobra.Command {
	var name, role, focus, stance, tone, anti, examples, backend string
	var save bool
	cmd := &cobra.Command{
		Use:   "one-shot",
		Short: "Draft a persona from interview answers in one LLM call",
		RunE: func(_ *cobra.Command, _ []string) error {
			body := map[string]any{
				"name": name, "role": role, "focus": focus, "stance": stance,
				"tone": tone, "anti_patterns": anti, "examples": examples,
				"backend": backend,
			}
			return daemonJSON(http.MethodPost, "/api/council/personas/oneshot", body)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name (required)")
	cmd.Flags().StringVar(&role, "role", "", "title or short role")
	cmd.Flags().StringVar(&focus, "focus", "", "focus area / domain expertise")
	cmd.Flags().StringVar(&stance, "stance", "", "debate stance (challenger / advocate / skeptic / etc.)")
	cmd.Flags().StringVar(&tone, "tone", "", "voice / tone")
	cmd.Flags().StringVar(&anti, "anti-patterns", "", "what to push back on")
	cmd.Flags().StringVar(&examples, "examples", "", "what kinds of proposals to engage with")
	cmd.Flags().StringVar(&backend, "backend", "", "ollama | openwebui (default: server policy)")
	cmd.Flags().BoolVar(&save, "save", false, "after drafting, POST to /api/council/personas to register")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newCouncilDraftsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List in-progress + completed persona-wizard drafts",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/council/personas/drafts") },
	}
}

func newCouncilDraftsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a single persona-wizard draft",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/council/personas/drafts/"+args[0], nil)
		},
	}
}

func newCouncilDraftsPurgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "purge",
		Short: "Delete ALL persona-wizard drafts (ignores retention window)",
		RunE: func(*cobra.Command, []string) error {
			return daemonJSON(http.MethodDelete, "/api/council/personas/drafts", nil)
		},
	}
}

func newCouncilPersonasCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "personas",
		Short: "List registered Council personas",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/council/personas") },
	}
}

func newCouncilRunCmd() *cobra.Command {
	var proposal, personas, mode string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a Council debate on a proposal",
		RunE: func(_ *cobra.Command, _ []string) error {
			body := map[string]any{"proposal": proposal, "mode": mode}
			if personas != "" {
				parts := []string{}
				for _, p := range strings.Split(personas, ",") {
					if p = strings.TrimSpace(p); p != "" {
						parts = append(parts, p)
					}
				}
				body["personas"] = parts
			}
			return daemonJSON(http.MethodPost, "/api/council/run", body)
		},
	}
	cmd.Flags().StringVar(&proposal, "proposal", "", "the proposal text (required)")
	cmd.Flags().StringVar(&personas, "personas", "", "comma-separated persona names (default: all)")
	cmd.Flags().StringVar(&mode, "mode", "quick", "debate or quick")
	_ = cmd.MarkFlagRequired("proposal")
	return cmd
}

func newCouncilRunsCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List past Council runs",
		RunE: func(_ *cobra.Command, _ []string) error {
			path := "/api/council/runs"
			if limit > 0 {
				path = path + "?limit=" + itoa(limit)
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "max runs to return")
	return cmd
}

func newCouncilGetRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get-run <id>",
		Short: "Fetch one Council run by id",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/council/runs/" + args[0]) },
	}
}

// newCouncilCancelCmd — v7.0.0 S3.
//
//	datawatch council cancel <run-id>
//
// Stops an in-flight council run; ctx.Cancel propagates to in-flight
// LLM calls. Returns 404 when the run has already finished.
func newCouncilCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel an in-flight Council run (v7.0.0 S3)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/council/runs/"+args[0]+"/cancel", nil)
		},
	}
}
