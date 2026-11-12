package mcp

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
)

// The pending item tools proxy the REST routes under
// /api/autonomous/pending-items. A pending item is a named piece of work that
// has no Automaton yet; an Automation member can wait on it until an Automaton
// is attached. The read tool changes nothing; add, attach and refresh write.

const pendingItemsPath = "/api/autonomous/pending-items"

func (s *Server) toolAutonomousPendingItems() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_pending_items",
		mcpsdk.WithDescription("Read the Automaton pending items: named pieces of work that have no Automaton yet. Returns \"policy\" (hold or skip) and \"auto_resume\" in effect, \"source\" (the file registry settings; \"configured\" is false when no registry path is set), \"last_refresh\" (when the file registry was last read and the counts of rows added, updated, done, dropped, kept and skipped, or the \"error\"), \"items\" and \"held\". Each item has \"id\", \"ref\" (the optional external label), \"title\", \"state\" (waiting, attached or done), \"source\" (internal or file), \"automaton_id\" (set once attached), the timestamps, and \"automations\" (the Automations that carry it as a member). Each \"held\" entry names an Automation waiting on a waiting item right now. Read-only; the same object as GET /api/autonomous/pending-items."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
		mcpsdk.WithString("state", mcpsdk.Enum("waiting", "attached", "done"), mcpsdk.Description("Optional state filter: waiting, attached or done.")),
		mcpsdk.WithString("automation_id", mcpsdk.Description("Optional Automation ID; narrows the items and holds to that Automation.")),
	)
}

func (s *Server) toolAutonomousPendingItemAdd() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_pending_item_add",
		mcpsdk.WithDescription("Record a waiting pending item: a named piece of work that has no Automaton yet. An Automation can carry it as a member (automation_insert_member with pending_item_id) and holds at it until an Automaton is attached with autonomous_pending_item_attach. The daemon generates the item ID. Returns the new item; the same object as POST /api/autonomous/pending-items."),
		mcpsdk.WithString("title", mcpsdk.Required(), mcpsdk.Description("Name of the work. Must not be blank.")),
		mcpsdk.WithString("ref", mcpsdk.Description("Optional external label for the item, for example the ID it has in a file registry or tracker.")),
	)
}

func (s *Server) toolAutonomousPendingItemAttach() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_pending_item_attach",
		mcpsdk.WithDescription("Attach an Automaton to a waiting pending item. Every Automation member that waits on the item becomes an ordinary Automaton member for that Automaton and, with pending_item_auto_resume on, the held Automation continues. The item moves to attached. Refused when the item is not waiting or the Automaton does not exist. The same as POST /api/autonomous/pending-items/{id}/attach."),
		mcpsdk.WithString("item_id", mcpsdk.Required(), mcpsdk.Description("The pending item ID (8 hex characters, from autonomous_pending_items).")),
		mcpsdk.WithString("automaton_id", mcpsdk.Required(), mcpsdk.Description("The Automaton ID to attach.")),
	)
}

func (s *Server) toolAutonomousPendingItemsRefresh() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_pending_items_refresh",
		mcpsdk.WithDescription("Read the pending item file registry now (autonomous.automation.pending_item_source.path) instead of waiting for the next refresh. Adds new rows as waiting items, updates titles, and marks items done when their state column says so; rows that disappeared follow pending_item_source.on_missing_row. Returns the counts of rows added, updated, done, dropped, kept and skipped, or the error. With no registry path set it reports that nothing is configured and changes nothing. The same as POST /api/autonomous/pending-items/refresh."),
	)
}

// PendingItemToolNames lists the pending item tools, for the parity test.
func PendingItemToolNames() []string {
	return []string{
		"autonomous_pending_items",
		"autonomous_pending_item_add",
		"autonomous_pending_item_attach",
		"autonomous_pending_items_refresh",
	}
}

func (s *Server) handleAutonomousPendingItems(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	q := url.Values{}
	if st := strings.TrimSpace(req.GetString("state", "")); st != "" {
		switch st {
		case "waiting", "attached", "done":
			q.Set("state", st)
		default:
			return toolErrorf("autonomous_pending_items", "state must be waiting, attached or done, got %q", st), nil
		}
	}
	if id := strings.TrimSpace(req.GetString("automation_id", "")); id != "" {
		q.Set("automation_id", id)
	}
	out, err := s.proxyGet(pendingItemsPath, q)
	if err != nil {
		return toolError("autonomous_pending_items", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleAutonomousPendingItemAdd(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	title := strings.TrimSpace(req.GetString("title", ""))
	if title == "" {
		return toolErrorf("autonomous_pending_item_add", "title is required"), nil
	}
	body := map[string]any{"title": title}
	if ref := strings.TrimSpace(req.GetString("ref", "")); ref != "" {
		body["ref"] = ref
	}
	out, err := s.proxyJSON(http.MethodPost, pendingItemsPath, body)
	if err != nil {
		return toolError("autonomous_pending_item_add", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleAutonomousPendingItemAttach(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	itemID := strings.TrimSpace(req.GetString("item_id", ""))
	if itemID == "" {
		return toolErrorf("autonomous_pending_item_attach", "item_id is required"), nil
	}
	if strings.ContainsAny(itemID, "/?#") {
		return toolErrorf("autonomous_pending_item_attach", "item_id must be a pending item ID, got %q", itemID), nil
	}
	automatonID := strings.TrimSpace(req.GetString("automaton_id", ""))
	if automatonID == "" {
		return toolErrorf("autonomous_pending_item_attach", "automaton_id is required"), nil
	}
	out, err := s.proxyJSON(http.MethodPost, pendingItemsPath+"/"+url.PathEscape(itemID)+"/attach",
		map[string]any{"automaton_id": automatonID})
	if err != nil {
		return toolError("autonomous_pending_item_attach", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleAutonomousPendingItemsRefresh(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, pendingItemsPath+"/refresh", nil)
	if err != nil {
		return toolError("autonomous_pending_items_refresh", err), nil
	}
	return textOK(string(out)), nil
}

// ---- autonomous_config_set parameters ----

// pendingItemConfigParam maps one autonomous_config_set parameter to its
// dotted config key. The dotted key's last segment is the field name inside
// the nested automation object that PUT /api/autonomous/config reads
// (pending_item_policy, pending_item_auto_resume, or one field of
// pending_item_source).
var pendingItemConfigParams = []struct {
	param string
	key   string
}{
	{"automation_pending_item_policy", config.PendingItemKeyPolicy},
	{"automation_pending_item_auto_resume", config.PendingItemKeyAutoResume},
	{"automation_pending_item_source_path", config.PendingItemKeyPath},
	{"automation_pending_item_source_id_pattern", config.PendingItemKeyIDPattern},
	{"automation_pending_item_source_id_column", config.PendingItemKeyIDColumn},
	{"automation_pending_item_source_title_column", config.PendingItemKeyTitleColumn},
	{"automation_pending_item_source_state_column", config.PendingItemKeyStateColumn},
	{"automation_pending_item_source_done_values", config.PendingItemKeyDoneValues},
	{"automation_pending_item_source_on_missing_row", config.PendingItemKeyOnMissingRow},
}

// pendingItemConfigOptions declares the nine pending item parameters of
// autonomous_config_set.
func pendingItemConfigOptions() []mcpsdk.ToolOption {
	return []mcpsdk.ToolOption{
		mcpsdk.WithString("automation_pending_item_policy", mcpsdk.Description("What an Automation does when it reaches a pending item member that is still waiting for an Automaton: hold (the default; the Automation waits at that member) or skip (it passes over the member) (autonomous.automation.pending_item_policy). Any other value is rejected.")),
		mcpsdk.WithBoolean("automation_pending_item_auto_resume", mcpsdk.Description("When true (the default) an Automation held on a pending item continues by itself as soon as an Automaton is attached; when false it stays held until an operator resumes it (autonomous.automation.pending_item_auto_resume).")),
		mcpsdk.WithString("automation_pending_item_source_path", mcpsdk.Description("File registry the daemon reads pending items from: a markdown table file path (autonomous.automation.pending_item_source.path). Empty (the default) means no file registry. An explicit empty string clears it.")),
		mcpsdk.WithString("automation_pending_item_source_id_pattern", mcpsdk.Description("Go regular expression a registry row's id must match to count as a pending item (autonomous.automation.pending_item_source.id_pattern). Default ^\\S+$ (any non-blank word). An invalid expression is rejected.")),
		mcpsdk.WithString("automation_pending_item_source_id_column", mcpsdk.Description("Registry column holding the item id: a header name (matched without regard to case) or a 1-based column number (autonomous.automation.pending_item_source.id_column). Default \"1\".")),
		mcpsdk.WithString("automation_pending_item_source_title_column", mcpsdk.Description("Registry column holding the item title: a header name (matched without regard to case) or a 1-based column number (autonomous.automation.pending_item_source.title_column). Default \"2\".")),
		mcpsdk.WithString("automation_pending_item_source_state_column", mcpsdk.Description("Registry column holding the item state: a header name (matched without regard to case) or a 1-based column number (autonomous.automation.pending_item_source.state_column). Default empty: the registry has no state column.")),
		mcpsdk.WithArray("automation_pending_item_source_done_values", mcpsdk.WithStringItems(), mcpsdk.Description("State column words that mean the item is done (autonomous.automation.pending_item_source.done_values). Default done, closed, shipped. An explicit [] restores the default.")),
		mcpsdk.WithString("automation_pending_item_source_on_missing_row", mcpsdk.Description("What happens to a file item whose row is no longer in the registry: keep (the default) leaves the item as it is, drop removes it while it is still waiting (autonomous.automation.pending_item_source.on_missing_row). Any other value is rejected.")),
	}
}

// pendingItemConfigText turns the raw argument of one parameter into the text
// config.ValidatePendingItemKey checks, the same text the REST write checks.
func pendingItemConfigText(param string, raw any) (string, bool) {
	switch v := raw.(type) {
	case string:
		return v, true
	case bool:
		if param == "automation_pending_item_auto_resume" {
			if v {
				return "true", true
			}
			return "false", true
		}
	case []any:
		if param == "automation_pending_item_source_done_values" {
			words := make([]string, 0, len(v))
			for _, item := range v {
				w, ok := item.(string)
				if !ok {
					return "", false
				}
				words = append(words, w)
			}
			return strings.Join(words, ","), true
		}
	case []string:
		if param == "automation_pending_item_source_done_values" {
			return strings.Join(v, ","), true
		}
	}
	return "", false
}

// pendingItemConfigArgs reads the pending item parameters of an
// autonomous_config_set call and returns them as the nested fields of the
// automation object (pending_item_policy, pending_item_auto_resume and
// pending_item_source). The result is empty when none was given. Parameters
// are presence-aware so an explicit empty string or empty list is sent. Every
// value is checked with config.ValidatePendingItemKey before anything is sent.
func pendingItemConfigArgs(req mcpsdk.CallToolRequest) (map[string]any, error) {
	args := req.GetArguments()
	out := map[string]any{}
	if args == nil {
		return out, nil
	}
	source := map[string]any{}
	for _, p := range pendingItemConfigParams {
		raw, ok := args[p.param]
		if !ok {
			continue
		}
		text, ok := pendingItemConfigText(p.param, raw)
		if !ok {
			return nil, &pendingItemArgError{param: p.param}
		}
		if err := config.ValidatePendingItemKey(p.key, text); err != nil {
			return nil, err
		}
		switch p.key {
		case config.PendingItemKeyPolicy:
			out["pending_item_policy"] = text
		case config.PendingItemKeyAutoResume:
			out["pending_item_auto_resume"] = text == "true"
		case config.PendingItemKeyDoneValues:
			words := []string{}
			for _, w := range strings.Split(text, ",") {
				if w = strings.TrimSpace(w); w != "" {
					words = append(words, w)
				}
			}
			source["done_values"] = words
		default:
			source[strings.TrimPrefix(p.key, "autonomous.automation.pending_item_source.")] = text
		}
	}
	if len(source) > 0 {
		out["pending_item_source"] = source
	}
	return out, nil
}

// pendingItemArgError reports a parameter given the wrong type of value.
type pendingItemArgError struct{ param string }

func (e *pendingItemArgError) Error() string {
	switch e.param {
	case "automation_pending_item_auto_resume":
		return e.param + " must be true or false"
	case "automation_pending_item_source_done_values":
		return e.param + " must be a list of words"
	default:
		return e.param + " must be a string"
	}
}
