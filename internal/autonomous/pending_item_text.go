// Plain-text rendering of the pending item routes' replies, shared by the CLI
// (datawatch autonomous pending-items ...) and the chat verbs (autonomous
// pending-items ...), so both surfaces read the same. Nothing here talks to
// the daemon: each Parse function decodes one REST body and each Render
// function turns the decoded value into text.

package autonomous

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---- what the routes return ----

// PendingItemTextSource is the "source" object of the list reply.
type PendingItemTextSource struct {
	Configured bool   `json:"configured"`
	Path       string `json:"path"`
}

// PendingItemTextRefresh is a file registry refresh result.
type PendingItemTextRefresh struct {
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

// PendingItemTextAutomation is an Automation that carries an item as a member.
type PendingItemTextAutomation struct {
	AutomationID string `json:"automation_id"`
	Name         string `json:"name"`
	MemberID     string `json:"member_id"`
	Held         bool   `json:"held"`
}

// PendingItemTextItem is one item of the list reply.
type PendingItemTextItem struct {
	ID          string                      `json:"id"`
	Ref         string                      `json:"ref"`
	Title       string                      `json:"title"`
	State       string                      `json:"state"`
	Source      string                      `json:"source"`
	AutomatonID string                      `json:"automaton_id"`
	Automations []PendingItemTextAutomation `json:"automations"`
}

// PendingItemTextHeld is an Automation waiting on a waiting item.
type PendingItemTextHeld struct {
	AutomationID string    `json:"automation_id"`
	Name         string    `json:"name"`
	MemberID     string    `json:"member_id"`
	ItemID       string    `json:"item_id"`
	Title        string    `json:"title"`
	Ref          string    `json:"ref"`
	Since        time.Time `json:"since"`
	Reason       string    `json:"reason"`
}

// PendingItemTextList is the list reply.
type PendingItemTextList struct {
	Policy      string                 `json:"policy"`
	AutoResume  bool                   `json:"auto_resume"`
	Source      PendingItemTextSource  `json:"source"`
	LastRefresh PendingItemTextRefresh `json:"last_refresh"`
	Items       []PendingItemTextItem  `json:"items"`
	Held        []PendingItemTextHeld  `json:"held"`
}

// PendingItemTextAttach is the attach reply.
type PendingItemTextAttach struct {
	Item      PendingItemTextItem `json:"item"`
	Converted []struct {
		AutomationID string   `json:"automation_id"`
		MemberIDs    []string `json:"member_ids"`
		InsertIDs    []string `json:"pending_insert_ids"`
	} `json:"converted_automations"`
	Resumed []string `json:"resumed_automations"`
	Notes   []string `json:"notes"`
}

// ParsePendingItemList decodes the body of GET /api/autonomous/pending-items.
func ParsePendingItemList(body []byte) (PendingItemTextList, error) {
	var r PendingItemTextList
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("decode pending items: %w", err)
	}
	return r, nil
}

// ParsePendingItem decodes one item, the body of POST /api/autonomous/pending-items.
func ParsePendingItem(body []byte) (PendingItemTextItem, error) {
	var it PendingItemTextItem
	if err := json.Unmarshal(body, &it); err != nil {
		return it, fmt.Errorf("decode pending item: %w", err)
	}
	return it, nil
}

// ParsePendingItemAttach decodes the body of the attach route.
func ParsePendingItemAttach(body []byte) (PendingItemTextAttach, error) {
	var r PendingItemTextAttach
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("decode attach result: %w", err)
	}
	return r, nil
}

// ParsePendingItemRefresh decodes the body of the refresh route.
func ParsePendingItemRefresh(body []byte) (PendingItemTextRefresh, error) {
	var r PendingItemTextRefresh
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("decode refresh result: %w", err)
	}
	return r, nil
}

// ---- plain text ----

func pendingItemTextTime(t time.Time) string {
	if t.IsZero() {
		return "an unknown time"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func pendingItemOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func pendingItemAutomationLabel(name, id string) string {
	if name == "" {
		return id
	}
	return fmt.Sprintf("%q (%s)", name, id)
}

// PendingItemHoldLine is the hold line of one held Automation.
func PendingItemHoldLine(h PendingItemTextHeld) string {
	line := fmt.Sprintf("held: Automation %s waits since %s",
		pendingItemAutomationLabel(h.Name, h.AutomationID), pendingItemTextTime(h.Since))
	if h.Reason != "" {
		line += ": " + h.Reason
	}
	return line
}

// PendingItemRefreshLine is one line of refresh counts.
func PendingItemRefreshLine(r PendingItemTextRefresh, label string) string {
	if r.At == nil {
		return label + ": never"
	}
	line := fmt.Sprintf("%s %s: %d rows, %d added, %d updated, %d done, %d dropped, %d kept, %d skipped",
		label, pendingItemTextTime(*r.At), r.Rows, r.Added, r.Updated, r.Done, r.Dropped, r.Kept, r.Skipped)
	if r.Error != "" {
		line += "; error: " + r.Error
	}
	return line
}

// RenderPendingItemList writes the policy line, one block per item, the
// Automata held on an item that is not in the list, and the file registry line.
// maxItems caps how many items are written (0 means all); the rest are counted
// in an "and N more" line.
func RenderPendingItemList(r PendingItemTextList, maxItems int) string {
	var b strings.Builder
	policy := r.Policy
	if policy == "" {
		policy = "hold"
	}
	fmt.Fprintf(&b, "Pending items (policy: %s, auto-resume: %s):\n", policy, pendingItemOnOff(r.AutoResume))
	holds := map[string]PendingItemTextHeld{}
	for _, h := range r.Held {
		holds[h.AutomationID+"/"+h.ItemID] = h
	}
	if len(r.Items) == 0 {
		fmt.Fprintln(&b, "  none")
	}
	items := r.Items
	more := 0
	if maxItems > 0 && len(items) > maxItems {
		more = len(items) - maxItems
		items = items[:maxItems]
	}
	shown := map[string]bool{}
	for _, it := range r.Items {
		shown[it.ID] = true // an item cut by maxItems is not "unlisted"
	}
	for _, it := range items {
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
				fmt.Fprintln(&b, "      "+PendingItemHoldLine(h))
				continue
			}
			fmt.Fprintf(&b, "      in Automation %s\n", pendingItemAutomationLabel(a.Name, a.AutomationID))
		}
	}
	if more > 0 {
		fmt.Fprintf(&b, "  and %d more\n", more)
	}
	var extra []PendingItemTextHeld
	for _, h := range r.Held {
		if !shown[h.ItemID] {
			extra = append(extra, h)
		}
	}
	if len(extra) > 0 {
		fmt.Fprintln(&b, "Held on an item not listed above:")
		for _, h := range extra {
			fmt.Fprintln(&b, "  "+PendingItemHoldLine(h))
		}
	}
	if r.Source.Configured {
		fmt.Fprintf(&b, "File registry: %s\n", r.Source.Path)
		fmt.Fprintln(&b, "  "+PendingItemRefreshLine(r.LastRefresh, "last refresh"))
	} else {
		fmt.Fprintln(&b, "File registry: not configured")
	}
	return b.String()
}

// RenderPendingItemCreated is the line after an item is recorded.
func RenderPendingItemCreated(it PendingItemTextItem) string {
	line := fmt.Sprintf("Recorded pending item %s (%s): %s", it.ID, it.State, it.Title)
	if it.Ref != "" {
		line += fmt.Sprintf(" [ref %s]", it.Ref)
	}
	return line + "\n"
}

// RenderPendingItemAttached is the report after an Automaton is attached.
func RenderPendingItemAttached(r PendingItemTextAttach) string {
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
	return b.String()
}

// RenderPendingItemRefreshed is the report after a file registry refresh. The
// returned error is the refresh's own error text, so a caller can mark the
// reply as failed; the text is complete either way.
func RenderPendingItemRefreshed(r PendingItemTextRefresh) (string, error) {
	if !r.Configured {
		return "No pending item file registry is configured (autonomous.automation.pending_item_source.path); nothing was read.\n", nil
	}
	text := PendingItemRefreshLine(r, "Refreshed") + "\n"
	if r.Error != "" {
		return text, fmt.Errorf("refresh failed: %s", r.Error)
	}
	return text, nil
}
