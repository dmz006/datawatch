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
		Short: "Unified stats / process-tree / sub-process monitor (BL171)",
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
