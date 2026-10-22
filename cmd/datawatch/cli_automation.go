// `datawatch automation ...` CLI subcommands: list, get, create, activate and
// the operator controls pause, resume, cancel, skip, insert, remove, edges and
// approve, and the LLM settings set-llm and set-verification-llm. Sibling of the automation_* MCP tools and the "automation" comm
// verbs. list/get/create call GET and POST /api/automations; activate calls
// POST /api/automations/{id}/activate; each control calls its own
// POST /api/automations/{id}/{pause,resume,cancel,skip_member,insert_member,
// remove_member,reorder_edges,approve_insert,set_llm,set_verification_llm,set_budget} and
// prints the updated Automation, with its budget block when it has a budget policy.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/inference"
)

func newAutomationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "automation",
		Short: "Automation (chained Automatons) management",
		Long: `Manage Automations.

Subcommands:
  list                List Automations
  get <id>            Show one Automation (members, edges, active cursors, budget)
  create              Create a draft Automation (members from a JSON file or inline JSON)
  activate <id>       Start a draft Automation
  pause <id>          Pause a running Automation
  resume <id>         Resume a paused Automation
  cancel <id>         Cancel an Automation (--cancel-in-flight true|false is required)
  skip <id> <member>  Skip a member that has not started, following one edge
  insert <id> <prd>   Insert a member (--reason is required; --urgent, --after)
  remove <id> <member>  Remove a member that has not started
  edges <id> <member> Rewire a member's on_complete, on_blocked and on_failed edges
  approve <id> <member>  Approve an agent-inserted member
  set-llm <id>        Set the execution and planning LLM (--backend, --model, --effort,
                      --planning-backend, --planning-model; an empty value clears)
  set-verification-llm <id>  Set the verification LLM (--backend, --model, --effort;
                      an empty value clears)
  set-budget <id>     Set, replace or clear the budget policy of an Automation or one member

Every control takes --actor, --reason and --expected-version; a stale
expected version is refused with HTTP 409.`,
	}
	cmd.AddCommand(newAutomationListCmd())
	cmd.AddCommand(newAutomationGetCmd())
	cmd.AddCommand(newAutomationCreateCmd())
	cmd.AddCommand(newAutomationActivateCmd())
	cmd.AddCommand(newAutomationPauseCmd())
	cmd.AddCommand(newAutomationResumeCmd())
	cmd.AddCommand(newAutomationCancelCmd())
	cmd.AddCommand(newAutomationSkipCmd())
	cmd.AddCommand(newAutomationInsertCmd())
	cmd.AddCommand(newAutomationRemoveCmd())
	cmd.AddCommand(newAutomationEdgesCmd())
	cmd.AddCommand(newAutomationApproveCmd())
	cmd.AddCommand(newAutomationSetLLMCmd())
	cmd.AddCommand(newAutomationSetVerificationLLMCmd())
	cmd.AddCommand(newAutomationSetBudgetCmd())
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

// automationControlFlags holds the flags every control subcommand shares.
type automationControlFlags struct {
	actor, reason   string
	expectedVersion int64
}

func (f *automationControlFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.actor, "actor", "", "Who is acting (default operator)")
	cmd.Flags().StringVar(&f.reason, "reason", "", "Reason, recorded in the audit trail")
	cmd.Flags().Int64Var(&f.expectedVersion, "expected-version", 0, "Refuse (HTTP 409) unless the Automation is at this version")
}

// body starts the request body with the shared fields that were set.
func (f *automationControlFlags) body(cmd *cobra.Command) map[string]any {
	body := map[string]any{}
	if f.actor != "" {
		body["actor"] = f.actor
	}
	if f.reason != "" {
		body["reason"] = f.reason
	}
	if cmd.Flags().Changed("expected-version") {
		body["expected_version"] = f.expectedVersion
	}
	return body
}

// automationControlOps maps each control subcommand to the REST sub-path it posts to.
var automationControlOps = map[string]string{
	"pause":   "pause",
	"resume":  "resume",
	"cancel":  "cancel",
	"skip":    "skip_member",
	"insert":  "insert_member",
	"remove":  "remove_member",
	"edges":   "reorder_edges",
	"approve": "approve_insert",

	"set-llm":              "set_llm",
	"set-verification-llm": "set_verification_llm",
	"set-budget":           "set_budget",
}

// runAutomationControl posts a control request for a subcommand verb and
// prints the updated Automation.
func runAutomationControl(id, verb string, body map[string]any) error {
	raw, err := automationAPI(http.MethodPost, "/api/automations/"+url.PathEscape(id)+"/"+automationControlOps[verb], body)
	if err != nil {
		return err
	}
	var a automationView
	if err := json.Unmarshal(raw, &a); err != nil {
		return fmt.Errorf("decode automation: %w", err)
	}
	return printAutomation(os.Stdout, a)
}

func newAutomationPauseCmd() *cobra.Command {
	var f automationControlFlags
	cmd := &cobra.Command{
		Use:   "pause <id>",
		Short: "Pause a running Automation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutomationControl(args[0], "pause", f.body(cmd))
		},
	}
	f.register(cmd)
	return cmd
}

func newAutomationResumeCmd() *cobra.Command {
	var f automationControlFlags
	cmd := &cobra.Command{
		Use:   "resume <id>",
		Short: "Resume a paused Automation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutomationControl(args[0], "resume", f.body(cmd))
		},
	}
	f.register(cmd)
	return cmd
}

func newAutomationCancelCmd() *cobra.Command {
	var f automationControlFlags
	var cancelInFlight string
	cmd := &cobra.Command{
		Use:   "cancel <id> --cancel-in-flight <true|false>",
		Short: "Cancel an Automation; --cancel-in-flight says what happens to members already running",
		Long: `Cancel an Automation. --cancel-in-flight is required and has no default:
true ends the sessions of members that are running now, false lets them finish
while no further member starts.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("cancel-in-flight") {
				return fmt.Errorf("--cancel-in-flight true|false is required: say whether members already running are cancelled")
			}
			choice, err := strconv.ParseBool(cancelInFlight)
			if err != nil {
				return fmt.Errorf("--cancel-in-flight must be true or false, got %q", cancelInFlight)
			}
			body := f.body(cmd)
			body["cancel_in_flight"] = choice
			return runAutomationControl(args[0], "cancel", body)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVar(&cancelInFlight, "cancel-in-flight", "", "Required: true cancels members running now, false lets them finish")
	return cmd
}

func newAutomationSkipCmd() *cobra.Command {
	var f automationControlFlags
	var edge string
	cmd := &cobra.Command{
		Use:   "skip <id> <member-id>",
		Short: "Skip a member that has not started and follow one of its edges",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch edge {
			case "on_complete", "on_blocked", "on_failed":
			default:
				return fmt.Errorf("--edge must be on_complete, on_blocked or on_failed, got %q", edge)
			}
			body := f.body(cmd)
			body["member_id"] = args[1]
			body["edge"] = edge
			return runAutomationControl(args[0], "skip", body)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVar(&edge, "edge", "on_complete", "Edge to follow after the skipped member: on_complete, on_blocked or on_failed")
	return cmd
}

func newAutomationInsertCmd() *cobra.Command {
	var f automationControlFlags
	var urgent bool
	var after string
	cmd := &cobra.Command{
		Use:   "insert <id> <prd-id>",
		Short: "Insert a member into a running Automation",
		Long: `Insert a member that runs the given Automaton (prd-id). --reason is required and
becomes the member's added reason. --urgent takes the urgent lane (one urgent member
at a time); --after places the member after the named member.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(f.reason) == "" {
				return fmt.Errorf("--reason is required to insert a member")
			}
			body := f.body(cmd)
			body["member"] = map[string]any{"prd_id": args[1]}
			body["urgent"] = urgent
			if after != "" {
				body["after"] = after
			}
			return runAutomationControl(args[0], "insert", body)
		},
	}
	f.register(cmd)
	cmd.Flags().BoolVar(&urgent, "urgent", false, "Use the urgent lane")
	cmd.Flags().StringVar(&after, "after", "", "Member ID to insert after")
	return cmd
}

func newAutomationRemoveCmd() *cobra.Command {
	var f automationControlFlags
	cmd := &cobra.Command{
		Use:   "remove <id> <member-id>",
		Short: "Remove a member that has not started",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := f.body(cmd)
			body["member_id"] = args[1]
			return runAutomationControl(args[0], "remove", body)
		},
	}
	f.register(cmd)
	return cmd
}

func newAutomationEdgesCmd() *cobra.Command {
	var f automationControlFlags
	var onComplete, onBlocked, onFailed string
	cmd := &cobra.Command{
		Use:   "edges <id> <member-id>",
		Short: "Rewire the outgoing edges of a member that has not started",
		Long: `Rewire a member's edges. At least one of --on-complete, --on-blocked and
--on-failed is required. All three edges are rewritten: one that is not named is
set to no next member (the chain stops there), so name every edge to keep.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fl := cmd.Flags()
			if !fl.Changed("on-complete") && !fl.Changed("on-blocked") && !fl.Changed("on-failed") {
				return fmt.Errorf("name at least one of --on-complete, --on-blocked, --on-failed")
			}
			body := f.body(cmd)
			body["member_id"] = args[1]
			body["on_complete"] = onComplete
			body["on_blocked"] = onBlocked
			body["on_failed"] = onFailed
			return runAutomationControl(args[0], "edges", body)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVar(&onComplete, "on-complete", "", "Member that runs when this one completes (empty: stop)")
	cmd.Flags().StringVar(&onBlocked, "on-blocked", "", "Member that runs when this one is blocked (empty: stop)")
	cmd.Flags().StringVar(&onFailed, "on-failed", "", "Member that runs when this one fails (empty: stop)")
	return cmd
}

func newAutomationApproveCmd() *cobra.Command {
	var f automationControlFlags
	cmd := &cobra.Command{
		Use:   "approve <id> <member-id>",
		Short: "Approve a member an agent inserted",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := f.body(cmd)
			body["member_id"] = args[1]
			return runAutomationControl(args[0], "approve", body)
		},
	}
	f.register(cmd)
	return cmd
}

// llmFlagKeys maps each LLM setting flag to its REST body key.
var llmFlagKeys = []struct{ flag, key string }{
	{"backend", "backend"},
	{"model", "model"},
	{"effort", "effort"},
	{"planning-backend", "planning_backend"},
	{"planning-model", "planning_model"},
}

// automationLLMBody builds the request body for set-llm and
// set-verification-llm. A key is sent only when its flag was given, so an
// empty value clears the field; at least one setting flag is required.
func automationLLMBody(cmd *cobra.Command, f *automationControlFlags, flags []string) (map[string]any, error) {
	body := f.body(cmd)
	set := 0
	for _, fk := range llmFlagKeys {
		var present bool
		for _, name := range flags {
			present = present || name == fk.flag
		}
		if !present || !cmd.Flags().Changed(fk.flag) {
			continue
		}
		v, _ := cmd.Flags().GetString(fk.flag)
		body[fk.key] = v
		set++
	}
	if set == 0 {
		names := make([]string, len(flags))
		for i, n := range flags {
			names[i] = "--" + n
		}
		return nil, fmt.Errorf("name at least one of %s (an empty value clears the field)", strings.Join(names, ", "))
	}
	return body, nil
}

func newAutomationSetLLMCmd() *cobra.Command {
	var f automationControlFlags
	var backend, model, effort, planningBackend, planningModel string
	flags := []string{"backend", "model", "effort", "planning-backend", "planning-model"}
	cmd := &cobra.Command{
		Use:   "set-llm <id>",
		Short: "Set the execution and planning LLM of an Automation",
		Long: `Set the LLM an Automation's members run with and the LLM that plans (decomposes)
them. At least one of --backend, --model, --effort, --planning-backend and
--planning-model is required. Only the flags given are sent; an empty value
(for example --model "") clears that field.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := automationLLMBody(cmd, &f, flags)
			if err != nil {
				return err
			}
			return runAutomationControl(args[0], "set-llm", body)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVar(&backend, "backend", "", "Execution LLM backend (empty: clear)")
	cmd.Flags().StringVar(&model, "model", "", "Execution LLM model (empty: clear)")
	cmd.Flags().StringVar(&effort, "effort", "", "Execution LLM effort (empty: clear)")
	cmd.Flags().StringVar(&planningBackend, "planning-backend", "", "Planning LLM backend (empty: clear)")
	cmd.Flags().StringVar(&planningModel, "planning-model", "", "Planning LLM model (empty: clear)")
	return cmd
}

func newAutomationSetVerificationLLMCmd() *cobra.Command {
	var f automationControlFlags
	var backend, model, effort string
	flags := []string{"backend", "model", "effort"}
	cmd := &cobra.Command{
		Use:   "set-verification-llm <id>",
		Short: "Set the verification LLM of an Automation",
		Long: `Set the LLM that verifies an Automation's members. At least one of --backend,
--model and --effort is required. Only the flags given are sent; an empty value
(for example --model "") clears that field.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := automationLLMBody(cmd, &f, flags)
			if err != nil {
				return err
			}
			return runAutomationControl(args[0], "set-verification-llm", body)
		},
	}
	f.register(cmd)
	cmd.Flags().StringVar(&backend, "backend", "", "Verification LLM backend (empty: clear)")
	cmd.Flags().StringVar(&model, "model", "", "Verification LLM model (empty: clear)")
	cmd.Flags().StringVar(&effort, "effort", "", "Verification LLM effort (empty: clear)")
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
	// Budget is the computed budget view; absent when the Automation has no
	// budget policy in play.
	Budget *autonomous.BudgetView `json:"budget"`
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
	_, _ = fmt.Fprintln(tw, "ID\tNAME\tSTATUS\tMEMBERS\tACTIVE")
	for _, a := range list {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", a.ID, a.Name, a.Status, len(a.Members), activeCursorsText(a.ActiveCursors))
	}
	return tw.Flush()
}

func printAutomation(w io.Writer, a automationView) error {
	_, _ = fmt.Fprintf(w, "ID:       %s\n", a.ID)
	_, _ = fmt.Fprintf(w, "Name:     %s\n", a.Name)
	_, _ = fmt.Fprintf(w, "Status:   %s\n", a.Status)
	_, _ = fmt.Fprintf(w, "Version:  %d\n", a.Version)
	_, _ = fmt.Fprintf(w, "Entry:    %s\n", dashIfEmpty(a.EntryMemberID))
	_, _ = fmt.Fprintf(w, "Active:   %s\n", activeCursorsText(a.ActiveCursors))
	_, _ = fmt.Fprintf(w, "Members:  %d\n", len(a.Members))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  MEMBER\tKIND\tTARGET\tON COMPLETE\tON BLOCKED\tON FAILED\tSTATUS")
	for _, m := range a.Members {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			m.ID, memberKindText(m), memberTargetText(m),
			edgeText(m.OnComplete), edgeText(m.OnBlocked), edgeText(m.OnFailed), dashIfEmpty(m.Status))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if a.Budget != nil {
		_, err := fmt.Fprint(w, autonomous.FormatAutomationBudgetText(*a.Budget))
		return err
	}
	return nil
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

// automationBudgetPolicyFlags are the flags that describe a budget policy;
// --clear and a member's --estimated-cost-usd are checked against them.
var automationBudgetPolicyFlags = []string{
	"ceiling-type", "ceiling-value", "window", "pacing", "on-breach", "backend",
	"allocation-estimate", "peer-window", "reset-time", "reset-weekday", "reset-day", "reset-zone",
}

func newAutomationSetBudgetCmd() *cobra.Command {
	var f automationControlFlags
	var member, ceilingType, window, pacing, onBreach, backend string
	var peerWindow, resetTime, resetWeekday, resetZone string
	var ceilingValue, estimatedCost float64
	var resetDay int
	var allocationEstimate, clear bool
	cmd := &cobra.Command{
		Use:   "set-budget <id>",
		Short: "Set, replace or clear the budget policy of an Automation or one member",
		Long: `Set the budget policy of an Automation, or of one member with --member. The
policy flags replace the whole policy: --ceiling-type (dollar or allocation_pct),
--ceiling-value and --window (automation, day, week or month) are required
together; --pacing (none or even), --on-breach (pause or block), --backend,
--allocation-estimate, --peer-window and the reset anchors are optional.
--clear removes the policy and cannot be combined with them.

On a breach the Automation pauses and resumes by itself at the window reset only
when the reset time is known: a day, week or month window anchored at
--reset-time, --reset-weekday, --reset-day and --reset-zone or at the calendar
default, or a reset time read from a rate-limit message. When the reset time is
unknown it goes to blocked and waits for the operator. An alert is raised either
way.

--allocation-estimate adds an estimated share of the plan allocation; it is
always labelled estimated and never pauses or blocks. --peer-window names, comma
separated, the peers whose spend counts toward the ceiling; it is off by default
and a shared account is not confirmed automatically.

--estimated-cost-usd (with --member) declares the expected cost of the member's
Automaton; on its own it changes only the estimate and 0 removes it.

Flags are checked before any request is sent. --expected-version must match the
Automation's current version or the daemon refuses with HTTP 409.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fl := cmd.Flags()
			var peers []string
			if fl.Changed("peer-window") {
				for _, p := range strings.Split(peerWindow, ",") {
					peers = append(peers, strings.TrimSpace(p))
				}
			}
			in := automationBudgetFlagValues{
				changed: fl.Changed, member: strings.TrimSpace(member),
				ceilingType: ceilingType, ceilingValue: ceilingValue, window: window, pacing: pacing,
				onBreach: onBreach, backend: backend, allocationEstimate: allocationEstimate, peerWindow: peers,
				resetTime: resetTime, resetWeekday: resetWeekday, resetDay: resetDay, resetZone: resetZone,
				estimatedCost: estimatedCost, clear: clear,
			}
			fields, err := in.fields()
			if err != nil {
				return err
			}
			body := f.body(cmd)
			for k, v := range fields {
				body[k] = v
			}
			return runAutomationControl(args[0], "set-budget", body)
		},
	}
	f.register(cmd)
	fl := cmd.Flags()
	fl.StringVar(&member, "member", "", "Set the override of this member instead of the Automation's own policy")
	fl.StringVar(&ceilingType, "ceiling-type", "", "What the ceiling measures: dollar, or allocation_pct (an estimate only)")
	fl.Float64Var(&ceilingValue, "ceiling-value", 0, "The ceiling, a positive number in the unit of --ceiling-type")
	fl.StringVar(&window, "window", "", "Window the ceiling applies to: automation, day, week or month")
	fl.StringVar(&pacing, "pacing", "", "Pacing within the window: none (default) or even")
	fl.StringVar(&onBreach, "on-breach", "", "When the ceiling is reached: pause (default) or block")
	fl.StringVar(&backend, "backend", "", "LLM backend whose spend is measured (default: the members')")
	fl.BoolVar(&allocationEstimate, "allocation-estimate", false, "Show an estimated share of the plan allocation (never pauses or blocks)")
	fl.StringVar(&peerWindow, "peer-window", "", "Comma-separated peers whose spend counts toward the ceiling (off by default)")
	fl.StringVar(&resetTime, "reset-time", "", "Time of day the day, week or month window resets, 24h HH:MM")
	fl.StringVar(&resetWeekday, "reset-weekday", "", "Weekday a week window resets, monday..sunday")
	fl.IntVar(&resetDay, "reset-day", 0, "Day of the month a month window resets, 1..28")
	fl.StringVar(&resetZone, "reset-zone", "", "IANA zone of the reset, such as America/New_York")
	fl.Float64Var(&estimatedCost, "estimated-cost-usd", 0, "With --member: expected cost in dollars of the member's Automaton (0 removes it)")
	fl.BoolVar(&clear, "clear", false, "Remove the policy instead of setting one")
	return cmd
}

// automationBudgetFlagValues carries the parsed set-budget flags; changed
// reports whether a flag was given on the command line.
type automationBudgetFlagValues struct {
	changed func(name string) bool

	member, ceilingType, window, pacing, onBreach, backend string
	resetTime, resetWeekday, resetZone                     string
	ceilingValue, estimatedCost                            float64
	resetDay                                               int
	allocationEstimate, clear                              bool
	peerWindow                                             []string
}

// fields checks the flags against each other and returns the REST fields they
// stand for. Every error names the flag at fault, and nothing is sent when one
// is returned.
func (v automationBudgetFlagValues) fields() (map[string]any, error) {
	var given []string
	for _, name := range automationBudgetPolicyFlags {
		if v.changed(name) {
			given = append(given, "--"+name)
		}
	}
	costGiven := v.changed("estimated-cost-usd")
	out := map[string]any{}
	if v.member != "" {
		out["member_id"] = v.member
	}

	if v.clear {
		if costGiven {
			given = append(given, "--estimated-cost-usd")
		}
		if len(given) > 0 {
			return nil, fmt.Errorf("--clear removes the policy and cannot be combined with %s", strings.Join(given, ", "))
		}
		out["clear"] = true
		return out, nil
	}
	if costGiven {
		if v.member == "" {
			return nil, fmt.Errorf("--estimated-cost-usd applies to a member; name it with --member")
		}
		if v.estimatedCost < 0 {
			return nil, fmt.Errorf("--estimated-cost-usd %v must not be negative", v.estimatedCost)
		}
		out["estimated_cost_usd"] = v.estimatedCost
	}
	if len(given) == 0 {
		if !costGiven {
			return nil, fmt.Errorf("nothing to set: give --ceiling-type, --ceiling-value and --window, or --clear to remove the policy")
		}
		return out, nil
	}

	switch v.ceilingType {
	case "dollar", "allocation_pct":
	case "":
		return nil, fmt.Errorf("--ceiling-type is required with the policy flags: dollar or allocation_pct")
	default:
		return nil, fmt.Errorf("--ceiling-type must be dollar or allocation_pct, got %q", v.ceilingType)
	}
	if v.ceilingValue <= 0 {
		return nil, fmt.Errorf("--ceiling-value is required and must be positive, got %v", v.ceilingValue)
	}
	switch v.window {
	case "automation", "day", "week", "month":
	case "":
		return nil, fmt.Errorf("--window is required with the policy flags: automation, day, week or month")
	default:
		return nil, fmt.Errorf("--window must be automation, day, week or month, got %q", v.window)
	}
	switch v.pacing {
	case "", "none", "even":
	default:
		return nil, fmt.Errorf("--pacing must be none or even, got %q", v.pacing)
	}
	switch v.onBreach {
	case "", "pause", "block":
	default:
		return nil, fmt.Errorf("--on-breach must be pause or block, got %q", v.onBreach)
	}
	if err := v.checkResetAnchor(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range v.peerWindow {
		switch {
		case p == "":
			return nil, fmt.Errorf("--peer-window entries must be non-empty")
		case seen[p]:
			return nil, fmt.Errorf("--peer-window lists %q more than once", p)
		}
		seen[p] = true
	}

	out["ceiling_type"], out["ceiling_value"], out["window"] = v.ceilingType, v.ceilingValue, v.window
	for k, s := range map[string]string{
		"pacing": v.pacing, "on_breach": v.onBreach, "backend": v.backend,
		"reset_time": v.resetTime, "reset_weekday": v.resetWeekday, "reset_zone": v.resetZone,
	} {
		if s != "" {
			out[k] = s
		}
	}
	if v.resetDay != 0 {
		out["reset_day"] = v.resetDay
	}
	if v.allocationEstimate {
		out["allocation_estimate"] = true
	}
	if len(v.peerWindow) > 0 {
		out["peer_window"] = v.peerWindow
	}
	return out, nil
}

// checkResetAnchor checks the reset flags: each must parse and each applies
// only to the windows that use it. The lifetime window has no reset.
func (v automationBudgetFlagValues) checkResetAnchor() error {
	resets := v.window == "day" || v.window == "week" || v.window == "month"
	if v.resetTime != "" {
		if !resets {
			return fmt.Errorf("--reset-time only applies to a day, week or month window")
		}
		if _, _, err := inference.ParseBudgetClock(v.resetTime); err != nil {
			return fmt.Errorf("--reset-time: %w", err)
		}
	}
	if v.resetZone != "" {
		if !resets {
			return fmt.Errorf("--reset-zone only applies to a day, week or month window")
		}
		if _, err := time.LoadLocation(v.resetZone); err != nil {
			return fmt.Errorf("--reset-zone %q is not a known IANA zone", v.resetZone)
		}
	}
	if v.resetWeekday != "" {
		if v.window != "week" {
			return fmt.Errorf("--reset-weekday only applies to a week window")
		}
		if _, ok := inference.ParseBudgetWeekday(v.resetWeekday); !ok {
			return fmt.Errorf("--reset-weekday %q must be monday..sunday", v.resetWeekday)
		}
	}
	if v.changed("reset-day") {
		if v.window != "month" {
			return fmt.Errorf("--reset-day only applies to a month window")
		}
		if v.resetDay < 1 || v.resetDay > 28 {
			return fmt.Errorf("--reset-day %d must be 1..28", v.resetDay)
		}
	}
	return nil
}
