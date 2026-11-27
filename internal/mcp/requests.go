// Request tools (delivery job 4). Owner: part B. The tools are the
// delivery.Tool* names. They call the service in this process: a loopback REST
// call would carry the tool server's own token and lose who is calling. Only
// federation.CallerSessionIDFromContext, which comes from the session's own
// credential, makes a caller a holder. There is no tool for "seen": only the
// worker's own program reports that.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// SetRequests wires the request service. The daemon wires it after the tools
// are being served.
func (s *Server) SetRequests(r delivery.RequestService) {
	s.requestsMu.Lock()
	defer s.requestsMu.Unlock()
	s.requests = r
}

// SetRequestAudit takes the web server's writer of the audit line for an ID
// datawatch does not have.
func (s *Server) SetRequestAudit(fn func(c delivery.Caller, id, verb string)) {
	s.requestsMu.Lock()
	defer s.requestsMu.Unlock()
	s.requestAudit = fn
}

func (s *Server) requestService() delivery.RequestService {
	s.requestsMu.RLock()
	defer s.requestsMu.RUnlock()
	return s.requests
}

// requestCaller says who a tool call is, from the credential that made it.
func (s *Server) requestCaller(ctx context.Context) delivery.Caller {
	c := delivery.Caller{Kind: delivery.CallerOperator, Surface: delivery.SurfaceMCP}
	principal := federation.PrincipalFromContext(ctx)
	peer := mcpPeerFromContext(ctx)
	switch id := federation.CallerSessionIDFromContext(ctx); {
	case id != "" && !strings.HasPrefix(id, auth.InstanceTokenOwnerPrefix):
		c.Kind, c.SessionID = delivery.CallerSession, id
	case id != "" || principal == "session-scoped":
		c.Kind = delivery.CallerSessionUnknown
	case peer != nil:
		c.Kind, c.Peer = delivery.CallerPeer, peer.Name
	case strings.HasPrefix(principal, "peer:"):
		c.Kind, c.Peer = delivery.CallerPeer, strings.TrimPrefix(principal, "peer:")
	case principal != "" && principal != "admin":
		c.Kind = delivery.CallerSessionUnknown
	case s.callerSessionID != "":
		// The tool server's own setting is not a credential.
		c.Kind, c.ClaimedSession = delivery.CallerSessionUnknown, s.callerSessionID
	}
	return c
}

// requestFail is a refusal as every surface words it, with the status.
func (s *Server) requestFail(c delivery.Caller, tool, id, verb string, err error) *mcpsdk.CallToolResult {
	if errors.Is(err, delivery.ErrRequestNotFound) {
		s.requestsMu.RLock()
		audit := s.requestAudit
		s.requestsMu.RUnlock()
		if audit != nil {
			audit(c, id, verb)
		}
	}
	return toolErrorf(tool, "HTTP %d: %v", delivery.HTTPStatus(err), err)
}

// requestArgs reads a tool's arguments. An unknown one is refused: dropping a
// mistyped argument would change what the call does.
func requestArgs(req mcpsdk.CallToolRequest, allowed []string) (map[string]string, error) {
	args := req.GetArguments()
	names := make([]string, 0, len(args))
	for name := range args {
		names = append(names, name)
	}
	sort.Strings(names)
	out := map[string]string{}
	for _, name := range names {
		if !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("unknown argument %q; this tool takes %s", name, strings.Join(allowed, ", "))
		}
		switch v := args[name].(type) {
		case string:
			out[name] = strings.TrimSpace(v)
		case bool:
			out[name] = strconv.FormatBool(v)
		default:
			return nil, fmt.Errorf("%s must be text", name)
		}
	}
	return out, nil
}

func requestBool(f map[string]string, name string) (bool, error) {
	if f[name] == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(f[name])
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return b, nil
}

// requestTool is one tool: its arguments, those it cannot do without, and the
// call it makes. run returns the verb for the audit line.
type requestTool struct {
	name     string
	allowed  []string
	required []string
	run      func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, f map[string]string) (verb string, out any, err error)
}

// requestArgError is a refusal made here, before the service is called.
type requestArgError struct{ error }

func (s *Server) requestHandler(t requestTool) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		if deny := mcpFedCap(ctx, federation.MCPToolCap[t.name]); deny != nil {
			return deny, nil
		}
		f, err := requestArgs(req, t.allowed)
		if err != nil {
			return toolError(t.name, err), nil
		}
		for _, name := range t.required {
			if f[name] == "" {
				return toolErrorf(t.name, "%s is required", name), nil
			}
		}
		svc := s.requestService()
		if svc == nil {
			return toolErrorf(t.name, "requests are not available: they need the running datawatch daemon"), nil
		}
		c := s.requestCaller(ctx)
		// What a call says about itself is kept as a claim only.
		c.ClaimedName, c.ClaimedSession = f["claimed_by"], orClaim(c.ClaimedSession, f["claimed_session"])
		if slices.Contains(t.allowed, "claimed_by") {
			c.ClaimedSession = orClaim(c.ClaimedSession, f["session_id"])
		}
		// A caller that goes away must not stop a change part way.
		verb, out, err := t.run(context.WithoutCancel(ctx), svc, c, f)
		var bad requestArgError
		switch {
		case errors.As(err, &bad):
			return toolError(t.name, bad.error), nil
		case err != nil:
			return s.requestFail(c, t.name, f["id"], verb, err), nil
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return toolError(t.name, err), nil
		}
		return textOK(string(b)), nil
	}
}

func orClaim(have, claim string) string {
	if have != "" {
		return have
	}
	return claim
}

// claimArgs are accepted by the holder's tools and never make a holder.
var claimArgs = []string{"claimed_by", "claimed_session", "session_id"}

func holderArgs(more ...string) []string {
	return append(append([]string{"id"}, more...), claimArgs...)
}

var contentArgs = []string{"ask", "detail", "params", "items", "project_dir"}

func requestTools() []requestTool {
	return []requestTool{
		{delivery.ToolRequestMine, nil, nil, func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, _ map[string]string) (string, any, error) {
			out, err := svc.Mine(ctx, c)
			return "mine", out, err
		}},
		{delivery.ToolRequestTake, holderArgs(), []string{"id"}, func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, f map[string]string) (string, any, error) {
			out, err := svc.Take(ctx, c, f["id"], delivery.TakeInput{Via: delivery.ViaTool})
			return delivery.VerbTake, out, err
		}},
		{delivery.ToolRequestAccept, holderArgs("code"), []string{"id"}, func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, f map[string]string) (string, any, error) {
			out, err := svc.Accept(ctx, c, f["id"], delivery.AcceptInput{Code: f["code"]})
			return delivery.VerbAccept, out, err
		}},
		{delivery.ToolRequestReport, holderArgs("text", "result_id", "rules_version"), []string{"id"}, func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, f map[string]string) (string, any, error) {
			out, err := svc.Report(ctx, c, f["id"], delivery.ReportInput{Text: f["text"], ResultID: f["result_id"], RulesVersion: f["rules_version"]})
			return delivery.VerbReport, out, err
		}},
		{delivery.ToolRequestDecline, holderArgs("reason_code", "text"), []string{"id", "reason_code"}, func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, f map[string]string) (string, any, error) {
			out, err := svc.Decline(ctx, c, f["id"], delivery.DeclineInput{ReasonCode: f["reason_code"], Text: f["text"]})
			return delivery.VerbDecline, out, err
		}},
		{delivery.ToolRequestCreate, append([]string{"scope_id", "scope_kind", "role", "kind", "host", "key", "source_type", "source_id", "after", "take_by", "due"}, contentArgs...), nil,
			func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, f map[string]string) (string, any, error) {
				in, err := delivery.SurfaceCreate(f)
				if err != nil {
					return "create", nil, requestArgError{err}
				}
				v, created, err := svc.Create(ctx, c, in)
				return "create", map[string]any{"request": v, "created": created}, err
			}},
		{delivery.ToolRequestList, []string{"id", "roles", "state", "kind", "role", "scope_kind", "scope_id", "holder"}, nil,
			func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, f map[string]string) (string, any, error) {
				roles, err := requestBool(f, "roles")
				if err != nil {
					return "list", nil, requestArgError{err}
				}
				switch {
				case roles:
					out, err := svc.Roles(ctx, c)
					return "roles", map[string]any{"roles": out}, err
				case f["id"] != "":
					out, err := svc.Get(ctx, c, f["id"])
					return "get", out, err
				}
				out, err := svc.List(ctx, c, delivery.RequestFilter{State: delivery.RequestState(f["state"]), Kind: delivery.RequestKind(f["kind"]),
					Role: delivery.Role(f["role"]), ScopeKind: delivery.ScopeKind(f["scope_kind"]), ScopeID: f["scope_id"], Holder: f["holder"]})
				return "list", map[string]any{"requests": out}, err
			}},
		{delivery.ToolRequestRole, []string{"action", "role", "scope_kind", "scope_id", "session_id", "force", "reason_code"}, []string{"action"}, requestRoleRun},
		{delivery.ToolRequestManage, append([]string{"action", "id", "reason_code", "words", "repeat_ok", "to_session", "entry_key", "role", "scope_kind", "scope_id"}, contentArgs...), []string{"action"}, requestManageRun},
	}
}

func requestRoleRun(ctx context.Context, svc delivery.RequestService, c delivery.Caller, f map[string]string) (string, any, error) {
	action := f["action"]
	if action != "give" && action != "release" {
		return action, nil, requestArgError{errors.New("action must be give or release")}
	}
	role, scope, err := delivery.SurfaceScope(f)
	if err != nil {
		return action, nil, requestArgError{err}
	}
	force, err := requestBool(f, "force")
	if err != nil {
		return action, nil, requestArgError{err}
	}
	in := delivery.RoleInput{Role: role, Scope: scope, SessionID: f["session_id"], Force: force, ReasonCode: f["reason_code"]}
	if action == "release" {
		return action, map[string]any{"ok": true}, svc.ReleaseRole(ctx, c, in)
	}
	if in.SessionID == "" {
		return action, nil, requestArgError{errors.New("session_id is required to give a role")}
	}
	out, err := svc.GiveRole(ctx, c, in)
	return action, out, err
}

func requestManageRun(ctx context.Context, svc delivery.RequestService, c delivery.Caller, f map[string]string) (string, any, error) {
	action, id := f["action"], f["id"]
	if action == "from-backlog" {
		in, err := delivery.SurfaceBacklog(f)
		if err != nil {
			return action, nil, requestArgError{err}
		}
		v, created, err := svc.FromBacklog(ctx, c, in)
		return action, map[string]any{"request": v, "created": created}, err
	}
	switch action {
	case delivery.VerbReissue, delivery.VerbDrop, delivery.VerbForce, delivery.VerbAcceptUnproven:
	default:
		return action, nil, requestArgError{errors.New("action must be reissue, drop, force, accept-unproven or from-backlog")}
	}
	if id == "" {
		return action, nil, requestArgError{errors.New("id is required")}
	}
	repeat, err := requestBool(f, "repeat_ok")
	if err != nil {
		return action, nil, requestArgError{err}
	}
	in := delivery.ReasonInput{ReasonCode: f["reason_code"], Words: f["words"], RepeatOK: repeat}
	var out delivery.RequestView
	switch action {
	case delivery.VerbReissue:
		out, err = svc.Reissue(ctx, c, id, in)
	case delivery.VerbDrop:
		out, err = svc.Drop(ctx, c, id, in)
	case delivery.VerbForce:
		out, err = svc.Force(ctx, c, id, delivery.ForceInput{ReasonCode: in.ReasonCode, Words: in.Words, ToSession: f["to_session"]})
	default:
		// The "done when" list keeps no waiver without the words that were said.
		if in.Words == "" {
			return action, nil, requestArgError{errors.New("words are required: say what was accepted without proof and why")}
		}
		out, err = svc.AcceptUnproven(ctx, c, id, in)
	}
	return action, out, err
}

// requestToolDefs are the tool listings.
func requestToolDefs() []func() mcpsdk.Tool {
	str := func(name, desc string, more ...mcpsdk.PropertyOption) mcpsdk.ToolOption {
		return mcpsdk.WithString(name, append(more, mcpsdk.Description(desc))...)
	}
	id := str("id", "Request id, such as r-1a2b3c4d. Use only an id datawatch gave you", mcpsdk.Required())
	scope := []mcpsdk.ToolOption{
		str("scope_id", "Project, task, Automaton, Automation, computer or session the role is for"),
		str("scope_kind", "project (default), task, automaton, automation, host or session"),
		str("role", "Role (default supervisor)"),
	}
	content := []mcpsdk.ToolOption{
		str("ask", "The ask, in words"), str("detail", "More of the ask"),
		str("params", "JSON object of text values"),
		str("items", "JSON array of done-when items: {id, statement, from, method (receipt, state:file, confirm or review), path, contains, effect, change, before, target}. No commands are run"),
		str("project_dir", "Project folder the work is in"),
	}
	tool := func(name, desc string, readOnly bool, opts ...mcpsdk.ToolOption) func() mcpsdk.Tool {
		return func() mcpsdk.Tool {
			all := append([]mcpsdk.ToolOption{mcpsdk.WithDescription(desc)}, opts...)
			if readOnly {
				all = append(all, mcpsdk.WithReadOnlyHintAnnotation(true))
			}
			return mcpsdk.NewTool(name, all...)
		}
	}
	return []func() mcpsdk.Tool{
		tool(delivery.ToolRequestMine, "List the open datawatch requests this session holds and its roles. No request content.", true),
		tool(delivery.ToolRequestTake, "Fetch the content of a request this session holds. Only the holder's own session credential may. The same take again gives the same content: if repeat is true, do not do the work twice.", false, id),
		tool(delivery.ToolRequestAccept, "Say that this session will do a request it holds. Holder only. It does not say the work is done.", false, id,
			str("code", "Receipt code written inside the request's content")),
		tool(delivery.ToolRequestReport, "Say that this session believes a request it holds is finished. Holder only. It never makes the request done: datawatch's own check does.", false, id,
			str("text", "What was done"), str("result_id", "Result-store id of the result"), str("rules_version", "Rules version the work followed")),
		tool(delivery.ToolRequestDecline, "Say that this session will not do a request it holds. Holder only.", false, id,
			str("reason_code", "Reason code: a-z, 0-9, dot, dash and underscore", mcpsdk.Required()), str("text", "Why")),
		tool(delivery.ToolRequestCreate, "Save a request addressed to a role. This computer only. With no key each call is a new request; the same key returns the request already made.", false,
			slices.Concat(scope, content, []mcpsdk.ToolOption{
				str("kind", "Request kind (default ask)"), str("key", "Your own id for this occurrence, never the content"),
				str("after", "Request that must be done first"), str("take_by", "Time to be taken, such as 30m"), str("due", "Time to be finished, such as 24h"),
				str("source_type", "Kind of record it came from"), str("source_id", "Id of that record"), str("host", "Must be this computer or empty"),
			})...),
		tool(delivery.ToolRequestList, "List requests (IDs, states, reason codes and the plain reading), read one by id, or list who holds which role. No request content. Only a proven one reads done.", true,
			str("id", "Read this one request"), mcpsdk.WithBoolean("roles", mcpsdk.Description("List the held roles instead")),
			str("state", "Only this state"), str("kind", "Only this kind"), str("role", "Only this role"),
			str("scope_kind", "Only this kind of scope"), str("scope_id", "Only this scope"), str("holder", "Only those this session holds")),
		tool(delivery.ToolRequestRole, "Give a role to a session or release it. One holder per role and scope; taking it from a live holder needs force and a reason code. Operator action: who did it is recorded, not enforced.", false,
			slices.Concat(scope, []mcpsdk.ToolOption{
				str("action", "give or release", mcpsdk.Required()), str("session_id", "Full session id"),
				mcpsdk.WithBoolean("force", mcpsdk.Description("Take the role from a live holder")), str("reason_code", "Needed with force"),
			})...),
		tool(delivery.ToolRequestManage, "Operator actions on requests: reissue, drop, force (move off its holder; it may run twice), accept-unproven (needs words) or from-backlog. Who did it is recorded, not enforced. A doer's accept-unproven is only a claim.", false,
			slices.Concat(scope, content, []mcpsdk.ToolOption{
				str("action", "reissue, drop, force, accept-unproven or from-backlog", mcpsdk.Required()),
				str("id", "Request id (all but from-backlog)"), str("reason_code", "Reason code"), str("words", "The words that were said"),
				mcpsdk.WithBoolean("repeat_ok", mcpsdk.Description("Reissue although the work may already have been done")),
				str("to_session", "force: move to this session; empty returns it to the role"), str("entry_key", "from-backlog: delivery list entry key"),
			})...),
	}
}

// addRequestTools registers the nine tools.
func (s *Server) addRequestTools(mcpSrv *server.MCPServer, tracked func(func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)) func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error)) {
	handlers := map[string]requestTool{}
	for _, t := range requestTools() {
		handlers[t.name] = t
	}
	for _, def := range requestToolDefs() {
		tool := def()
		mcpSrv.AddTool(tool, tracked(s.requestHandler(handlers[tool.Name])))
	}
}

// queueAsRequest turns an older-queue completion or failure that names a
// request into the holder's report or decline. ok is false when it is not one.
func (s *Server) queueAsRequest(ctx context.Context, tool, id, verb string, call func(delivery.RequestService, delivery.Caller) error) (*mcpsdk.CallToolResult, bool) {
	svc := s.requestService()
	if svc == nil || !delivery.IsRequestID(id) {
		return nil, false
	}
	c := s.requestCaller(ctx)
	if err := call(svc, c); err != nil {
		return s.requestFail(c, tool, id, verb, err), true
	}
	return mcpsdk.NewToolResultText("Request " + id + ": " + verb + " recorded. It is not done until datawatch's check says so."), true
}

// listQueueItems reads the queue and reports a queue that cannot be read, where
// the store can say so, so that it never reads empty.
func listQueueItems(store *session.QueueStore, role, state string) ([]*session.QueueItem, error) {
	if q, ok := any(store).(interface {
		ListErr(role, state string) ([]*session.QueueItem, error)
	}); ok {
		return q.ListErr(role, state)
	}
	return store.List(role, state), nil
}
