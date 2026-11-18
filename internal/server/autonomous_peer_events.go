// Control surface of the peer event subscriber: start, stop and read the
// goroutines that read a federation peer's lifecycle events into this
// daemon's local dispatch (autonomous.PeerEventSubscriber).
//
//	GET  /api/autonomous/peer-events                         what is running
//	POST /api/autonomous/peer-events/start {"peer","queue","since"}
//	POST /api/autonomous/peer-events/stop  {"peer"}
//
// The subscriber exists only while autonomous.notify.forwarding.peer_events is
// on; while it is off the GET answers enabled=false with nothing running and
// start answers 409. Reading needs autonomous:read, start and stop need
// autonomous:write. No answer carries a token.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// PeerEventControl is what the routes need from the subscriber wiring.
type PeerEventControl interface {
	// Enabled reports whether autonomous.notify.forwarding.peer_events is on.
	Enabled() bool
	// Start begins reading src.Peer. The read lives as long as the daemon, not
	// as long as the request that started it.
	Start(src autonomous.PeerEventSource) error
	Stop(peer string) bool
	Running() []string
}

// SetPeerEvents wires the peer event subscriber control. Nil leaves the
// routes answering 503.
func (s *Server) SetPeerEvents(c PeerEventControl) { s.peerEvents = c }

// peerEventsView is the GET body.
type peerEventsView struct {
	Enabled bool     `json:"enabled"`
	Running []string `json:"running"`
}

type peerEventsRequest struct {
	Peer  string `json:"peer"`
	Queue string `json:"queue"`
	Since uint64 `json:"since"`
}

// handleAutonomousPeerEvents serves the three routes above.
func (s *Server) handleAutonomousPeerEvents(w http.ResponseWriter, r *http.Request) {
	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/autonomous/peer-events"), "/")
	switch {
	case action == "" && r.Method == http.MethodGet:
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
	case (action == "start" || action == "stop") && r.Method == http.MethodPost:
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
	default:
		if action == "" {
			w.Header().Set("Allow", http.MethodGet)
		} else {
			w.Header().Set("Allow", http.MethodPost)
		}
		http.Error(w, "method not allowed or unknown peer-events route", http.StatusMethodNotAllowed)
		return
	}
	if s.peerEvents == nil {
		http.Error(w, "peer event subscriber is not wired", http.StatusServiceUnavailable)
		return
	}
	if action == "" {
		running := s.peerEvents.Running()
		sort.Strings(running)
		if running == nil {
			running = []string{}
		}
		writeJSONOK(w, peerEventsView{Enabled: s.peerEvents.Enabled(), Running: running})
		return
	}
	var req peerEventsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "the body must be JSON: {\"peer\":..., \"queue\":...}", http.StatusBadRequest)
		return
	}
	req.Peer = strings.TrimSpace(req.Peer)
	if req.Peer == "" {
		http.Error(w, "peer is required", http.StatusBadRequest)
		return
	}
	if action == "stop" {
		stopped := s.peerEvents.Stop(req.Peer)
		writeJSONOK(w, map[string]any{"peer": req.Peer, "stopped": stopped})
		return
	}
	if !s.peerEvents.Enabled() {
		http.Error(w, "peer event subscription is off: set autonomous.notify.forwarding.peer_events to true", http.StatusConflict)
		return
	}
	err := s.peerEvents.Start(autonomous.PeerEventSource{Peer: req.Peer, Queue: req.Queue, SinceCursor: req.Since})
	switch {
	case err == nil:
		writeJSONOK(w, map[string]any{"peer": req.Peer, "queue": strings.TrimSpace(req.Queue), "started": true})
	case errors.Is(err, autonomous.ErrPeerEventRunning):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, autonomous.ErrPeerEventRefused):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// peerEventControl is the production PeerEventControl: it owns the subscriber
// and the daemon-lifetime context the reads run under.
type peerEventControl struct {
	ctx     context.Context
	sub     *autonomous.PeerEventSubscriber
	enabled func() bool
}

// NewPeerEventControl wraps sub. ctx is the daemon's context: every read ends
// when it does. enabled reads autonomous.notify.forwarding.peer_events live.
func NewPeerEventControl(ctx context.Context, sub *autonomous.PeerEventSubscriber, enabled func() bool) PeerEventControl {
	return &peerEventControl{ctx: ctx, sub: sub, enabled: enabled}
}

func (c *peerEventControl) Enabled() bool { return c.enabled != nil && c.enabled() }
func (c *peerEventControl) Start(src autonomous.PeerEventSource) error {
	return c.sub.Start(c.ctx, src)
}
func (c *peerEventControl) Stop(peer string) bool { return c.sub.Stop(peer) }
func (c *peerEventControl) Running() []string     { return c.sub.Running() }
