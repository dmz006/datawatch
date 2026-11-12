// datawatch autonomous pending-items [--state S] [--automation ID] [--json]
// datawatch autonomous pending-item-add --title T [--ref R] [--json]
// datawatch autonomous pending-item-attach <item-id> --automaton <id> [--json]
// datawatch autonomous pending-items-refresh [--json]
//
// The pending item registry over the CLI. A pending item is a named piece of
// work that has no Automaton yet; an Automation member can wait on it until an
// Automaton is attached. These commands proxy the routes under
// /api/autonomous/pending-items. Plain output lists each item with its state
// and, for a held Automation, the hold line; --json prints the REST body
// untouched. A refused request prints the server's own message.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const pendingItemsRoute = "/api/autonomous/pending-items"

// ---- what the routes return ----

type pendingItemCLISource struct {
	Configured bool   `json:"configured"`
	Path       string `json:"path"`
}

type pendingItemCLIRefresh struct {
	Configured bool       `json:"configured"`
	At         *time.Time `json:"at"`
	Rows       int        `json:"rows"`
	Added      int        `json:"added"`
	Updated    int        `json:"updated"`
	Done       int        `json:"done"`
	Dropped    int        `json:"dropped"`
	Kept       int        `json:"kept"`
	Skipped    int        `json:"skipped"`
	Error      string     `json:"error"`
}

type pendingItemCLIAutomation struct {
	AutomationID string `json:"automation_id"`
	Name         string `json:"name"`
	MemberID     string `json:"member_id"`
	Held         bool   `json:"held"`
}

type pendingItemCLIItem struct {
	ID          string                     `json:"id"`
	Ref         string                     `json:"ref"`
	Title       string                     `json:"title"`
	State       string                     `json:"state"`
	Source      string                     `json:"source"`
	AutomatonID string                     `json:"automaton_id"`
	Automations []pendingItemCLIAutomation `json:"automations"`
}

type pendingItemCLIHeld struct {
	AutomationID string    `json:"automation_id"`
	Name         string    `json:"name"`
	MemberID     string    `json:"member_id"`
	ItemID       string    `json:"item_id"`
	Title        string    `json:"title"`
	Ref          string    `json:"ref"`
	Since        time.Time `json:"since"`
	Reason       string    `json:"reason"`
}

type pendingItemCLIList struct {
	Policy      string                `json:"policy"`
	AutoResume  bool                  `json:"auto_resume"`
	Source      pendingItemCLISource  `json:"source"`
	LastRefresh pendingItemCLIRefresh `json:"last_refresh"`
	Items       []pendingItemCLIItem  `json:"items"`
	Held        []pendingItemCLIHeld  `json:"held"`
}

type pendingItemCLIAttach struct {
	Item      pendingItemCLIItem `json:"item"`
	Converted []struct {
		AutomationID string   `json:"automation_id"`
		MemberIDs    []string `json:"member_ids"`
		InsertIDs    []string `json:"pending_insert_ids"`
	} `json:"converted_automations"`
	Resumed []string `json:"resumed_automations"`
	Notes   []string `json:"notes"`
}

// ---- the request ----

// pendingItemRequest sends one request to a pending item route and returns the
// 2xx body untouched. A refused request becomes an error carrying the server's
// message: the body already starts with the status word ("404:", "409:",
// "400:"), and any other refusal is prefixed with its HTTP status.
func pendingItemRequest(method, path string, q url.Values, body any) ([]byte, error) {
	u := daemonURL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = strings.NewReader(string(buf))
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, pendingItemServerError(resp.StatusCode, raw)
	}
	return raw, nil
}

// pendingItemServerError turns a refused response into the server's message.
func pendingItemServerError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("HTTP %d", status)
	}
	if len(msg) >= 4 && msg[3] == ':' && strings.Trim(msg[:3], "0123456789") == "" {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("HTTP %d: %s", status, msg)
}

// pendingItemOutput prints a reply: the raw body for --json, else the text
// render builds from the decoded body.
func pendingItemOutput(cmd *cobra.Command, body []byte, asJSON bool, render func(io.Writer, []byte) error) error {
	if asJSON {
		_, err := cmd.OutOrStdout().Write(body)
		return err
	}
	return render(cmd.OutOrStdout(), body)
}

// ---- plain text ----

func pendingItemTime(t time.Time) string {
	if t.IsZero() {
		return "an unknown time"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// renderPendingItemList writes the policy line, one block per item, the Automata
// held on an item that is not in the list, and the file registry line.
func renderPendingItemList(w io.Writer, body []byte) error {
	var r pendingItemCLIList
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("decode pending items: %w", err)
	}
	var b strings.Builder
	policy := r.Policy
	if policy == "" {
		policy = "hold"
	}
	fmt.Fprintf(&b, "Pending items (policy: %s, auto-resume: %s):\n", policy, onOff(r.AutoResume))
	holds := map[string]pendingItemCLIHeld{}
	for _, h := range r.Held {
		holds[h.AutomationID+"/"+h.ItemID] = h
	}
	shown := map[string]bool{}
	if len(r.Items) == 0 {
		fmt.Fprintln(&b, "  none")
	}
	for _, it := range r.Items {
		shown[it.ID] = true
		line := fmt.Sprintf("  %s  %-8s  %s", it.ID, it.State, it.Title)
		if it.Ref != "" {
			line += fmt.Sprintf(" [ref %s]", it.Ref)
		}
		if it.Source != "" && it.Source != "internal" {
			line += fmt.Sprintf(" (%s)", it.Source)
		}
		fmt.Fprintln(&b, line)
		if it.AutomatonID != "" {
			fmt.Fprintf(&b, "      Automaton %s attached\n", it.AutomatonID)
		}
		for _, a := range it.Automations {
			if h, ok := holds[a.AutomationID+"/"+it.ID]; ok && a.Held {
				fmt.Fprintln(&b, "      "+pendingItemHoldLine(h))
				continue
			}
			fmt.Fprintf(&b, "      in Automation %s\n", pendingItemAutomationLabel(a.Name, a.AutomationID))
		}
	}
	var extra []pendingItemCLIHeld
	for _, h := range r.Held {
		if !shown[h.ItemID] {
			extra = append(extra, h)
		}
	}
	if len(extra) > 0 {
		fmt.Fprintln(&b, "Held on an item not listed above:")
		for _, h := range extra {
			fmt.Fprintln(&b, "  "+pendingItemHoldLine(h))
		}
	}
	if r.Source.Configured {
		fmt.Fprintf(&b, "File registry: %s\n", r.Source.Path)
		fmt.Fprintln(&b, "  "+pendingItemRefreshLine(r.LastRefresh, "last refresh"))
	} else {
		fmt.Fprintln(&b, "File registry: not configured")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func pendingItemAutomationLabel(name, id string) string {
	if name == "" {
		return id
	}
	return fmt.Sprintf("%q (%s)", name, id)
}

// pendingItemHoldLine is the hold line of one held Automation.
func pendingItemHoldLine(h pendingItemCLIHeld) string {
	line := fmt.Sprintf("held: Automation %s waits since %s",
		pendingItemAutomationLabel(h.Name, h.AutomationID), pendingItemTime(h.Since))
	if h.Reason != "" {
		line += ": " + h.Reason
	}
	return line
}

// pendingItemRefreshLine is one line of refresh counts.
func pendingItemRefreshLine(r pendingItemCLIRefresh, label string) string {
	if r.At == nil {
		return label + ": never"
	}
	line := fmt.Sprintf("%s %s: %d rows, %d added, %d updated, %d done, %d dropped, %d kept, %d skipped",
		label, pendingItemTime(*r.At), r.Rows, r.Added, r.Updated, r.Done, r.Dropped, r.Kept, r.Skipped)
	if r.Error != "" {
		line += "; error: " + r.Error
	}
	return line
}

func renderPendingItemCreated(w io.Writer, body []byte) error {
	var it pendingItemCLIItem
	if err := json.Unmarshal(body, &it); err != nil {
		return fmt.Errorf("decode pending item: %w", err)
	}
	var b strings.Builder
	line := fmt.Sprintf("Recorded pending item %s (%s): %s", it.ID, it.State, it.Title)
	if it.Ref != "" {
		line += fmt.Sprintf(" [ref %s]", it.Ref)
	}
	fmt.Fprintln(&b, line)
	_, err := io.WriteString(w, b.String())
	return err
}

func renderPendingItemAttached(w io.Writer, body []byte) error {
	var r pendingItemCLIAttach
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("decode attach result: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Attached Automaton %s to pending item %s (%s): %s\n",
		r.Item.AutomatonID, r.Item.ID, r.Item.State, r.Item.Title)
	for _, c := range r.Converted {
		parts := []string{}
		if len(c.MemberIDs) > 0 {
			parts = append(parts, "members "+strings.Join(c.MemberIDs, ", "))
		}
		if len(c.InsertIDs) > 0 {
			parts = append(parts, "held inserts "+strings.Join(c.InsertIDs, ", "))
		}
		fmt.Fprintf(&b, "  converted in Automation %s: %s\n", c.AutomationID, strings.Join(parts, "; "))
	}
	for _, id := range r.Resumed {
		fmt.Fprintf(&b, "  resumed Automation %s\n", id)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "  note: %s\n", n)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func renderPendingItemRefreshed(w io.Writer, body []byte) error {
	var r pendingItemCLIRefresh
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("decode refresh result: %w", err)
	}
	if !r.Configured {
		_, err := io.WriteString(w, "No pending item file registry is configured (autonomous.automation.pending_item_source.path); nothing was read.\n")
		return err
	}
	if _, err := io.WriteString(w, pendingItemRefreshLine(r, "Refreshed")+"\n"); err != nil {
		return err
	}
	if r.Error != "" {
		return fmt.Errorf("refresh failed: %s", r.Error)
	}
	return nil
}

// ---- the commands ----

// newAutonomousPendingItemsCmd is `datawatch autonomous pending-items`: the
// registry, the policy in effect and the Automata held on a waiting item.
func newAutonomousPendingItemsCmd() *cobra.Command {
	var state, automation string
	var asJSON bool
	c := &cobra.Command{
		Use:   "pending-items",
		Short: "List the Automaton pending items and the Automata held on them",
		Long: "List the pending items: named pieces of work that have no Automaton " +
			"yet. Each line gives the item ID, its state (waiting, attached or " +
			"done), its title and the Automations that carry it; an Automation " +
			"held on a waiting item gets a hold line with the time it started " +
			"waiting and the reason. The policy, the auto-resume setting and " +
			"the file registry's last refresh follow. A read: nothing changes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if state != "" {
				q.Set("state", state)
			}
			if automation != "" {
				q.Set("automation_id", automation)
			}
			body, err := pendingItemRequest(http.MethodGet, pendingItemsRoute, q, nil)
			if err != nil {
				return err
			}
			return pendingItemOutput(cmd, body, asJSON, renderPendingItemList)
		},
	}
	c.Flags().StringVar(&state, "state", "", "show only items in this state: waiting, attached or done")
	c.Flags().StringVar(&automation, "automation", "", "narrow the items and holds to one Automation ID")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// newAutonomousPendingItemAddCmd is `datawatch autonomous pending-item-add`:
// record a waiting item.
func newAutonomousPendingItemAddCmd() *cobra.Command {
	var title, ref string
	var asJSON bool
	c := &cobra.Command{
		Use:   "pending-item-add --title T [--ref R]",
		Short: "Record a waiting pending item: work that has no Automaton yet",
		Long: "Record a waiting pending item. The daemon generates its ID. An " +
			"Automation can carry it as a member (automation insert --pending-item) " +
			"and holds at it until an Automaton is attached with " +
			"pending-item-attach. --ref is an optional external label.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := map[string]any{"title": title}
			if ref != "" {
				req["ref"] = ref
			}
			body, err := pendingItemRequest(http.MethodPost, pendingItemsRoute, nil, req)
			if err != nil {
				return err
			}
			return pendingItemOutput(cmd, body, asJSON, renderPendingItemCreated)
		},
	}
	c.Flags().StringVar(&title, "title", "", "name of the work (required)")
	c.Flags().StringVar(&ref, "ref", "", "optional external label for the item")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	_ = c.MarkFlagRequired("title")
	return c
}

// newAutonomousPendingItemAttachCmd is `datawatch autonomous
// pending-item-attach`: attach an Automaton to a waiting item.
func newAutonomousPendingItemAttachCmd() *cobra.Command {
	var automaton string
	var asJSON bool
	c := &cobra.Command{
		Use:   "pending-item-attach <item-id> --automaton <id>",
		Short: "Attach an Automaton to a waiting pending item",
		Long: "Attach an Automaton to a waiting pending item. Every Automation " +
			"member that waits on the item becomes an ordinary Automaton member " +
			"for that Automaton and, with pending_item_auto_resume on, the held " +
			"Automation continues. Refused when the item is not waiting or the " +
			"Automaton does not exist.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := pendingItemRequest(http.MethodPost,
				pendingItemsRoute+"/"+url.PathEscape(args[0])+"/attach", nil,
				map[string]any{"automaton_id": automaton})
			if err != nil {
				return err
			}
			return pendingItemOutput(cmd, body, asJSON, renderPendingItemAttached)
		},
	}
	c.Flags().StringVar(&automaton, "automaton", "", "the Automaton ID to attach (required)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	_ = c.MarkFlagRequired("automaton")
	return c
}

// newAutonomousPendingItemsRefreshCmd is `datawatch autonomous
// pending-items-refresh`: read the file registry now.
func newAutonomousPendingItemsRefreshCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "pending-items-refresh",
		Short: "Read the pending item file registry now",
		Long: "Read the pending item file registry " +
			"(autonomous.automation.pending_item_source.path) now instead of " +
			"waiting for the next refresh. New rows become waiting items, titles " +
			"are updated, and items whose state column says so move to done; rows " +
			"that disappeared follow pending_item_source.on_missing_row. With no " +
			"registry path set nothing is read.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := pendingItemRequest(http.MethodPost, pendingItemsRoute+"/refresh", nil, nil)
			if err != nil {
				return err
			}
			return pendingItemOutput(cmd, body, asJSON, renderPendingItemRefreshed)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
