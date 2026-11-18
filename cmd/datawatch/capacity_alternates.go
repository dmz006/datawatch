package main

import (
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/inference"
)

// capacityAlternatesSource is everything the fall-through alternates resolver
// reads. Each field is read on every call, so an operator edit (lane fill
// backends, an LLM entry's fallthrough_target mark, a cap, the disabled switch)
// applies to the next admission without a restart. Tests fill it with fakes.
type capacityAlternatesSource struct {
	// LaneFill is the live lane fill configuration: which backend the Claude
	// lanes and the GPU lanes run, and the GPU lanes' model.
	LaneFill func() autonomous.LaneFillConfig
	// Entries lists the LLM entries (unredacted copies).
	Entries func() []inference.LLM
	// Caps is the lane fill cap source: each entry's enabled switch and its
	// effective in-flight cap, the same numbers the ledger limits and the lane
	// counts come from.
	Caps autonomous.LaneCapSource
	// Inflight is the number of sessions in flight on a backend's llm: pool.
	Inflight func(backend string) int
}

// capacityAlternatesFor builds the resolver wired into the autonomous manager
// with SetCapacityAlternates. For a task requested on (backend, model) it lists
// the other pairs the task may be admitted on when its requested pool is full,
// in preference order:
//
//  1. The lane fill pair: the other of the Claude lane backend and the GPU lane
//     backend (autonomous.lane_fill), with that backend's default model. A task
//     on neither lane backend has no lane fill pair.
//  2. Every LLM entry marked fallthrough_target, in name order, on the
//     requested model when the entry serves it and on the entry's default model
//     otherwise.
//
// An alternate is left out when its backend is disabled, is already at its
// in-flight cap, or does not serve the model it would run; the requested pair
// is never listed. The result only suggests: the manager still asks the ledger
// whether each alternate's pools would be granted.
//
// The manager calls the resolver for worker requests only; a verifier, judge or
// planner request never reaches it (see Manager.pickFallthrough).
func capacityAlternatesFor(src capacityAlternatesSource) autonomous.CapacityAlternatesFn {
	return func(backend, model string) []autonomous.CapacityAlternate {
		if src.Entries == nil {
			return nil
		}
		entries := map[string]inference.LLM{}
		names := []string{}
		for _, e := range src.Entries() {
			if _, dup := entries[e.Name]; dup {
				continue
			}
			entries[e.Name] = e
			names = append(names, e.Name)
		}
		sort.Strings(names)
		caps := map[string]autonomous.LaneCapEntry{}
		if src.Caps != nil {
			for _, c := range src.Caps.LaneCapEntries() {
				caps[c.Backend] = c
			}
		}

		var out []autonomous.CapacityAlternate
		seen := map[autonomous.CapacityAlternate]bool{}
		add := func(altBackend, altModel string) {
			alt := autonomous.CapacityAlternate{Backend: altBackend, Model: altModel}
			if altBackend == "" || (altBackend == backend && altModel == model) || seen[alt] {
				return
			}
			seen[alt] = true
			out = append(out, alt)
		}
		// usable reports whether an alternate may carry model: its entry exists,
		// is enabled, has room under its cap and serves the model.
		usable := func(altBackend, altModel string) bool {
			entry, ok := entries[altBackend]
			if !ok {
				return false
			}
			c, ok := caps[altBackend]
			if !ok || !c.Enabled {
				return false
			}
			if c.Cap > 0 && src.Inflight != nil && src.Inflight(altBackend) >= c.Cap {
				return false
			}
			return entryServesModel(entry, altModel)
		}

		// 1. The lane fill pair.
		if src.LaneFill != nil {
			lf := src.LaneFill()
			claude, gpu := lf.EffectiveClaudeBackend(), lf.EffectiveGPUBackend()
			var altBackend, altModel string
			switch backend {
			case claude:
				altBackend, altModel = gpu, lf.GPUModel
			case gpu:
				altBackend = claude
			}
			if altBackend != "" && altBackend != backend {
				if e, ok := entries[altBackend]; ok {
					if altModel == "" {
						altModel = entryDefaultModel(e)
					}
					if usable(altBackend, altModel) {
						add(altBackend, altModel)
					}
				}
			}
		}

		// 2. Entries the operator marked as allowed fall-through targets.
		for _, name := range names {
			e := entries[name]
			if !e.FallthroughTarget {
				continue
			}
			altModel := entryDefaultModel(e)
			if model != "" && entryServesModel(e, model) {
				altModel = model
			}
			if usable(name, altModel) {
				add(name, altModel)
			}
		}
		return out
	}
}

// entryDefaultModel is the model an LLM entry runs when nothing asks for
// another: its model field, else the first model of its models list, else "".
func entryDefaultModel(l inference.LLM) string {
	if m := strings.TrimSpace(l.Model); m != "" {
		return m
	}
	for _, m := range l.Models {
		if m.Model != "" {
			return m.Model
		}
	}
	return ""
}

// entryServesModel reports whether the entry can run model. An empty model is
// the entry's own default and always served; an entry that lists no models
// says nothing against any model, so it serves any; otherwise the model must be
// on the list, as written or without its provider prefix.
func entryServesModel(l inference.LLM, model string) bool {
	if strings.TrimSpace(model) == "" {
		return true
	}
	if len(l.Models) == 0 {
		return l.Model == "" || l.Model == model || nodeModelName(l.Model) == nodeModelName(model)
	}
	return listsModel(l, model)
}

// ledgerInflight counts the leases that hold a backend's llm: pool, the pool
// the in-flight cap is enforced on.
func ledgerInflight(led *capacity.Ledger) func(backend string) int {
	return func(backend string) int {
		if led == nil {
			return 0
		}
		pool := "llm:" + backend
		n := 0
		for _, l := range led.Leases() {
			for _, p := range l.Pools {
				if p == pool {
					n++
					break
				}
			}
		}
		return n
	}
}

// registryEntries lists the registry's entries as value copies, or none when
// the registry is not available.
func registryEntries(registry func() *inference.Registry) func() []inference.LLM {
	return func() []inference.LLM {
		if registry == nil {
			return nil
		}
		reg := registry()
		if reg == nil {
			return nil
		}
		list := reg.List()
		out := make([]inference.LLM, 0, len(list))
		for _, l := range list {
			if l != nil {
				out = append(out, *l)
			}
		}
		return out
	}
}
