package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

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
}

var classifyWiring classifyDeps

// ledgerGate adapts the capacity ledger's node:<name> pool to classifier.Gate.
type ledgerGate struct {
	led *atomic.Pointer[capacity.Ledger]
	seq atomic.Uint64
}

func (g *ledgerGate) Acquire(ctx context.Context, node, model string) (func(), error) {
	led := g.led.Load()
	if led == nil {
		return nil, fmt.Errorf("capacity ledger not ready")
	}
	holder := fmt.Sprintf("classify:%s:%d", node, g.seq.Add(1))
	if err := led.Acquire(ctx, capacity.Request{Holder: holder, Pools: []string{"node:" + node}, Node: node}, 0, nil, nil); err != nil {
		return nil, err
	}
	return func() { led.Release(holder) }, nil
}

// newClassifyRuntimeProvider builds the provider installed as
// classifyRuntimeProvider. The client and calibration tunables are read from
// the live classifier.* config on every call, so operator changes apply
// without a restart.
func newClassifyRuntimeProvider(cfg *config.Config, d *classifyDeps) func(node, backend, model string) (autonomouspkg.ClassifyRuntime, error) {
	gate := &ledgerGate{led: &d.ledger}
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
