// Requests over chat: "request list|show|mine|roles|role|create|take|accept|
// report|decline|reissue|drop|force|accept-unproven|from-backlog", through the
// /api/requests loopback. The loopback uses the daemon's own token, which
// holds nothing, so chat can never take or report. A message sent for a
// session or peer names it in a header, and the daemon then reads the call as
// that session's or peer's: this only ever lowers who is calling. No reply
// shows a request's content.

package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
)

// requestChatItems bounds the request lines of a chat reply.
const requestChatItems = 20

const requestUsage = "usage: request [list] | show <id> | mine | roles | role give session=<id> scope=<id> | role release scope=<id> | " +
	"create scope=<id> ask=<words> | take|accept|report|decline <id> | reissue|drop|force|accept-unproven <id> reason=<code> | " +
	"from-backlog <entry key> scope=<id> (free text goes after ask=, text= or words=, last in the message)"

func isRequestCommand(text string) bool {
	_, _, ok := delivery.RequestChatVerb(text)
	return ok
}

func parseRequest(text string) Command {
	verb, rest, _ := delivery.RequestChatVerb(text)
	return Command{Type: CmdRequest, RequestVerb: verb, Text: rest}
}

// requestFreeText are the options whose value is the rest of the message.
var requestFreeText = []string{"ask", "text", "words"}

// requestWords splits a message into its plain words and its options. No error
// repeats a word of the message: it may be part of a request's content.
func requestWords(rest string, allowed ...string) (plain []string, opts map[string]string, err error) {
	opts = map[string]string{}
	for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
		word := rest
		if i := strings.IndexAny(rest, " \t\n"); i >= 0 {
			word = rest[:i]
		}
		name, value, isOption := strings.Cut(word, "=")
		if !isOption {
			plain, rest = append(plain, word), rest[len(word):]
			continue
		}
		name = strings.ToLower(name)
		if !slices.Contains(allowed, name) {
			return nil, nil, fmt.Errorf("it takes only %s (free text goes after ask=, text= or words=)", strings.Join(allowed, "=, ")+"=")
		}
		if slices.Contains(requestFreeText, name) {
			value, rest = strings.TrimSpace(rest[len(name)+1:]), ""
		} else {
			rest = rest[len(word):]
		}
		if _, twice := opts[name]; twice || value == "" {
			return nil, nil, fmt.Errorf("give %s= once, with a value", name)
		}
		opts[name] = value
	}
	return plain, opts, nil
}

// requestFields renames chat's short option names to the shared ones.
func requestFields(opts map[string]string) map[string]string {
	short := map[string]string{"scope": "scope_id", "session": "session_id", "reason": "reason_code", "to": "to_session"}
	out := map[string]string{}
	for name, v := range opts {
		if long, ok := short[name]; ok {
			name = long
		}
		out[name] = v
	}
	return out
}

// requestChatCall is one loopback call and how its answer reads.
type requestChatCall struct {
	method, path string
	body         any
	text         func(answer []byte) (string, error)
}

func requestDecoded[T any](render func(T) string) func([]byte) (string, error) {
	return func(b []byte) (string, error) {
		var v T
		if err := json.Unmarshal(b, &v); err != nil {
			return "", err
		}
		return render(v), nil
	}
}

type requestListAnswer struct {
	Requests []delivery.RequestView `json:"requests"`
}

type requestRolesAnswer struct {
	Roles []delivery.RoleBinding `json:"roles"`
}

type requestCreatedAnswer struct {
	Request delivery.RequestView `json:"request"`
	Created bool                 `json:"created"`
}

func requestChanged(v delivery.RequestView) string { return v.ID + ": " + delivery.RenderLabel(v) }

func requestMade(a requestCreatedAnswer) string {
	if a.Created {
		return "created " + requestChanged(a.Request)
	}
	return "already there " + requestChanged(a.Request)
}

// requestChat builds the call of one chat command.
func requestChat(verb, rest string) (requestChatCall, error) {
	var call requestChatCall
	post := func(path string, body any, text func([]byte) (string, error)) (requestChatCall, error) {
		return requestChatCall{http.MethodPost, path, body, text}, nil
	}
	get := func(path string, text func([]byte) (string, error)) (requestChatCall, error) {
		return requestChatCall{http.MethodGet, path, nil, text}, nil
	}
	allowed := map[string][]string{
		"list":         {"state", "kind", "role", "scope", "scope_kind", "holder"},
		"role":         {"session", "scope", "scope_kind", "role", "force", "reason"},
		"create":       {"scope", "scope_kind", "role", "kind", "host", "key", "after", "take_by", "due", "source_type", "source_id", "project_dir", "ask"},
		"from-backlog": {"scope", "scope_kind", "role", "project_dir", "ask"},
		"accept":       {"code"},
		"report":       {"result_id", "rules_version", "text"},
		"decline":      {"reason", "text"},
		"reissue":      {"reason", "repeat_ok", "words"},
		"drop":         {"reason", "words"},
		"force":        {"reason", "to", "words"},

		delivery.VerbAcceptUnproven: {"reason", "words"},
	}[verb]
	plain, opts, err := requestWords(rest, allowed...)
	if err != nil {
		return call, err
	}
	f := requestFields(opts)
	// want is how many plain words the command takes.
	want := func(n int, what string) error {
		if len(plain) != n {
			return fmt.Errorf("request %s takes %s", verb, what)
		}
		return nil
	}
	switch verb {
	case "list", "mine", "roles", "create":
		if err := want(0, "options only"); err != nil {
			return call, err
		}
	case "role":
		if err := want(1, "give or release"); err != nil {
			return call, err
		}
	case "from-backlog":
		if err := want(1, "one entry key"); err != nil {
			return call, err
		}
	default:
		if err := want(1, "one request id"); err != nil {
			return call, err
		}
	}

	switch verb {
	case "list":
		q := url.Values{}
		for name, v := range f {
			q.Set(name, v)
		}
		target := delivery.RouteRequests
		if len(q) > 0 {
			target += "?" + q.Encode()
		}
		return get(target, requestDecoded(func(a requestListAnswer) string { return delivery.RenderRequests(a.Requests, requestChatItems) }))
	case "mine":
		return get(delivery.RouteRequestsMine, requestDecoded(func(m delivery.MineReply) string { return delivery.RenderMine(m, requestChatItems) }))
	case "roles":
		return get(delivery.RouteRequestRoles, requestDecoded(func(a requestRolesAnswer) string { return delivery.RenderRoles(a.Roles) }))
	case "role":
		action := strings.ToLower(plain[0])
		if action != "give" && action != "release" {
			return call, errors.New("request role takes give or release")
		}
		role, scope, err := delivery.SurfaceScope(f)
		if err != nil {
			return call, errors.New("scope= is required")
		}
		in := delivery.RoleInput{Role: role, Scope: scope, SessionID: f["session_id"], ReasonCode: f["reason_code"]}
		if v := f["force"]; v != "" {
			if in.Force, err = strconv.ParseBool(v); err != nil {
				return call, errors.New("force= is true or false")
			}
		}
		if action == "release" {
			return post(delivery.RouteRequestRoleRelease, in, func([]byte) (string, error) { return "released", nil })
		}
		if in.SessionID == "" {
			return call, errors.New("session= is required to give a role")
		}
		return post(delivery.RouteRequestRoleGive, in, requestDecoded(func(b delivery.RoleBinding) string { return delivery.RenderRoles([]delivery.RoleBinding{b}) }))
	case "create":
		in, err := delivery.SurfaceCreate(f)
		if err != nil {
			return call, requestInputWords(err)
		}
		return post(delivery.RouteRequests, in, requestDecoded(requestMade))
	case "from-backlog":
		f["entry_key"] = plain[0]
		in, err := delivery.SurfaceBacklog(f)
		if err != nil {
			return call, requestInputWords(err)
		}
		return post(delivery.RouteRequestFromBacklog, in, requestDecoded(requestMade))
	}

	id := plain[0]
	target := delivery.RouteRequests + "/" + url.PathEscape(id) + "/" + verb
	body := map[string]any{}
	for name, v := range f {
		body[name] = v
	}
	changed := requestDecoded(requestChanged)
	switch verb {
	case "show":
		return get(delivery.RouteRequests+"?id="+url.QueryEscape(id), requestDecoded(delivery.RenderRequest))
	case delivery.VerbTake:
		// The content is never shown in chat.
		return post(target, body, requestDecoded(func(t delivery.TakeReply) string { return t.Request.ID + ": taken; its content is not shown in chat" }))
	case delivery.VerbAccept, delivery.VerbReport:
		return post(target, body, changed)
	}
	if f["reason_code"] == "" {
		return call, fmt.Errorf("request %s needs reason=", verb)
	}
	switch verb {
	case delivery.VerbAcceptUnproven:
		if f["words"] == "" {
			return call, errors.New("request accept-unproven needs words=: what was accepted without proof and why")
		}
	case delivery.VerbReissue:
		if v := f["repeat_ok"]; v != "" {
			ok, err := strconv.ParseBool(v)
			if err != nil {
				return call, errors.New("repeat_ok= is true or false")
			}
			body["repeat_ok"] = ok
		}
	case delivery.VerbForce:
		changed = requestDecoded(func(v delivery.RequestView) string { return requestChanged(v) + " (moved; it may run twice)" })
	}
	return post(target, body, changed)
}

// requestInputWords drops the shared "invalid request" line from a refusal.
func requestInputWords(err error) error {
	return errors.New(strings.TrimPrefix(err.Error(), delivery.ErrRequestInput.Error()+"\n"))
}

// requestDo sends one loopback request for caller ("" is the operator).
func (r *Router) requestDo(method, target, jsonBody, caller string) (string, error) {
	if r.webPort == 0 {
		return "", fmt.Errorf("REST loopback not configured")
	}
	var rdr io.Reader
	if jsonBody != "" {
		rdr = bytes.NewReader([]byte(jsonBody))
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", r.webPort, target), rdr)
	if err != nil {
		return "", err
	}
	if jsonBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(federation.HeaderRequestSurface, string(delivery.SurfaceChat))
	if caller != "" {
		req.Header.Set(federation.HeaderDeliveryCaller, caller)
	}
	return r.commDo(req)
}

// handleRequest serves the request commands. caller is the session or peer the
// message was sent for, or "".
func (r *Router) handleRequest(cmd Command, caller string) {
	label := "request " + cmd.RequestVerb
	call, err := requestChat(cmd.RequestVerb, cmd.Text)
	if err != nil {
		r.reply(label+" not run", err.Error()+"\n"+requestUsage)
		return
	}
	payload := ""
	if call.body != nil {
		b, err := json.Marshal(call.body)
		if err != nil {
			r.reply(label+" failed", err.Error())
			return
		}
		payload = string(b)
	}
	out, err := r.requestDo(call.method, call.path, payload, caller)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	text, err := call.text([]byte(out))
	if err != nil {
		r.reply(label+" failed", "unreadable answer from the daemon")
		return
	}
	r.reply(label, text)
}

// handleRequestRest serves a "rest" command on a request route, so it too is
// recorded as chat's and under the session or peer it was sent for. The daemon
// refuses a take from chat, so no content comes back. It reports false for
// every other rest command.
func (r *Router) handleRequestRest(cmd Command, caller string) bool {
	u, err := url.Parse(cmd.RestPath)
	if err != nil {
		return false
	}
	if p := path.Clean("/" + u.Path); p != delivery.RouteRequests && !strings.HasPrefix(p, delivery.RouteRequests+"/") {
		return false
	}
	label := "rest " + cmd.RestMethod + " " + cmd.RestPath
	body, err := r.requestDo(cmd.RestMethod, cmd.RestPath, cmd.RestBody, caller)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return true
	}
	r.reply(label, body)
	return true
}
