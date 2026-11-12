// Package compute — verifier cap gate: the one decision both verifier gates
// share.
//
// Two gates use the same measurement store and the same freshness bound and
// free-memory floor: the admission gate (the capacity sync clamps a node's
// verifier pool to 1 unless its measurement supports the configured slots)
// and the write gate (saving a node with verifier_slots above 1 is refused
// unless the same measurement supports it). Both go through
// ResolveVerifierCap / CheckVerifierSlotsWrite here, so neither can drift
// from the other. The caller passes the store, the floor and the age; the
// config accessors that produce them are the single source of those values.
package compute

import "time"

// measurementFor returns the node's stored measurement, or a zero-time
// measurement naming the node when there is none, so CapCoverage reports
// "no fresh measurement" against the right node.
func measurementFor(store *HeadroomStore, node string) *HeadroomMeasurement {
	if m, ok := store.Get(node); ok {
		return &m
	}
	return &HeadroomMeasurement{Node: node}
}

// ResolveVerifierCap is the admission cap for a node: the configured verifier
// slot count when it is 1 or less (no measurement is needed to keep a cap of
// 1, and an explicit 0 stays 0), otherwise the cap CapCoverage supports from
// the node's measurement in store. A refusal comes back with the cap clamped
// to 1 and the CapRefusal that explains it.
func ResolveVerifierCap(store *HeadroomStore, node string, configured, minFreeMB int, maxAge time.Duration, now time.Time) (int, *CapRefusal) {
	if configured <= 1 {
		return configured, nil
	}
	return CapCoverage(measurementFor(store, node), configured, minFreeMB, now, maxAge)
}

// CheckVerifierSlotsWrite is the write gate for a node's verifier_slots: a
// value of 1 or less is always accepted; a value above 1 is accepted only
// when the node's measurement in store supports it. A refusal is returned as
// the *CapRefusal, whose message names the node, the measured free memory
// and the shortfall.
func CheckVerifierSlotsWrite(store *HeadroomStore, node string, slots, minFreeMB int, maxAge time.Duration, now time.Time) error {
	if _, refusal := ResolveVerifierCap(store, node, slots, minFreeMB, maxAge, now); refusal != nil {
		return refusal
	}
	return nil
}
