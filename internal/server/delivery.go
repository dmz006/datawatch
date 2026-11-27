// Delivery check REST surface.

package server

import "github.com/dmz006/datawatch/internal/delivery"

// SetDelivery wires the delivery check exposed under /api/delivery.
func (s *HTTPServer) SetDelivery(d delivery.Service) {}
