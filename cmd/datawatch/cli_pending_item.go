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

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const pendingItemsRoute = "/api/autonomous/pending-items"

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
//
// The text is built by internal/autonomous/pending_item_text.go, the same
// renderer the chat verbs use.

func renderPendingItemList(w io.Writer, body []byte) error {
	r, err := autonomous.ParsePendingItemList(body)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, autonomous.RenderPendingItemList(r, 0))
	return err
}

func renderPendingItemCreated(w io.Writer, body []byte) error {
	it, err := autonomous.ParsePendingItem(body)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, autonomous.RenderPendingItemCreated(it))
	return err
}

func renderPendingItemAttached(w io.Writer, body []byte) error {
	r, err := autonomous.ParsePendingItemAttach(body)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, autonomous.RenderPendingItemAttached(r))
	return err
}

func renderPendingItemRefreshed(w io.Writer, body []byte) error {
	r, err := autonomous.ParsePendingItemRefresh(body)
	if err != nil {
		return err
	}
	text, refreshErr := autonomous.RenderPendingItemRefreshed(r)
	if _, err := io.WriteString(w, text); err != nil {
		return err
	}
	return refreshErr
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
