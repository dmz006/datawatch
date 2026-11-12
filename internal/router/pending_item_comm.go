// Pending items over the chat channel. A pending item is a named piece of work
// that has no Automaton yet; an Automation member can wait on it until an
// Automaton is attached. The four verbs proxy the routes under
// /api/autonomous/pending-items, the same ones the MCP tools and the CLI use,
// and render with the shared text in internal/autonomous/pending_item_text.go:
//
//	autonomous pending-items [waiting|attached|done] [automation-id]
//	autonomous pending-item-add <title...> [ref=<label>]
//	autonomous pending-item-attach <item-id> <automaton-id>
//	autonomous pending-items-refresh

package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	pendingItemsRoute = "/api/autonomous/pending-items"

	pendingItemsUsage       = "usage: autonomous pending-items [waiting|attached|done] [automation-id]"
	pendingItemAddUsage     = "usage: autonomous pending-item-add <title...> [ref=<label>]"
	pendingItemAttachUsage  = "usage: autonomous pending-item-attach <item-id> <automaton-id>"
	pendingItemsRefreshHelp = "usage: autonomous pending-items-refresh"

	// pendingItemCommItems is the most items one list reply carries.
	pendingItemCommItems = 10
)

// PendingItemCommVerbs lists the pending item comm verbs, for the parity test.
func PendingItemCommVerbs() []string {
	return []string{"pending-items", "pending-item-add", "pending-item-attach", "pending-items-refresh"}
}

// handlePendingItemVerb answers the pending item verbs of the autonomous
// command. args[0] is the verb, as the autonomous handler passes it.
func (r *Router) handlePendingItemVerb(verb string, args []string) {
	switch strings.ReplaceAll(verb, "_", "-") {
	case "pending-items":
		r.replyPendingItems(args)
	case "pending-item-add":
		r.replyPendingItemAdd(args)
	case "pending-item-attach":
		r.replyPendingItemAttach(args)
	case "pending-items-refresh":
		r.replyPendingItemsRefresh(args)
	}
}

// pendingItemStates are the values the list verb takes as a state filter.
var pendingItemStates = map[string]bool{"waiting": true, "attached": true, "done": true}

// replyPendingItems reads the registry. The first argument is a state filter
// when it is a state word; anything else is an Automation id.
func (r *Router) replyPendingItems(args []string) {
	rest := args[1:]
	if len(rest) > 2 {
		r.reply("autonomous pending-items failed", pendingItemsUsage)
		return
	}
	q := url.Values{}
	if len(rest) > 0 && pendingItemStates[strings.ToLower(rest[0])] {
		q.Set("state", strings.ToLower(rest[0]))
		rest = rest[1:]
	}
	if len(rest) > 1 || (len(rest) == 1 && pendingItemStates[strings.ToLower(rest[0])]) {
		r.reply("autonomous pending-items failed", pendingItemsUsage)
		return
	}
	if len(rest) == 1 {
		q.Set("automation_id", rest[0])
	}
	out, err := r.commGet(pendingItemsRoute, q)
	if err != nil {
		r.reply("autonomous pending-items failed", err.Error())
		return
	}
	list, err := autonomous.ParsePendingItemList([]byte(out))
	if err != nil {
		r.reply("autonomous pending-items failed", err.Error())
		return
	}
	r.reply("autonomous pending-items", strings.TrimRight(autonomous.RenderPendingItemList(list, pendingItemCommItems), "\n"))
}

// replyPendingItemAdd records a waiting item. The title is every word that is
// not a ref= token, in order; a ref= token carries the optional external label.
func (r *Router) replyPendingItemAdd(args []string) {
	var words []string
	ref := ""
	for _, a := range args[1:] {
		if v, ok := strings.CutPrefix(a, "ref="); ok {
			ref = strings.TrimSpace(v)
			continue
		}
		words = append(words, a)
	}
	title := strings.TrimSpace(strings.Join(words, " "))
	if title == "" {
		r.reply("autonomous pending-item-add failed", pendingItemAddUsage)
		return
	}
	req := map[string]string{"title": title}
	if ref != "" {
		req["ref"] = ref
	}
	body, _ := json.Marshal(req)
	out, err := r.commJSON(http.MethodPost, pendingItemsRoute, string(body))
	if err != nil {
		r.reply("autonomous pending-item-add failed", err.Error())
		return
	}
	it, err := autonomous.ParsePendingItem([]byte(out))
	if err != nil {
		r.reply("autonomous pending-item-add failed", err.Error())
		return
	}
	r.reply("autonomous pending-item-add", strings.TrimRight(autonomous.RenderPendingItemCreated(it), "\n"))
}

// replyPendingItemAttach attaches an Automaton to a waiting item.
func (r *Router) replyPendingItemAttach(args []string) {
	if len(args) != 3 || strings.TrimSpace(args[1]) == "" || strings.TrimSpace(args[2]) == "" {
		r.reply("autonomous pending-item-attach failed", pendingItemAttachUsage)
		return
	}
	itemID := strings.TrimSpace(args[1])
	if strings.ContainsAny(itemID, "/?#") {
		r.reply("autonomous pending-item-attach failed", "item id must be a pending item id, got "+itemID+"; "+pendingItemAttachUsage)
		return
	}
	body, _ := json.Marshal(map[string]string{"automaton_id": strings.TrimSpace(args[2])})
	out, err := r.commJSON(http.MethodPost, pendingItemsRoute+"/"+url.PathEscape(itemID)+"/attach", string(body))
	if err != nil {
		r.reply("autonomous pending-item-attach failed", err.Error())
		return
	}
	res, err := autonomous.ParsePendingItemAttach([]byte(out))
	if err != nil {
		r.reply("autonomous pending-item-attach failed", err.Error())
		return
	}
	r.reply("autonomous pending-item-attach", strings.TrimRight(autonomous.RenderPendingItemAttached(res), "\n"))
}

// replyPendingItemsRefresh reads the file registry now.
func (r *Router) replyPendingItemsRefresh(args []string) {
	if len(args) > 1 {
		r.reply("autonomous pending-items-refresh failed", pendingItemsRefreshHelp)
		return
	}
	out, err := r.commJSON(http.MethodPost, pendingItemsRoute+"/refresh", "")
	if err != nil {
		r.reply("autonomous pending-items-refresh failed", err.Error())
		return
	}
	res, err := autonomous.ParsePendingItemRefresh([]byte(out))
	if err != nil {
		r.reply("autonomous pending-items-refresh failed", err.Error())
		return
	}
	text, refreshErr := autonomous.RenderPendingItemRefreshed(res)
	text = strings.TrimRight(text, "\n")
	if refreshErr != nil {
		r.reply("autonomous pending-items-refresh failed", text)
		return
	}
	r.reply("autonomous pending-items-refresh", text)
}
