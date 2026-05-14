// BL303 S2 — `datawatch guardrail ...` CLI subcommands.
// Guardrail library + profile CRUD + per-Automaton overrides.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

func newGuardrailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guardrail",
		Short: "Guardrail library and profile management",
		Long: `Manage the guardrail library and per-Automaton guardrail overrides.

Subcommands:
  library                                 List the guardrail library
  profile list                            List guardrail profiles
  profile create --name <n> [--guardrails csv]  Create a profile
  profile get <id>                        Get one profile
  profile update <id> [flags]             Update a profile
  profile delete <id>                     Delete a profile
  automaton set <prd-id> [flags]          Set per-Automaton guardrail override`,
	}
	cmd.AddCommand(
		newGuardrailLibraryCmd(),
		newGuardrailProfileCmd(),
		newGuardrailAutomatonCmd(),
	)
	return cmd
}

// ── library ───────────────────────────────────────────────────────────────

func newGuardrailLibraryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "library",
		Short: "List the guardrail library (built-in scan guardrails + skill-contributed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return guardrailAPIGet("/api/autonomous/guardrails")
		},
	}
}

// ── profile ───────────────────────────────────────────────────────────────

func newGuardrailProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage guardrail profiles",
	}
	var name, description, guardrailsCSV string

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List guardrail profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return guardrailAPIGet("/api/autonomous/guardrail_profiles")
		},
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a guardrail profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			guardrails := csvToSlice(guardrailsCSV)
			body := map[string]any{"name": name, "description": description, "guardrails": guardrails}
			return guardrailAPIPost("/api/autonomous/guardrail_profiles", body)
		},
	}
	createCmd.Flags().StringVar(&name, "name", "", "Profile name (required)")
	createCmd.Flags().StringVar(&description, "description", "", "Optional description")
	createCmd.Flags().StringVar(&guardrailsCSV, "guardrails", "", "Comma-separated guardrail names")
	_ = createCmd.MarkFlagRequired("name")

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get one guardrail profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return guardrailAPIGet("/api/autonomous/guardrail_profiles/" + args[0])
		},
	}

	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a guardrail profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			guardrails := csvToSlice(guardrailsCSV)
			body := map[string]any{"name": name, "description": description, "guardrails": guardrails}
			return guardrailAPIPut("/api/autonomous/guardrail_profiles/"+args[0], body)
		},
	}
	updateCmd.Flags().StringVar(&name, "name", "", "New profile name")
	updateCmd.Flags().StringVar(&description, "description", "", "New description")
	updateCmd.Flags().StringVar(&guardrailsCSV, "guardrails", "", "New guardrail list (comma-separated)")

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a guardrail profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return guardrailAPIDelete("/api/autonomous/guardrail_profiles/" + args[0])
		},
	}

	cmd.AddCommand(listCmd, createCmd, getCmd, updateCmd, deleteCmd)
	return cmd
}

// ── automaton ─────────────────────────────────────────────────────────────

func newGuardrailAutomatonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "automaton",
		Short: "Per-Automaton guardrail override",
	}

	var profile, perTaskCSV, perStoryCSV string
	setCmd := &cobra.Command{
		Use:   "set <prd-id>",
		Short: "Set per-Automaton guardrail overrides (profile / per-task / per-story)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{
				"guardrail_profile":    profile,
				"per_task_guardrails":  csvToSlice(perTaskCSV),
				"per_story_guardrails": csvToSlice(perStoryCSV),
			}
			return guardrailAPIPut("/api/autonomous/prds/"+args[0]+"/guardrails", body)
		},
	}
	setCmd.Flags().StringVar(&profile, "profile", "", "Named guardrail profile")
	setCmd.Flags().StringVar(&perTaskCSV, "per-task", "", "Per-task guardrails (comma-separated; beats profile)")
	setCmd.Flags().StringVar(&perStoryCSV, "per-story", "", "Per-story guardrails (comma-separated; beats profile)")

	cmd.AddCommand(setCmd)
	return cmd
}

// ── REST helpers ──────────────────────────────────────────────────────────

func guardrailAPIGet(path string) error {
	resp, err := daemonClient().Get(daemonURL() + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
	return nil
}

func guardrailAPIPost(path string, body any) error {
	return guardrailBodyRequest(http.MethodPost, path, body)
}

func guardrailAPIPut(path string, body any) error {
	return guardrailBodyRequest(http.MethodPut, path, body)
}

func guardrailAPIDelete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, daemonURL()+path, nil)
	if err != nil {
		return err
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	fmt.Println("deleted")
	return nil
}

func guardrailBodyRequest(method, path string, body any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(method, daemonURL()+path, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := daemonClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	fmt.Println(string(rb))
	return nil
}

func csvToSlice(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
