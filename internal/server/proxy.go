package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/gorilla/websocket"
)

// handleProxyWS relays a WebSocket connection between the client and a remote
// datawatch server. Route: /api/proxy/{serverName}/ws
func (s *Server) handleProxyWS(w http.ResponseWriter, r *http.Request) {
	// Extract server name: /api/proxy/<name>/ws
	path := strings.TrimPrefix(r.URL.Path, "/api/proxy/")
	idx := strings.Index(path, "/")
	if idx < 0 {
		http.Error(w, "missing server name", http.StatusBadRequest)
		return
	}
	serverName := path[:idx]

	remote := s.findServer(serverName)
	if remote == nil {
		http.Error(w, fmt.Sprintf("server %q not found or disabled", serverName), http.StatusNotFound)
		return
	}

	// Build remote WS URL
	remoteURL, err := url.Parse(remote.URL)
	if err != nil {
		http.Error(w, "invalid server URL", http.StatusBadRequest)
		return
	}
	scheme := "ws"
	if remoteURL.Scheme == "https" {
		scheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/ws", scheme, remoteURL.Host)

	// Connect to remote WS
	header := http.Header{}
	if remote.Token != "" {
		header.Set("Authorization", "Bearer "+remote.Token)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	remoteConn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot connect to remote WS: %v", err), http.StatusBadGateway)
		return
	}

	// Upgrade client connection
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		remoteConn.Close()
		return
	}

	// Bidirectional relay
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			clientConn.Close()
			remoteConn.Close()
		})
	}

	// Remote → Client
	go func() {
		defer closeBoth()
		for {
			msgType, data, err := remoteConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(msgType, data); err != nil {
				return
			}
		}
	}()

	// Client → Remote
	defer closeBoth()
	for {
		msgType, data, err := clientConn.ReadMessage()
		if err != nil {
			return
		}
		if err := remoteConn.WriteMessage(msgType, data); err != nil {
			return
		}
	}
}

// findServer looks up a remote server by name from config.
func (s *Server) findServer(name string) *config.RemoteServerConfig {
	for i := range s.cfg.Servers {
		if s.cfg.Servers[i].Name == name && s.cfg.Servers[i].Enabled {
			return &s.cfg.Servers[i]
		}
	}
	return nil
}

// handleAggregatedSessions returns sessions from all remote servers + local,
// tagged with their source server name.
func (s *Server) handleAggregatedSessions(w http.ResponseWriter, r *http.Request) {
	type taggedSession struct {
		*session.Session
		Server string `json:"server"`
	}

	// Local sessions
	local := s.manager.ListSessions()
	results := make([]taggedSession, 0, len(local))
	for _, sess := range local {
		results = append(results, taggedSession{Session: sess, Server: "local"})
	}

	// Remote sessions — fetch in parallel with timeout
	type fetchResult struct {
		server   string
		sessions []taggedSession
	}
	ch := make(chan fetchResult, len(s.cfg.Servers))

	for _, sv := range s.cfg.Servers {
		if !sv.Enabled {
			continue
		}
		go func(sv config.RemoteServerConfig) {
			client := &http.Client{Timeout: 5 * time.Second}
			apiURL := strings.TrimRight(sv.URL, "/") + "/api/sessions"
			req, err := http.NewRequest(http.MethodGet, apiURL, nil)
			if err != nil {
				ch <- fetchResult{server: sv.Name}
				return
			}
			if sv.Token != "" {
				req.Header.Set("Authorization", "Bearer "+sv.Token)
			}
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("[proxy] %s: fetch sessions failed: %v", sv.Name, err)
				ch <- fetchResult{server: sv.Name}
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var sessions []*session.Session
			if err := json.Unmarshal(body, &sessions); err != nil {
				log.Printf("[proxy] %s: parse sessions: %v", sv.Name, err)
				ch <- fetchResult{server: sv.Name}
				return
			}
			tagged := make([]taggedSession, 0, len(sessions))
			for _, sess := range sessions {
				tagged = append(tagged, taggedSession{Session: sess, Server: sv.Name})
			}
			ch <- fetchResult{server: sv.Name, sessions: tagged}
		}(sv)
	}

	// Collect results
	enabledCount := 0
	for _, sv := range s.cfg.Servers {
		if sv.Enabled {
			enabledCount++
		}
	}
	for i := 0; i < enabledCount; i++ {
		result := <-ch
		results = append(results, result.sessions...)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) //nolint:errcheck
}
