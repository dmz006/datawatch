package router

import (
	"net/url"
	"strings"
)

// traceUsage is the comm usage line of the trace verbs.
const traceUsage = "usage: trace session <id> [tool=<t>] [backend=<b>] [kind=<k>] [errors=true] [limit=<n>] | trace automaton <id> [same filters] | trace rollup [dimension=session|task|story|automaton|agent|backend] [key=<k>] | trace coverage [backend=<b>]"

// traceVerbKeys lists the key=value words each trace verb accepts, for the
// parity test.
var traceVerbKeys = map[string][]string{
	"session":   {"tool", "backend", "kind", "errors", "limit", "loop_threshold"},
	"automaton": {"tool", "backend", "kind", "errors", "limit", "loop_threshold"},
	"rollup":    {"dimension", "key", "loop_threshold"},
	"coverage":  {"backend"},
}

// TraceCommVerbs lists the trace comm verbs ("trace <verb>"), for the
// parity test.
func TraceCommVerbs() []string { return []string{"session", "automaton", "rollup", "coverage"} }

// TraceCommKeys returns the key=value words "trace <verb>" accepts.
func TraceCommKeys(verb string) []string { return append([]string(nil), traceVerbKeys[verb]...) }

// handleTraceCmd serves "trace session|automaton|rollup|coverage" through the
// REST loopback (GET /api/trace/*), the same routes the CLI and the MCP tools
// use, and relays the JSON.
func (r *Router) handleTraceCmd(cmd Command) {
	const label = "trace"
	words := strings.Fields(cmd.Text)
	if len(words) == 0 {
		r.reply(label+" failed", traceUsage)
		return
	}
	verb := strings.ToLower(words[0])
	allowed, ok := traceVerbKeys[verb]
	if !ok {
		r.reply(label+" failed", traceUsage)
		return
	}
	rest := words[1:]
	path := "/api/trace/" + verb
	if verb == "session" || verb == "automaton" {
		if len(rest) == 0 || strings.Contains(rest[0], "=") {
			r.reply(label+" "+verb+" failed", traceUsage)
			return
		}
		path += "/" + url.PathEscape(rest[0])
		rest = rest[1:]
	}
	q := url.Values{}
	for _, tok := range rest {
		k, v, found := strings.Cut(tok, "=")
		if !found || v == "" || !traceKeyAllowed(allowed, k) {
			r.reply(label+" "+verb+" failed", traceUsage)
			return
		}
		q.Set(k, v)
	}
	body, err := r.commGet(path, q)
	if err != nil {
		r.reply(label+" "+verb+" failed", err.Error())
		return
	}
	r.reply(label+" "+verb, body)
}

func traceKeyAllowed(allowed []string, k string) bool {
	for _, a := range allowed {
		if a == k {
			return true
		}
	}
	return false
}
