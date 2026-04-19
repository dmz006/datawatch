// BL112 — service-mode reconciler.
//
// S8.2 ships ProjectProfile.Mode + the idle-reaper exemption for
// service-mode workers. BL112 wires the parent's startup pass that
// re-attaches them after a restart: walks every Driver that
// implements Discovery, asks for instances labelled
// `datawatch.role=agent-worker`, looks each one up in the project
// store, and reconstructs the in-memory Agent record when the
// profile.Mode == "service".
//
// Ephemeral workers are not reconstructed — they're meant to die
// with the parent. The reconciler logs them as "orphan; will be
// terminated" so the operator can audit + clean up; an opt-in
// "reap_orphan_ephemerals" config knob (BL112-followup) could then
// auto-call driver.Terminate.

package agents

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

// ErrDiscoveryUnsupported is the sentinel a Driver returns when it
// cannot enumerate by label. Reconciler logs + skips.
var ErrDiscoveryUnsupported = errors.New("driver does not support label discovery")

// ReconcileResult summarises one reconcile pass.
type ServiceReconcileResult struct {
	// Reattached is the list of agent IDs whose service-mode workers
	// were re-attached to the in-memory registry.
	Reattached []string `json:"reattached"`
	// Orphans is the list of running instances that don't match a
	// service-mode profile in the store. Includes both genuinely
	// orphaned ephemeral workers AND service workers whose profile
	// has been deleted since the spawn.
	Orphans []string `json:"orphans"`
	// SkippedKinds is the list of cluster kinds whose driver doesn't
	// support label discovery.
	SkippedKinds []string `json:"skipped_kinds,omitempty"`
	// Errors collects per-driver discovery failures so one bad
	// kubectl context doesn't abort the whole pass.
	Errors []string `json:"errors,omitempty"`
}

// ReconcileServiceMode walks every registered driver, asks for
// labelled instances, and re-attaches those whose project profile
// has Mode == "service". The pass is read-mostly: it does NOT
// terminate orphan ephemerals (operator-driven cleanup; the
// `service reconcile apply` orphan-prune is a follow-up).
//
// Safe to call repeatedly — an instance whose AgentID already exists
// in the registry is skipped silently.
func (m *Manager) ReconcileServiceMode(ctx context.Context) (*ServiceReconcileResult, error) {
	res := &ServiceReconcileResult{}
	if m.projects == nil {
		return res, nil
	}

	selector := map[string]string{"datawatch.role": "agent-worker"}

	// Snapshot the driver map + known agent IDs so the reconcile
	// pass doesn't race a concurrent RegisterDriver / Spawn call.
	m.mu.Lock()
	drivers := make(map[string]Driver, len(m.drivers))
	for k, d := range m.drivers {
		drivers[k] = d
	}
	known := make(map[string]bool, len(m.agents))
	for id := range m.agents {
		known[id] = true
	}
	m.mu.Unlock()

	// Build the per-kind cluster list so each driver runs against
	// every cluster it might host workers in. Docker has no per-
	// cluster surface in practice (one daemon per host) — we still
	// pass the first matching cluster profile for label parity.
	clustersByKind := map[string][]*profile.ClusterProfile{}
	if m.clusters != nil {
		for _, c := range m.clusters.List() {
			clustersByKind[string(c.Kind)] = append(clustersByKind[string(c.Kind)], c)
		}
	}

	// Iterate kinds in deterministic order so the audit trail is reproducible.
	kinds := make([]string, 0, len(drivers))
	for k := range drivers {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)

	scan := func(kind string, disc Discovery, cluster *profile.ClusterProfile) []DiscoveredInstance {
		insts, err := disc.ListLabelled(ctx, cluster, selector)
		if err != nil {
			label := kind
			if cluster != nil {
				label = kind + "/" + cluster.Name
			}
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", label, err))
			return nil
		}
		return insts
	}

	for _, kind := range kinds {
		disc, ok := drivers[kind].(Discovery)
		if !ok {
			res.SkippedKinds = append(res.SkippedKinds, kind)
			continue
		}
		var insts []DiscoveredInstance
		clusters := clustersByKind[kind]
		switch {
		case len(clusters) == 0:
			// No registered cluster for this kind — call once with nil
			// (Docker path).
			insts = scan(kind, disc, nil)
		default:
			for _, c := range clusters {
				insts = append(insts, scan(kind, disc, c)...)
			}
		}
		for _, inst := range insts {
			if inst.AgentID == "" {
				continue // not a datawatch-spawned worker (no agent_id label)
			}
			if known[inst.AgentID] {
				continue // already in registry — restart was clean
			}
			proj, _ := m.projects.Get(inst.ProjectProfile)
			if proj == nil || proj.Mode != "service" {
				res.Orphans = append(res.Orphans, inst.AgentID)
				continue
			}

			cluster, err := m.clusters.Get(inst.ClusterProfile)
			if err != nil || cluster == nil {
				res.Errors = append(res.Errors,
					fmt.Sprintf("agent %s: cluster profile %q not found",
						inst.AgentID, inst.ClusterProfile))
				continue
			}

			a := &Agent{
				ID:             inst.AgentID,
				ProjectProfile: inst.ProjectProfile,
				ClusterProfile: inst.ClusterProfile,
				Branch:         inst.Branch,
				ParentAgentID:  inst.ParentAgentID,
				DriverInstance: inst.DriverInstance,
				ContainerAddr:  inst.Addr,
				State:          StateReady, // running + reattached
				CreatedAt:      time.Now().UTC(),
				ReadyAt:        time.Now().UTC(),
				LastActivityAt: time.Now().UTC(),
				project:        proj,
				cluster:        cluster,
			}
			m.mu.Lock()
			m.agents[a.ID] = a
			m.mu.Unlock()
			res.Reattached = append(res.Reattached, a.ID)
			emit(m.Auditor, "service_reattach", a.ID,
				inst.ProjectProfile, inst.ClusterProfile, string(StateReady),
				"reattached after parent restart", nil)
		}
	}

	sort.Strings(res.Reattached)
	sort.Strings(res.Orphans)
	sort.Strings(res.SkippedKinds)
	return res, nil
}
