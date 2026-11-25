// MCP tools reach the daemon over its loopback REST API. The routes there that
// type into a session are for the operator's own surfaces: a tool types only
// through SendToolInput, which refuses while a prompt waits for the operator.
// Tool arguments are joined into loopback paths, so every request is checked
// as it leaves rather than trusted to name the route its tool meant.

package mcp

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
)

var errToolTypingRoute = errors.New("refused: MCP tools may not call the daemon routes that type into a session; nothing was typed (use send_input)")

// typingRoute reports whether u is a loopback route that ends in text typed
// into a session, here or on a peer, and the session it names if the path
// does. The path is checked as written and cleaned: the server redirects an
// unclean path to the cleaned one with the method and body unchanged.
func typingRoute(u *url.URL) (sessionID string, ok bool) {
	for _, p := range []string{u.Path, path.Clean("/" + u.Path)} {
		// Session send, the chat command simulator ("send" included) and
		// every route forwarded to a peer.
		switch {
		case p == "/api/sessions/send", p == "/api/test/message",
			p == "/api/proxy", strings.HasPrefix(p, "/api/proxy/"):
			return "", true
		}
		if rest, found := strings.CutPrefix(p, "/api/sessions/"); found {
			if id, isInput := strings.CutSuffix(strings.TrimRight(rest, "/"), "/input"); isInput {
				return id, true
			}
		}
	}
	return "", false
}

// loopbackDo sends one request from an MCP tool to the daemon. Only reads may
// name a route that types into a session.
func (s *Server) loopbackDo(c *http.Client, req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		if id, ok := typingRoute(req.URL); ok {
			if s.manager != nil {
				s.manager.AuditRefusedToolRoute(id, errToolTypingRoute.Error())
			}
			return nil, errToolTypingRoute
		}
	}
	return c.Do(req)
}
