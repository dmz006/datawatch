// v7.0.0 S1 — CLI for the ComputeNode registry.
//
//	datawatch compute node list
//	datawatch compute node get <name>
//	datawatch compute node add <name> --kind <kind> --address <addr> [--monitoring-endpoint URL] [--max-models N] [--gpu-mem-gb N]
//	datawatch compute node update <name> [...same flags as add...]
//	datawatch compute node delete <name>
//	datawatch compute node health <name>
//	datawatch compute node detail <name>     (on-demand pull from monitoring sidecar)
//	datawatch compute node drain <name> [--until RFC3339] [--reason TEXT]
//	datawatch compute node undrain <name>
//	datawatch compute pin config-get
//	datawatch compute pin config-set --seconds N   (pin keeper interval; 0 = off)

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

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
	cmd.AddCommand(newComputePinCmd())
	cmd.AddCommand(newComputeHeadroomCmd())
	return cmd
}

// newComputePinCmd exposes the global pin keeper interval
// (pin_check_seconds). Per-node pins are flags on `compute node add|update`.
func newComputePinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pin",
		Short: "Pin keeper settings (pin_check_seconds)",
	}
	cmd.AddCommand(newComputePinConfigGetCmd(), newComputePinConfigSetCmd())
	return cmd
}

func newComputePinConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config-get",
		Short: "Show pin_check_seconds (0 = pin keeper off)",
		RunE:  func(*cobra.Command, []string) error { return daemonGetSection("/api/config", "pin_check_seconds") },
	}
}

func newComputePinConfigSetCmd() *cobra.Command {
	var seconds int
	cmd := &cobra.Command{
		Use:   "config-set",
		Short: "Set pin_check_seconds (takes effect after the next daemon restart)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("seconds") {
				return fmt.Errorf("--seconds is required")
			}
			if seconds < 0 {
				return fmt.Errorf("--seconds must be a whole number >= 0 (0 = pin keeper off)")
			}
			return daemonJSON(http.MethodPut, "/api/config", map[string]any{"pin_check_seconds": seconds})
		},
	}
	cmd.Flags().IntVar(&seconds, "seconds", 0, "seconds between pin keeper checks (0 = off, default 300)")
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
		Use:   "versions <name>",
		Short: "The node's reported backend version and which capability minimums it meets",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/compute/nodes/" + args[0] + "/versions")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "health <name>",
		Short: "Static + maintenance state for a ComputeNode",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/compute/nodes/" + args[0] + "/health")
		},
	})
	drainCmd := &cobra.Command{
		Use:   "drain <name>",
		Short: "Drain a ComputeNode (stop routing new sessions); optional --until / --reason",
		Long: `Drains a ComputeNode: the scheduler stops routing new sessions to it.
--until takes an RFC3339 timestamp that bounds the drain (omitted =
open-ended until undrained); --reason records why. The daemon replies
with the node health JSON.`,
		Args: cobra.ExactArgs(1),
		RunE: newComputeNodeDrainRunE,
	}
	drainCmd.Flags().String("until", "", "RFC3339 timestamp until which the drain is active (empty = open-ended until undrained)")
	drainCmd.Flags().String("reason", "", "reason to record with the drain")
	cmd.AddCommand(drainCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "undrain <name>",
		Short: "Clear the drain state of a ComputeNode",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/compute/nodes/"+args[0]+"/drain", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "detail <name>",
		Short: "On-demand pull from the Node's monitoring sidecar (--listen)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/compute/nodes/" + args[0] + "/detail")
		},
	})
	cmd.AddCommand(newComputeNodeServedModelsCmd())
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
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/marketplace/ollama/tasks/" + args[0])
		},
	})
	return cmd
}

// newComputeNodeAddCmd builds either `add` or `update` (PUT vs POST).
func newComputeNodeAddCmd(update bool) *cobra.Command {
	var (
		kind, address, monitoringEndpoint, gpuVendor, gpuModel   string
		maxModels, gpuMemGB, ramGB, gpus, priority, sessionSlots int
		verifierSlots, verifierMinFreeMB, verifierContextMB      int
		costPerHour                                              float64
		tagsCSV, allowedCSV, deniedCSV                           string
		pinnedModel                                              string
		allowModelSwap                                           bool
		tlsCAFile, tlsCertFingerprint                            string
		tlsInsecureSkipVerify                                    bool
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
		Long: short + `.

TLS for https nodes: --tls-ca-file trusts an extra CA bundle, --tls-cert-fingerprint
pins the node's leaf certificate, and --tls-insecure-skip-verify turns certificate
checking off for the node (audited, shown in the PWA, and not allowed together with
the other two). The server validates the combination and its error is printed as is.
Update replaces the whole node, so repeat every setting you want to keep.`,
		Args: cobra.ExactArgs(1),
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
				"node_pinned_model":        pinnedModel,
				"allow_model_swap":         allowModelSwap,
				"session_slots":            sessionSlots,
				"verifier_context_mb":      verifierContextMB,
				"tls_ca_file":              tlsCAFile,
				"tls_cert_fingerprint":     tlsCertFingerprint,
				"tls_insecure_skip_verify": tlsInsecureSkipVerify,
			}
			// The nullable verifier slots are -1 by default, meaning "unset":
			// omitted from the body so POST leaves them at the node's derived
			// defaults and UPDATE clears any earlier override. A value below
			// -1 is a typo and is refused here, not at the server.
			if verifierSlots < -1 {
				return fmt.Errorf("--verifier-slots must be -1 (unset) or >= 0")
			}
			if verifierSlots >= 0 {
				body["verifier_slots"] = verifierSlots
			}
			if verifierMinFreeMB < -1 {
				return fmt.Errorf("--verifier-min-free-mem-mb must be -1 (unset) or >= 0")
			}
			if verifierMinFreeMB >= 0 {
				body["verifier_min_free_mem_mb"] = verifierMinFreeMB
			}
			if verifierContextMB < 0 {
				return fmt.Errorf("--verifier-context-mb must be >= 0")
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
	cmd.Flags().IntVar(&sessionSlots, "session-slots", 0, "worker count for this node (0 = derive from observer GPU data: one slot per GPU, limited by free GPU memory; capped by max_concurrent_sessions). Update replaces the whole node, so omitting this clears the override")
	cmd.Flags().IntVar(&verifierSlots, "verifier-slots", -1, "worker slots for verifiers on this node (-1 = unset: 1 if the node hosts a local engine, else 0; explicit 0 = no verifier slots). Update replaces the whole node, so -1 clears the override")
	cmd.Flags().IntVar(&verifierMinFreeMB, "verifier-min-free-mem-mb", -1, "free GPU memory in MB required before a verifier slot starts here (-1 = unset: 1024 MB; 0 turns the check off). Update replaces the whole node, so -1 resets the default")
	cmd.Flags().IntVar(&verifierContextMB, "verifier-context-mb", 0, "extra context memory in MB one verifier slot holds, used to size the advice figure from the node's free GPU memory (0 = none). Update replaces the whole node, so omitting this clears it to 0")
	cmd.Flags().StringVar(&tagsCSV, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&allowedCSV, "allowed-consumers", "", "comma-separated consumer names (council|ask|agent_spawn|session_spawn|*)")
	cmd.Flags().StringVar(&deniedCSV, "denied-consumers", "", "comma-separated consumer names; denied always wins")
	cmd.Flags().StringVar(&pinnedModel, "pinned-model", "", "pin this node to one model; other explicit models are refused unless --allow-model-swap (empty = no pin)")
	cmd.Flags().BoolVar(&allowModelSwap, "allow-model-swap", false, "let an explicit request replace the pinned model (update replaces the whole node, so pass --pinned-model again to keep the pin)")
	cmd.Flags().StringVar(&tlsCAFile, "tls-ca-file", "", "PEM CA bundle trusted in addition to the system roots when connecting to this node over https (update replaces the whole node, so omitting this clears it)")
	cmd.Flags().StringVar(&tlsCertFingerprint, "tls-cert-fingerprint", "", "SHA-256 fingerprint of the node's leaf certificate, 64 hex chars, colons allowed (update replaces the whole node, so omitting this clears the pin)")
	cmd.Flags().BoolVar(&tlsInsecureSkipVerify, "tls-insecure-skip-verify", false, "turn certificate checking off for this node; audited and shown in the PWA; cannot be combined with --tls-ca-file or --tls-cert-fingerprint (update replaces the whole node, so omitting this turns checking back on)")
	return cmd
}

// newComputeNodeDrainRunE is the RunE for `compute node drain <name>`. It
// validates --until, builds the request body (only the keys that were set)
// and POSTs it to the daemon, which replies with the node health JSON.
func newComputeNodeDrainRunE(cmd *cobra.Command, args []string) error {
	until, _ := cmd.Flags().GetString("until")
	reason, _ := cmd.Flags().GetString("reason")
	if err := validateComputeDrainUntil(until); err != nil {
		return err
	}
	body := map[string]any{}
	if until != "" {
		body["until"] = until
	}
	if reason != "" {
		body["reason"] = reason
	}
	return daemonJSON(http.MethodPost, "/api/compute/nodes/"+args[0]+"/drain", body)
}

// validateComputeDrainUntil rejects a --until that is set but not a valid
// RFC3339 timestamp before anything is sent to the daemon.
func validateComputeDrainUntil(until string) error {
	if until == "" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, until); err != nil {
		return fmt.Errorf("--until must be an RFC3339 timestamp (got %q)", until)
	}
	return nil
}

// newComputeHeadroomCmd exposes the node's verifier headroom: a fresh idle
// measurement (measure) and the stored measurement (get). Both call the
// node's /headroom REST route, the same service the other surfaces use.
func newComputeHeadroomCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "headroom",
		Short: "Verifier headroom: measure a node while idle, or show the stored measurement",
		Long: "Verifier headroom commands.\n\n" +
			"  datawatch compute headroom measure <node>   measure the node now while idle and store it (refused while busy)\n" +
			"  datawatch compute headroom get <node>       show the stored measurement, its age and the cap it supports (read only)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "measure <node>",
		Short: "Measure the node's headroom now (idle nodes only) and store it",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/compute/nodes/"+args[0]+"/headroom", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get <node>",
		Short: "Show the stored headroom measurement for the node (read only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/compute/nodes/" + args[0] + "/headroom")
		},
	})
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
