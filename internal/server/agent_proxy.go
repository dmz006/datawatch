// F10 sprint 3 (S3.5) — reverse proxy for agent workers.
//
// Routes /api/proxy/agent/{id}/<rest> to the worker container's
// HTTP API at http://<Agent.ContainerAddr>/<rest>. Used by:
//
//   • Operators driving a worker's API directly from the parent UI
//   • Sprint 3.6 session-on-worker binding — session API calls get
//     transparently forwarded when session.AgentID != ""
//   • Sprint 4 K8s where workers have no Ingress and the parent is
//     the only path in
//
// Why a sub-namespace under /api/proxy/agent/?
// F16's existing /api/proxy/{serverName}/... aggregates remote
// datawatch peers. Agent worker IDs are 32-char hex; a poorly-named
// remote server could collide. The "agent/" prefix removes ambiguity
// and lets each handler keep a focused lookup path.
//
// Auth: Sprint 3 forwards the parent's session token unmodified.
// Sprint 5 (PQC) introduces per-worker identity and the proxy
// rewrites the auth header to the worker's identity.

package server

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// handleAgentProxy forwards an HTTP request to the worker addressed by
// agentID at the given remainingPath ("/" + everything after the ID).
// Caller (handleProxy dispatcher) has already stripped the
// "/api/proxy/agent/<id>" prefix.
func (s *Server) handleAgentProxy(w http.ResponseWriter, r *http.Request, agentID, remainingPath string) {
	if s.agentMgr == nil {
		http.Error(w, "agent manager not available", http.StatusServiceUnavailable)
		return
	}
	if agentID == "" {
		http.Error(w, "missing agent id", http.StatusBadRequest)
		return
	}
	a := s.agentMgr.Get(agentID)
	if a == nil {
		http.Error(w, fmt.Sprintf("agent %q not found", agentID), http.StatusNotFound)
		return
	}
	if a.ContainerAddr == "" {
		// Worker is starting / lost its IP / on host network.
		// 503 lets clients retry rather than 404 which they'd
		// interpret as "agent gone forever".
		http.Error(w, "agent has no reachable address yet", http.StatusServiceUnavailable)
		return
	}

	// WebSocket upgrade requests get the WS relay path.
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		s.handleAgentProxyWS(w, r, a.ContainerAddr, remainingPath)
		return
	}

	targetURL := "http://" + a.ContainerAddr + remainingPath
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for k, vals := range r.Header {
		for _, v := range vals {
			proxyReq.Header.Add(k, v)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("worker unreachable: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// handleAgentProxyWS bidirectionally relays a WebSocket connection
// between the client and the worker at containerAddr. Mirrors the
// F16 server-proxy WS handler but talks to a docker-bridge IP.
func (s *Server) handleAgentProxyWS(w http.ResponseWriter, r *http.Request, containerAddr, remainingPath string) {
	wsURL := "ws://" + containerAddr + remainingPath
	if r.URL.RawQuery != "" {
		wsURL += "?" + r.URL.RawQuery
	}

	header := http.Header{}
	for k, vals := range r.Header {
		// Skip headers gorilla/websocket sets itself.
		switch strings.ToLower(k) {
		case "upgrade", "connection", "sec-websocket-key",
			"sec-websocket-version", "sec-websocket-extensions",
			"sec-websocket-protocol":
			continue
		}
		for _, v := range vals {
			header.Add(k, v)
		}
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	remoteConn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		http.Error(w, fmt.Sprintf("worker WS dial: %v", err), http.StatusBadGateway)
		return
	}

	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		remoteConn.Close()
		return
	}

	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			clientConn.Close()
			remoteConn.Close()
		})
	}

	go func() {
		defer closeBoth()
		for {
			t, data, err := remoteConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(t, data); err != nil {
				return
			}
		}
	}()

	defer closeBoth()
	for {
		t, data, err := clientConn.ReadMessage()
		if err != nil {
			return
		}
		if err := remoteConn.WriteMessage(t, data); err != nil {
			return
		}
	}
}
