// One shadow runs at a time in a daemon, a hand merge's and a finish-time
// plan's alike: each runs a project's commands, and each command may take
// memory up to its limit.

package mergeshadow

import (
	"context"
	"fmt"
	"sync/atomic"
)

var slot = make(chan struct{}, 1)

type slotKey struct{}

// slotHold marks the context of the shadow that holds the slot.
type slotHold struct{ released atomic.Bool }

// AcquireSlot waits until no other shadow of this process runs, or ctx ends.
// The returned context holds the slot, so that what runs under it does not
// wait for itself. release gives the slot back and is safe to call twice.
func AcquireSlot(ctx context.Context) (held context.Context, release func(), err error) {
	if h, ok := ctx.Value(slotKey{}).(*slotHold); ok && !h.released.Load() {
		return ctx, func() {}, nil
	}
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return ctx, func() {}, fmt.Errorf("waiting for the shadow that is already running: %w", ctx.Err())
	}
	h := &slotHold{}
	return context.WithValue(ctx, slotKey{}, h), func() {
		if h.released.CompareAndSwap(false, true) {
			<-slot
		}
	}, nil
}
