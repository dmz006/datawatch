// v7.0.0 S2 — CLI for the LLM-inference registry.

package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

func newLLMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "Manage LLM-inference registry (v7.0.0 S2)",
		Long: `LLMs are named definitions (kind, model, ordered ComputeNode failover).
Consumers (Council, /api/ask, persona wizard, agent spawn) call LLMs by name;
the dispatcher routes through the configured Nodes with adapter-specific
protocols. Adapters today: ollama, openwebui, opencode, claude.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List every LLM",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/llms") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Fetch one LLM",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/llms/" + args[0]) },
	})
	cmd.AddCommand(newLLMAddCmd(false))
	cmd.AddCommand(newLLMAddCmd(true))
	cmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Remove an LLM",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/llms/"+args[0], nil)
		},
	})
	testCmd := &cobra.Command{
		Use:   "test <name>",
		Short: "Send one inference probe through this LLM (verifies reachability + adapter)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt, _ := cmd.Flags().GetString("prompt")
			body := map[string]any{}
			if prompt != "" {
				body["prompt"] = prompt
			}
			return daemonJSON(http.MethodPost, "/api/llms/"+args[0]+"/test", body)
		},
	}
	testCmd.Flags().String("prompt", "", "prompt text (default: short reachability probe)")
	cmd.AddCommand(testCmd)
	cmd.AddCommand(newLLMEnableCmd(true))
	cmd.AddCommand(newLLMEnableCmd(false))
	cmd.AddCommand(newLLMModelsCmd())
	cmd.AddCommand(newLLMInUseCmd())
	cmd.AddCommand(newLLMRefreshModelsCmd())
	cmd.AddCommand(newLLMReassignCmd())
	cmd.AddCommand(newLLMForceDeleteCmd())
	return cmd
}

func newLLMEnableCmd(enable bool) *cobra.Command {
	use := "enable <name>"
	short := "Enable an LLM (PATCH /api/llms/{name}/enabled enabled=true)"
	if !enable {
		use = "disable <name>"
		short = "Disable an LLM — dispatcher refuses to route through it until re-enabled"
	}
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{"enabled": enable}
			if pretest, _ := cmd.Flags().GetBool("pretest"); pretest && enable {
				body["pretest"] = true
			}
			return daemonJSON(http.MethodPatch, "/api/llms/"+args[0]+"/enabled", body)
		},
	}
	if enable {
		c.Flags().Bool("pretest", false, "run a one-shot reachability probe before flipping enabled=true")
	}
	return c
}

func newLLMAddCmd(update bool) *cobra.Command {
	var (
		kind, model, computeNodesCSV, apiKeyRef, tagsCSV string
		timeoutSeconds, consoleCols, consoleRows          int
		costInput, costOutput                             float64
		binary, outputMode, inputMode                    string
		autoGitInit, autoGitCommit                       bool
		skipPerms, channelEnabled, autoAccept            bool
		permMode, defaultEffort, fallbackChainCSV        string
	)
	use := "add <name>"
	short := "Add a new LLM"
	method := http.MethodPost
	urlBuilder := func(_ string) string { return "/api/llms" }
	if update {
		use = "update <name>"
		short = "Update an existing LLM"
		method = http.MethodPut
		urlBuilder = func(name string) string { return "/api/llms/" + name }
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{
				"name":            args[0],
				"kind":            kind,
				"model":           model,
				"timeout_seconds": timeoutSeconds,
			}
			if apiKeyRef != "" {
				body["api_key_ref"] = apiKeyRef
			}
			if computeNodesCSV != "" {
				nodes := []string{}
				for _, n := range strings.Split(computeNodesCSV, ",") {
					if n = strings.TrimSpace(n); n != "" {
						nodes = append(nodes, n)
					}
				}
				body["compute_nodes"] = nodes
			}
			if tagsCSV != "" {
				body["tags"] = splitCSV(tagsCSV)
			}
			if costInput > 0 {
				body["cost_per_1k_input"] = costInput
			}
			if costOutput > 0 {
				body["cost_per_1k_output"] = costOutput
			}
			if binary != "" {
				body["binary"] = binary
			}
			if consoleCols > 0 {
				body["console_cols"] = consoleCols
			}
			if consoleRows > 0 {
				body["console_rows"] = consoleRows
			}
			if outputMode != "" {
				body["output_mode"] = outputMode
			}
			if inputMode != "" {
				body["input_mode"] = inputMode
			}
			if autoGitInit {
				body["auto_git_init"] = true
			}
			if autoGitCommit {
				body["auto_git_commit"] = true
			}
			if skipPerms {
				body["skip_permissions"] = true
			}
			if channelEnabled {
				body["channel_enabled"] = true
			}
			if autoAccept {
				body["auto_accept_disclaimer"] = true
			}
			if permMode != "" {
				body["permission_mode"] = permMode
			}
			if defaultEffort != "" {
				body["default_effort"] = defaultEffort
			}
			if fallbackChainCSV != "" {
				body["fallback_chain"] = splitCSV(fallbackChainCSV)
			}
			return daemonJSON(method, urlBuilder(args[0]), body)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "ollama", "ollama | openwebui | opencode | claude | claude-code | aider | goose | gemini | shell")
	cmd.Flags().StringVar(&model, "model", "", "model name (e.g. llama3:8b, claude-sonnet-4-6)")
	cmd.Flags().StringVar(&computeNodesCSV, "compute-nodes", "", "comma-separated ordered ComputeNode names for failover (local kinds only)")
	cmd.Flags().StringVar(&apiKeyRef, "api-key-ref", "", "literal key OR ${secret:name} reference (cloud kinds)")
	cmd.Flags().IntVar(&timeoutSeconds, "timeout-seconds", 0, "per-call timeout (0 = adapter default; local 300s, cloud 60s)")
	cmd.Flags().StringVar(&tagsCSV, "tags", "", "comma-separated tags")
	cmd.Flags().Float64Var(&costInput, "cost-per-1k-input", 0, "USD per 1k input tokens (cloud accounting)")
	cmd.Flags().Float64Var(&costOutput, "cost-per-1k-output", 0, "USD per 1k output tokens")
	// Session-backend fields (B/C/D)
	cmd.Flags().StringVar(&binary, "binary", "", "path to the backend binary (session-backend kinds)")
	cmd.Flags().IntVar(&consoleCols, "console-cols", 0, "terminal width in columns (0 = session default)")
	cmd.Flags().IntVar(&consoleRows, "console-rows", 0, "terminal height in rows (0 = session default)")
	cmd.Flags().StringVar(&outputMode, "output-mode", "", "output display mode: terminal | log | chat")
	cmd.Flags().StringVar(&inputMode, "input-mode", "", "input mode: tmux | none")
	cmd.Flags().BoolVar(&autoGitInit, "auto-git-init", false, "initialize git repo in project dir if missing")
	cmd.Flags().BoolVar(&autoGitCommit, "auto-git-commit", false, "commit before/after each session")
	// Claude-code-specific fields
	cmd.Flags().BoolVar(&skipPerms, "skip-permissions", false, "pass --dangerously-skip-permissions (claude-code only)")
	cmd.Flags().BoolVar(&channelEnabled, "channel-enabled", false, "enable MCP channel bridge (claude-code only)")
	cmd.Flags().BoolVar(&autoAccept, "auto-accept-disclaimer", false, "auto-accept startup disclaimers (claude-code only)")
	cmd.Flags().StringVar(&permMode, "permission-mode", "", "permission mode: plan | acceptEdits | auto | bypassPermissions | dontAsk (claude-code only)")
	cmd.Flags().StringVar(&defaultEffort, "default-effort", "", "default effort: quick | normal | thorough (claude-code only)")
	cmd.Flags().StringVar(&fallbackChainCSV, "fallback-chain", "", "comma-separated profile fallback chain (claude-code only)")
	return cmd
}

// newLLMModelsCmd — llm models list|add|remove
func newLLMModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models <subcommand>",
		Short: "Manage enabled models for an LLM",
	}
	// models list <llm-name>
	cmd.AddCommand(&cobra.Command{
		Use:   "list <llm-name>",
		Short: "List enabled models for an LLM",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/llms/" + args[0] + "/models")
		},
	})
	// models add <llm-name> --node <cn> --model <m>
	addCmd := &cobra.Command{
		Use:   "add <llm-name>",
		Short: "Add an enabled model (and optionally bind to a compute node)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, _ := cmd.Flags().GetString("node")
			model, _ := cmd.Flags().GetString("model")
			body := map[string]any{"model": model}
			if node != "" {
				body["node"] = node
			}
			return daemonJSON(http.MethodPost, "/api/llms/"+args[0]+"/models", body)
		},
	}
	addCmd.Flags().String("node", "", "compute node name (leave empty for SaaS kinds)")
	addCmd.Flags().String("model", "", "model name (required)")
	_ = addCmd.MarkFlagRequired("model")
	cmd.AddCommand(addCmd)
	// models remove <llm-name> --node <cn> --model <m>
	rmCmd := &cobra.Command{
		Use:   "remove <llm-name>",
		Short: "Remove an enabled model from an LLM",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, _ := cmd.Flags().GetString("node")
			model, _ := cmd.Flags().GetString("model")
			body := map[string]any{"model": model}
			if node != "" {
				body["node"] = node
			}
			return daemonJSON(http.MethodDelete, "/api/llms/"+args[0]+"/models", body)
		},
	}
	rmCmd.Flags().String("node", "", "compute node name (required for multi-node LLMs)")
	rmCmd.Flags().String("model", "", "model name (required)")
	_ = rmCmd.MarkFlagRequired("model")
	cmd.AddCommand(rmCmd)
	return cmd
}

// newLLMInUseCmd — llm in-use <name> [--filter <text>] [--page N] [--size N]
func newLLMInUseCmd() *cobra.Command {
	var filter string
	var page, size int
	cmd := &cobra.Command{
		Use:   "in-use <name>",
		Short: "Show active sessions, automata, and personas using this LLM",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			qs := fmt.Sprintf("?page=%d&size=%d", page, size)
			if filter != "" {
				qs += "&filter=" + filter
			}
			return daemonGet("/api/llms/" + args[0] + "/in_use" + qs)
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "AND substring filter across name/state columns")
	cmd.Flags().IntVar(&page, "page", 1, "page number (1-based)")
	cmd.Flags().IntVar(&size, "size", 5, "page size (5, 10, or 50)")
	return cmd
}

// newLLMRefreshModelsCmd — llm refresh-models <name>
func newLLMRefreshModelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh-models <name>",
		Short: "Trigger a model-list refresh from all compute nodes for this LLM",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/llms/"+args[0]+"/refresh_models", nil)
		},
	}
}

// newLLMReassignCmd — llm reassign <name> --to-llm <other> [--to-model <m>]
func newLLMReassignCmd() *cobra.Command {
	var toLLM, toModel string
	cmd := &cobra.Command{
		Use:   "reassign <name>",
		Short: "Reassign all active bindings from this LLM to another",
		Long: `Updates every active session, automaton, and persona currently using <name>
to use the target LLM instead. Running sessions pick up the change on
their next LLM call; waiting_input / planning sessions switch immediately.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{"to_llm": toLLM}
			if toModel != "" {
				body["to_model"] = toModel
			}
			return daemonJSON(http.MethodPost, "/api/llms/"+args[0]+"/reassign", body)
		},
	}
	cmd.Flags().StringVar(&toLLM, "to-llm", "", "target LLM name (required)")
	cmd.Flags().StringVar(&toModel, "to-model", "", "specific model within the target LLM (optional)")
	_ = cmd.MarkFlagRequired("to-llm")
	return cmd
}

// newLLMForceDeleteCmd — llm force-delete <name>
func newLLMForceDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "force-delete <name>",
		Short: "Cancel all active bindings then delete (destructive — terminates in-progress work)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := map[string]any{"confirm": "yes I understand this terminates active work"}
			return daemonJSON(http.MethodPost, "/api/llms/"+args[0]+"/force_delete", body)
		},
	}
}

var _ = fmt.Sprintf
