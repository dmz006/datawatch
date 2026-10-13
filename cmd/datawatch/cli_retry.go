// BL410 Phase 1 — `datawatch retry ...` CLI subcommands.
// Retry profile CRUD + per-Automaton retry-policy overrides. Mirrors
// cli_guardrail.go exactly (same REST helpers, same command shape) —
// found live 2026-10-13 that this command was documented in
// docs/howto/retry-policy.md but never actually built, propagating a
// pre-existing fabrication from docs/howto/guardrail-library.md's own
// (since-verified-real) CLI section into a new doc without checking.

package main

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newRetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retry",
		Short: "Retry-policy profile and per-Automaton override management",
		Long: `Manage retry profiles and per-Automaton retry-policy overrides (BL410).

Subcommands:
  profile list                                  List retry profiles
  profile create --name <n> [flags]             Create a profile
  profile get <id>                              Get one profile
  profile update <id> [flags]                   Update a profile
  profile delete <id>                           Delete a profile
  automaton set <prd-id> [flags]                Set per-Automaton retry-policy override`,
	}
	cmd.AddCommand(
		newRetryProfileCmd(),
		newRetryAutomatonCmd(),
	)
	return cmd
}

// ── profile ───────────────────────────────────────────────────────────────

func newRetryProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage retry profiles",
	}
	var name, description, budgetsCSV string
	var ceilingTotal, infraEscalateAfter int
	var contentAutoFix, autoFixSkipApproval bool

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List retry profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return guardrailAPIGet("/api/autonomous/retry_profiles")
		},
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a retry profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			budgets, err := csvToIntMap(budgetsCSV)
			if err != nil {
				return err
			}
			body := map[string]any{
				"name": name, "description": description, "budgets": budgets,
				"ceiling_total": ceilingTotal, "infra_retry_escalate_after": infraEscalateAfter,
			}
			if cmd.Flags().Changed("content-auto-fix") {
				body["content_auto_fix_enabled"] = contentAutoFix
			}
			if cmd.Flags().Changed("auto-fix-skip-approval") {
				body["auto_fix_skip_approval"] = autoFixSkipApproval
			}
			return guardrailAPIPost("/api/autonomous/retry_profiles", body)
		},
	}
	createCmd.Flags().StringVar(&name, "name", "", "Profile name (required)")
	createCmd.Flags().StringVar(&description, "description", "", "Optional description")
	createCmd.Flags().StringVar(&budgetsCSV, "budgets", "", `Per-category budgets, e.g. "infra=4,content=1,scope=0,policy=1"`)
	createCmd.Flags().IntVar(&ceilingTotal, "ceiling-total", 0, "Total retry budget for this profile (0 = inherit global)")
	createCmd.Flags().IntVar(&infraEscalateAfter, "infra-escalate-after", 0, "Same-config infra retries before escalating (0 = inherit global)")
	createCmd.Flags().BoolVar(&contentAutoFix, "content-auto-fix", true, "Enable content-category auto-fix under this profile")
	createCmd.Flags().BoolVar(&autoFixSkipApproval, "auto-fix-skip-approval", true, "Synthesized corrective work skips per-story approval under this profile")
	_ = createCmd.MarkFlagRequired("name")

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get one retry profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return guardrailAPIGet("/api/autonomous/retry_profiles/" + args[0])
		},
	}

	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a retry profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			budgets, err := csvToIntMap(budgetsCSV)
			if err != nil {
				return err
			}
			body := map[string]any{
				"name": name, "description": description, "budgets": budgets,
				"ceiling_total": ceilingTotal, "infra_retry_escalate_after": infraEscalateAfter,
			}
			if cmd.Flags().Changed("content-auto-fix") {
				body["content_auto_fix_enabled"] = contentAutoFix
			}
			if cmd.Flags().Changed("auto-fix-skip-approval") {
				body["auto_fix_skip_approval"] = autoFixSkipApproval
			}
			return guardrailAPIPut("/api/autonomous/retry_profiles/"+args[0], body)
		},
	}
	updateCmd.Flags().StringVar(&name, "name", "", "New profile name")
	updateCmd.Flags().StringVar(&description, "description", "", "New description")
	updateCmd.Flags().StringVar(&budgetsCSV, "budgets", "", `New per-category budgets, e.g. "infra=4"`)
	updateCmd.Flags().IntVar(&ceilingTotal, "ceiling-total", 0, "New ceiling total")
	updateCmd.Flags().IntVar(&infraEscalateAfter, "infra-escalate-after", 0, "New infra escalation threshold")
	updateCmd.Flags().BoolVar(&contentAutoFix, "content-auto-fix", true, "New content-auto-fix-enabled value")
	updateCmd.Flags().BoolVar(&autoFixSkipApproval, "auto-fix-skip-approval", true, "New auto-fix-skip-approval value")

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a retry profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return guardrailAPIDelete("/api/autonomous/retry_profiles/" + args[0])
		},
	}

	cmd.AddCommand(listCmd, createCmd, getCmd, updateCmd, deleteCmd)
	return cmd
}

// ── automaton ─────────────────────────────────────────────────────────────

func newRetryAutomatonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "automaton",
		Short: "Per-Automaton retry-policy override",
	}

	var profile, policyCSV string
	var ceilingTotal int
	var contentAutoFix, autoFixSkipApproval bool
	setCmd := &cobra.Command{
		Use:   "set <prd-id>",
		Short: "Set per-Automaton retry-policy overrides (profile / explicit budgets / ceiling)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			policy, err := csvToIntMap(policyCSV)
			if err != nil {
				return err
			}
			body := map[string]any{
				"retry_profile": profile, "retry_policy": policy, "retry_ceiling_total": ceilingTotal,
			}
			if cmd.Flags().Changed("content-auto-fix") {
				body["content_auto_fix_enabled"] = contentAutoFix
			}
			if cmd.Flags().Changed("auto-fix-skip-approval") {
				body["auto_fix_skip_approval"] = autoFixSkipApproval
			}
			return guardrailAPIPut("/api/autonomous/prds/"+args[0]+"/set_retry_policy", body)
		},
	}
	setCmd.Flags().StringVar(&profile, "profile", "", "Named retry profile")
	setCmd.Flags().StringVar(&policyCSV, "policy", "", `Explicit per-category budgets (beats profile), e.g. "infra=4,content=1"`)
	setCmd.Flags().IntVar(&ceilingTotal, "ceiling-total", 0, "Explicit ceiling for this PRD (beats profile/global)")
	setCmd.Flags().BoolVar(&contentAutoFix, "content-auto-fix", true, "Explicit content-auto-fix-enabled override for this PRD")
	setCmd.Flags().BoolVar(&autoFixSkipApproval, "auto-fix-skip-approval", true, "Explicit auto-fix-skip-approval override for this PRD")

	cmd.AddCommand(setCmd)
	return cmd
}

// csvToIntMap parses "k=v,k=v" into map[string]int — the CLI-flag
// sibling of the MCP tools' *_json parameters, same shape csvToSlice
// (cli_guardrail.go) already uses for string lists.
func csvToIntMap(s string) (map[string]int, error) {
	if s == "" {
		return nil, nil
	}
	out := map[string]int{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, err
		}
		out[strings.TrimSpace(k)] = n
	}
	return out, nil
}
