// launch_gate.go runs the preflight probe before a pi session is launched on
// a compute node and keeps the node's unhealthy mark in step with the result.
package pi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// NodeHealthMarker is the part of the capacity ledger the launch gate needs.
type NodeHealthMarker interface {
	MarkNodeUnhealthy(node, reason string)
	ClearNodeUnhealthy(node string)
}

// ErrLaunchDeferred is returned when a pi launch was not made because the
// node failed its preflight. The task executor treats it as no attempt
// consumed: its text carries the model-unresponsive failure wording, so the
// model-unresponsive requeue path picks it up.
var ErrLaunchDeferred = errors.New("pi launch deferred")

// launchGate is the process-wide preflight wiring, set once at startup.
var launchGate struct {
	mu       sync.RWMutex
	marker   NodeHealthMarker
	nodeName func(addr string) string
	client   HTTPDoer
}

// SetLaunchGate wires the unhealthy-mark sink and the address-to-node-name
// resolver. A nil marker leaves the gate probing without marking. A nil
// resolver uses the node address as the name.
func SetLaunchGate(marker NodeHealthMarker, nodeName func(addr string) string) {
	launchGate.mu.Lock()
	defer launchGate.mu.Unlock()
	launchGate.marker = marker
	launchGate.nodeName = nodeName
}

// setPreflightClient replaces the probe's HTTP client (tests).
func setPreflightClient(c HTTPDoer) {
	launchGate.mu.Lock()
	defer launchGate.mu.Unlock()
	launchGate.client = c
}

// preflightBeforeLaunch probes the node before a pi launch. With no node
// address (a non-node launch) or no model it does nothing. On failure it
// marks the node unhealthy with the reason and returns a launch-deferred
// error; on success it clears the node's mark.
func preflightBeforeLaunch(ctx context.Context, nodeAddr, model string) error {
	if nodeAddr == "" || model == "" {
		return nil
	}
	launchGate.mu.RLock()
	marker, resolve, client := launchGate.marker, launchGate.nodeName, launchGate.client
	launchGate.mu.RUnlock()
	if client == nil {
		client = http.DefaultClient
	}
	node := nodeAddr
	if resolve != nil {
		if n := resolve(nodeAddr); n != "" {
			node = n
		}
	}
	err := Preflight(ctx, client, nodeBaseURL(nodeAddr), model)
	if err != nil {
		if marker != nil {
			marker.MarkNodeUnhealthy(node, err.Error())
		}
		return fmt.Errorf("%w: DATAWATCH_PI_FAILED: node %s not launched, Request timed out: %v", ErrLaunchDeferred, node, err)
	}
	if marker != nil {
		marker.ClearNodeUnhealthy(node)
	}
	return nil
}
