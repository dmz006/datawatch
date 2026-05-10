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

// NodeKind enumerates the LLM-API protocol the ComputeNode speaks.
//
// v7.0.0-alpha.23 (#245 follow-up): Operator-corrected 2026-05-09 —
// Kind was previously a deployment-shape enum (local/remote/ssh/docker/
// k8s/remote-proxy) which mixed two concerns: the API the daemon talks
// to AND how the daemon gets to the host. The deployment dimension
// moves to a future "Routing" field (see docs/plans/post-v7-routing.md).
// Kind now means strictly: "what API protocol does the LLM endpoint
// at <address> speak?" Only the protocols datawatch directly supports
// are exposed; the rest live in docs/plans/post-v7-llm-kinds.md as
// roadmap.
//
// Deprecated values still parse (Validate() accepts them so existing
// nodes.json reads without crashing) but are not selectable in the
// PWA dropdown. PWA shows a one-time migration banner that asks the
// operator to re-pick a supported Kind per affected Node.
type NodeKind string

const (
	// Currently supported (selectable in PWA).
	KindOllama       NodeKind = "ollama"        // Ollama HTTP API at <address>:11434
	KindOpenAICompat NodeKind = "openai-compat" // OpenAI-compatible /v1/chat/completions (covers OpenWebUI, vLLM, LMStudio, llama.cpp server, OpenAI itself)

	// Deprecated (alpha.23) — still parse for back-compat; flagged for migration.
	KindLocal       NodeKind = "local"        // pre-alpha.23: same host as daemon. Migration: pick ollama or openai-compat per node.
	KindSSH         NodeKind = "ssh"          // pre-alpha.23: reachable via ssh. Migration: SSH isn't an LLM API; re-pick.
	KindDocker      NodeKind = "docker"       // pre-alpha.23: local docker container. Migration: re-pick by what the container serves.
	KindK8s         NodeKind = "k8s"          // pre-alpha.23: k8s pod. Migration: re-pick.
	KindRemote      NodeKind = "remote"       // pre-alpha.23: remote host. Migration: re-pick.
	KindRemoteProxy NodeKind = "remote-proxy" // pre-alpha.23: remote datawatch instance. Migration: re-pick.
)

// SupportedKinds is the set the PWA dropdown exposes. AllKinds (below)
// keeps the deprecated values too for Validate() back-compat.
var SupportedKinds = []NodeKind{KindOllama, KindOpenAICompat}

// AllKinds is the validation set (supported + deprecated).
var AllKinds = []NodeKind{
	KindOllama, KindOpenAICompat,
	KindLocal, KindSSH, KindDocker, KindK8s, KindRemote, KindRemoteProxy,
}

// IsDeprecated reports whether k is one of the pre-alpha.23 deployment
// kinds that the operator must migrate away from. Used by the PWA
// migration banner + the dispatcher's "refuse to route" gate.
func (k NodeKind) IsDeprecated() bool {
	switch k {
	case KindLocal, KindSSH, KindDocker, KindK8s, KindRemote, KindRemoteProxy:
		return true
	}
	return false
}

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
	// AutoTags are daemon-applied internal markers (e.g. migration
	// audit, auto-link source). v7.0.0-alpha.23 (Q7): PWA strips these
	// from the user-visible tag list. Daemon-internal lookups can
	// union Tags + AutoTags when needed.
	AutoTags []string `yaml:"auto_tags,omitempty" json:"auto_tags,omitempty"`

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

	// Hardware spec (v7.0.0-alpha.19 #245). Optional + auto-detect:
	// datawatch-stats sidecar fills these on first push. Operator can
	// also set manually via the Edit popup. Empty fields don't block
	// dispatcher routing — they just hide the Compute filter UI's
	// capability options until populated.
	//
	// Operator-spec'd 2026-05-09: "filter for compute node type — need
	// to know i have linux with blackwell nvidia or arm on thor nvidia
	// blackwell and memory size, number cpu/cores".
	Hardware HardwareSpec `yaml:"hardware,omitempty" json:"hardware,omitempty"`

	// Kind-specific connection details (alpha.19). Each kind reads only
	// the fields it cares about; others are ignored. PWA Add/Edit
	// popup shows the right subset based on Kind.
	SSH    *SSHConfig    `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	Docker *DockerConfig `yaml:"docker,omitempty" json:"docker,omitempty"`
	K8s    *K8sConfig    `yaml:"k8s,omitempty" json:"k8s,omitempty"`

	// v7.0.0-alpha.23 (Q6) — operator-set on/off via PWA sliding switch.
	// Inverse-bool semantics matching LLM.Disabled (zero value = enabled
	// for back-compat with existing nodes.json).
	Disabled bool `yaml:"disabled,omitempty" json:"disabled,omitempty"`

	// LastDispatchError is the most-recent dispatcher refusal reason
	// (empty when healthy). Surfaced as the !-badge tooltip on the PWA
	// switch (Q6). Populated by the dispatcher on per-Node failure.
	LastDispatchError string `yaml:"last_dispatch_error,omitempty" json:"last_dispatch_error,omitempty"`

	// Bookkeeping.
	CreatedAt   time.Time `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt   time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
	AutoCreated bool      `yaml:"auto_created,omitempty" json:"auto_created,omitempty"`
}

// HardwareSpec captures auto-detectable + operator-overridable hardware
// info. v7.0.0-alpha.19 #245.
type HardwareSpec struct {
	OS          string `yaml:"os,omitempty" json:"os,omitempty"`                     // linux | macos | windows
	Arch        string `yaml:"arch,omitempty" json:"arch,omitempty"`                 // x86_64 | arm64 | aarch64
	GPUVendor   string `yaml:"gpu_vendor,omitempty" json:"gpu_vendor,omitempty"`     // nvidia | amd | intel | apple | none | other
	GPUPlatform string `yaml:"gpu_platform,omitempty" json:"gpu_platform,omitempty"` // jetson-thor | grace-hopper | cloud-h100 | bare-metal | desktop | …
	GPUModel    string `yaml:"gpu_model,omitempty" json:"gpu_model,omitempty"`       // e.g. "blackwell", "h100", "rtx-4090", "m3-max"
	GPUCount    int    `yaml:"gpu_count,omitempty" json:"gpu_count,omitempty"`       // number of GPUs
	MemoryGB    int    `yaml:"memory_gb,omitempty" json:"memory_gb,omitempty"`       // total system RAM in GB
	CPUCores    int    `yaml:"cpu_cores,omitempty" json:"cpu_cores,omitempty"`       // logical CPU cores
}

// SSHConfig — kind=ssh connection params.
type SSHConfig struct {
	Host    string `yaml:"host" json:"host"`
	Port    int    `yaml:"port,omitempty" json:"port,omitempty"`         // default 22
	User    string `yaml:"user,omitempty" json:"user,omitempty"`         // default current user
	KeyPath string `yaml:"key_path,omitempty" json:"key_path,omitempty"` // path to private key
}

// DockerConfig — kind=docker connection params.
type DockerConfig struct {
	Endpoint    string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`         // default unix:///var/run/docker.sock
	ImagePrefix string `yaml:"image_prefix,omitempty" json:"image_prefix,omitempty"` // optional registry/namespace prefix
}

// K8sConfig — kind=k8s connection params. Cluster MUST reference an
// existing Cluster Profile (operator-spec'd Q2: dropdown only).
type K8sConfig struct {
	ClusterProfile string `yaml:"cluster_profile" json:"cluster_profile"`             // ClusterProfile name (required)
	Namespace      string `yaml:"namespace,omitempty" json:"namespace,omitempty"`     // default "default"
	ServiceAccount string `yaml:"service_account,omitempty" json:"service_account,omitempty"`
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
// v7.0.0-alpha.23: Kind defaults to KindOllama (safe assumption for
// observed peers — they're typically running ollama). Operator can
// re-pick via the form. Shape hint kept as a tag marker.
func AutoCreatedFromStatsPeer(peerName, peerAddr, shape string) *Node {
	now := time.Now().UTC()
	autoTags := []string{"v7-cfg-migration"}
	if strings.EqualFold(shape, "C") {
		autoTags = append(autoTags, "shape:cluster")
	}
	return &Node{
		Name:    peerName,
		Kind:    KindOllama,
		Address: peerAddr,
		DeclaredCapacity: DeclaredCapacity{
			MaxConcurrentModels: 1, // safe default; operator bumps for clusters
		},
		SchedulingPriority: 50,
		AutoCreated:        true,
		AutoTags:           autoTags, // alpha.23 Q7: PWA strips from display
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

// MigrateAutoTags is a one-time daemon-startup pass: any tag whose
// value is in the historical auto-applied set moves from Tags to
// AutoTags so the PWA stops showing it. Returns the count of nodes
// touched. Idempotent.
//
// v7.0.0-alpha.23 Q7: PWA renders only Tags; AutoTags hidden.
var historicalAutoTags = map[string]bool{
	"v7-cfg-migration": true,
}

func (r *Registry) MigrateAutoTags() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	touched := 0
	for _, n := range r.nodes {
		var keep []string
		moved := false
		for _, t := range n.Tags {
			if historicalAutoTags[t] {
				if !containsTag(n.AutoTags, t) {
					n.AutoTags = append(n.AutoTags, t)
				}
				moved = true
				continue
			}
			keep = append(keep, t)
		}
		if moved {
			n.Tags = keep
			n.UpdatedAt = time.Now().UTC()
			touched++
		}
	}
	if touched > 0 {
		_ = r.persistLocked()
	}
	return touched
}

func containsTag(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
