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
	"net/http"

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
	return cmd
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
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
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
	cmd.Flags().StringVar(&kind, "kind", "remote", "local | ssh | docker | k8s | remote | remote-proxy")
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
