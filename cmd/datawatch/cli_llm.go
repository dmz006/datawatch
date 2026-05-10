// v7.0.0 S2 — CLI for the LLM-inference registry.

package main

import (
	"encoding/json"
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
		timeoutSeconds                                   int
		costInput, costOutput                            float64
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
			b, _ := json.Marshal(body)
			_ = b
			return daemonJSON(method, urlBuilder(args[0]), body)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "ollama", "ollama | openwebui | opencode | claude")
	cmd.Flags().StringVar(&model, "model", "", "model name (e.g. llama3:8b, claude-sonnet-4-6)")
	cmd.Flags().StringVar(&computeNodesCSV, "compute-nodes", "", "comma-separated ordered ComputeNode names for failover (local kinds only)")
	cmd.Flags().StringVar(&apiKeyRef, "api-key-ref", "", "literal key OR ${secret:name} reference (cloud kinds)")
	cmd.Flags().IntVar(&timeoutSeconds, "timeout-seconds", 0, "per-call timeout (0 = adapter default; local 300s, cloud 60s)")
	cmd.Flags().StringVar(&tagsCSV, "tags", "", "comma-separated tags")
	cmd.Flags().Float64Var(&costInput, "cost-per-1k-input", 0, "USD per 1k input tokens (cloud accounting)")
	cmd.Flags().Float64Var(&costOutput, "cost-per-1k-output", 0, "USD per 1k output tokens")
	return cmd
}

var _ = fmt.Sprintf
