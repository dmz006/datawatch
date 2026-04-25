// BL171 (S9) — `datawatch observer …` + `datawatch setup ebpf` CLI
// subcommands. Thin REST proxies for the observer surface, plus a
// local one-shot installer for eBPF capabilities.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func newObserverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "observer",
		Short: "Unified stats / process-tree / sub-process monitor",
		Long: `Query the datawatch-observer subsystem — structured host +
cpu + mem + disk + gpu + sessions + envelopes + process tree. See
docs/api/observer.md for the full contract.

Subcommands:
  stats                    StatsResponse v2 snapshot
  envelopes                Envelope rollup (per-session / per-backend)
  envelope <id>            Process tree for one envelope
  config-get               Read observer config
  config-set <json>        Replace observer config`,
	}
	cmd.AddCommand(
		newObserverStatsCmd(),
		newObserverEnvelopesCmd(),
		newObserverEnvelopeCmd(),
		newObserverConfigGetCmd(),
		newObserverConfigSetCmd(),
		newObserverPeerCmd(),
		newObserverAgentCmd(),
	)
	return cmd
}

// S13 — agent-flavoured CLI alias. F10 workers register as Shape A
// peers keyed by agent_id; `observer agent stats` is just a friendlier
// face on `observer peer stats` for operators who think in agents.
func newObserverAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Observer view of F10 ephemeral agents (S13)",
		Long: `F10 workers register as Shape A peers automatically when the
parent's observer.peers.allow_register is on. peer name = agent ID.

Subcommands:
  list              List agent peers (alias of observer peer list)
  stats <agent_id>  Last StatsResponse v2 from this agent`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use: "list", Short: "List agent peers (subset of observer peer list)",
			RunE: func(*cobra.Command, []string) error { return daemonGet("/api/observer/peers") },
		},
		&cobra.Command{
			Use: "stats <agent_id>", Short: "Last-known snapshot from this agent",
			Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonGet("/api/observer/peers/" + args[0] + "/stats")
			},
		},
	)
	return cmd
}

// BL172 (S11) — peer registry CLI parity. `datawatch observer peer …`
// surfaces the same /api/observer/peers/* endpoints the PWA card and
// the MCP tools use, so an operator on the parent host can manage
// federated Shape B / C peers without curl + jq.
func newObserverPeerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "peer",
		Short: "Manage federated observer peers (Shape B / C)",
		Long: `Manage Shape B (datawatch-stats standalone) and Shape C
(cluster container) peers registered with this datawatch.

Subcommands:
  list                          List registered peers (TokenHash redacted)
  get <name>                    Detail for one peer
  stats <name>                  Last-known StatsResponse v2 snapshot
  register <name> [shape] [ver] Mint a token (only opportunity to capture it)
  delete <name>                 De-register; peer auto-re-registers next push`,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use: "list", Short: "List registered peers",
			RunE: func(*cobra.Command, []string) error { return daemonGet("/api/observer/peers") },
		},
		&cobra.Command{
			Use: "get <name>", Short: "Peer detail (TokenHash redacted)",
			Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonGet("/api/observer/peers/" + args[0])
			},
		},
		&cobra.Command{
			Use: "stats <name>", Short: "Last-known StatsResponse v2 from this peer",
			Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonGet("/api/observer/peers/" + args[0] + "/stats")
			},
		},
		&cobra.Command{
			Use: "register <name> [shape] [version]",
			Short: "Mint a bearer token for a new Shape B / C peer",
			Args: cobra.RangeArgs(1, 3),
			RunE: func(_ *cobra.Command, args []string) error {
				body := map[string]any{"name": args[0]}
				if len(args) >= 2 {
					body["shape"] = args[1]
				}
				if len(args) >= 3 {
					body["version"] = args[2]
				}
				return daemonJSON(http.MethodPost, "/api/observer/peers", body)
			},
		},
		&cobra.Command{
			Use: "delete <name>", Short: "De-register a peer (rotates the token)",
			Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonJSON(http.MethodDelete, "/api/observer/peers/"+args[0], nil)
			},
		},
	)
	return cmd
}

func newObserverStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use: "stats", Short: "StatsResponse v2 snapshot",
		RunE: func(*cobra.Command, []string) error { return daemonGet("/api/observer/stats") },
	}
}

func newObserverEnvelopesCmd() *cobra.Command {
	return &cobra.Command{
		Use: "envelopes", Short: "Envelope rollup",
		RunE: func(*cobra.Command, []string) error { return daemonGet("/api/observer/envelopes") },
	}
}

func newObserverEnvelopeCmd() *cobra.Command {
	return &cobra.Command{
		Use: "envelope <id>", Short: "Process tree for one envelope",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/observer/envelope?id=" + args[0])
		},
	}
}

func newObserverConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "config-get", Short: "Read observer config",
		RunE: func(*cobra.Command, []string) error { return daemonGet("/api/observer/config") },
	}
}

func newObserverConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "config-set <json>", Short: "Replace observer config (full body)",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var body any
			if err := json.Unmarshal([]byte(args[0]), &body); err != nil {
				return fmt.Errorf("parse JSON: %w", err)
			}
			return daemonJSON(http.MethodPut, "/api/observer/config", body)
		},
	}
}

// `datawatch setup ebpf` already ships in main.go (setcap-based,
// per-binary). v4.1.0 (S9) extends that command via a post-hook in
// the existing setup flow — see newSetupEBPFCmd in main.go. The
// extension: after CAP_BPF is granted, also flip
// observer.ebpf_enabled=true via the live REST (best-effort) so the
// next observer tick picks it up without an operator config edit.
// Shape C (k8s / docker cluster container) still relies on manifest
// capabilities rather than this command — documented under
// docs/api/observer.md#shape-c.

// unused in this file; the observer CLI only hosts the `observer`
// subcommand tree. Kept here to anchor the pointer comment above
// when grepping for `setup ebpf` context.
var _ = runtime.GOOS
var _ = exec.Command
var _ = os.Geteuid
var _ = strings.HasPrefix
