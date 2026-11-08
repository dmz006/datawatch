package autonomous

import (
	"log"
	"strings"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/placementtags"
)

// Placement is where a task runs: which compute node, on that node which
// backend and model, in which lane and role, for which project, and under
// which automation (PRD) in which stage of a kind of work. It is the input
// the placement-tags feature derives tags from; the session side stores only
// the derived tag strings (Session.SystemTags), never the facts.
type Placement struct {
	// Node is the compute node the task's lease was granted on.
	Node string
	// Peer is the federated peer name for work that runs on a remote
	// datawatch peer; empty for work that runs locally.
	Peer    string
	Backend string
	Model   string
	Lane    string // "gpu" | "claude" | ""
	Role    string // "worker" | "verifier" | "planner" | "judge" | ""
	Project string
	// Automation is the Automation the work runs under (empty for work that
	// has none): the fact the custom axes read under "Automation".
	Automation string
	// Automaton is the Automaton (PRD) the work belongs to: the fact the
	// custom axes read under "PRDID".
	Automaton string
	// Stage is the stage of the work (empty when it is not stage-typed).
	Stage string
	// Kind is the kind of the work ("task", "decompose", ...; empty when
	// unset).
	Kind string
}

// ToFacts maps the placement onto the placementtags model's session-facts
// shape, custom axes included. A field left empty simply takes no tag.
func (p Placement) ToFacts() placementtags.PlacementFacts {
	return placementtags.PlacementFacts{
		Compute: p.Node,
		Peer:    p.Peer,
		Backend: p.Backend,
		Lane:    p.Lane,
		Role:    p.Role,
		Project: p.Project,
		// The stage and kind facts ride along when the work has them; a field
		// left empty takes no tag.
		Automaton:  p.Automaton,
		Automation: p.Automation,
		Stage:      p.Stage,
		Kind:       p.Kind,
		Fields: map[string]string{
			"Model":      p.Model,
			"Node":       p.Node,
			"Backend":    p.Backend,
			"Role":       p.Role,
			"Project":    p.Project,
			"PRDID":      p.Automaton,
			"Automaton":  p.Automaton,
			"Automation": p.Automation,
			"Stage":      p.Stage,
			"Kind":       p.Kind,
		},
	}
}

// placementTagsSettings converts the config block onto the placementtags
// derive settings. EffectivePlacement must be applied by the caller, so an
// unset axis still gets its default template.
func placementTagsSettings(pc config.PlacementConfig) placementtags.Settings {
	ax := func(a config.PlacementAxis) placementtags.AxisSetting {
		return placementtags.AxisSetting{Enabled: a.Enabled, Template: a.Template}
	}
	s := placementtags.Settings{
		Compute:    ax(pc.Compute),
		Peer:       ax(pc.Peer),
		Backend:    ax(pc.Backend),
		Lane:       ax(pc.Lane),
		Role:       ax(pc.Role),
		Project:    ax(pc.Project),
		Automaton:  ax(pc.Automaton),
		Automation: ax(pc.Automation),
		Stage:      ax(pc.Stage),
		Kind:       ax(pc.Kind),
	}
	if len(pc.Custom) > 0 {
		s.Custom = make([]placementtags.CustomAxis, 0, len(pc.Custom))
		for _, c := range pc.Custom {
			s.Custom = append(s.Custom, placementtags.CustomAxis{
				Name:     c.Name,
				Field:    c.Field,
				Template: c.Template,
				Enabled:  c.Enabled,
			})
		}
	}
	return s
}

// DerivePlacementTags returns, sorted and deduplicated, the placement tags
// the placement p takes under config block pc (EffectivePlacement applied):
// one tag per enabled axis whose fact is non-empty. With the master switch
// off, or when no axis is enabled, the result is empty.
func DerivePlacementTags(p Placement, pc config.PlacementConfig) []string {
	if !pc.Enabled {
		return nil
	}
	return placementtags.Derive(p.ToFacts(), placementTagsSettings(pc.EffectivePlacement()))
}

// SystemTagsSink replaces one session's system tags with a new set. The
// session manager implements it (session.Manager.SetSystemTags).
type SystemTagsSink interface {
	SetSystemTags(sessionID string, tags []string) error
}

// NodeChooser names the compute node a task is currently placed on. The
// capacity ledger provides it via Manager.heldNode (the lease's node).
type NodeChooser func(taskID string) string

// ApplyPlacementTags replaces sessionID's system tags with the placement
// tags of p under pc. The operator tags on the session are not part of
// either list and are never touched. With the master switch off (or every
// axis disabled) the call still applies: the session's system tags are
// cleared, so a switch flip removes the tags that used to be written.
// p.Node is resolved through node when empty, so a caller that only knows
// the task still lands the tag on the leased node.
func ApplyPlacementTags(sink SystemTagsSink, node NodeChooser, taskID, sessionID string, p Placement, pc config.PlacementConfig) error {
	if sink == nil || sessionID == "" {
		return nil
	}
	if p.Node == "" && node != nil {
		p.Node = node(taskID)
	}
	tags := DerivePlacementTags(p, pc)
	return sink.SetSystemTags(sessionID, tags)
}

// workerLaunchPlacement is the placement fact a task worker carries at
// launch (and on every retry: the same two facts, re-derived, ride the
// re-tag of the same or a new session): the node it landed on, the backend
// and model it runs under, the lane that backend serves, the worker role,
// the kind of work (task or auto-fix, the caller's TaskLaunchKind), the
// project it works in, and the Automaton, Automation and stage the
// work runs under (stage empty when the Automaton is not stage-typed).
func (m *Manager) workerLaunchPlacement(prd *PRD, placedNode, backend, model, project, kind string) Placement {
	return Placement{
		Node:       placedNode,
		Backend:    backend,
		Model:      model,
		Lane:       laneForBackend(backend),
		Role:       string(capacity.RoleWorker),
		Project:    project,
		Automaton:  prd.ID,
		Automation: prd.AutomationID,
		Stage:      m.StageOfPRD(prd),
		Kind:       kind,
	}
}

// autoFixTaskKind is the launch kind an auto-fix worker session carries —
// the "auto-fix" member of the server's launch-kind vocabulary (see
// validateSessionLaunch in internal/server/session_launch_tags.go), so a
// fix attempt reads as auto-fix on the Automata page, not as an ordinary
// task.
const autoFixTaskKind = "auto-fix"

// autoFixTaskTitlePrefix is the title prefix the synthesized corrective
// tasks carry (synthesizeCorrectiveWorkAttempt), which is what marks a
// freshly spawned corrective task as an auto-fix launch.
const autoFixTaskTitlePrefix = "Auto-fix: "

// TaskLaunchKind is the launch kind a task worker session carries:
// "auto-fix" when the launch is an auto-fix attempt — a re-run of a
// previous failed attempt that carries the fix context (retryHint), or a
// synthesized corrective task the content auto-fix created — and "task" on
// a genuine first run of a new task. The value stays inside the server's
// launch-kind vocabulary.
func TaskLaunchKind(t *Task, retryHint string) string {
	if strings.TrimSpace(retryHint) != "" {
		return autoFixTaskKind
	}
	if t != nil && strings.HasPrefix(t.Title, autoFixTaskTitlePrefix) {
		return autoFixTaskKind
	}
	return "task"
}

// laneForBackend names the execution lane a worker backend serves, as far
// as the placement side can tell from the backend alone: the local/LLM
// backends ride the gpu lane, the Claude backend rides the claude lane.
func laneForBackend(backend string) string {
	switch backend {
	case "claude":
		return string(LaneTypeClaude)
	case "pi", "pi-prompt", "ollama", "vllm":
		return string(LaneTypeGPU)
	default:
		return ""
	}
}

// TagTaskPlacement replaces the session's system tags with the placement
// tags of where the task was placed. Best-effort: a sink or settings source
// that is not wired (tests, capacity off) is a no-op, and a write failure
// only logs — the session has already started and must not lose its task.
// When the placement master switch is off this clears the session's system
// tags instead of leaving stale ones.
//
// Node and peer names resolve per task. node: comes from the capacity
// ledger (the node the task's lease landed on — a ledger value, not an
// inference). peer: comes from the placement record only — the explicit
// Placement.Peer field when the placing code named it, or the wired peer
// source when the record leaves it unset — and is never inferred from DNS,
// an IP address or the machine's hostname (PlacementPeer,
// placement_tags_peer.go). Local work names no peer and gets no peer tag.
func (m *Manager) TagTaskPlacement(taskID, sessionID string, p Placement) {
	// peer axis: the explicit record wins; the wired source only fills an
	// unset record. A nil source is the peer placement build's placeholder
	// today: record-only, local work gets no peer tag.
	if name := PlacementPeer(p, m.peerChooserFunc(), taskID); name != "" {
		p.Peer = name
	}
	m.mu.Lock()
	sink, settings := m.placementTagsSink, m.placementTagsSettings
	m.mu.Unlock()
	if sink == nil || settings == nil {
		return
	}
	if err := ApplyPlacementTags(sink, m.heldNodeFunc(), taskID, sessionID, p, settings()); err != nil {
		log.Printf("[autonomous] placement tags: task=%s session=%s: %v", taskID, sessionID, err)
	}
}

// SetPlacementTagSink wires the sink TagTaskPlacement writes the placement
// tags through (session.Manager implements it). Wired by the daemon; nil
// clears the placement-tag path.
func (m *Manager) SetPlacementTagSink(s SystemTagsSink) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.placementTagsSink = s
}

// SetPlacementTagSettings wires the source of the tags.placement config
// block the placement tags are derived from. It is called on every
// placement so an operator edit (API, CLI or config reload) takes effect on
// the next task without a restart. nil clears the placement-tag path.
func (m *Manager) SetPlacementTagSettings(f func() config.PlacementConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.placementTagsSettings = f
}

// SetPlacementPeerSource wires the source of the peer axis for the placement
// tags (placement_tags_peer.go): the peer placement build names the federated
// peer a task is placed on, "" for local work. A correct source returns only
// an explicit registered peer name — never one resolved by DNS, an IP
// address or the machine's hostname (explicit field over inference). An
// explicit peer already on the Placement record always wins over the source.
// Unset (nil) means the peer must come from the placement record, which is
// the correct behavior for today's local-only placements. Guarded by mu.
func (m *Manager) SetPlacementPeerSource(s PeerSource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.placementPeerSource = s
}

// peerChooserFunc wraps the wired peer source as PlacementPeer's argument.
// A nil source stays nil so PlacementPeer reports no peer for local work.
func (m *Manager) peerChooserFunc() PeerSource {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.placementPeerSource
}

// heldNodeFunc wraps Manager.heldNode as the placement NodeChooser: the
// capacity ledger is the source of truth for which node the task's lease
// was granted on.
func (m *Manager) heldNodeFunc() NodeChooser {
	return m.heldNode
}
