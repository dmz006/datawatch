package classifier

import (
	"context"
	"fmt"
)

// Gate is the node inference-load limit. The production implementation is the
// capacity ledger's node:<name> pool (internal/capacity). Classify calls share
// that pool: a classify call occupies the same GPU/CPU as a generate call and
// may force a model load, so exempting it would let verdict reads starve or
// thrash the work they judge. See docs/plans/reports/classifier-api-shape.md.
type Gate interface {
	// Acquire takes one slot of pool "node:<node>" and returns its release.
	Acquire(ctx context.Context, node, model string) (release func(), err error)
}

// GateError is returned when the node pool refuses or cannot be reached. The
// caller turns it into an "infra" block; a classify call is never run outside
// the limit.
type GateError struct {
	Node  string
	Cause error
}

func (e *GateError) Error() string {
	return fmt.Sprintf("classifier: node %q inference-load limit: %v", e.Node, e.Cause)
}

// Unwrap returns the gate's cause.
func (e *GateError) Unwrap() error { return e.Cause }

type heldSlotKey struct{}

// WithHeldSlot marks ctx as running under the capacity lease named holder, so
// a gate can run a call on the slot that lease already holds instead of
// waiting for a second one.
func WithHeldSlot(ctx context.Context, holder string) context.Context {
	return context.WithValue(ctx, heldSlotKey{}, holder)
}

// HeldSlot returns the lease holder set by WithHeldSlot, or "".
func HeldSlot(ctx context.Context) string {
	holder, _ := ctx.Value(heldSlotKey{}).(string)
	return holder
}

// WithGate runs fn holding one slot of the node's inference-load pool. A nil
// gate is an error, not a bypass.
func WithGate(ctx context.Context, g Gate, node, model string, fn func(context.Context) error) error {
	if g == nil {
		return &GateError{Node: node, Cause: fmt.Errorf("no gate configured")}
	}
	release, err := g.Acquire(ctx, node, model)
	if err != nil {
		return &GateError{Node: node, Cause: err}
	}
	defer release()
	return fn(ctx)
}

// FailureCategory maps a classifier error to the failure category the caller
// must record on the resulting block. Every classifier failure (capability,
// gate, transport, timeout) is "infra"; a block is never downgraded.
func FailureCategory(err error) string {
	if err == nil {
		return ""
	}
	return "infra"
}
