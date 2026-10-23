// Comm-channel verbs for the Automata tag registry and per-Automaton tag
// assignment: autonomous tags|tag-create|tag-rename|tag-move|tag-color|
// tag-delete|set-tags. Each verb proxies the matching REST endpoint.

package router

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// commTagRequest is the REST call a tag verb translates into.
type commTagRequest struct {
	method string
	path   string
	body   map[string]any
}

// parseCommTagList splits a comma-separated tag-ID list; an empty list is
// valid and clears the assignment.
func parseCommTagList(arg string) []string {
	ids := []string{}
	for _, s := range strings.Split(arg, ",") {
		if s = strings.TrimSpace(s); s != "" {
			ids = append(ids, s)
		}
	}
	return ids
}

// buildCommTagRequest validates the args of a tag verb and returns the REST
// request it maps to. A non-empty usage string means the args were invalid.
// args[0] is the verb as typed.
func buildCommTagRequest(verb string, args []string) (req commTagRequest, usage string) {
	const base = "/api/autonomous/tags"
	switch strings.ReplaceAll(verb, "_", "-") {
	case "tags":
		return commTagRequest{method: http.MethodGet, path: base}, ""
	case "tag-create":
		usage = "usage: autonomous tag-create <name> [parent=<id>] [color=<hex>] [description=<text>]"
		if len(args) < 2 {
			return req, usage
		}
		body := map[string]any{"name": args[1]}
		rest := args[2:]
		for i := 0; i < len(rest); i++ {
			switch {
			case strings.HasPrefix(rest[i], "parent="):
				body["parent_id"] = strings.TrimPrefix(rest[i], "parent=")
			case strings.HasPrefix(rest[i], "color="):
				body["color"] = strings.TrimPrefix(rest[i], "color=")
			case strings.HasPrefix(rest[i], "description="):
				// the description runs to the end of the line
				desc := strings.TrimPrefix(rest[i], "description=")
				body["description"] = strings.TrimSpace(strings.Join(append([]string{desc}, rest[i+1:]...), " "))
				i = len(rest)
			default:
				return req, usage
			}
		}
		return commTagRequest{method: http.MethodPost, path: base, body: body}, ""
	case "tag-rename":
		if len(args) < 3 {
			return req, "usage: autonomous tag-rename <tag-id> <new-name>"
		}
		return commTagRequest{method: http.MethodPatch, path: base + "/" + url.PathEscape(args[1]),
			body: map[string]any{"name": strings.Join(args[2:], " ")}}, ""
	case "tag-move":
		if len(args) != 3 {
			return req, "usage: autonomous tag-move <tag-id> <parent-id|root>"
		}
		parent := args[2]
		if strings.EqualFold(parent, "root") {
			parent = ""
		}
		return commTagRequest{method: http.MethodPatch, path: base + "/" + url.PathEscape(args[1]),
			body: map[string]any{"parent_id": parent}}, ""
	case "tag-color":
		if len(args) != 3 {
			return req, "usage: autonomous tag-color <tag-id> <color>"
		}
		return commTagRequest{method: http.MethodPatch, path: base + "/" + url.PathEscape(args[1]),
			body: map[string]any{"color": args[2]}}, ""
	case "tag-delete":
		if len(args) != 2 {
			return req, "usage: autonomous tag-delete <tag-id>"
		}
		return commTagRequest{method: http.MethodDelete, path: base + "/" + url.PathEscape(args[1])}, ""
	case "set-tags":
		if len(args) < 2 || len(args) > 3 {
			return req, "usage: autonomous set-tags <prd-id> <tag-id1,tag-id2,...> (omit the list to clear)"
		}
		list := ""
		if len(args) == 3 {
			list = args[2]
		}
		return commTagRequest{method: http.MethodPost,
			path: "/api/autonomous/prds/" + url.PathEscape(args[1]) + "/set_tags",
			body: map[string]any{"tag_ids": parseCommTagList(list)}}, ""
	}
	return req, "usage: autonomous tags"
}

// handleAutonomousTagVerb runs one tag verb through the REST loopback.
func (r *Router) handleAutonomousTagVerb(verb string, args []string) {
	name := "autonomous " + strings.ReplaceAll(verb, "_", "-")
	req, usage := buildCommTagRequest(verb, args)
	if usage != "" {
		r.reply(name+" failed", usage)
		return
	}
	payload := ""
	if req.body != nil {
		b, _ := json.Marshal(req.body)
		payload = string(b)
	}
	out, err := r.commJSON(req.method, req.path, payload)
	if err != nil {
		r.reply(name+" failed", err.Error())
		return
	}
	if strings.TrimSpace(out) == "" {
		r.reply(name, "ok")
		return
	}
	r.reply(name, prettyJSON(out))
}
