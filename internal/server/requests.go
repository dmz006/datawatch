// Requests over REST (delivery job 4). Owner: part B. The routes are the
// delivery.Route* names; GET /api/requests?id= reads one request.
//
// The caller is built from the credential only. A body's claimed_by or session
// fields are kept as claims and never make a holder. Reading needs queue:read
// and every other call queue:write. Who may create or manage requests is
// recorded and not enforced; the service refuses everyone but the holder for
// take, seen, accept, report and decline, and refuses other computers.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// SetRequests wires the request service. The daemon wires it after the web
// server is serving.
func (s *HTTPServer) SetRequests(r delivery.RequestService) {
	if s != nil && s.api != nil {
		s.api.deliveryMu.Lock()
		defer s.api.deliveryMu.Unlock()
		s.api.requests = r
	}
}

// requestService is the wired service, or nil.
func (s *Server) requestService() delivery.RequestService {
	s.deliveryMu.RLock()
	defer s.deliveryMu.RUnlock()
	return s.requests
}

const requestsUnavailable = "requests are not available"

// requestMaxBody bounds a request call's body.
const requestMaxBody = 1 << 20

// requestCaller says who made a call, from the credential. On an admin call the
// caller header (set by chat commands acting for a session or peer) can only
// lower it to an unnamed session or a peer: neither holds anything.
func (s *Server) requestCaller(r *http.Request) delivery.Caller {
	ctx := r.Context()
	c := delivery.Caller{Kind: delivery.CallerOperator, Surface: delivery.SurfaceREST}
	switch delivery.Surface(r.Header.Get(federation.HeaderRequestSurface)) {
	case delivery.SurfaceCLI:
		c.Surface = delivery.SurfaceCLI
	case delivery.SurfaceChat:
		c.Surface = delivery.SurfaceChat
	}
	if sessionCapsFromContext(ctx) != nil {
		id := ""
		if s.sessionTokens != nil {
			if tok := callerTokenFromContext(ctx); tok != "" {
				id = s.sessionTokens.SessionIDForToken(tok)
			}
		}
		// A credential that is tied to no one session names no holder.
		if id == "" || strings.HasPrefix(id, auth.InstanceTokenOwnerPrefix) {
			c.Kind = delivery.CallerSessionUnknown
		} else {
			c.Kind, c.SessionID = delivery.CallerSession, id
		}
		return c
	}
	if peer := peerFromContext(ctx); peer != nil {
		c.Kind, c.Peer = delivery.CallerPeer, peer.Name
		return c
	}
	if h := r.Header.Get(federation.HeaderDeliveryCaller); delivery.NonOperator(h) {
		kind, name, _ := strings.Cut(h, ":")
		if !deliveryCallerRe.MatchString(h) || name == "unknown" {
			name = ""
		}
		if kind == "peer" {
			c.Kind, c.Peer = delivery.CallerPeer, name
		} else {
			c.Kind, c.ClaimedSession = delivery.CallerSessionUnknown, name
		}
	}
	return c
}

// auditRequestUnknown records a call that named a request datawatch does not
// have. The line holds identifiers only. The assistant tools use it too.
func (s *Server) auditRequestUnknown(c delivery.Caller, id, verb string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor: c.Observed(), Action: "request_unknown_id", SessionID: c.SessionID,
		Details: map[string]any{"request_id": delivery.SafeRequestID(id), "verb": verb, "surface": string(c.Surface)},
	})
}

// requestRefused writes the service's refusal with its status.
func (s *Server) requestRefused(w http.ResponseWriter, c delivery.Caller, id, verb string, err error) {
	if errors.Is(err, delivery.ErrRequestNotFound) {
		s.auditRequestUnknown(c, id, verb)
	}
	http.Error(w, err.Error(), delivery.HTTPStatus(err))
}

// requestDecode reads at most one JSON object and refuses an unknown field. An
// empty body leaves into as it is.
func requestDecode(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestMaxBody))
	dec.DisallowUnknownFields()
	err := dec.Decode(into)
	if errors.Is(err, io.EOF) {
		return true
	}
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		http.Error(w, "bad request: one JSON object only", http.StatusBadRequest)
		return false
	}
	return true
}

// requestClaims is what a holder call may say about itself. None of it counts
// towards holding.
type requestClaims struct {
	ClaimedBy      string `json:"claimed_by,omitempty"`
	ClaimedSession string `json:"claimed_session,omitempty"`
	Session        string `json:"session,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Via            string `json:"via,omitempty"`
}

func (cl requestClaims) apply(c delivery.Caller) delivery.Caller {
	c.ClaimedName = cl.ClaimedBy
	for _, v := range []string{cl.ClaimedSession, cl.SessionID, cl.Session} {
		if v != "" && c.ClaimedSession == "" {
			c.ClaimedSession = v
		}
	}
	return c
}

type requestCreateBody struct {
	delivery.CreateInput
	Content delivery.SurfaceContent `json:"content"`
}

type requestBacklogBody struct {
	delivery.BacklogInput
	Content delivery.SurfaceContent `json:"content"`
}

// requestCall is one route's work: it decodes its body and calls the service.
// ok false means it has already written a refusal of the body.
type requestCall func(w http.ResponseWriter, r *http.Request, svc delivery.RequestService, c delivery.Caller) (reply any, ok bool, err error)

type requestRoute struct {
	method string
	call   requestCall
}

func requestAnswer[T any](v T, err error) (any, bool, error) { return v, true, err }

// requestHolderRoute is one of the calls only the holder may make.
func requestHolderRoute[B any](claims func(*B) requestClaims, call func(context.Context, delivery.RequestService, delivery.Caller, B) (any, error)) requestRoute {
	return requestRoute{http.MethodPost, func(w http.ResponseWriter, r *http.Request, svc delivery.RequestService, c delivery.Caller) (any, bool, error) {
		var body B
		if !requestDecode(w, r, &body) {
			return nil, false, nil
		}
		out, err := call(context.WithoutCancel(r.Context()), svc, claims(&body).apply(c), body)
		return out, true, err
	}}
}

// requestBodyRoute is a call that takes one body.
func requestBodyRoute[B any](call func(context.Context, delivery.RequestService, delivery.Caller, B) (any, error)) requestRoute {
	return requestRoute{http.MethodPost, func(w http.ResponseWriter, r *http.Request, svc delivery.RequestService, c delivery.Caller) (any, bool, error) {
		var body B
		if !requestDecode(w, r, &body) {
			return nil, false, nil
		}
		// A caller that goes away must not stop a change part way.
		out, err := call(context.WithoutCancel(r.Context()), svc, c, body)
		return out, true, err
	}}
}

type requestSeenBody struct {
	requestClaims
	Digest          string `json:"digest,omitempty"`
	ReporterVersion string `json:"reporter_version,omitempty"`
}

type requestAcceptBody struct {
	requestClaims
	Code string `json:"code,omitempty"`
}

type requestReportBody struct {
	requestClaims
	delivery.ReportInput
}

type requestDeclineBody struct {
	requestClaims
	delivery.DeclineInput
}

// requestIDRoute is POST /api/requests/{id}/{verb}.
func requestIDRoute(id, verb string) (requestRoute, bool) {
	switch verb {
	case delivery.VerbTake:
		return requestHolderRoute(func(b *requestClaims) requestClaims { return *b },
			func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, b requestClaims) (any, error) {
				return svc.Take(ctx, c, id, delivery.TakeInput{Via: b.Via})
			}), true
	case delivery.VerbSeen:
		return requestHolderRoute(func(b *requestSeenBody) requestClaims { return b.requestClaims },
			func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, b requestSeenBody) (any, error) {
				return svc.Seen(ctx, c, id, delivery.SeenInput{Digest: b.Digest, Via: b.Via, ReporterVersion: b.ReporterVersion})
			}), true
	case delivery.VerbAccept:
		return requestHolderRoute(func(b *requestAcceptBody) requestClaims { return b.requestClaims },
			func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, b requestAcceptBody) (any, error) {
				return svc.Accept(ctx, c, id, delivery.AcceptInput{Code: b.Code})
			}), true
	case delivery.VerbReport:
		return requestHolderRoute(func(b *requestReportBody) requestClaims { return b.requestClaims },
			func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, b requestReportBody) (any, error) {
				return svc.Report(ctx, c, id, b.ReportInput)
			}), true
	case delivery.VerbDecline:
		return requestHolderRoute(func(b *requestDeclineBody) requestClaims { return b.requestClaims },
			func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, b requestDeclineBody) (any, error) {
				return svc.Decline(ctx, c, id, b.DeclineInput)
			}), true
	case delivery.VerbReissue, delivery.VerbDrop, delivery.VerbAcceptUnproven:
		return requestBodyRoute(func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, in delivery.ReasonInput) (any, error) {
			switch verb {
			case delivery.VerbReissue:
				return svc.Reissue(ctx, c, id, in)
			case delivery.VerbDrop:
				return svc.Drop(ctx, c, id, in)
			}
			return svc.AcceptUnproven(ctx, c, id, in)
		}), true
	case delivery.VerbForce:
		return requestBodyRoute(func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, in delivery.ForceInput) (any, error) {
			return svc.Force(ctx, c, id, in)
		}), true
	}
	return requestRoute{}, false
}

func requestList(_ http.ResponseWriter, r *http.Request, svc delivery.RequestService, c delivery.Caller) (any, bool, error) {
	q := r.URL.Query()
	if id := q.Get("id"); id != "" {
		return requestAnswer(svc.Get(r.Context(), c, id))
	}
	views, err := svc.List(r.Context(), c, delivery.RequestFilter{
		State: delivery.RequestState(q.Get("state")), Kind: delivery.RequestKind(q.Get("kind")), Role: delivery.Role(q.Get("role")),
		ScopeKind: delivery.ScopeKind(q.Get("scope_kind")), ScopeID: q.Get("scope_id"), Holder: q.Get("holder"),
	})
	if views == nil {
		views = []delivery.RequestView{}
	}
	return map[string]any{"requests": views}, true, err
}

func requestMine(_ http.ResponseWriter, r *http.Request, svc delivery.RequestService, c delivery.Caller) (any, bool, error) {
	return requestAnswer(svc.Mine(r.Context(), c))
}

func requestRoles(_ http.ResponseWriter, r *http.Request, svc delivery.RequestService, c delivery.Caller) (any, bool, error) {
	roles, err := svc.Roles(r.Context(), c)
	if roles == nil {
		roles = []delivery.RoleBinding{}
	}
	return map[string]any{"roles": roles}, true, err
}

func requestCreated(v delivery.RequestView, created bool, err error) (any, error) {
	return map[string]any{"request": v, "created": created}, err
}

var requestCreateRoute = requestBodyRoute(func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, body requestCreateBody) (any, error) {
	in := body.CreateInput
	in.Content = body.Content.Content()
	key, err := delivery.SurfaceKey(in.Key)
	if err != nil {
		return nil, err
	}
	in.Key = key
	return requestCreated(svc.Create(ctx, c, in))
})

// requestRoutes are the routes whose path holds no ID, by the part after
// /api/requests.
var requestRoutes = map[string][]requestRoute{
	"":      {{http.MethodGet, requestList}, requestCreateRoute},
	"mine":  {{http.MethodGet, requestMine}},
	"roles": {{http.MethodGet, requestRoles}},
	"roles/give": {requestBodyRoute(func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, in delivery.RoleInput) (any, error) {
		return svc.GiveRole(ctx, c, in)
	})},
	"roles/release": {requestBodyRoute(func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, in delivery.RoleInput) (any, error) {
		return map[string]any{"ok": true}, svc.ReleaseRole(ctx, c, in)
	})},
	"from-backlog": {requestBodyRoute(func(ctx context.Context, svc delivery.RequestService, c delivery.Caller, body requestBacklogBody) (any, error) {
		in := body.BacklogInput
		in.Content = body.Content.Content()
		return requestCreated(svc.FromBacklog(ctx, c, in))
	})},
}

// handleRequests serves /api/requests and everything under it.
func (s *Server) handleRequests(w http.ResponseWriter, r *http.Request) {
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, delivery.RouteRequests), "/")
	routes, known := requestRoutes[sub]
	id, verb := "", sub
	if !known {
		var found bool
		if id, verb, found = strings.Cut(sub, "/"); found {
			var rt requestRoute
			if rt, known = requestIDRoute(id, verb); known {
				routes = []requestRoute{rt}
			}
		}
	}
	if !known {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var route *requestRoute
	for i := range routes {
		if routes[i].method == r.Method {
			route = &routes[i]
		}
	}
	if route == nil {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	required := federation.CapQueueWrite
	if r.Method == http.MethodGet {
		required = federation.CapQueueRead
	}
	if !s.fedCap(w, r, required) {
		return
	}
	svc := s.requestService()
	if svc == nil {
		http.Error(w, requestsUnavailable, http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodGet && sub == "" {
		if q := r.URL.Query().Get("id"); q != "" {
			id, verb = q, "get"
		}
	}
	c := s.requestCaller(r)
	reply, ok, err := route.call(w, r, svc, c)
	switch {
	case !ok:
	case err != nil:
		s.requestRefused(w, c, id, verb, err)
	default:
		writeJSONOK(w, reply)
	}
}

// ----- the older work queue ---------------------------------------------------

// queueRequest turns a queue completion or failure that names a request into
// the holder's report or decline.
func (s *Server) queueRequest(w http.ResponseWriter, r *http.Request, id, verb string, call func(delivery.RequestService, delivery.Caller) error) {
	svc := s.requestService()
	if svc == nil {
		http.Error(w, requestsUnavailable, http.StatusServiceUnavailable)
		return
	}
	c := s.requestCaller(r)
	if err := call(svc, c); err != nil {
		s.requestRefused(w, c, id, verb, err)
		return
	}
	writeJSONOK(w, map[string]interface{}{"ok": true})
}

// listQueue reads the queue and reports a queue that cannot be read, where the
// store can say so, so that it never reads empty.
func listQueue(store *session.QueueStore, role, state string) ([]*session.QueueItem, error) {
	if q, ok := any(store).(interface {
		ListErr(role, state string) ([]*session.QueueItem, error)
	}); ok {
		return q.ListErr(role, state)
	}
	return store.List(role, state), nil
}

// ----- other computers --------------------------------------------------------

// requestForOtherComputer reports a forwarded call that would make or change a
// tracked request on another computer. It may read the body, and puts it back.
func requestForOtherComputer(r *http.Request, remotePath string) bool {
	p := path.Clean("/" + remotePath)
	if p == delivery.RouteRequests || strings.HasPrefix(p, delivery.RouteRequests+"/") {
		return true
	}
	switch p {
	case "/api/mcp/call", "/api/queue/complete", "/api/queue/fail", "/api/test/message":
	default:
		return false
	}
	if r.Body == nil {
		return false
	}
	data, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(data), r.Body))
	if err != nil {
		return false
	}
	var body struct {
		Tool string `json:"tool"`
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	// The same reading the other computer makes: the first JSON value.
	if json.NewDecoder(bytes.NewReader(data)).Decode(&body) != nil {
		return false
	}
	switch p {
	case "/api/mcp/call":
		for _, name := range delivery.RequestToolNames {
			if body.Tool == name {
				return true
			}
		}
		return false
	case "/api/test/message":
		_, _, ok := delivery.RequestChatVerb(body.Text)
		return ok
	}
	return delivery.IsRequestID(body.ID)
}

// refuseCrossHostRequest writes the refusal for such a call: nothing is sent.
func refuseCrossHostRequest(w http.ResponseWriter, r *http.Request, remotePath string) bool {
	if !requestForOtherComputer(r, remotePath) {
		return false
	}
	http.Error(w, delivery.ErrCrossHost.Error(), delivery.HTTPStatus(delivery.ErrCrossHost))
	return true
}
