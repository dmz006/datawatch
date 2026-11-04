package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server"
)

// servedModelsCache is the daemon's cache of what each compute node serves,
// set once at startup by wireModelRouting. The pin-time model check reads
// through it, and the REST server can be handed it later; nil until then.
var servedModelsCache *compute.ServedModelsCache

// modelRoutingConfig returns the live autonomous.model_routing block, or the
// zero block (every key at its default) when there is no config.
func modelRoutingConfig(cfg func() *config.Config) config.ModelRoutingConfig {
	if cfg == nil {
		return config.ModelRoutingConfig{}
	}
	c := cfg()
	if c == nil {
		return config.ModelRoutingConfig{}
	}
	return c.Autonomous.ModelRouting
}

// newServedModelsCache builds the served-models cache. The cache age and the
// read timeout come from autonomous.model_routing on every call, so a config
// change applies without a restart. Nodes are read with the real HTTP lister.
func newServedModelsCache(cfg func() *config.Config) *compute.ServedModelsCache {
	return compute.NewServedModelsCache(
		compute.HTTPModelLister(),
		func() time.Duration { return modelRoutingConfig(cfg).EffectiveServedModelsCacheAge() },
		func() time.Duration { return modelRoutingConfig(cfg).EffectiveProbeTimeout() },
		nil,
	)
}

// modelRoutingSettings returns the live pin-time settings for the Automaton
// manager: the refusal mode, whether a refusal raises an alert and whether the
// verifier follows its worker's node.
func modelRoutingSettings(cfg func() *config.Config) autonomous.ModelRoutingSettings {
	mr := modelRoutingConfig(cfg)
	return autonomous.ModelRoutingSettings{
		RefusalMode:               mr.EffectiveRefusalMode(),
		AlertOnRefusal:            mr.EffectiveAlertOnRefusal(),
		VerifierFollowsWorkerNode: mr.EffectiveVerifierFollowsWorkerNode(),
	}
}

// modelCheckEntry returns the LLM entry that hosts model for backend when the
// session runs on a local engine. With a backend it follows hostingEntry. With
// no backend the model's provider prefix picks the entry, so a pin that names
// no backend is still checked when its model is served by a local engine.
func modelCheckEntry(reg *inference.Registry, backend, model string) (inference.LLM, bool) {
	if backend != "" {
		entry, _, gated := hostingEntry(reg, backend, model)
		return entry, gated
	}
	provider, _, found := strings.Cut(model, "/")
	if !found || provider == "" {
		return inference.LLM{}, false
	}
	return engineEntry(reg, provider, model)
}

// newModelHostCheck builds the pin-time check the Automaton manager asks
// whether a node serves a model. A backend that runs on no local engine (a
// hosted backend such as claude-code) is not applicable and is never refused.
// For a local engine the nodes checked are the hosting LLM entry's
// compute_nodes when it lists any, otherwise every enabled registered node;
// they are read through cache and judged with the configured
// unknown_node_policy.
//
// A node argument pins the check to that node: one that is not a registered
// node, or is outside the entry's compute_nodes, is refused with a message
// naming it.
func newModelHostCheck(cfg func() *config.Config, reg func() *inference.Registry, nodes func() *compute.Registry, cache *compute.ServedModelsCache) autonomous.ModelHostCheckFn {
	return func(ctx context.Context, backend, model, node string) autonomous.ModelHostVerdict {
		v := autonomous.ModelHostVerdict{Model: model, Node: node}
		if model == "" {
			return unregisteredNodeVerdict(v, nodes, node)
		}
		if cache == nil {
			return v
		}
		var ireg *inference.Registry
		if reg != nil {
			ireg = reg()
		}
		entry, applicable := modelCheckEntry(ireg, backend, model)
		if !applicable {
			return v
		}
		v.Applicable = true

		var creg *compute.Registry
		if nodes != nil {
			creg = nodes()
		}
		registered := map[string]*compute.Node{}
		var enabled []*compute.Node
		if creg != nil {
			for _, n := range creg.List() {
				registered[n.Name] = n
				if !n.Disabled {
					enabled = append(enabled, n)
				}
			}
		}

		if node != "" {
			n, ok := registered[node]
			if !ok {
				v.Refused, v.NodeUnknown = true, true
				v.Message = fmt.Sprintf("node %q does not serve model %q: it is not a registered node", node, model)
				return v
			}
			if len(entry.ComputeNodes) > 0 && !containsString(entry.ComputeNodes, node) {
				v.Refused = true
				v.Message = fmt.Sprintf("node %q does not serve model %q: it is not one of the compute nodes of LLM entry %q (%s)",
					node, model, entry.Name, strings.Join(entry.ComputeNodes, ", "))
				return v
			}
			res := compute.CheckModel(model, node, cache.GetAll(ctx, []*compute.Node{n}, false),
				modelRoutingConfig(cfg).EffectiveUnknownNodePolicy())
			return fillModelVerdict(v, res)
		}

		var toCheck []*compute.Node
		if len(entry.ComputeNodes) > 0 {
			for _, name := range entry.ComputeNodes {
				if n, ok := registered[name]; ok && !n.Disabled {
					toCheck = append(toCheck, n)
				}
			}
		} else {
			toCheck = enabled
		}
		res := compute.CheckModel(model, "", cache.GetAll(ctx, toCheck, false),
			modelRoutingConfig(cfg).EffectiveUnknownNodePolicy())
		return fillModelVerdict(v, res)
	}
}

// unregisteredNodeVerdict answers a check that has no model to look for. Only a
// named node is checked, and only for being a registered compute node: an
// unregistered one comes back refused with NodeUnknown set, anything else
// leaves v as it is (not applicable). A node pin on a unit with no resolved
// model still must name a node that exists.
func unregisteredNodeVerdict(v autonomous.ModelHostVerdict, nodes func() *compute.Registry, node string) autonomous.ModelHostVerdict {
	if node == "" {
		return v
	}
	if nodes != nil {
		if creg := nodes(); creg != nil {
			for _, n := range creg.List() {
				if n.Name == node {
					return v
				}
			}
		}
	}
	v.Applicable, v.Refused, v.NodeUnknown = true, true, true
	v.Message = fmt.Sprintf("node %q is not a registered node", node)
	return v
}

// fillModelVerdict copies a compute.CheckModel result into the verdict: the
// refusal, the unknown-node flag and, for a refusal, the message that names the
// model and the nodes checked.
func fillModelVerdict(v autonomous.ModelHostVerdict, res compute.ModelCheckResult) autonomous.ModelHostVerdict {
	v.Refused = res.Refused
	v.NodeUnknown = res.NodeUnknown
	v.ServedBy = append([]string{}, res.Served...)
	v.Checked = make([]autonomous.ModelHostNodeState, 0, len(res.Checked))
	for _, c := range res.Checked {
		v.Checked = append(v.Checked, autonomous.ModelHostNodeState{Node: c.Node, State: c.State, Reason: c.Reason})
	}
	if res.Refused {
		v.Message = (&compute.NoNodeServesModelError{Result: res}).Error()
	}
	return v
}

// wireModelRouting builds the served-models cache and gives the Automaton
// manager the pin-time model check and the live routing settings. The REST
// server is handed the same cache and check. It runs at
// daemon start, next to wireCapacity.
func wireModelRouting(cfg *config.Config, amgr *autonomous.Manager, httpServer *server.HTTPServer) {
	live := func() *config.Config { return cfg }
	servedModelsCache = newServedModelsCache(live)
	check := newModelHostCheck(live, httpServer.InferenceRegistry, httpServer.ComputeRegistry, servedModelsCache)
	amgr.SetModelHostCheck(check)
	// The REST route reads the same cache and asks the same check, so its
	// answer and a pin-time refusal cannot disagree.
	httpServer.SetServedModelsCache(servedModelsCache)
	httpServer.SetModelHostCheck(check)
	amgr.SetModelRoutingSettings(func() autonomous.ModelRoutingSettings { return modelRoutingSettings(live) })
}
