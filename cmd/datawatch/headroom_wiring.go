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
	"log"
	"time"

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

// recordConfigVerifierSlotsRefusals is the config load and reload half of the
// write gate. Each compute node in the config with verifier_slots above 1 is
// checked against the store with the same floor and age the write gate uses.
// The configured value is kept in the config, so it applies as soon as a
// measurement supports it; until then the admission gate holds the node's
// verifier cap at 1, and this function records that as a logged refusal.
// It returns the refusal texts, one per refused node.
func recordConfigVerifierSlotsRefusals(store *compute.HeadroomStore, cfg *config.Config, now time.Time) []string {
	if cfg == nil {
		return nil
	}
	var refused []string
	for _, c := range cfg.ComputeNodes {
		n := computeNodeFromConfig(c)
		if n.VerifierSlots == nil || *n.VerifierSlots <= 1 {
			continue
		}
		err := compute.CheckVerifierSlotsWrite(store, n.Name, *n.VerifierSlots,
			cfg.Autonomous.EffectiveVerifierMinFreeMB(), cfg.Autonomous.EffectiveVerifierHeadroomMaxAge(), now)
		if err == nil {
			continue
		}
		log.Printf("[capacity] config verifier_slots %d kept for node %s, effective cap held at 1: %s",
			*n.VerifierSlots, n.Name, err)
		refused = append(refused, err.Error())
	}
	return refused
}
