// v7.0.0 S4 — Generic Server-Sent Events infrastructure.
//
// Reusable across surfaces. Council uses it first (per-run live
// updates); future surfaces (run logs, automata progress, observer
// peer state) reuse the same SSE writer + per-topic subscriber
// registry.
//
// Per BL295 design ASK 6: SSE for monitoring (one-way, no upgrade
// dance). Operator-injection (writeable channel) reserved for v7.x.
//
// Event shape (text/event-stream):
//
//	event: <type>
//	data: <json-encoded payload>
//	id: <monotonic int (per-topic)>
//	\n
//
// Subscribers connect via standard EventSource on the consumer endpoint
// (e.g. GET /api/council/runs/{id}/events). The handler calls
// SSEHub.Subscribe(topic, w) which writes the headers + holds the
// connection open until ctx is cancelled or the publisher closes the
// topic.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

// SSEHub holds per-topic subscriber lists. Goroutine-safe.
type SSEHub struct {
	mu   sync.RWMutex
	subs map[string]map[*sseClient]struct{} // topic -> set of clients
	ids  sync.Map                            // topic -> *atomic.Int64
}

// NewSSEHub returns an empty hub.
func NewSSEHub() *SSEHub {
	return &SSEHub{subs: map[string]map[*sseClient]struct{}{}}
}

// sseClient is one open subscriber connection.
type sseClient struct {
	ch     chan sseEvent
	closed atomic.Bool
}

// sseEvent is one frame queued to deliver to a subscriber.
type sseEvent struct {
	Type    string
	Payload any
	ID      int64
}

// Publish delivers an event to every subscriber on `topic`. Closed/
// dropped subscribers are pruned. Non-blocking — slow subscribers
// drop the event rather than backpressuring the publisher.
func (h *SSEHub) Publish(topic, eventType string, payload any) {
	id := h.nextID(topic)
	h.mu.RLock()
	clients := h.subs[topic]
	if len(clients) == 0 {
		h.mu.RUnlock()
		return
	}
	// Snapshot so we don't hold the read lock while writing channels.
	snapshot := make([]*sseClient, 0, len(clients))
	for c := range clients {
		snapshot = append(snapshot, c)
	}
	h.mu.RUnlock()
	evt := sseEvent{Type: eventType, Payload: payload, ID: id}
	for _, c := range snapshot {
		if c.closed.Load() {
			continue
		}
		select {
		case c.ch <- evt:
		default:
			// Slow subscriber — drop this event for them. Better than
			// blocking the publisher; subscriber sees the gap via id
			// jump and can re-fetch via REST if it cares.
		}
	}
}

// CloseTopic ends the topic — every subscriber gets a synthetic
// "close" event, then their connections finish naturally. New
// Subscribe calls on the topic still work (a future publisher can
// reopen).
func (h *SSEHub) CloseTopic(topic string) {
	h.mu.Lock()
	clients := h.subs[topic]
	delete(h.subs, topic)
	h.mu.Unlock()
	for c := range clients {
		if !c.closed.Swap(true) {
			close(c.ch)
		}
	}
}

func (h *SSEHub) nextID(topic string) int64 {
	v, _ := h.ids.LoadOrStore(topic, &atomic.Int64{})
	a := v.(*atomic.Int64)
	return a.Add(1)
}

// Subscribe upgrades w to text/event-stream and blocks until the
// client disconnects, ctx cancels, or the topic is closed. Returns
// the number of events written.
func (h *SSEHub) Subscribe(topic string, w http.ResponseWriter, r *http.Request) (int, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return 0, fmt.Errorf("response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx hint: don't buffer

	c := &sseClient{ch: make(chan sseEvent, 64)}
	h.addClient(topic, c)
	defer h.removeClient(topic, c)

	// Send a hello event so clients know the connection is live before
	// the first real publish. id=0 reserved for hellos.
	fmt.Fprintf(w, "event: hello\ndata: {}\nid: 0\n\n")
	flusher.Flush()

	written := 0
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		case evt, ok := <-c.ch:
			if !ok {
				// Topic closed by publisher.
				fmt.Fprintf(w, "event: close\ndata: {}\nid: 0\n\n")
				flusher.Flush()
				return written, nil
			}
			payloadJSON, err := json.Marshal(evt.Payload)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\nid: %d\n\n", evt.Type, payloadJSON, evt.ID)
			flusher.Flush()
			written++
		}
	}
}

func (h *SSEHub) addClient(topic string, c *sseClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[topic] == nil {
		h.subs[topic] = map[*sseClient]struct{}{}
	}
	h.subs[topic][c] = struct{}{}
}

func (h *SSEHub) removeClient(topic string, c *sseClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if clients, ok := h.subs[topic]; ok {
		delete(clients, c)
		if len(clients) == 0 {
			delete(h.subs, topic)
		}
	}
	if !c.closed.Swap(true) {
		// Drain any pending events; do NOT close c.ch (publisher may
		// still be holding a reference and closing twice panics).
		go func() {
			for range c.ch {
			}
		}()
	}
}

// SubscriberCount reports how many open subscribers a topic currently
// has. Useful for /api/sse/topics introspection (future).
func (h *SSEHub) SubscriberCount(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[topic])
}
