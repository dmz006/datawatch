// Requests over REST (delivery job 4). Owner: part B. STUB: no route is
// registered yet. The routes are the delivery.Route* names; the caller is built
// from the credential only, never from the body.

package server

import "github.com/dmz006/datawatch/internal/delivery"

// SetRequests wires the request service.
func (s *HTTPServer) SetRequests(r delivery.RequestService) {
	if s != nil && s.api != nil {
		s.api.requests = r
	}
}
