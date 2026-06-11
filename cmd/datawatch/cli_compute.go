// v7.0.0 S1 — CLI for the ComputeNode registry.
//
//	datawatch compute node list
//	datawatch compute node get <name>
//	datawatch compute node add <name> --kind <kind> --address <addr> [--monitoring-endpoint URL] [--max-models N] [--gpu-mem-gb N]
//	datawatch compute node update <name> [...same flags as add...]
//	datawatch compute node delete <name>
//	datawatch compute node health <name>
//	datawatch compute node detail <name>     (on-demand pull from monitoring sidecar)

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

func newComputeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compute",
		Short: "Manage ComputeNode registry (v7.0.0 S1)",
		Long: `ComputeNodes are anywhere local LLM workloads run: a host, a GPU box,
a cluster behind a load balancer, a containerized runtime, a remote-proxied
datawatch peer. The LLM registry (S2) routes calls through Nodes via
ordered failover. See docs/plans/2026-05-08-v7.0.0-plan.md § 5.`,
	}
	cmd.AddCommand(newComputeNodeCmd())
	cmd.AddCommand(newComputeMigrateCmd())
	return cmd
}

// newComputeMigrateCmd — BL342: expose PUT /api/migration/compute-kinds as a CLI command.
// Without this, headless installs had no path forward when all CLI-creatable
// NodeKinds (local/remote/ssh/docker/k8s/remote-proxy) were rejected by the
// dispatcher. The web-UI migration banner called this endpoint already; we're
// just surfacing it on the CLI.
func newComputeMigrateCmd() *cobra.Command {
	var kind string
	var all bool

	cmd := &cobra.Command{
		Use:   "migrate [<node-name>]",
		Short: "Migrate deprecated ComputeNode kind(s) to ollama or openai-compat",
		Long: `Migrates one or all ComputeNodes from a deprecated kind
(local | remote | ssh | docker | k8s | remote-proxy) to a supported kind.

Examples:
  # List nodes that need migration:
  datawatch compute migrate

  # Migrate one node:
  datawatch compute migrate mynode --kind ollama

  # Migrate all deprecated nodes at once:
  datawatch compute migrate --all --kind ollama`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if all {
				if kind == "" {
					return fmt.Errorf("--kind required with --all (use: ollama | openai-compat)")
				}
				return computeMigrateAll(kind)
			}
			if len(args) == 0 {
				// No args + no --all: show list of deprecated nodes.
				return daemonGet("/api/migration/compute-kinds")
			}
			if kind == "" {
				return fmt.Errorf("--kind required (use: ollama | openai-compat)")
			}
			return daemonJSON(http.MethodPut, "/api/migration/compute-kinds/"+args[0], map[string]any{"kind": kind})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "target kind: ollama or openai-compat")
	cmd.Flags().BoolVar(&all, "all", false, "migrate every deprecated ComputeNode in one pass")
	return cmd
}

// computeMigrateAll fetches the deprecated-node list then PUTs each one.
func computeMigrateAll(kind string) error {
	req, err := http.NewRequest(http.MethodGet, daemonURL()+"/api/migration/compute-kinds", nil)
	if err != nil {
		return err
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var result struct {
		Nodes []struct {
			Name string `json:"name"`
			Kind string `json:"current_kind"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if len(result.Nodes) == 0 {
		fmt.Println("No deprecated ComputeNodes found — nothing to migrate.")
		return nil
	}
	fmt.Printf("Migrating %d node(s) to kind=%q…\n", len(result.Nodes), kind)
	var failed int
	for _, n := range result.Nodes {
		if err := daemonJSON(http.MethodPut, "/api/migration/compute-kinds/"+n.Name, map[string]any{"kind": kind}); err != nil {
			fmt.Fprintf(os.Stderr, "  [error] %s (%s): %v\n", n.Name, n.Kind, err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d node(s) failed to migrate", failed)
	}
	return nil
}

func newComputeNodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "ComputeNode CRUD + health + on-demand detail",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List every ComputeNode",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/compute/nodes") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get <name>",
		Short: "Fetch one ComputeNode",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/compute/nodes/" + args[0]) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "health <name>",
		Short: "Static + maintenance state for a ComputeNode",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/compute/nodes/" + args[0] + "/health") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "detail <name>",
		Short: "On-demand pull from the Node's monitoring sidecar (--listen)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/compute/nodes/" + args[0] + "/detail") },
	})
	cmd.AddCommand(newComputeNodeAddCmd(false))
	cmd.AddCommand(newComputeNodeAddCmd(true))
	cmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a ComputeNode from the registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/compute/nodes/"+args[0], nil)
		},
	})
	// alpha.23b — observer-peer attach/detach.
	cmd.AddCommand(&cobra.Command{
		Use:   "attach-observer <name> <peer>",
		Short: "Attach a registered observer peer (datawatch-stats) to this ComputeNode",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPut, "/api/compute/nodes/"+args[0]+"/observer-peer", map[string]any{"peer": args[1]})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "detach-observer <name>",
		Short: "Clear the observer-peer binding on this ComputeNode",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/compute/nodes/"+args[0]+"/observer-peer", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "observer-free",
		Short: "List registered observer peers with no bound ComputeNode",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/observer/peers/free") },
	})
	// alpha.24 #231 — per-node grouping + meta-peers aggregator.
	cmd.AddCommand(&cobra.Command{
		Use:   "observer-by-node",
		Short: "Group local observer peers by their bound ComputeNode (alpha.24)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/observer/peers/by-node") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "federation-meta-peers",
		Short: "Federation meta-peers view: peers grouped by ComputeNode across primaries (alpha.24)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/federation/meta-peers") },
	})
	// alpha.33 #244 — marketplace + per-Node Ollama model pull/remove.
	cmd.AddCommand(&cobra.Command{
		Use:   "pull-model <name> <model>",
		Short: "Pull an Ollama model on a ComputeNode (background; returns task_id)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/compute/nodes/"+args[0]+"/models/pull", map[string]any{"model": args[1]})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "remove-model <name> <model>",
		Short: "Remove an Ollama model from a ComputeNode",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/compute/nodes/"+args[0]+"/models/"+args[1], nil)
		},
	})
	return cmd
}

func newMarketplaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "marketplace",
		Short: "Ollama marketplace: catalog + pull task status (alpha.33 #244)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "catalog",
		Short: "List embedded curated Ollama model catalog",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/marketplace/ollama/catalog") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "task <task_id>",
		Short: "Poll a model-pull task by ID",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/marketplace/ollama/tasks/" + args[0]) },
	})
	return cmd
}

// newComputeNodeAddCmd builds either `add` or `update` (PUT vs POST).
func newComputeNodeAddCmd(update bool) *cobra.Command {
	var (
		kind, address, monitoringEndpoint, gpuVendor, gpuModel string
		maxModels, gpuMemGB, ramGB, gpus, priority             int
		costPerHour                                            float64
		tagsCSV, allowedCSV, deniedCSV                         string
	)
	use := "add <name>"
	short := "Add a new ComputeNode"
	method := http.MethodPost
	urlBuilder := func(_ string) string { return "/api/compute/nodes" }
	if update {
		use = "update <name>"
		short = "Update an existing ComputeNode"
		method = http.MethodPut
		urlBuilder = func(name string) string { return "/api/compute/nodes/" + name }
	}
	deprecatedKinds := map[string]bool{
		"local": true, "ssh": true, "docker": true,
		"k8s": true, "remote": true, "remote-proxy": true,
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if deprecatedKinds[kind] {
				return fmt.Errorf(
					"kind %q is no longer supported — use --kind ollama or --kind openai-compat\n"+
						"If you have existing nodes with deprecated kinds, run: datawatch compute migrate",
					kind,
				)
			}
			body := map[string]any{
				"name":                args[0],
				"kind":                kind,
				"address":             address,
				"monitoring_endpoint": monitoringEndpoint,
				"scheduling_priority": priority,
				"cost_per_hour":       costPerHour,
				"declared_capacity": map[string]any{
					"gpus":                  gpus,
					"gpu_mem_gb":            gpuMemGB,
					"ram_gb":                ramGB,
					"max_concurrent_models": maxModels,
					"gpu_vendor":            gpuVendor,
					"gpu_model":             gpuModel,
				},
			}
			if tagsCSV != "" {
				body["tags"] = splitCSV(tagsCSV)
			}
			perm := map[string]any{}
			if allowedCSV != "" {
				perm["allowed_consumers"] = splitCSV(allowedCSV)
			}
			if deniedCSV != "" {
				perm["denied_consumers"] = splitCSV(deniedCSV)
			}
			if len(perm) > 0 {
				body["permissions"] = perm
			}
			b, _ := json.Marshal(body)
			_ = b
			return daemonJSON(method, urlBuilder(args[0]), body)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "ollama", "ollama | openai-compat  (deprecated: local | ssh | docker | k8s | remote | remote-proxy)")
	cmd.Flags().StringVar(&address, "address", "", "host:port or URL (required for ssh/remote/remote-proxy)")
	cmd.Flags().StringVar(&monitoringEndpoint, "monitoring-endpoint", "", "stub --listen URL (e.g. https://gpu-1:9001/api/stats) for on-demand detail")
	cmd.Flags().IntVar(&maxModels, "max-models", 0, "declared capacity: max concurrent models")
	cmd.Flags().IntVar(&gpuMemGB, "gpu-mem-gb", 0, "declared capacity: GPU memory in GB")
	cmd.Flags().IntVar(&ramGB, "ram-gb", 0, "declared capacity: system RAM in GB")
	cmd.Flags().IntVar(&gpus, "gpus", 0, "declared capacity: number of GPUs")
	cmd.Flags().StringVar(&gpuVendor, "gpu-vendor", "", "nvidia | amd | intel | (blank)")
	cmd.Flags().StringVar(&gpuModel, "gpu-model", "", "free-form model string (e.g. RTX 4090)")
	cmd.Flags().IntVar(&priority, "priority", 50, "scheduling priority 0-100 (higher = preferred)")
	cmd.Flags().Float64Var(&costPerHour, "cost-per-hour", 0, "USD/hour cost for scheduler accounting")
	cmd.Flags().StringVar(&tagsCSV, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&allowedCSV, "allowed-consumers", "", "comma-separated consumer names (council|ask|agent_spawn|session_spawn|*)")
	cmd.Flags().StringVar(&deniedCSV, "denied-consumers", "", "comma-separated consumer names; denied always wins")
	return cmd
}

func splitCSV(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		if r == ' ' && cur == "" {
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

var _ = fmt.Sprintf
