// MCP resource subscriptions for lifecycle events.
//
// With mcp.resource_subscriptions on, an MCP client can call
// resources/subscribe on the lifecycle events resource and receive
// notifications/resources/updated whenever a matching Automata lifecycle
// event fires. The subscription is one more consumer of the existing
// lifecycle dispatcher (autonomous.Manager.Subscribe): it uses the same
// autonomous.LifecycleFilter, the same event kinds and the same delivery
// gate (autonomous.Manager.LifecycleEventDeliverable) as a persisted
// subscription. It is not a second event system: nothing is stored, no sink
// type is added and persisted subscriptions are untouched. A resource
// subscription lives and dies with its MCP session.
//
// The subscribed URI carries the filter as query parameters:
//
//	datawatch://events/lifecycle?kinds=task.failed,prd.completed&level=task
//
// Parameters (all optional, but kinds or level is required, as for a
// persisted subscription): kinds (comma separated event kinds), level (prd,
// task or automation), prd_id, task_id and automation_id.
//
// With the setting off none of this is installed: the server advertises
// resources without subscribe, registers no events resource, and refuses
// resources/subscribe as before.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	// lifecycleEventsResourceURI is the lifecycle events resource. A
	// subscription URI is this base plus an optional filter query.
	lifecycleEventsResourceURI = "datawatch://events/lifecycle"
	// lifecycleEventsResourceTemplate lets a client discover the filter
	// parameters (RFC 6570 query expansion).
	lifecycleEventsResourceTemplate = lifecycleEventsResourceURI + "{?kinds,level,prd_id,task_id,automation_id}"
	// resourceUpdatedMethod is the MCP notification sent for a matching event.
	resourceUpdatedMethod = "notifications/resources/updated"
	// maxResourceSubscriptionsPerSession bounds the subscriptions one MCP
	// session can hold, so a client cannot register dispatcher consumers
	// without limit.
	maxResourceSubscriptionsPerSession = 32
	// lifecycleEventsReadLimit is how many recent Automata events a read of
	// the resource returns.
	lifecycleEventsReadLimit = 50
)

// LifecycleBus is the part of the Automata manager a resource subscription
// needs: the lifecycle dispatcher and the delivery gate every subscription
// runs. *autonomous.Manager implements it.
type LifecycleBus interface {
	Subscribe(f autonomous.LifecycleFilter, fn func(autonomous.LifecycleEvent)) (cancel func())
	LifecycleEventDeliverable(f autonomous.LifecycleFilter, ev autonomous.LifecycleEvent) bool
}

// resourceSubscriptions tracks the live resource subscriptions of every MCP
// session. It is nil on a server whose setting is off.
type resourceSubscriptions struct {
	mu   sync.Mutex
	bus  LifecycleBus
	subs map[string]map[string]func() // session ID -> subscribed URI -> dispatcher cancel
	// notify sends the updated notification to one session. It is the MCP
	// server's SendNotificationToSpecificClient; tests replace it.
	notify func(sessionID, method string, params map[string]any) error
}

func newResourceSubscriptions() *resourceSubscriptions {
	return &resourceSubscriptions{subs: make(map[string]map[string]func())}
}

// SetLifecycleBus wires the Automata manager's dispatcher into resource
// subscriptions. It does nothing when mcp.resource_subscriptions is off.
func (s *Server) SetLifecycleBus(bus LifecycleBus) {
	if s.resourceSubs == nil {
		return
	}
	s.resourceSubs.mu.Lock()
	s.resourceSubs.bus = bus
	s.resourceSubs.mu.Unlock()
}

// ResourceSubscriptionsEnabled reports whether mcp.resource_subscriptions was
// on when the server started.
func (s *Server) ResourceSubscriptionsEnabled() bool { return s.resourceSubs != nil }

// ParseLifecycleResourceURI turns a lifecycle events resource URI into the
// filter it names and validates it with the rules a persisted subscription
// follows (a known kind, a valid level, kinds or level set).
func ParseLifecycleResourceURI(raw string) (autonomous.LifecycleFilter, error) {
	var f autonomous.LifecycleFilter
	base, query, _ := strings.Cut(raw, "?")
	if base != lifecycleEventsResourceURI {
		return f, fmt.Errorf("%q is not the lifecycle events resource (%s)", base, lifecycleEventsResourceURI)
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return f, fmt.Errorf("filter query: %w", err)
	}
	for name := range values {
		switch name {
		case "kinds", "level", "prd_id", "task_id", "automation_id":
		default:
			return f, fmt.Errorf("unknown filter parameter %q (want kinds, level, prd_id, task_id or automation_id)", name)
		}
	}
	if kinds := strings.TrimSpace(values.Get("kinds")); kinds != "" {
		for _, k := range strings.Split(kinds, ",") {
			f.Kinds = append(f.Kinds, strings.TrimSpace(k))
		}
	}
	f.Level = autonomous.LifecycleLevel(strings.TrimSpace(values.Get("level")))
	f.PRDID = strings.TrimSpace(values.Get("prd_id"))
	f.TaskID = strings.TrimSpace(values.Get("task_id"))
	f.AutomationID = strings.TrimSpace(values.Get("automation_id"))
	// The persisted-subscription validator owns the filter rules; the sink is
	// a placeholder it accepts, so the two paths cannot drift apart.
	probe := autonomous.Subscription{Filter: f, Sink: autonomous.SubscriptionSink{Type: autonomous.SinkMCPQueue, Target: "resource"}}
	if err := autonomous.ValidateSubscription(probe, time.Now()); err != nil {
		return autonomous.LifecycleFilter{}, err
	}
	return f, nil
}

// hooks returns the mcp-go hooks that service resources/subscribe and
// resources/unsubscribe and release a session's subscriptions when it goes.
func (r *resourceSubscriptions) hooks() *server.Hooks {
	h := &server.Hooks{}
	h.AddBeforeSubscribe(r.beforeSubscribe)
	h.AddAfterSubscribe(r.afterSubscribe)
	h.AddAfterUnsubscribe(r.afterUnsubscribe)
	h.AddOnUnregisterSession(func(_ context.Context, sess server.ClientSession) {
		if sess != nil {
			r.releaseSession(sess.SessionID())
		}
	})
	return h
}

// beforeSubscribe refuses a subscription it cannot honor. A hook cannot
// return an error, so it blanks the URI, which the server answers with an
// invalid-params error and without calling afterSubscribe. The reason is
// logged, because the protocol error only says the uri is unusable.
func (r *resourceSubscriptions) beforeSubscribe(ctx context.Context, _ any, req *mcpsdk.SubscribeRequest) {
	if err := r.check(ctx, req.Params.URI); err != nil {
		log.Printf("[mcp] resources/subscribe %q refused: %v", req.Params.URI, err)
		req.Params.URI = ""
	}
}

// check validates a subscribe request: a lifecycle events URI with a valid
// filter, a live bus and room in the session's quota.
func (r *resourceSubscriptions) check(ctx context.Context, uri string) error {
	if _, err := ParseLifecycleResourceURI(uri); err != nil {
		return err
	}
	sess := server.ClientSessionFromContext(ctx)
	if sess == nil {
		return fmt.Errorf("no MCP session")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bus == nil {
		return fmt.Errorf("lifecycle events are unavailable (Automata are not running)")
	}
	held := r.subs[sess.SessionID()]
	if _, dup := held[uri]; !dup && len(held) >= maxResourceSubscriptionsPerSession {
		return fmt.Errorf("session already holds %d resource subscriptions", maxResourceSubscriptionsPerSession)
	}
	return nil
}

// afterSubscribe registers the subscription on the lifecycle dispatcher.
// Subscribing a URI twice keeps the first registration.
func (r *resourceSubscriptions) afterSubscribe(ctx context.Context, _ any, req *mcpsdk.SubscribeRequest, _ *mcpsdk.EmptyResult) {
	sess := server.ClientSessionFromContext(ctx)
	if sess == nil {
		return
	}
	r.add(sess.SessionID(), req.Params.URI)
}

// add registers one (session, URI) subscription on the dispatcher.
func (r *resourceSubscriptions) add(sessionID, uri string) {
	f, err := ParseLifecycleResourceURI(uri)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bus == nil {
		return
	}
	if _, dup := r.subs[sessionID][uri]; dup {
		return
	}
	bus := r.bus
	cancel := bus.Subscribe(f, func(ev autonomous.LifecycleEvent) {
		r.deliver(bus, sessionID, uri, f, ev)
	})
	if r.subs[sessionID] == nil {
		r.subs[sessionID] = make(map[string]func())
	}
	r.subs[sessionID][uri] = cancel
}

// deliver runs on the dispatcher's per-subscriber goroutine. It applies the
// same delivery gate as a persisted subscription and sends the update. A
// session that no longer exists loses its subscriptions.
func (r *resourceSubscriptions) deliver(bus LifecycleBus, sessionID, uri string, f autonomous.LifecycleFilter, ev autonomous.LifecycleEvent) {
	if !bus.LifecycleEventDeliverable(f, ev) {
		return
	}
	params := map[string]any{
		"uri": uri,
		"_meta": map[string]any{
			"event_id": ev.ID,
			"kind":     ev.Kind,
			"prd_id":   ev.PRDID,
			"task_id":  ev.TaskID,
		},
	}
	r.mu.Lock()
	notify := r.notify
	r.mu.Unlock()
	if notify == nil {
		return
	}
	if err := notify(sessionID, resourceUpdatedMethod, params); err != nil && err == server.ErrSessionNotFound {
		go r.releaseSession(sessionID)
	}
}

// afterUnsubscribe drops one subscription. Unsubscribing a URI that was never
// subscribed is a no-op, as the protocol expects.
func (r *resourceSubscriptions) afterUnsubscribe(ctx context.Context, _ any, req *mcpsdk.UnsubscribeRequest, _ *mcpsdk.EmptyResult) {
	sess := server.ClientSessionFromContext(ctx)
	if sess == nil {
		return
	}
	r.remove(sess.SessionID(), req.Params.URI)
}

func (r *resourceSubscriptions) remove(sessionID, uri string) {
	r.mu.Lock()
	cancel := r.subs[sessionID][uri]
	delete(r.subs[sessionID], uri)
	if len(r.subs[sessionID]) == 0 {
		delete(r.subs, sessionID)
	}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// releaseSession drops every subscription of one MCP session.
func (r *resourceSubscriptions) releaseSession(sessionID string) {
	r.mu.Lock()
	held := r.subs[sessionID]
	delete(r.subs, sessionID)
	r.mu.Unlock()
	for _, cancel := range held {
		cancel()
	}
}

// count returns how many subscriptions one session holds (tests).
func (r *resourceSubscriptions) count(sessionID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs[sessionID])
}

// registerLifecycleEventsResource adds the lifecycle events resource and its
// filter template. It runs only when the setting is on.
func (s *Server) registerLifecycleEventsResource() {
	const name = "Automata Lifecycle Events"
	const desc = "Recent Automata events. Subscribe with resources/subscribe on a URI with a filter " +
		"(kinds, level, prd_id, task_id, automation_id as query parameters; kinds or level is required) " +
		"to receive notifications/resources/updated when a matching lifecycle event fires. " +
		"The filter and event kinds are the same as a persisted subscription's; list the kinds with autonomous_event_kinds."
	res := mcpsdk.NewResource(lifecycleEventsResourceURI, name,
		mcpsdk.WithResourceDescription(desc),
		mcpsdk.WithMIMEType("application/json"),
	)
	s.srv.AddResource(res, s.handleLifecycleEventsResource)
	tmpl := mcpsdk.NewResourceTemplate(lifecycleEventsResourceTemplate, name+" (filtered)",
		mcpsdk.WithTemplateDescription(desc),
		mcpsdk.WithTemplateMIMEType("application/json"),
	)
	s.srv.AddResourceTemplate(tmpl, s.handleLifecycleEventsResource)
	globalResourceRegistry.resources = append(globalResourceRegistry.resources, ResourceDescriptor{
		URI: lifecycleEventsResourceURI, Name: name, Description: desc, MIMEType: "application/json",
	})
	globalResourceRegistry.templates = append(globalResourceRegistry.templates, ResourceTemplateDescriptor{
		URITemplate: lifecycleEventsResourceTemplate, Name: name + " (filtered)", Description: desc, MIMEType: "application/json",
	})
}

// handleLifecycleEventsResource reads the resource: the filter the URI names
// and the most recent Automata events. A URI that names no filter reads the
// events with no filter; an invalid filter is an error, not an empty read.
func (s *Server) handleLifecycleEventsResource(_ context.Context, req mcpsdk.ReadResourceRequest) ([]mcpsdk.ResourceContents, error) {
	uri := req.Params.URI
	filter := map[string]any{}
	if strings.Contains(uri, "?") {
		f, err := ParseLifecycleResourceURI(uri)
		if err != nil {
			return nil, err
		}
		if len(f.Kinds) > 0 {
			filter["kinds"] = f.Kinds
		}
		if f.Level != "" {
			filter["level"] = f.Level
		}
		for k, v := range map[string]string{"prd_id": f.PRDID, "task_id": f.TaskID, "automation_id": f.AutomationID} {
			if v != "" {
				filter[k] = v
			}
		}
	}
	out := map[string]any{"uri": uri, "filter": filter}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(lifecycleEventsReadLimit))
	raw, err := s.proxyGet(autonomousEventLogPath, q)
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["events"] = json.RawMessage(raw)
	}
	body, _ := json.Marshal(out)
	return []mcpsdk.ResourceContents{
		mcpsdk.TextResourceContents{URI: uri, MIMEType: "application/json", Text: string(body)},
	}, nil
}
