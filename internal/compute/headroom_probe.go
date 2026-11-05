// Package compute — headroom measurement probe.
//
// headroom.go defines the gate (does a fresh measurement support a cap?).
// This file defines what feeds it: a measurement of one node's free
// memory with its resident model loaded, and the memory one extra
// parallel request needs.
//
// The measurement is idle-only: Measure never runs on a node that is
// currently running work (Held > 0 is refused with ErrNodeBusy), so the
// numbers it reports describe an idle node, not one under load.
//
// The probe itself is injected as an interface (HeadroomProbe); this
// file deliberately contains no HTTP or other transport details — the
// concrete probe is supplied by the daemon wiring task. Tests use a
// fake probe and an injected clock, so the code path is identical in
// tests and production and never depends on wall time.

package compute

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNodeBusy is returned when a node has workers holding a lease, i.e.
// it is running work now. A node under load is never measured.
var ErrNodeBusy = errors.New("node busy")

// ErrModelNotAllowed is returned when the allow-list is empty, or when
// the model currently resident on the node is not in it. Measuring an
// unauthorised (or unknowable) model set would report numbers the gate
// must not be allowed to rely on.
var ErrModelNotAllowed = errors.New("model not allowed")

// HeadroomProbe reads a node's memory state. Implementations are
// supplied by the daemon wiring task (or by tests); this package never
// talks to a concrete transport directly.
type HeadroomProbe interface {
	// ReadFreeMB returns the node's free memory (MB, free video memory
	// when the node has GPU data, else free system memory) and the name
	// of the model currently resident on the node.
	ReadFreeMB(ctx context.Context, node Node) (freeMB int, residentModel string, err error)
	// RequestMemoryMB returns how many MB one extra parallel request
	// needs beyond the resident model, estimated by issuing a probe
	// request for model.
	RequestMemoryMB(ctx context.Context, node Node, model string) (perRequestMB int, err error)
}

// MeasureDeps carries everything Measure needs. Nothing is read from
// globals: the clock is injected (Now) and the probe is injected
// (Probe), so tests drive both.
type MeasureDeps struct {
	Probe HeadroomProbe // required; the measurement transport
	Now   func() time.Time

	// Held is the number of workers holding a lease on the node now.
	// > 0 means the node is running work and is never measured
	// (ErrNodeBusy).
	Held int

	// AllowedModels are the models this measurement is permitted to
	// involve. The caller is responsible for sorting them smallest
	// first; Measure always picks the first one (AllowedModels[0]) as
	// the probe request's model. Empty = refused (ErrModelNotAllowed).
	AllowedModels []string

	Node Node

	// PerProcessMB is optional, from observer data: a known memory
	// cost per parallel request in MB. When > 0 it is used as
	// PerRequestMB and the RequestMemoryMB probe request is skipped.
	PerProcessMB int
}

// Measure produces a HeadroomMeasurement for deps.Node.
//
// Rules, in order:
//
//  1. Held > 0 → ErrNodeBusy. Never measure a node that is running
//     work; a node under load reports a free memory that understates
//     its idle headroom.
//
//  2. Empty AllowedModels → ErrModelNotAllowed.
//
//  3. ReadFreeMB is always probed first; if the resident model is not
//     in AllowedModels → ErrModelNotAllowed.
//
//  4. PerRequestMB comes from deps.PerProcessMB when that is > 0 (the
//     RequestMemoryMB request is not sent at all); otherwise it comes
//     from Probe.RequestMemoryMB using the smallest allowed model
//     (AllowedModels[0]).
//
// The measurement's MeasuredAt is deps.Now().
func Measure(ctx context.Context, deps MeasureDeps) (HeadroomMeasurement, error) {
	if deps.Held > 0 {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: %w", nodeID(deps.Node), ErrNodeBusy)
	}
	if len(deps.AllowedModels) == 0 {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: %w", nodeID(deps.Node), ErrModelNotAllowed)
	}
	if deps.Probe == nil {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: probe required", nodeID(deps.Node))
	}
	if deps.Now == nil {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: clock required", nodeID(deps.Node))
	}

	freeMB, resident, err := deps.Probe.ReadFreeMB(ctx, deps.Node)
	if err != nil {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: read free memory: %v", nodeID(deps.Node), err)
	}
	if !modelAllowed(resident, deps.AllowedModels) {
		return HeadroomMeasurement{}, fmt.Errorf("measure node %s: resident model %q: %w",
			nodeID(deps.Node), resident, ErrModelNotAllowed)
	}

	perRequestMB := deps.PerProcessMB
	if perRequestMB <= 0 {
		probeModel := deps.AllowedModels[0] // caller kept it sorted smallest first
		perRequestMB, err = deps.Probe.RequestMemoryMB(ctx, deps.Node, probeModel)
		if err != nil {
			return HeadroomMeasurement{}, fmt.Errorf("measure node %s: request memory: %v", nodeID(deps.Node), err)
		}
	}

	return HeadroomMeasurement{
		Node:          nodeID(deps.Node),
		FreeMB:        freeMB,
		PerRequestMB:  perRequestMB,
		ResidentModel: resident,
		MeasuredAt:    deps.Now(),
	}, nil
}

func modelAllowed(resident string, allowed []string) bool {
	for _, m := range allowed {
		if m == resident {
			return true
		}
	}
	return false
}

func nodeID(n Node) string {
	if n.Name != "" {
		return n.Name
	}
	return "unknown"
}
