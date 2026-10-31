// BL257 Phase 1 v6.8.0 — CLI subcommands for the identity / Telos layer.
//
//	datawatch identity get [--field <name>]
//	datawatch identity set --field <name> --value <value>
//	datawatch identity show           (alias for get with full pretty-print)
//	datawatch identity edit           (opens identity.yaml in $EDITOR)
//	datawatch identity backups [--json]
//	datawatch identity restore [--backup <name>]
//	datawatch identity reset [--yes]
//
// All commands talk to the running daemon via /api/identity.
//
// `set` uses HTTP PATCH so non-supplied fields are preserved. To
// completely replace, edit the file via `datawatch identity edit` or PUT
// directly to the REST endpoint.

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// identityStdin and identityStdinIsTerminal are the reset confirmation's
// input; tests replace them.
var (
	identityStdin           io.Reader = os.Stdin
	identityStdinIsTerminal           = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

// identityWipeRefusedCode is the code the daemon puts in the 409 body of a
// refused empty identity write.
const identityWipeRefusedCode = "identity_wipe_refused"

func newIdentityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "identity",
		Short: "Manage operator identity / Telos (BL257 Phase 1)",
		Long: `Operator identity is a structured self-description (role, north-star
goals, current projects, values, current focus, context notes) loaded
from ~/.datawatch/identity.yaml. The daemon injects it into every LLM
session's L0 wake-up layer so AI work stays anchored to operator
priorities.

Every write keeps a timestamped backup of the identity it replaces
(identity.backups_to_keep sets how many; list them with 'identity
backups'). A write that would replace a saved identity with an empty one
is refused by the daemon with HTTP 409 and nothing is written, so a
script or a half-filled wizard cannot wipe it. To clear the identity on
purpose use 'identity reset'; it keeps a backup too. To bring a backup
back use 'identity restore' (the newest by default, or --backup <name>).

Source: PAI's Telos concept. See docs/plans/2026-05-02-pai-comparison-analysis.md §8.`,
	}
	cmd.AddCommand(newIdentityGetCmd())
	cmd.AddCommand(newIdentityShowCmd())
	cmd.AddCommand(newIdentitySetCmd())
	cmd.AddCommand(newIdentityEditCmd())
	cmd.AddCommand(newIdentityConfigureCmd()) // BL257 P2 v6.8.1
	cmd.AddCommand(newIdentityBackupsCmd())
	cmd.AddCommand(newIdentityRestoreCmd())
	cmd.AddCommand(newIdentityResetCmd())
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
	defer resp.Body.Close() //nolint:errcheck
	cur := map[string]any{}
	if resp.StatusCode/100 == 2 {
		body, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(body, &cur)
	}

	type field struct {
		key    string
		label  string
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
	return identityJSON(os.Stdout, http.MethodPut, "/api/identity", body)
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
			defer resp.Body.Close() //nolint:errcheck
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
		RunE: func(c *cobra.Command, _ []string) error {
			if field == "" || value == "" {
				return fmt.Errorf("--field and --value are required")
			}
			patch, err := identityFieldPatchMap(field, value)
			if err != nil {
				return err
			}
			return identityJSON(c.OutOrStdout(), http.MethodPatch, "/api/identity", patch)
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

// identityRequest sends method+body to the daemon and returns the 2xx
// reply. A non-2xx answer becomes an error; the daemon's refusal of an
// empty write (409 identity_wipe_refused) keeps its sentence, which points
// at 'identity reset', instead of a bare status line.
func identityRequest(method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, daemonURL()+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, identityHTTPError(resp.StatusCode, raw)
	}
	return raw, nil
}

// identityHTTPError turns a non-2xx identity reply into an error.
func identityHTTPError(status int, raw []byte) error {
	var refusal struct {
		Error      string   `json:"error"`
		Code       string   `json:"code"`
		FieldsKept []string `json:"fields_kept"`
	}
	if status == http.StatusConflict && json.Unmarshal(raw, &refusal) == nil &&
		refusal.Code == identityWipeRefusedCode && refusal.Error != "" {
		msg := "HTTP 409 " + identityWipeRefusedCode + ": " + refusal.Error + ". Nothing was written"
		if len(refusal.FieldsKept) > 0 {
			msg += "; the saved identity keeps: " + strings.Join(refusal.FieldsKept, ", ")
		}
		return fmt.Errorf("%s. Run 'datawatch identity reset' to clear it on purpose", msg)
	}
	return fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(raw)))
}

// identityJSON is daemonJSON for the identity writes: same output, but the
// daemon's refusal text is kept in the error.
func identityJSON(out io.Writer, method, path string, body any) error {
	raw, err := identityRequest(method, path, body)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		_, _ = fmt.Fprintln(out, "ok")
		return nil
	}
	printIdentityJSON(out, raw)
	return nil
}

// printIdentityJSON writes raw as indented JSON, or as is when it is not JSON.
func printIdentityJSON(out io.Writer, raw []byte) {
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		pretty, _ := json.MarshalIndent(v, "", "  ")
		_, _ = fmt.Fprintln(out, string(pretty))
		return
	}
	_, _ = out.Write(raw)
}

type identityBackupEntry struct {
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	Role       string    `json:"role"`
	FieldCount int       `json:"field_count"`
}

func newIdentityBackupsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "backups",
		Short: "List the saved identity backups, newest first",
		Long: `Lists the timestamped backups the daemon keeps of the identity: name,
local time, role and how many fields have content. Newest first.
'identity restore --backup <name>' brings one back. --json prints the
daemon's reply unchanged.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := identityRequest(http.MethodGet, "/api/identity/backups", nil)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				printIdentityJSON(out, raw)
				return nil
			}
			var reply struct {
				Backups []identityBackupEntry `json:"backups"`
			}
			if err := json.Unmarshal(raw, &reply); err != nil {
				return fmt.Errorf("unreadable backups reply: %w", err)
			}
			if len(reply.Backups) == 0 {
				_, _ = fmt.Fprintln(out, "no backups yet")
				return nil
			}
			sort.SliceStable(reply.Backups, func(i, j int) bool {
				return reply.Backups[i].CreatedAt.After(reply.Backups[j].CreatedAt)
			})
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "NAME\tTIME\tROLE\tFIELDS")
			for _, b := range reply.Backups {
				role := b.Role
				if role == "" {
					role = "-"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", b.Name,
					b.CreatedAt.Local().Format("2006-01-02 15:04:05"), role, b.FieldCount)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the daemon's reply as JSON")
	return cmd
}

func newIdentityRestoreCmd() *cobra.Command {
	var backup string
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore the identity from a backup (default: the newest)",
		Long: `Brings a saved identity backup back and prints the restored identity.
Without --backup the newest backup is used. The identity being replaced is
backed up first, so a restore can itself be undone. List the names with
'datawatch identity backups'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name := strings.TrimSpace(backup)
			if name == "" {
				name = "latest"
			}
			return identityJSON(cmd.OutOrStdout(), http.MethodPost, "/api/identity/restore",
				map[string]any{"backup": name})
		},
	}
	cmd.Flags().StringVar(&backup, "backup", "latest", "backup name from 'identity backups', or latest")
	return cmd
}

func newIdentityResetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Clear the identity on purpose (a backup is kept)",
		Long: `Clears the operator identity. This is the only way to empty it: a
set, configure or other write that would leave it empty is refused.

A backup of the identity being cleared is kept, and
'datawatch identity restore' undoes the reset.

Without --yes you are asked to type RESET; anything else cancels and
nothing is sent. With no terminal on stdin, --yes is required.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes {
				if !identityStdinIsTerminal() {
					return fmt.Errorf("identity reset needs confirmation, but stdin is not a terminal; pass --yes to clear the identity")
				}
				out := cmd.OutOrStdout()
				_, _ = fmt.Fprint(out, "This clears the operator identity. A backup is kept, and 'datawatch identity restore' undoes it.\nType RESET to continue: ")
				line, _ := bufio.NewReader(identityStdin).ReadString('\n')
				if strings.TrimSpace(line) != "RESET" {
					_, _ = fmt.Fprintln(out, "Cancelled; the identity was not changed.")
					return nil
				}
			}
			return identityJSON(cmd.OutOrStdout(), http.MethodPost, "/api/identity/reset", nil)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the typed confirmation")
	return cmd
}
