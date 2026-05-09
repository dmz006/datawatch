// Package compute — v7.0.0 ComputeNode registry.
//
// A ComputeNode is anywhere local LLM workloads run: a host, a GPU
// box, a cluster behind a load balancer, a containerized runtime, a
// remote-proxied datawatch peer. Each Node has identity, declared
// capacity, monitoring, RBAC, scheduling priority, costs, and
// maintenance windows. The LLM registry (S2) references Nodes by
// name; consumers (Council, /api/ask, BL297 wizard, agent spawn,
// session spawning) call LLMs which dispatch to Nodes via ordered
// failover.
//
// Operator-decided 2026-05-08 (BL295 design interview Q12):
//   name = ComputeNode (not "GPU-Resource", not "Pool")
//
// Operator-decided 2026-05-08 (BL295 design interview Q13):
//   Rich schema: name, address, kind, monitoring_endpoint,
//   declared_capacity, tags, costs, permissions,
//   scheduling_priority, maintenance_windows.
//
// Operator-decided 2026-05-08 (BL295 design interview ASK 23):
//   Reuse existing cmd/datawatch-stats as the monitoring stub. A
//   stats peer push can auto-create its ComputeNode if one doesn't
//   exist yet (kind=remote, sensible defaults; operator edits later).
//
// See docs/plans/2026-05-08-v7.0.0-plan.md § 5 S1.

package compute

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// NodeKind enumerates how the daemon reaches the ComputeNode.
type NodeKind string

const (
	KindLocal       NodeKind = "local"        // same host as the daemon
	KindSSH         NodeKind = "ssh"          // reachable via ssh
	KindDocker      NodeKind = "docker"       // local docker container
	KindK8s         NodeKind = "k8s"          // k8s pod (in-cluster)
	KindRemote      NodeKind = "remote"       // remote host running datawatch-stats; reached via the address field
	KindRemoteProxy NodeKind = "remote-proxy" // remote datawatch instance acting as a proxy for downstream Nodes
)

// AllKinds is the set used by validation + UI dropdowns.
var AllKinds = []NodeKind{KindLocal, KindSSH, KindDocker, KindK8s, KindRemote, KindRemoteProxy}

// DeclaredCapacity is the operator-stated upper bound on what the
// Node can run. Real-time capacity comes from the monitoring stub's
// latest snapshot (live polling lands in S1 too).
type DeclaredCapacity struct {
	GPUs                 int    `yaml:"gpus,omitempty" json:"gpus,omitempty"`
	GPUMemGB             int    `yaml:"gpu_mem_gb,omitempty" json:"gpu_mem_gb,omitempty"`
	RAMGB                int    `yaml:"ram_gb,omitempty" json:"ram_gb,omitempty"`
	MaxConcurrentModels  int    `yaml:"max_concurrent_models,omitempty" json:"max_concurrent_models,omitempty"`
	GPUVendor            string `yaml:"gpu_vendor,omitempty" json:"gpu_vendor,omitempty"`             // "nvidia" / "amd" / "intel" / ""
	GPUModel             string `yaml:"gpu_model,omitempty" json:"gpu_model,omitempty"`
}

// MaintenanceWindow is one operator-declared blackout. Scheduler
// (future) will avoid placing fresh workloads on a Node during a
// window. Open-ended (To zero) means "from now until manually
// closed".
type MaintenanceWindow struct {
	From   time.Time `yaml:"from" json:"from"`
	To     time.Time `yaml:"to,omitempty" json:"to,omitempty"`
	Reason string    `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// Permissions controls which consumer kinds can place workloads on
// this Node. Empty AllowedConsumers means "all consumers"; presence
// in DeniedConsumers always wins.
//
// Consumer names: "council", "agent_spawn", "ask", "session_spawn".
// Wildcard "*" matches any.
type Permissions struct {
	AllowedConsumers []string `yaml:"allowed_consumers,omitempty" json:"allowed_consumers,omitempty"`
	DeniedConsumers  []string `yaml:"denied_consumers,omitempty" json:"denied_consumers,omitempty"`
}

// Node is one ComputeNode entry in the registry.
type Node struct {
	// Identity.
	Name    string   `yaml:"name" json:"name"`
	Kind    NodeKind `yaml:"kind" json:"kind"`
	Address string   `yaml:"address,omitempty" json:"address,omitempty"`

	// Monitoring stub coordinates. The stub (datawatch-stats) pushes
	// metrics to the daemon; MonitoringEndpoint is the optional
	// sidecar URL the daemon can pull from for on-demand drill-down
	// (e.g. "https://gpu-1:9001/api/stats").
	MonitoringEndpoint string `yaml:"monitoring_endpoint,omitempty" json:"monitoring_endpoint,omitempty"`

	// Capacity.
	DeclaredCapacity DeclaredCapacity `yaml:"declared_capacity,omitempty" json:"declared_capacity,omitempty"`

	// Operator-supplied taxonomy.
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`

	// Cost accounting. CostPerHour is in USD; for SaaS-fronting
	// Nodes prefer LLM-side cost_per_1k_tokens (S2 LLM registry).
	CostPerHour float64 `yaml:"cost_per_hour,omitempty" json:"cost_per_hour,omitempty"`

	// RBAC.
	Permissions Permissions `yaml:"permissions,omitempty" json:"permissions,omitempty"`

	// Scheduler hint: 0..100, higher = preferred when multiple
	// Nodes are eligible for the same workload. Default 50.
	SchedulingPriority int `yaml:"scheduling_priority,omitempty" json:"scheduling_priority,omitempty"`

	// Operator-declared blackouts.
	MaintenanceWindows []MaintenanceWindow `yaml:"maintenance_windows,omitempty" json:"maintenance_windows,omitempty"`

	// Bookkeeping.
	CreatedAt   time.Time `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt   time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
	AutoCreated bool      `yaml:"auto_created,omitempty" json:"auto_created,omitempty"`
}

// Validate returns the first reason this Node is malformed, or nil.
// Called by registry CRUD before persisting.
func (n *Node) Validate() error {
	if strings.TrimSpace(n.Name) == "" {
		return errors.New("compute node: name required")
	}
	if !validNodeName(n.Name) {
		return fmt.Errorf("compute node: invalid name %q (use [a-z0-9._-]+ kebab-case)", n.Name)
	}
	if n.Kind == "" {
		return errors.New("compute node: kind required")
	}
	if !validKind(n.Kind) {
		return fmt.Errorf("compute node: unknown kind %q (allowed: %v)", n.Kind, AllKinds)
	}
	if n.Kind == KindRemote || n.Kind == KindRemoteProxy || n.Kind == KindSSH {
		if strings.TrimSpace(n.Address) == "" {
			return fmt.Errorf("compute node: kind %q requires address", n.Kind)
		}
	}
	if n.SchedulingPriority < 0 || n.SchedulingPriority > 100 {
		return fmt.Errorf("compute node: scheduling_priority %d out of range [0,100]", n.SchedulingPriority)
	}
	if n.DeclaredCapacity.GPUs < 0 || n.DeclaredCapacity.GPUMemGB < 0 ||
		n.DeclaredCapacity.RAMGB < 0 || n.DeclaredCapacity.MaxConcurrentModels < 0 {
		return errors.New("compute node: declared_capacity fields must be >= 0")
	}
	for i, w := range n.MaintenanceWindows {
		if w.From.IsZero() {
			return fmt.Errorf("compute node: maintenance_windows[%d].from is required", i)
		}
		if !w.To.IsZero() && w.To.Before(w.From) {
			return fmt.Errorf("compute node: maintenance_windows[%d].to is before .from", i)
		}
	}
	return nil
}

// InMaintenance returns true if any window contains now (or is open
// from a past From with zero To).
func (n *Node) InMaintenance(now time.Time) bool {
	for _, w := range n.MaintenanceWindows {
		if w.From.After(now) {
			continue
		}
		if w.To.IsZero() || !w.To.Before(now) {
			return true
		}
	}
	return false
}

// AllowsConsumer applies the RBAC matrix for the named consumer.
// Empty AllowedConsumers means "all"; explicit Denied always wins.
func (n *Node) AllowsConsumer(consumer string) bool {
	consumer = strings.ToLower(strings.TrimSpace(consumer))
	for _, d := range n.Permissions.DeniedConsumers {
		if matchConsumer(d, consumer) {
			return false
		}
	}
	if len(n.Permissions.AllowedConsumers) == 0 {
		return true
	}
	for _, a := range n.Permissions.AllowedConsumers {
		if matchConsumer(a, consumer) {
			return true
		}
	}
	return false
}

func matchConsumer(pattern, consumer string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "*" || pattern == consumer {
		return true
	}
	return false
}

func validNodeName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func validKind(k NodeKind) bool {
	for _, x := range AllKinds {
		if x == k {
			return true
		}
	}
	return false
}

// AutoCreatedFromStatsPeer derives a sensible default Node when a
// datawatch-stats peer pushes for the first time and no matching
// Node exists yet. Operator can edit later via REST/MCP/CLI/comm/UI.
//
// peerName  → Node.Name
// peerAddr  → Node.Address (from the http.Request RemoteAddr)
// shape     → "B" → KindRemote; "C" → KindK8s; otherwise KindRemote
func AutoCreatedFromStatsPeer(peerName, peerAddr, shape string) *Node {
	kind := KindRemote
	if strings.EqualFold(shape, "C") {
		kind = KindK8s
	}
	now := time.Now().UTC()
	return &Node{
		Name:    peerName,
		Kind:    kind,
		Address: peerAddr,
		DeclaredCapacity: DeclaredCapacity{
			MaxConcurrentModels: 1, // safe default; operator bumps for clusters
		},
		SchedulingPriority: 50,
		AutoCreated:        true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}
