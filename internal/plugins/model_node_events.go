package plugins

import (
	"context"
	"time"
)

// DefaultModelNodeEventInterval is how often the model-node events task
// resolves the model-use reports and feeds node reachability to the
// recorder.
const DefaultModelNodeEventInterval = 15 * time.Second

// ModelUseSource is the narrow interface a per-plugin model-use report
// source must implement (internal/server's endpoint supplies one).
type ModelUseSource interface {
	ModelUse(ctx context.Context, name string) (*ModelUseReport, error)
}

// ModelNodeEvents is the long-running events task that owns the
// model_node_unreachable condition: on each interval pass it resolves
// every service plugin's model-use report from the source and feeds
// each node's answered/did-not-answer state to the shared
// ServiceEventRecorder, the single place model_node_unreachable is
// raised (and cleared) — once per transition. It runs whether or not
// anyone queries the model-use endpoint, so a node that stops answering
// is reported without a request driving the observation.
type ModelNodeEvents struct {
	// Recorder receives the raise and the clear. A nil recorder makes
	// the task a no-op (Run returns immediately).
	Recorder *ServiceEventRecorder
	// Source resolves each plugin's report; nil disables the task.
	Source ModelUseSource
	// Plugins returns the service plugins to pass for. It is re-resolved
	// on every pass, so a service plugin registered while the task runs
	// is picked up and one that disappears drops out. Nil disables.
	Plugins func(ctx context.Context) []string
	// Interval between passes; zero uses DefaultModelNodeEventInterval.
	Interval time.Duration
}

// Run blocks on passes until ctx is cancelled: one immediate pass, then
// one per interval tick. Each pass feeds every plugin's report to the
// recorder — raising model_node_unreachable for every node that did not
// answer and clearing it for every node that did — so the raise happens
// without any request to the model-use endpoint.
func (e *ModelNodeEvents) Run(ctx context.Context) {
	if e == nil || e.Recorder == nil || e.Source == nil || e.Plugins == nil {
		return
	}
	interval := e.Interval
	if interval <= 0 {
		interval = DefaultModelNodeEventInterval
	}
	pass := func() {
		for _, name := range e.Plugins(ctx) {
			rep, err := e.Source.ModelUse(ctx, name)
			if err != nil || rep == nil {
				continue // unresolvable plugin: nothing to pass for
			}
			RecordModelUseNodes(e.Recorder, rep)
		}
	}
	pass()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pass()
		}
	}
}
