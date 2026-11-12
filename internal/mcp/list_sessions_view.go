// list_sessions view / states / since params (BL432 / GH#242).
//
// The compact session-list view lives in the REST handler
// (GET /api/sessions?view=summary&states=&since=), so list_sessions
// forwards to it over the loopback instead of re-implementing the
// truncation, terminal cap and state expansion here. Without any of the
// three params list_sessions keeps its original in-process behavior.

package mcp

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// sessionsOmittedHeader mirrors the REST header that reports how many
// terminal sessions the summary view's cap dropped.
const sessionsOmittedHeader = "X-Datawatch-Sessions-Omitted"

// listSessionsViewQuery builds the REST query for list_sessions when the
// caller used view / states / since / group_by / tag_prefix. ok is false
// when none was given (the legacy path applies); errMsg is non-empty for
// invalid input. The grouped read is served by the same REST handler that
// does the grouping, so this only forwards the params and refuses an axis
// the handler would 400.
func listSessionsViewQuery(req mcpsdk.CallToolRequest) (q url.Values, ok bool, errMsg string) {
	view := req.GetString("view", "")
	states := req.GetString("states", "")
	since := req.GetString("since", "")
	groupBy := strings.TrimSpace(req.GetString("group_by", ""))
	tagPrefix := strings.TrimSpace(req.GetString("tag_prefix", ""))
	switch view {
	case "", "full", "summary":
	default:
		return nil, false, fmt.Sprintf("Error: unknown view %q (supported: summary, full).", view)
	}
	if groupBy != "" {
		switch groupBy {
		case "tag", "state", "node", "project", "automaton":
		default:
			return nil, false, fmt.Sprintf("Error: unknown group_by %q (supported: tag, state, node, project, automaton).", groupBy)
		}
	}
	if view != "summary" && states == "" && since == "" && groupBy == "" {
		return nil, false, ""
	}
	if req.GetBool("orphaned", false) {
		return nil, false, "Error: orphaned cannot be combined with view=summary, states, since or group_by."
	}
	q = url.Values{}
	if view == "summary" {
		q.Set("view", "summary")
	}
	if states != "" {
		q.Set("states", states)
	}
	if since != "" {
		q.Set("since", since)
	}
	if groupBy != "" {
		q.Set("group_by", groupBy)
		if tagPrefix != "" {
			q.Set("tag_prefix", tagPrefix)
		}
	}
	for _, k := range []string{"name", "state", "backend", "alive"} {
		if v := req.GetString(k, ""); v != "" {
			q.Set(k, v)
		}
	}
	for _, v := range req.GetStringSlice("tags", nil) {
		if v != "" {
			q.Add("tag", v)
		}
	}
	for _, tag := range sessionTagShortcuts(req) {
		q.Add("tag", tag)
	}
	if tm := req.GetString("tag_match", ""); tm != "" {
		if tm != "any" && tm != "all" {
			return nil, false, `Error: tag_match: must be "any" or "all".`
		}
		q.Set("tag_match", tm)
	}
	return q, true, ""
}

// listSessionsViaREST serves list_sessions through GET /api/sessions. The
// JSON array is returned as-is; when the summary view's terminal cap
// dropped sessions, a second text item says how many.
func (s *Server) listSessionsViaREST(q url.Values) (*mcpsdk.CallToolResult, error) {
	body, hdr, err := s.proxyGetWithHeaders("/api/sessions", q)
	if err != nil {
		return toolError("list_sessions_via_rest", err), nil
	}
	res := mcpsdk.NewToolResultText(string(body))
	if n, _ := strconv.Atoi(hdr.Get(sessionsOmittedHeader)); n > 0 {
		res.Content = append(res.Content, mcpsdk.NewTextContent(fmt.Sprintf(
			"%d older terminal session(s) omitted by the summary view; use view=full or narrow with states/since to see them.", n)))
	}
	return res, nil
}
