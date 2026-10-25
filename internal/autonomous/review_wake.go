package autonomous

import (
	"context"
	"sync"
)

// reviewWakes lets the live executor re-scan after an operator review decision
// without a second executor being launched. Each running Automaton arms one
// buffered channel of capacity one; a decision signals it without blocking and
// leaves a pending flag the executor goroutine checks before it gives up its
// slot.
type reviewWakes struct {
	mu      sync.Mutex
	ch      map[string]chan struct{}
	pending map[string]bool
}

// reviewWakeKey is the context key under which the wake channel reaches
// Manager.Run.
type reviewWakeKey struct{}

// arm creates (or returns the existing) wake channel for the Automaton.
func (w *reviewWakes) arm(id string) chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ch == nil {
		w.ch = map[string]chan struct{}{}
		w.pending = map[string]bool{}
	}
	if c, ok := w.ch[id]; ok {
		return c
	}
	c := make(chan struct{}, 1)
	w.ch[id] = c
	return c
}

// disarm drops the Automaton's wake channel and any pending decision.
func (w *reviewWakes) disarm(id string) {
	w.mu.Lock()
	delete(w.ch, id)
	delete(w.pending, id)
	w.mu.Unlock()
}

// signal records a decision for the Automaton and nudges its channel without
// blocking. It reports whether a wake was armed.
func (w *reviewWakes) signal(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.ch[id]
	if !ok {
		return false
	}
	w.pending[id] = true
	select {
	case c <- struct{}{}:
	default:
	}
	return true
}

// takePending reports whether a decision arrived since the last call, and
// clears the flag along with any nudge still buffered on the channel.
func (w *reviewWakes) takePending(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	was := w.pending[id]
	delete(w.pending, id)
	if c, ok := w.ch[id]; ok {
		select {
		case <-c:
		default:
		}
	}
	return was
}

// reviewWakeNudged reports, without blocking, whether a review decision has
// nudged the wake channel since the last check, and consumes the nudge. A nil
// channel never reports one, so a Run without a live executor is unaffected.
func reviewWakeNudged(wake <-chan struct{}) bool {
	select {
	case <-wake:
		return true
	default:
		return false
	}
}

// withReviewWake returns ctx carrying the Automaton's wake channel.
func withReviewWake(ctx context.Context, c <-chan struct{}) context.Context {
	return context.WithValue(ctx, reviewWakeKey{}, c)
}

// reviewWakeFrom returns the wake channel carried by ctx, or nil.
func reviewWakeFrom(ctx context.Context) <-chan struct{} {
	c, _ := ctx.Value(reviewWakeKey{}).(<-chan struct{})
	return c
}
