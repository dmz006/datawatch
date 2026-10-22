// `datawatch automation ...` CLI subcommands: list, get, create, activate.
// Sibling of the automation_* MCP tools and the "automation" comm verbs.
// list/get/create call GET and POST /api/automations; activate calls
// POST /api/automations/{id}/activate.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newAutomationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "automation",
		Short: "Automation (chained Automatons) management",
		Long: `Manage Automations.

Subcommands:
  list                List Automations
  get <id>            Show one Automation (members, edges, active cursors)
  create              Create a draft Automation (members from a JSON file or inline JSON)
  activate <id>       Start a draft Automation`,
	}
	cmd.AddCommand(newAutomationListCmd())
	cmd.AddCommand(newAutomationGetCmd())
	cmd.AddCommand(newAutomationCreateCmd())
	cmd.AddCommand(newAutomationActivateCmd())
	return cmd
}

func newAutomationListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Automations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := automationAPI(http.MethodGet, "/api/automations", nil)
			if err != nil {
				return err
			}
			if asJSON {
				fmt.Println(string(raw))
				return nil
			}
			var list []automationView
			if err := json.Unmarshal(raw, &list); err != nil {
				return fmt.Errorf("decode automation list: %w", err)
			}
			return printAutomationList(os.Stdout, list)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the raw REST response")
	return cmd
}

func newAutomationGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show one Automation (members, edges, active cursors)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := automationAPI(http.MethodGet, "/api/automations/"+url.PathEscape(args[0]), nil)
			if err != nil {
				return err
			}
			if asJSON {
				fmt.Println(string(raw))
				return nil
			}
			var a automationView
			if err := json.Unmarshal(raw, &a); err != nil {
				return fmt.Errorf("decode automation: %w", err)
			}
			return printAutomation(os.Stdout, a)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the raw REST response")
	return cmd
}

func newAutomationCreateCmd() *cobra.Command {
	var name, description, entry, members string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a draft Automation (members from a JSON file or inline JSON)",
		Long: `Create a draft Automation. --members takes a JSON array of members, either
inline (the value starts with "[") or as the path of a file that holds it.
Members may be prd, fork, join or automation (nested) kinds; the automation_create
MCP tool documents each member field. The Automation stays a draft until activate.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{"name": name}
			if description != "" {
				body["description"] = description
			}
			if entry != "" {
				body["entry_member_id"] = entry
			}
			if members != "" {
				parsed, err := automationMembersArg(members)
				if err != nil {
					return err
				}
				body["members"] = parsed
			}
			raw, err := automationAPI(http.MethodPost, "/api/automations", body)
			if err != nil {
				return err
			}
			var a automationView
			if err := json.Unmarshal(raw, &a); err != nil {
				return fmt.Errorf("decode created automation: %w", err)
			}
			fmt.Printf("created automation %s (%s, %d members)\n", a.ID, a.Status, len(a.Members))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Automation name (required)")
	cmd.Flags().StringVar(&description, "description", "", "Optional description")
	cmd.Flags().StringVar(&entry, "entry-member", "", "Member ID where the chain starts")
	cmd.Flags().StringVar(&members, "members", "", "Members as a JSON array: inline, or the path of a file")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newAutomationActivateCmd() *cobra.Command {
	var actor, reason string
	cmd := &cobra.Command{
		Use:   "activate <id>",
		Short: "Activate a draft Automation (starts its entry member)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if actor != "" {
				body["actor"] = actor
			}
			if reason != "" {
				body["reason"] = reason
			}
			return guardrailAPIPost("/api/automations/"+url.PathEscape(args[0])+"/activate", body)
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "", "Who is activating (default operator)")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason, recorded in the audit trail")
	return cmd
}

// automationMembersArg reads the --members value: an inline JSON array when
// it starts with "[", otherwise the path of a file that holds one.
func automationMembersArg(v string) ([]map[string]any, error) {
	raw := strings.TrimSpace(v)
	if !strings.HasPrefix(raw, "[") {
		b, err := os.ReadFile(v) // #nosec G304 -- operator-supplied path to a members file
		if err != nil {
			return nil, fmt.Errorf("read --members file: %w", err)
		}
		raw = strings.TrimSpace(string(b))
	}
	var members []map[string]any
	if err := json.Unmarshal([]byte(raw), &members); err != nil {
		return nil, fmt.Errorf("--members must be a JSON array of members: %w", err)
	}
	return members, nil
}

// automationAPI calls the daemon REST API for the Automation verbs and
// returns the response body. A status of 400 or above is an error that
// carries the daemon's message.
func automationAPI(method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, daemonURL()+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

// automationView is the part of the Automation REST record that the
// human-readable list and get output prints.
type automationView struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Status        string                 `json:"status"`
	EntryMemberID string                 `json:"entry_member_id"`
	ActiveCursors []string               `json:"active_cursors"`
	Version       int64                  `json:"version"`
	Members       []automationMemberView `json:"members"`
}

// automationMemberView is one member of an Automation. The kind fields
// (prd_id, automation_id, fork_members, join_members, on_branch_failure)
// are set only for the kinds that use them.
type automationMemberView struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	PRDID           string   `json:"prd_id"`
	AutomationID    string   `json:"automation_id"`
	ForkMembers     []string `json:"fork_members"`
	JoinMembers     []string `json:"join_members"`
	OnBranchFailure string   `json:"on_branch_failure"`
	OnComplete      string   `json:"on_complete"`
	OnBlocked       string   `json:"on_blocked"`
	OnFailed        string   `json:"on_failed"`
	Status          string   `json:"status"`
}

func printAutomationList(w io.Writer, list []automationView) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tMEMBERS\tACTIVE")
	for _, a := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", a.ID, a.Name, a.Status, len(a.Members), activeCursorsText(a.ActiveCursors))
	}
	return tw.Flush()
}

func printAutomation(w io.Writer, a automationView) error {
	fmt.Fprintf(w, "ID:       %s\n", a.ID)
	fmt.Fprintf(w, "Name:     %s\n", a.Name)
	fmt.Fprintf(w, "Status:   %s\n", a.Status)
	fmt.Fprintf(w, "Version:  %d\n", a.Version)
	fmt.Fprintf(w, "Entry:    %s\n", dashIfEmpty(a.EntryMemberID))
	fmt.Fprintf(w, "Active:   %s\n", activeCursorsText(a.ActiveCursors))
	fmt.Fprintf(w, "Members:  %d\n", len(a.Members))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  MEMBER\tKIND\tTARGET\tON COMPLETE\tON BLOCKED\tON FAILED\tSTATUS")
	for _, m := range a.Members {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			m.ID, memberKindText(m), memberTargetText(m),
			edgeText(m.OnComplete), edgeText(m.OnBlocked), edgeText(m.OnFailed), dashIfEmpty(m.Status))
	}
	return tw.Flush()
}

func memberKindText(m automationMemberView) string {
	if m.Kind == "" {
		return "prd"
	}
	return m.Kind
}

// memberTargetText names what a member runs: its Automaton, its nested
// Automation, or the branch members of a fork or join.
func memberTargetText(m automationMemberView) string {
	switch m.Kind {
	case "automation":
		return m.AutomationID
	case "fork":
		return "branches " + strings.Join(m.ForkMembers, ",")
	case "join":
		rule := m.OnBranchFailure
		if rule == "" {
			rule = "all_must_succeed"
		}
		return "branches " + strings.Join(m.JoinMembers, ",") + " (" + rule + ")"
	default:
		return m.PRDID
	}
}

func activeCursorsText(ids []string) string {
	if len(ids) == 0 {
		return "-"
	}
	return strings.Join(ids, ",")
}

func edgeText(next string) string {
	if next == "" {
		return "stop"
	}
	return next
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
