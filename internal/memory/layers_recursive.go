// BL96 — wake-up stack extension for F10 recursive/nested agents.
//
// The 4-layer stack (L0..L3) was designed for single-host sessions.
// F10 multi-agent scenarios (spawned children, peer workers) need
// two more layers + a per-agent identity:
//
//   L4: Parent agent's working context — what the spawning agent was
//       doing, files touched, decisions in flight, the task it
//       handed off. Loaded once when a child boots so it doesn't
//       re-derive context the parent already knows.
//   L5: Peer-agent visibility — which sibling agents are currently
//       running for the same parent, on which branches/tasks. Lets
//       a worker pick up where a sibling left off, avoid duplicate
//       effort, and recognise when a peer's result becomes useful.
//
// Per-agent L0 identity (today identity.txt is host-wide) is overlaid
// from <data_dir>/agents/<agent_id>/identity.txt when present;
// missing falls through to the host file. Lets each spawned agent
// announce itself differently ("I'm the validator for spawn X")
// without affecting the host's own identity.

package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PeerAgent is the narrow shape Layers needs from agents.Manager
// so the memory pkg doesn't have to import agents (avoid cycle).
type PeerAgent struct {
	ID            string
	ProjectName   string
	ClusterName   string
	Branch        string
	Task          string
	State         string
	ParentAgentID string
	CreatedAt     time.Time
}

// PeerLister returns the agents currently tracked. Implemented by an
// adapter wrapping agents.Manager.List() in main.
type PeerLister interface {
	ListAgents() []PeerAgent
}

// SetPeerLister wires the agent-list source. Optional — when nil,
// L5 returns "" (single-host operation, no peer visibility).
func (l *Layers) SetPeerLister(p PeerLister) { l.peers = p }

// L0ForAgent returns the per-agent identity overlay when present,
// else the host-wide L0. The overlay file lives at
// <data_dir>/agents/<agent_id>/identity.txt and is operator- (or
// orchestrator-) generated when an agent has a specialised role.
func (l *Layers) L0ForAgent(agentID string) string {
	if agentID == "" {
		return l.L0()
	}
	overlayPath := filepath.Join(l.dataDir, "agents", agentID, "identity.txt")
	data, err := os.ReadFile(overlayPath)
	if err != nil || len(data) == 0 {
		return l.L0()
	}
	return strings.TrimSpace(string(data))
}

// L4 returns the parent agent's working context: recent memories
// written under the parent's namespace (memories the spawning agent
// captured before issuing the spawn). Bounded by maxChars so a long-
// running parent's context doesn't blow the worker's wake budget.
//
// parentNamespace is typically `parent.EffectiveNamespace()` —
// derived from the parent profile, not the parent agent ID. Empty
// returns "" (top-level spawn, no parent to inherit from).
func (l *Layers) L4(parentNamespace string, maxChars int) string {
	if parentNamespace == "" {
		return ""
	}
	if maxChars <= 0 {
		maxChars = 2000
	}
	nb, ok := l.retriever.store.(NamespacedBackend)
	if !ok {
		return ""
	}
	// Build a synthetic query vector by re-using the embed surface
	// the retriever already has — but namespace search needs a
	// query, so use the parent namespace name as the query string
	// (returns the most recent + most relevant entries in the
	// parent's bucket).
	memories, err := l.retriever.RecallInNamespaces(parentNamespace, []string{parentNamespace})
	if err != nil || len(memories) == 0 {
		_ = nb // silence unused-when-empty
		return ""
	}

	var b strings.Builder
	b.WriteString("## Parent agent context\n")
	totalChars := b.Len()
	for _, m := range memories {
		line := fmt.Sprintf("- [%s] %s\n", m.Role, truncateStr(m.Content, 200))
		if totalChars+len(line) > maxChars {
			break
		}
		b.WriteString(line)
		totalChars += len(line)
	}
	if b.Len() <= len("## Parent agent context\n") {
		return ""
	}
	return b.String()
}

// L5 returns a brief on the worker's siblings — other agents that
// share the supplied parentAgentID. Self (selfID) is filtered out.
// Returns "" when no PeerLister is wired or no siblings exist.
func (l *Layers) L5(selfID, parentAgentID string) string {
	if l.peers == nil || parentAgentID == "" {
		return ""
	}
	var sibs []PeerAgent
	for _, a := range l.peers.ListAgents() {
		if a.ID == selfID || a.ParentAgentID != parentAgentID {
			continue
		}
		sibs = append(sibs, a)
	}
	if len(sibs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Sibling workers\n")
	for _, s := range sibs {
		fmt.Fprintf(&b, "- %s  state=%s  branch=%s  task=%s\n",
			truncateStr(s.ID, 12), s.State,
			truncateStr(s.Branch, 30),
			truncateStr(s.Task, 60))
	}
	return b.String()
}

// WakeUpContextForAgent extends WakeUpContext with the L4 + L5
// layers. selfID is this worker's agent ID; parentAgentID + parent
// namespace are zero-value for top-level spawns (which fall back to
// the host wake-up).
func (l *Layers) WakeUpContextForAgent(selfID, parentAgentID, parentNamespace, projectDir string) string {
	var parts []string

	if id := l.L0ForAgent(selfID); id != "" {
		parts = append(parts, "## Identity\n"+id)
	}
	if l1 := l.L1(projectDir, 2000); l1 != "" {
		parts = append(parts, "## Key Facts\n"+l1)
	}
	if l4 := l.L4(parentNamespace, 2000); l4 != "" {
		parts = append(parts, l4) // already includes its header
	}
	if l5 := l.L5(selfID, parentAgentID); l5 != "" {
		parts = append(parts, l5)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}
