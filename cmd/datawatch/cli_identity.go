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
	"bufio"
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
	cmd.AddCommand(newIdentityConfigureCmd()) // BL257 P2 v6.8.1
	return cmd
}

// newIdentityConfigureCmd — BL257 P2 v6.8.1. Interactive 6-step prompt
// that walks the operator through all six identity fields, captures
// each answer, and PUTs the assembled document. Mirrors the PWA
// robot-icon Identity Wizard.
func newIdentityConfigureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Interactive 6-step identity setup wizard (BL257 P2)",
		Long: `Walks through six prompts (role, north-star goals, current projects,
values, current focus, context notes), captures each answer from
stdin, and PUTs the assembled identity to /api/identity.

For list fields, separate items with commas or newlines. Press Enter
on an empty prompt to keep the existing value.`,
		RunE: func(*cobra.Command, []string) error { return runIdentityConfigure() },
	}
}

func runIdentityConfigure() error {
	// Fetch existing identity to pre-fill prompts.
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(daemonURL() + "/api/identity")
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close()
	cur := map[string]any{}
	if resp.StatusCode/100 == 2 {
		body, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(body, &cur)
	}

	type field struct {
		key   string
		label string
		isList bool
	}
	fields := []field{
		{"role", "Role (e.g. platform engineer)", false},
		{"north_star_goals", "North-Star Goals (comma-separated)", true},
		{"current_projects", "Current Projects (comma-separated)", true},
		{"values", "Values (comma-separated)", true},
		{"current_focus", "Current Focus", false},
		{"context_notes", "Context Notes", false},
	}

	// Read sequentially from stdin.
	body := map[string]any{}
	for k, v := range cur {
		body[k] = v // start with existing
	}
	r := bufio.NewReader(os.Stdin)
	for i, f := range fields {
		var existing string
		if cv, ok := cur[f.key]; ok {
			if s, ok := cv.(string); ok {
				existing = s
			} else if arr, ok := cv.([]any); ok {
				parts := make([]string, 0, len(arr))
				for _, a := range arr {
					if s, ok := a.(string); ok {
						parts = append(parts, s)
					}
				}
				existing = strings.Join(parts, ", ")
			}
		}
		fmt.Printf("[%d/%d] %s\n", i+1, len(fields), f.label)
		if existing != "" {
			fmt.Printf("      current: %s\n", existing)
		}
		fmt.Print("      → ")
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue // keep existing
		}
		if f.isList {
			parts := strings.Split(line, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			body[f.key] = out
		} else {
			body[f.key] = line
		}
	}
	fmt.Println("\nSaving identity…")
	return daemonJSON(http.MethodPut, "/api/identity", body)
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
