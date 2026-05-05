// BL257 Phase 1 v6.8.0 — CLI subcommands for the identity / Telos layer.
//
//	datawatch identity get [--field <name>]
//	datawatch identity set --field <name> --value <value>
//	datawatch identity show           (alias for get with full pretty-print)
//	datawatch identity edit           (opens identity.yaml in $EDITOR)
//
// All commands talk to the running daemon via /api/identity.
//
// `set` uses HTTP PATCH so non-supplied fields are preserved. To
// completely replace, edit the file via `datawatch identity edit` or PUT
// directly to the REST endpoint.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newIdentityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "identity",
		Short: "Manage operator identity / Telos (BL257 Phase 1)",
		Long: `Operator identity is a structured self-description (role, north-star
goals, current projects, values, current focus, context notes) loaded
from ~/.datawatch/identity.yaml. The daemon injects it into every LLM
session's L0 wake-up layer so AI work stays anchored to operator
priorities.

Source: PAI's Telos concept. See docs/plans/2026-05-02-pai-comparison-analysis.md §8.`,
	}
	cmd.AddCommand(newIdentityGetCmd())
	cmd.AddCommand(newIdentityShowCmd())
	cmd.AddCommand(newIdentitySetCmd())
	cmd.AddCommand(newIdentityEditCmd())
	return cmd
}

func newIdentityGetCmd() *cobra.Command {
	var field string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the operator identity (or one field)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if field == "" {
				return daemonGet("/api/identity")
			}
			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Get(daemonURL() + "/api/identity")
			if err != nil {
				return fmt.Errorf("daemon not reachable: %w", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode/100 != 2 {
				return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			}
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				return err
			}
			v := m[field]
			if v == nil {
				return fmt.Errorf("field %q not set", field)
			}
			b, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
	cmd.Flags().StringVar(&field, "field", "", "single field to print (role|north_star_goals|current_projects|values|current_focus|context_notes)")
	return cmd
}

func newIdentityShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Pretty-print the full identity",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/identity") },
	}
}

func newIdentitySetCmd() *cobra.Command {
	var field, value string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set a single identity field (PATCH semantics — others preserved)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if field == "" || value == "" {
				return fmt.Errorf("--field and --value are required")
			}
			patch, err := identityFieldPatchMap(field, value)
			if err != nil {
				return err
			}
			return daemonJSON(http.MethodPatch, "/api/identity", patch)
		},
	}
	cmd.Flags().StringVar(&field, "field", "", "field to set (role|north_star_goals|current_projects|values|current_focus|context_notes)")
	cmd.Flags().StringVar(&value, "value", "", "value (comma-separated for list fields)")
	return cmd
}

func newIdentityEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open ~/.datawatch/identity.yaml in $EDITOR",
		RunE: func(*cobra.Command, []string) error {
			ed := os.Getenv("EDITOR")
			if ed == "" {
				ed = "vi"
			}
			home, _ := os.UserHomeDir()
			p := filepath.Join(home, ".datawatch", "identity.yaml")
			cmd := exec.Command(ed, p)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			return cmd.Run()
		},
	}
}

func identityFieldPatchMap(field, value string) (map[string]any, error) {
	field = strings.ToLower(strings.TrimSpace(field))
	patch := map[string]any{}
	listOf := func(s string) []string {
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	switch field {
	case "role":
		patch["role"] = value
	case "north_star_goals", "goals":
		patch["north_star_goals"] = listOf(value)
	case "current_projects", "projects":
		patch["current_projects"] = listOf(value)
	case "values":
		patch["values"] = listOf(value)
	case "current_focus", "focus":
		patch["current_focus"] = value
	case "context_notes", "notes":
		patch["context_notes"] = value
	default:
		return nil, fmt.Errorf("unknown identity field %q (allowed: role, north_star_goals, current_projects, values, current_focus, context_notes)", field)
	}
	return patch, nil
}
