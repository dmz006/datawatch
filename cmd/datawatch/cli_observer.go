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
	"strconv"
	"strings"
	"text/tabwriter"

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
		newObserverEnvelopesAllPeersCmd(),
		newObserverEnvelopeCmd(),
		newObserverConfigGetCmd(),
		newObserverConfigSetCmd(),
		newObserverPeerCmd(),
		newObserverAgentCmd(),
		newObserverPullCmd(),
	)
	return cmd
}

// S13 — agent-flavoured CLI alias. F10 workers register as Shape A
// peers keyed by agent_id; `observer agent stats` is just a friendlier
// face on `observer peer stats` for operators who think in agents.
func newObserverAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Observer view of ephemeral agent workers",
		Long: `Ephemeral workers register as observer peers automatically when
the parent's observer.peers.allow_register is on. peer name = agent ID.

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
		Short: "Manage federated observer peers",
		Long: `Manage federated observer peers — standalone datawatch-stats
daemons and cluster-container deployments — registered with this datawatch.

Subcommands:
  list                          List registered peers (TokenHash redacted)
  get <name>                    Detail for one peer
  stats <name>                  Last-known StatsResponse v2 snapshot
  register <name> [shape] [ver] Mint a token (only opportunity to capture it)
  delete <name>                 De-register; peer auto-re-registers next push`,
	}
	peerList := &cobra.Command{
		Use:   "list",
		Short: "List registered peers (push / pull / shadow) with source and staleness",
		Long: `List observer peers — push-registered peers, pulled peers
(source=pull) and shadow rows (federated peers with observer_pull off)
— as a table. The SOURCE column is push, pull or shadow; STALE reflects
the row's pull state (stale / ok when pull is enabled, "-" when the peer
has no pull configured).

Use --json to print the raw GET /api/observer/peers response instead.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if raw, _ := cmd.Flags().GetBool("json"); raw {
				return daemonGet("/api/observer/peers")
			}
			body, err := daemonJSONBody(http.MethodGet, "/api/observer/peers", nil)
			if err != nil {
				return err
			}
			table, err := renderObserverPeersTable(body)
			if err != nil {
				// Not the expected JSON envelope — show the raw body
				// rather than hiding the server response.
				fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(string(body)))
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), table)
			return nil
		},
	}
	cmd.AddCommand(peerList,
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
			Use:   "register <name> [shape] [version]",
			Short: "Mint a bearer token for a new federated peer",
			Args:  cobra.RangeArgs(1, 3),
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
	peerList.Flags().Bool("json", false, "print the raw JSON response instead of the table")
	return cmd
}

// Observer pull protocol — read-only status surface. The per-peer pull
// targets and their latest pull state come from GET /api/observer/pull
// (same data as the observer_pull_status MCP tool). Configuration stays
// on `datawatch config set observer.pull.*` and `federation peer
// add/update --observer-pull*`.
func newObserverPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Observer pull protocol",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show per-peer observer pull status (GET /api/observer/pull)",
			Long: `Show the observer pull protocol's current per-peer state: source,
shadow (also push-registered), stale (+ reason: none / timeout / error /
forbidden / not_permitted / bad_payload / unknown), last success and the
effective interval/timeout in seconds.

Use --json to print the raw GET /api/observer/pull response instead.`,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if raw, _ := cmd.Flags().GetBool("json"); raw {
					return daemonGet("/api/observer/pull")
				}
				body, err := daemonJSONBody(http.MethodGet, "/api/observer/pull", nil)
				if err != nil {
					return err
				}
				table, err := renderObserverPullStatusTable(body)
				if err != nil {
					// Not the expected JSON envelope — show the raw body
					// rather than hiding the server response.
					fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(string(body)))
					return nil
				}
				fmt.Fprint(cmd.OutOrStdout(), table)
				return nil
			},
		},
	)
	cmd.Commands()[0].Flags().Bool("json", false, "print the raw JSON response instead of the table")
	return cmd
}

// ── Observer peer / pull table rendering ───────────────────────────────────
// renderObserverPeersTable renders a GET /api/observer/peers response
// ({"peers":[…]}) as a table with a SOURCE column (push / pull / shadow —
// a shadow row is a federated peer with observer_pull off and no push
// registration) and a STALE column (stale / ok when the row's pull is
// enabled, "-" when the peer has no pull state).
func renderObserverPeersTable(body []byte) (string, error) {
	var payload struct {
		Peers []map[string]any `json:"peers"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode /api/observer/peers: %w", err)
	}
	table := [][]string{{"NAME", "SHAPE", "VERSION", "SOURCE", "STALE", "LAST_PUSH"}}
	for _, row := range payload.Peers {
		table = append(table, []string{
			cellString(row["name"]),
			cellString(row["shape"]),
			cellString(row["version"]),
			observerPeersSourceCell(row),
			observerPeersStaleCell(row),
			cellString(row["last_push_at"]),
		})
	}
	return renderCLITable(table), nil
}

// renderObserverPullStatusTable renders a GET /api/observer/pull response
// ({"peers":[…]}) as a table.
func renderObserverPullStatusTable(body []byte) (string, error) {
	var payload struct {
		Peers []map[string]any `json:"peers"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode /api/observer/pull: %w", err)
	}
	table := [][]string{{"NAME", "SOURCE", "SHADOW", "STALE", "STALE_REASON", "LAST_SUCCESS", "INTERVAL_S", "TIMEOUT_S"}}
	for _, row := range payload.Peers {
		table = append(table, []string{
			cellString(row["name"]),
			cellString(row["source"]),
			cellString(row["shadow"]),
			staleYesNoCell(row["stale"]),
			cellString(row["stale_reason"]),
			cellString(row["last_success_at"]),
			cellString(row["interval_s"]),
			cellString(row["timeout_s"]),
		})
	}
	return renderCLITable(table), nil
}

// observerPeersSourceCell returns the SOURCE column value for one peer
// row: "shadow" when the row is flagged shadow (federated, pull off),
// otherwise the row's source (push or pull).
func observerPeersSourceCell(row map[string]any) string {
	if b, _ := row["shadow"].(bool); b {
		return "shadow"
	}
	if s, _ := row["source"].(string); s != "" {
		return s
	}
	return "-"
}

// observerPeersStaleCell returns the STALE column value for one peer
// row: "stale" / "ok" when the row's pull object is enabled, "-" when the
// peer has no pull configured (push-only or shadow).
func observerPeersStaleCell(row map[string]any) string {
	pull, ok := row["pull"].(map[string]any)
	if !ok {
		return "-"
	}
	if enabled, _ := pull["enabled"].(bool); !enabled {
		return "-"
	}
	return staleYesNoCell(pull["stale"])
}

// staleYesNoCell renders a boolean stale field as "stale" / "ok".
func staleYesNoCell(v any) string {
	if b, _ := v.(bool); b {
		return "stale"
	}
	return "ok"
}

// cellString renders one JSON cell for table output; missing/empty values
// become "-".
func cellString(v any) string {
	switch s := v.(type) {
	case nil:
		return "-"
	case string:
		if s == "" {
			return "-"
		}
		return s
	case bool:
		if s {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case int:
		return strconv.Itoa(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// renderCLITable renders rows (header first) as a tab-aligned table.
func renderCLITable(rows [][]string) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		_, _ = w.Write([]byte(strings.Join(row, "\t") + "\n"))
	}
	_ = w.Flush()
	return b.String()
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

// BL180 Phase 2 cross-host (v5.12.0).
func newObserverEnvelopesAllPeersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "envelopes-all-peers",
		Short: "Federation-aware envelope view (local + every peer with cross-peer Caller attribution)",
		RunE: func(*cobra.Command, []string) error {
			return daemonGet("/api/observer/envelopes/all-peers")
		},
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
