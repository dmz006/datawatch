// Headroom wiring — the daemon's one verifier headroom-measurement store.
//
// newHeadroomWiring is the single production constructor for the store that
// the verifier cap gates read. It creates one compute.HeadroomStore and the
// admission gate over it; attach hands that same instance to the REST server,
// whose verifier_slots write gate reads it. The capacity wiring receives the
// gate, so the admission gate and the write gate read one store, one floor
// (autonomous.verifier_min_free_mem_mb) and one freshness bound
// (autonomous.verifier_headroom_max_age_hours), both through the config
// accessors. The MCP and CLI surfaces write through the REST node routes, so
// they reach the same write gate and need no store of their own.
package main

import (
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
)

// headroomWiring carries the one store and the admission gate built over it.
type headroomWiring struct {
	Store *compute.HeadroomStore
	Gate  verifierCapGate
}

// newHeadroomWiring creates the daemon's single headroom store and the
// admission gate over it. Call once per daemon start.
func newHeadroomWiring(cfg *config.Config) headroomWiring {
	store := compute.NewHeadroomStore()
	return headroomWiring{Store: store, Gate: newVerifierCapGate(store, cfg)}
}

// attach wires the store into the REST server (the verifier_slots write gate).
// A nil server (the web server disabled) is skipped.
func (w headroomWiring) attach(srv *server.HTTPServer) {
	if srv == nil {
		return
	}
	srv.SetHeadroomStore(w.Store)
}
