// v7.0.0-alpha.35 #38 — first-party UnifiedPush + ntfy-compatible
// SSE provider. Tier-1 push delivery so datawatch-app stops needing
// FCM / Google / third-party push infrastructure.
//
// Endpoints:
//
//	GET  /api/push/<topic>            SSE stream (ntfy-compat)
//	POST /api/push/<topic>            publish event
//	POST /api/push/register           mobile-app endpoint registration
//	GET  /.well-known/unifiedpush     discovery
//
// Per-event auto-emit (waiting_input) wired through PublishToTopic from
// the daemon's existing alert/state pipeline. Council decisions / session
// errors / algorithm phase completions land in alpha.35a once topic
// taxonomy is finalized.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
)

// PushEvent — one event broadcast on a topic. Exported so in-process
// callers (main.go alert pipeline, future council/algorithm emit) can
// publish without importing internal types.
type PushEvent struct {
	ID       string         `json:"id"`
	Time     int64          `json:"time"`     // unix seconds (ntfy-compat)
	Event    string         `json:"event"`    // "message" | "open" | "keepalive"
	Topic    string         `json:"topic"`
	Title    string         `json:"title,omitempty"`
	Message  string         `json:"message"`
	Priority int            `json:"priority,omitempty"` // ntfy-compat: 1-5
	Tags     []string       `json:"tags,omitempty"`
	Click    string         `json:"click,omitempty"`
	Extras   map[string]any `json:"extras,omitempty"`
}

// pushClient — one open SSE listener.
type pushClient struct {
	id    string
	topic string
	ch    chan PushEvent
}

// pushKeys — VAPID / P256DH web push keys from the mobile client (BL330).
type pushKeys struct {
	P256DH string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// pushRegistration — UnifiedPush mobile-app endpoint registration.
type pushRegistration struct {
	ID           string     `json:"id"`
	Endpoint     string     `json:"endpoint"`
	ClientID     string     `json:"client_id,omitempty"`
	Token        string     `json:"token,omitempty"` // bearer for the endpoint POST
	Keys         *pushKeys  `json:"keys,omitempty"`  // BL330: P256DH/Auth for web-push encryption
	RegisteredAt time.Time  `json:"registered_at"`
}

type pushHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[string]*pushClient // topic → clientID → client
	registered  []pushRegistration                 // mobile push endpoints
}

var globalPushHub = &pushHub{subscribers: map[string]map[string]*pushClient{}}

// PublishToTopic — public in-process emit. Daemon code calls this when
// an event of interest happens (waiting_input, council decision, etc.).
// Fans out to live SSE subscribers + registered mobile push endpoints.
func PublishToTopic(topic string, ev PushEvent) {
	if topic == "" {
		return
	}
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("dw-%d", time.Now().UnixNano())
	}
	if ev.Time == 0 {
		ev.Time = time.Now().Unix()
	}
	if ev.Event == "" {
		ev.Event = "message"
	}
	ev.Topic = topic

	globalPushHub.mu.RLock()
	subs := globalPushHub.subscribers[topic]
	for _, c := range subs {
		// Non-blocking send so a slow client can't stall the publish.
		select {
		case c.ch <- ev:
		default:
		}
	}
	regs := append([]pushRegistration(nil), globalPushHub.registered...)
	globalPushHub.mu.RUnlock()

	// Fan out to registered mobile endpoints in a goroutine.
	for _, r := range regs {
		go publishToEndpoint(r, ev)
	}
}

func publishToEndpoint(r pushRegistration, ev PushEvent) {
	body, _ := json.Marshal(ev)
	req, err := http.NewRequest(http.MethodPost, r.Endpoint, strings.NewReader(string(body)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if r.Token != "" {
		req.Header.Set("Authorization", "Bearer "+r.Token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// handlePushTopic — GET (SSE subscribe) or POST (publish) to /api/push/<topic>
func (s *Server) handlePushTopic(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapCommRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapCommWrite) {
			return
		}
	}
	topic := strings.TrimPrefix(r.URL.Path, "/api/push/")
	if topic == "" || strings.Contains(topic, "/") {
		http.Error(w, "topic required (no slashes)", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handlePushSubscribe(w, r, topic)
	case http.MethodPost:
		s.handlePushPublish(w, r, topic)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request, topic string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	clientID := fmt.Sprintf("c-%d", time.Now().UnixNano())
	client := &pushClient{id: clientID, topic: topic, ch: make(chan PushEvent, 16)}

	globalPushHub.mu.Lock()
	if globalPushHub.subscribers[topic] == nil {
		globalPushHub.subscribers[topic] = map[string]*pushClient{}
	}
	globalPushHub.subscribers[topic][clientID] = client
	globalPushHub.mu.Unlock()

	defer func() {
		globalPushHub.mu.Lock()
		delete(globalPushHub.subscribers[topic], clientID)
		globalPushHub.mu.Unlock()
		close(client.ch)
	}()

	// Send an initial "open" event so clients confirm the stream is live.
	openEv := PushEvent{ID: "open-" + clientID, Time: time.Now().Unix(), Event: "open", Topic: topic, Message: "stream open"}
	if data, err := json.Marshal(openEv); err == nil {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Keepalive every 25s to defeat proxy idle timeouts.
	keep := time.NewTicker(25 * time.Second)
	defer keep.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-client.ch:
			if data, err := json.Marshal(ev); err == nil {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		case <-keep.C:
			ka := PushEvent{ID: fmt.Sprintf("ka-%d", time.Now().UnixNano()), Time: time.Now().Unix(), Event: "keepalive", Topic: topic}
			if data, err := json.Marshal(ka); err == nil {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handlePushPublish(w http.ResponseWriter, r *http.Request, topic string) {
	var body struct {
		Title    string         `json:"title,omitempty"`
		Message  string         `json:"message"`
		Priority int            `json:"priority,omitempty"`
		Tags     []string       `json:"tags,omitempty"`
		Click    string         `json:"click,omitempty"`
		Extras   map[string]any `json:"extras,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Message == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}
	ev := PushEvent{
		Title:    body.Title,
		Message:  body.Message,
		Priority: body.Priority,
		Tags:     body.Tags,
		Click:    body.Click,
		Extras:   body.Extras,
	}
	PublishToTopic(topic, ev)
	writeJSONOK(w, map[string]any{"ok": true, "topic": topic})
}

// handlePushRegister — POST /api/push/register (BL330)
// Body: {"endpoint":"...", "keys":{"p256dh":"...","auth":"..."}}
// Endpoint alone is sufficient for registration; keys are optional for plain-HTTP callbacks.
// Returns {"ok":true, "id":"<registration_id>"}
//
// GET /api/push/register — returns list of all registrations.
func (s *Server) handlePushRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapCommRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapCommWrite) {
			return
		}
	}
	switch r.Method {
	case http.MethodGet:
		// List all registrations (admin endpoint).
		globalPushHub.mu.RLock()
		regs := append([]pushRegistration(nil), globalPushHub.registered...)
		globalPushHub.mu.RUnlock()
		writeJSONOK(w, map[string]any{"ok": true, "registrations": regs})
	case http.MethodPost:
		var body pushRegistration
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Endpoint == "" {
			http.Error(w, "endpoint required", http.StatusBadRequest)
			return
		}
		body.RegisteredAt = time.Now().UTC()
		globalPushHub.mu.Lock()
		// Idempotent: replace existing registration matching client_id (if provided) or endpoint.
		updated := false
		for i := range globalPushHub.registered {
			existing := &globalPushHub.registered[i]
			if (body.ClientID != "" && existing.ClientID == body.ClientID) ||
				(body.ClientID == "" && existing.Endpoint == body.Endpoint) {
				// Preserve the existing ID so the caller's reference stays valid.
				if existing.ID != "" {
					body.ID = existing.ID
				}
				globalPushHub.registered[i] = body
				updated = true
				break
			}
		}
		if !updated {
			if body.ID == "" {
				body.ID = fmt.Sprintf("reg-%d", time.Now().UnixNano())
			}
			globalPushHub.registered = append(globalPushHub.registered, body)
		}
		regID := body.ID
		globalPushHub.mu.Unlock()
		writeJSONOK(w, map[string]any{"ok": true, "id": regID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePushUnregister — DELETE /api/push/unregister (BL330)
// Body: {"endpoint":"..."} or {"id":"<registration_id>"}
func (s *Server) handlePushUnregister(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommWrite) {
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		ID       string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	globalPushHub.mu.Lock()
	out := globalPushHub.registered[:0]
	removed := 0
	for _, reg := range globalPushHub.registered {
		if (body.ID != "" && reg.ID == body.ID) || (body.Endpoint != "" && reg.Endpoint == body.Endpoint) {
			removed++
			continue
		}
		out = append(out, reg)
	}
	globalPushHub.registered = out
	globalPushHub.mu.Unlock()
	writeJSONOK(w, map[string]any{"ok": true, "removed": removed})
}

// handlePushNotify — POST /api/push/notify (BL330)
// Trigger push delivery to a specific registration or all registrations.
func (s *Server) handlePushNotify(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		RegistrationID string         `json:"registration_id"`
		Payload        map[string]any `json:"payload"`
		Title          string         `json:"title,omitempty"`
		Message        string         `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	ev := PushEvent{
		Title:   body.Title,
		Message: body.Message,
		Extras:  body.Payload,
	}
	globalPushHub.mu.RLock()
	regs := append([]pushRegistration(nil), globalPushHub.registered...)
	globalPushHub.mu.RUnlock()
	sent := 0
	for _, reg := range regs {
		if body.RegistrationID == "" || reg.ID == body.RegistrationID {
			go publishToEndpoint(reg, ev)
			sent++
		}
	}
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "sent": sent}) //nolint:errcheck
}

// handleUnifiedPushDiscovery — GET /.well-known/unifiedpush
func (s *Server) handleUnifiedPushDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// UnifiedPush discovery doc (BL330 — mobile-expected format).
	// Per the spec: https://unifiedpush.org/spec/discovery/
	writeJSONOK(w, map[string]any{
		"version": 1,
		"unifiedpush": map[string]any{
			"gateway": "/api/push/notify",
		},
	})
}
