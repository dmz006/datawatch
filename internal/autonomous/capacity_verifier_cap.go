package autonomous

import (
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
)

// DefaultVerifierCapMaxAge is the default freshness bound for the headroom
// measurement behind a node's verifier cap: a measurement older than this (or
// missing) counts as no measurement, so the cap runs at the supported minimum
// (1) until the node is measured again while idle.
const DefaultVerifierCapMaxAge = 10 * time.Minute

// VerifierCapPolicy computes the effective verifier cap for one compute node:
//
//	effective = min(configured verifier_slots, headroom-supported cap)
//
// The configured slots come from the node's verifier_slots key (0 or absent
// means compute.DefaultNodeVerifierSlots). The headroom-supported cap comes
// from compute.CapCoverage over the daemon's HeadroomStore: with no fresh
// measurement (store missing, node never measured, or the measurement older
// than MaxAge) the node supports only one concurrent verifier. A configured
// cap above the supported one is refused by the gate and the node runs at the
// supported value; the gate's refusal (a *compute.CapRefusal naming the node
// and reason) is returned so the daemon can log it once per node per change
// and expose it in the /api/capacity snapshot.
//
// The policy is a pure read: it never measures anything, never blocks, and
// reads no network or process state. The caller supplies Now (tests use a
// fixed time) and the store (nil means no measurement source).
type VerifierCapPolicy struct {
	Node string // compute node name
	// Slots is the node's configured verifier_slots; <= 0 means the default
	// (compute.DefaultNodeVerifierSlots).
	Slots int
	// Headroom is the daemon's headroom measurement store. Nil means no
	// measurement can exist, so a configured cap above 1 is refused.
	Headroom *compute.HeadroomStore
	// MinFreeMB is the gate's minimum free MB (optional).
	MinFreeMB int
	// MaxAge bounds how old a measurement may be. <= 0 means
	// DefaultVerifierCapMaxAge.
	MaxAge time.Duration
	// Now supplies the current time. Nil means time.Now.
	Now func() time.Time
}

// Effective returns the node's effective verifier cap (>= 1) and the gate's
// refusal when the configured cap was refused (nil when the configured cap
// ran as asked, which includes the default cap of 1).
func (p *VerifierCapPolicy) Effective() (int, *compute.CapRefusal) {
	configured := p.Slots
	if configured <= 0 {
		configured = compute.DefaultNodeVerifierSlots
	}
	if configured <= 1 {
		// The default cap is always supported; the gate cannot refuse it.
		return configured, nil
	}
	maxAge := p.MaxAge
	if maxAge <= 0 {
		maxAge = DefaultVerifierCapMaxAge
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	var m *compute.HeadroomMeasurement
	if p.Headroom != nil {
		if got, ok := p.Headroom.Get(p.Node); ok {
			m = &got
		}
	}
	supported, refusal := compute.CapCoverage(m, configured, p.MinFreeMB, now, maxAge)
	if refusal != nil && (refusal.Node == "" || refusal.Node == "unknown") {
		// The gate does not know the node's name; the policy does.
		refusal.Node = p.Node
	}
	if supported < configured {
		return supported, refusal
	}
	return supported, nil
}

// SnapshotCap is the VerifierCapPolicy verdict in the ledger's reporting
// shape: the snapshot and wait-reason source for /api/capacity.
func (p *VerifierCapPolicy) SnapshotCap() capacity.VerifierCap {
	effective, refusal := p.Effective()
	configured := p.Slots
	if configured <= 0 {
		configured = compute.DefaultNodeVerifierSlots
	}
	out := capacity.VerifierCap{Configured: configured, Effective: effective}
	if refusal != nil {
		out.Refusal = refusal.Error()
	}
	return out
}
