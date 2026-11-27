package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/classifier"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// classifyDeps are the daemon pieces the classify runtime needs. They are
// filled in as the daemon wires each one; a missing piece makes the runtime
// fail closed (a gate-less client blocks every call as an infra error).
type classifyDeps struct {
	reg     *inference.Registry
	nodes   func(name string) (*compute.Node, error)
	ledger  atomic.Pointer[capacity.Ledger]
	dataDir string
	prober  *classifier.Prober
	// capacityWait is the capacity wait setting, read live (unset: nil).
	capacityWait atomic.Pointer[func() time.Duration]
}

// nodeWait is the current bound on a wait for a node slot, 0 when unwired.
func (d *classifyDeps) nodeWait() time.Duration {
	if fn := d.capacityWait.Load(); fn != nil {
		return (*fn)()
	}
	return 0
}

var classifyWiring classifyDeps

// defaultClassifyNodeWait bounds a classify call's wait for a node slot when
// the capacity wait setting is not wired.
const defaultClassifyNodeWait = 4 * time.Hour

// ledgerGate adapts the capacity ledger's node:<name> pool to classifier.Gate.
type ledgerGate struct {
	led *atomic.Pointer[capacity.Ledger]
	seq atomic.Uint64
	// wait bounds one wait for a node slot (nil: the capacity default).
	wait func() time.Duration
}

func (g *ledgerGate) Acquire(ctx context.Context, node, model string) (func(), error) {
	led := g.led.Load()
	if led == nil {
		return nil, fmt.Errorf("capacity ledger not ready")
	}
	// A caller that already holds a slot on this node (a verifier) runs the
	// call on it. A second slot would be a wait on the caller's own: on a
	// one-slot node it could never end.
	if own := classifier.HeldSlot(ctx); own != "" && leaseOnNode(led.LeaseOf(own), node) {
		return func() {}, nil
	}
	holder := fmt.Sprintf("classify:%s:%d", node, g.seq.Add(1))
	wait := defaultClassifyNodeWait
	if g.wait != nil {
		if d := g.wait(); d > 0 {
			wait = d
		}
	}
	if err := led.Acquire(ctx, capacity.Request{Holder: holder, Pools: []string{"node:" + node}, Node: node}, wait, nil, nil); err != nil {
		return nil, err
	}
	return func() { led.Release(holder) }, nil
}

// leaseOnNode reports whether ls holds a slot on node: its executor pool, its
// verifier pool, or a lease recorded on the node outside the full pool.
func leaseOnNode(ls *capacity.Lease, node string) bool {
	if ls == nil || node == "" {
		return false
	}
	return ls.Node == node || slices.Contains(ls.Pools, "node:"+node) || slices.Contains(ls.Pools, capacity.VerifierPool(node))
}

// newClassifyRuntimeProvider builds the provider installed as
// classifyRuntimeProvider. The client and calibration tunables are read from
// the live classifier.* config on every call, so operator changes apply
// without a restart.
func newClassifyRuntimeProvider(cfg *config.Config, d *classifyDeps) func(node, backend, model string) (autonomouspkg.ClassifyRuntime, error) {
	gate := &ledgerGate{led: &d.ledger, wait: d.nodeWait}
	d.prober = classifier.NewProber(nil, func(name string) (string, error) { return classifyBaseURL(d, name) }, classifier.ProbeConfigFrom(cfg.Classifier), nil)
	return func(node, backend, model string) (autonomouspkg.ClassifyRuntime, error) {
		if d.nodes == nil {
			return autonomouspkg.ClassifyRuntime{}, fmt.Errorf("compute registry not ready")
		}
		if d.reg != nil && backend != "" {
			if l, err := d.reg.Get(backend); err == nil && l != nil {
				if node == "" && len(l.ComputeNodes) > 0 {
					node = l.ComputeNodes[0]
				}
				if model == "" {
					model = l.Model
				}
			}
		}
		if node == "" {
			return autonomouspkg.ClassifyRuntime{}, fmt.Errorf("no compute node resolved for %q", backend)
		}
		baseURL := func(name string) (string, error) { return classifyBaseURL(d, name) }
		ccfg := classifier.ClientConfigFrom(cfg.Classifier)
		client := classifier.NewClient(nil, baseURL, gate, d.prober, ccfg)
		fp := classifier.NewFingerprint(model, "", node, "", "")
		return autonomouspkg.ClassifyRuntime{
			Client:      client,
			Calibration: classifier.FileStore{Dir: filepath.Join(d.dataDir, "classifier")},
			Route:       autonomouspkg.VerdictRoute{Node: node, Model: model, Path: string(classifier.PathLogprobs)},
			Path:        classifier.PathLogprobs,
			Fingerprint: fp,
		}, nil
	}
}

// classifyBaseURL resolves a compute node name to its inference base URL.
func classifyBaseURL(d *classifyDeps, name string) (string, error) {
	if d.nodes == nil {
		return "", fmt.Errorf("compute registry not ready")
	}
	n, err := d.nodes(name)
	if err != nil {
		return "", err
	}
	addr := strings.TrimRight(n.Address, "/")
	if addr == "" {
		return "", fmt.Errorf("node %q has no address", name)
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	return addr, nil
}
