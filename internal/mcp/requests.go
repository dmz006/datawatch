// Request tools (delivery job 4). Owner: part B. STUB: no tool is registered
// yet. The tools are the delivery.Tool* names. They call the service in this
// process: a loopback REST call would carry the tool server's own token and
// lose who is calling. Only federation.CallerSessionIDFromContext, which comes
// from the session's own credential, makes a caller a holder.

package mcp

import "github.com/dmz006/datawatch/internal/delivery"

// SetRequests wires the request service.
func (s *Server) SetRequests(r delivery.RequestService) { s.requests = r }
