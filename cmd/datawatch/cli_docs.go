// BL274 Sprint 1, v6.16.0 — CLI subcommands for the docs-as-MCP surface.
//
//   datawatch docs search <query> [--limit N] [--sources core,skill:foo]
//   datawatch docs read <path> [--anchor <anchor>]
//   datawatch docs list-howtos
//   datawatch docs apply <howto-id> [--params k=v,...] [--mode plan]
//   datawatch docs trust list
//   datawatch docs trust add <source>
//   datawatch docs trust remove <source>
//   datawatch docs trust pending
//   datawatch docs trust accept <source...>
//   datawatch docs trust dismiss <source...>
//   datawatch docs trust export

package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func newDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Search + apply how-tos via the docs-as-MCP-interface (BL274)",
	}
	cmd.AddCommand(newDocsSearchCmd())
	cmd.AddCommand(newDocsReadCmd())
	cmd.AddCommand(newDocsListHowtosCmd())
	cmd.AddCommand(newDocsApplyCmd())
	cmd.AddCommand(newDocsTrustCmd())
	return cmd
}

func newDocsSearchCmd() *cobra.Command {
	var limit int
	var sources string
	c := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the docs corpus",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			q := url.Values{}
			q.Set("q", strings.Join(args, " "))
			if limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", limit))
			}
			if sources != "" {
				q.Set("sources", sources)
			}
			return daemonGet("/api/docs/search?" + q.Encode())
		},
	}
	c.Flags().IntVar(&limit, "limit", 10, "max hits")
	c.Flags().StringVar(&sources, "sources", "", "comma-separated source filter (core,skill:<n>,plugin:<n>)")
	return c
}

func newDocsReadCmd() *cobra.Command {
	var anchor string
	c := &cobra.Command{
		Use:   "read <path>",
		Short: "Read one section of a doc by path + anchor",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			q := url.Values{}
			q.Set("path", args[0])
			if anchor != "" {
				q.Set("anchor", anchor)
			}
			return daemonGet("/api/docs/read?" + q.Encode())
		},
	}
	c.Flags().StringVar(&anchor, "anchor", "", "section slug (heading slugified)")
	return c
}

func newDocsListHowtosCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-howtos",
		Short: "List runnable how-tos with provenance",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/docs/list-howtos") },
	}
}

func newDocsApplyCmd() *cobra.Command {
	var paramsRaw string
	var mode string
	var approvalToken string
	var riskGate bool
	c := &cobra.Command{
		Use:   "apply <howto-id>",
		Short: "Produce an MCP-call plan (mode=plan) or execute it (mode=execute --approval-token …)",
		Long: `Plan-then-execute flow for a how-to.

  # 1. Plan — returns the step list + an approval_token (5-minute TTL).
  datawatch docs apply howto/secrets-manager.md --params name=GH_TOKEN,value=ghp_…

  # 2. Execute — consumes the token; runs each step via the in-process MCP dispatcher.
  datawatch docs apply howto/secrets-manager.md --mode execute --approval-token <token>

Add --risk-gate to pause before each mutating step and issue a continuation token
(LLM-translated plans force this on automatically).`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			params := map[string]string{}
			if paramsRaw != "" {
				for _, kv := range strings.Split(paramsRaw, ",") {
					if eq := strings.Index(kv, "="); eq > 0 {
						params[strings.TrimSpace(kv[:eq])] = strings.TrimSpace(kv[eq+1:])
					}
				}
			}
			body := map[string]interface{}{
				"howto_id":  args[0],
				"params":    params,
				"mode":      mode,
				"risk_gate": riskGate,
			}
			if approvalToken != "" {
				body["approval_token"] = approvalToken
			}
			return daemonJSON("POST", "/api/docs/apply", body)
		},
	}
	c.Flags().StringVar(&paramsRaw, "params", "", "comma-separated k=v pairs")
	c.Flags().StringVar(&mode, "mode", "plan", "'plan' (default) or 'execute'")
	c.Flags().StringVar(&approvalToken, "approval-token", "", "approval token from a prior plan call (required for --mode execute)")
	c.Flags().BoolVar(&riskGate, "risk-gate", false, "pause before each mutating step; issue a continuation token (LLM-translated plans force this on)")
	return c
}

func newDocsTrustCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "trust", Short: "Manage doc-source trust list"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List trusted sources",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/docs/trust") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "add <source>",
		Short: "Trust a source (skill:<n> or plugin:<n>)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON("POST", "/api/docs/trust", map[string]string{"source": args[0], "granted_by": "operator"})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "remove <source>",
		Short: "Untrust a source",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonJSON("DELETE", "/api/docs/trust/"+args[0], nil) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pending",
		Short: "List sources awaiting an operator trust decision",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/docs/trust/pending") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "accept <source>...",
		Short: "Trust one or more pending sources (multi-arg)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON("POST", "/api/docs/trust/accept", map[string][]string{"sources": args})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "dismiss <source>...",
		Short: "Drop one or more pending sources without trusting them",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON("POST", "/api/docs/trust/dismiss", map[string][]string{"sources": args})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "export",
		Short: "Print current trusted sources as a YAML snippet for committing to config",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/docs/trust/export") },
	})
	return cmd
}
