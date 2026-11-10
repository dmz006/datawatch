// api_dashboard_work_map.go — the work map data endpoint:
// GET /api/dashboard/work-map.
//
// The work map is a graph of what is working where: one node per project,
// Automation, Automaton, task and live session, with an edge for every
// parent→child link. It is built from the two read surfaces the daemon
// already keeps:
//
//   - the trace roll-up (internal/trace): the Automaton, task and session
//     nodes plus their events/error counts, and the task→Automaton and
//     task→session links;
//   - the session placement tags (compute:*, lane:*, role:*, project:*,
//     automation:*, automaton:*): the node/lane/role of every session and
//     the project/Automation containers.
//
// Tasks are capped per Automaton (the newest first, the cap reported in
// the Automaton node's tasks_omitted) so a long-lived Automaton cannot
// flood the card. The response also carries a "running" list — the live
// sessions in an active state, newest first — which the PWA card rotates
// under the graph.
//
// The endpoint is a read-only view: a missing trace store or session
// manager degrades the graph (trace:false, fewer nodes) instead of
// failing, and only a daemon with neither subsystem is a 503.
//
//	GET /api/dashboard/work-map   dashboard:read
package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/trace"
)

// workMapMaxTasksPerAutomaton caps the task nodes attached to one
// Automaton (newest first); the overflow is counted in the Automaton
// node's tasks_omitted field.
const workMapMaxTasksPerAutomaton = 8

// Node kinds, in the parent→child order of the map.
const (
	workMapKindProject    = "project"
	workMapKindAutomation = "automation"
	workMapKindAutomaton  = "automaton"
	workMapKindTask       = "task"
	workMapKindSession    = "session"
)

// workMapNode is one vertex of the work map. Node, Lane and Role are the
// session placement axes (compute, lane, role) the vertex is known to
// run in; Parent is the id of the vertex it hangs under ("" for roots).
// Events and Errors come from the trace roll-up; TasksOmitted is the
// overflow beyond the per-Automaton task cap.
type workMapNode struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Label        string `json:"label"`
	Status       string `json:"status"`
	Node         string `json:"node,omitempty"`
	Lane         string `json:"lane,omitempty"`
	Role         string `json:"role,omitempty"`
	Parent       string `json:"parent,omitempty"`
	Events       int    `json:"events,omitempty"`
	Errors       int    `json:"errors,omitempty"`
	TasksOmitted int    `json:"tasks_omitted,omitempty"`
}

// workMapEdge is one parent→child link.
type workMapEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// workMapRunning is one live session for the card's rotation strip.
type workMapRunning struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	State string `json:"state"`
	Node  string `json:"node,omitempty"`
	Lane  string `json:"lane,omitempty"`
	Role  string `json:"role,omitempty"`
}

// workMapResponse is the GET /api/dashboard/work-map body.
type workMapResponse struct {
	GeneratedAt string           `json:"generated_at"`
	Trace       bool             `json:"trace"`
	Counts      map[string]int   `json:"counts"`
	Nodes       []workMapNode    `json:"nodes"`
	Edges       []workMapEdge    `json:"edges"`
	Running     []workMapRunning `json:"running"`
}

// workMapAxes pulls the work-map placement axes out of one session's
// effective tag list. Each tag is an "axis:value" pair; the first
// occurrence of an axis wins. Only the axes the map renders are kept.
func workMapAxes(tags []string) map[string]string {
	axes := map[string]string{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		i := strings.IndexByte(t, ':')
		if i <= 0 {
			continue
		}
		axis, val := t[:i], t[i+1:]
		if val == "" {
			continue
		}
		switch axis {
		case "compute", "lane", "role", "project", "automaton", "automation":
			if _, ok := axes[axis]; !ok {
				axes[axis] = val
			}
		}
	}
	return axes
}

// workMapBuilder accumulates the graph while handleWorkMap walks the two
// data sources.
type workMapBuilder struct {
	nodes map[string]*workMapNode
	// edges is deduped by from+"|"+to.
	edges map[string]workMapEdge
}

func newWorkMapBuilder() *workMapBuilder {
	return &workMapBuilder{
		nodes: map[string]*workMapNode{},
		edges: map[string]workMapEdge{},
	}
}

func (b *workMapBuilder) node(id, kind, label string) *workMapNode {
	n, ok := b.nodes[id]
	if !ok {
		n = &workMapNode{ID: id, Kind: kind, Label: label}
		b.nodes[id] = n
	}
	return n
}

func (b *workMapBuilder) edge(from, to string) {
	if from == "" || to == "" || from == to {
		return
	}
	b.edges[from+"|"+to] = workMapEdge{From: from, To: to}
}

// workMapStatusLabel maps a raw session state to the short status word
// the map renders ("running", "waiting", "error", "done" or the state
// itself for anything else).
func workMapStatusLabel(state string) string {
	switch state {
	case "running":
		return "running"
	case "waiting_input":
		return "waiting"
	case "complete":
		return "done"
	case "failed", "killed":
		return "error"
	}
	return state
}

// workMapNodeStatus aggregates the statuses of one container's direct
// children: an error anywhere wins, then any running child, then ok.
func workMapNodeStatus(childStatuses []string) string {
	running := false
	for _, s := range childStatuses {
		switch s {
		case "error", "failed", "killed":
			return "error"
		case "running":
			running = true
		}
	}
	if running {
		return "running"
	}
	return "ok"
}

// handleWorkMap serves GET /api/dashboard/work-map.
func (s *Server) handleWorkMap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapDashboardRead) {
		return
	}

	// The live sessions and their placement tags.
	var sessions []*workMapSession
	hasManager := false
	if s.manager != nil {
		hasManager = true
		for _, sess := range s.manager.ListSessions() {
			if sess == nil {
				continue
			}
			tags := append(append([]string{}, sess.Tags...), sess.SystemTags...)
			tags = append(tags, s.manager.InheritedTags(sess)...)
			axes := workMapAxes(tags)
			sessions = append(sessions, &workMapSession{
				id:        sess.FullID,
				label:     sessionWorkMapLabel(sess.Name, sess.FullID),
				state:     string(sess.State),
				updatedAt: sess.UpdatedAt,
				axes:      axes,
			})
		}
	}

	// The trace roll-up: Automaton/task/session rollups plus the links.
	var (
		traceOn            bool
		sessionRollups     = map[string]trace.Rollup{}
		taskRollups        = map[string]trace.Rollup{}
		automatonRollups   = map[string]trace.Rollup{}
		taskToAutomaton    = map[string]string{}
		taskToSessions     = map[string][]string{}
		sessionToAutomaton = map[string]string{}
		taskLastSeen       = map[string]time.Time{}
	)
	if s.traceStore != nil {
		if evs, err := s.traceStore.ReadAll(); err == nil {
			traceOn = true
			opts := trace.RollupOptions{}
			for _, ro := range trace.BuildRollups(evs, trace.DimSession, opts) {
				sessionRollups[ro.Key] = ro
			}
			for _, ro := range trace.BuildRollups(evs, trace.DimTask, opts) {
				taskRollups[ro.Key] = ro
			}
			for _, ro := range trace.BuildRollups(evs, trace.DimAutomaton, opts) {
				automatonRollups[ro.Key] = ro
			}
			for _, ev := range evs {
				if ev.TaskID != "" {
					if a := ev.AutomatonID; a != "" {
						if _, ok := taskToAutomaton[ev.TaskID]; !ok {
							taskToAutomaton[ev.TaskID] = a
						}
					}
					if ev.SessionID != "" {
						list := taskToSessions[ev.TaskID]
						if !containsString(list, ev.SessionID) {
							taskToSessions[ev.TaskID] = append(list, ev.SessionID)
						}
					}
					if ev.Timestamp.After(taskLastSeen[ev.TaskID]) {
						taskLastSeen[ev.TaskID] = ev.Timestamp
					}
				}
				if ev.SessionID != "" && ev.AutomatonID != "" {
					if _, ok := sessionToAutomaton[ev.SessionID]; !ok {
						sessionToAutomaton[ev.SessionID] = ev.AutomatonID
					}
				}
			}
		}
	}

	if !hasManager && !traceOn {
		http.Error(w, "the work map needs a session store or a trace store; neither is available on this daemon", http.StatusServiceUnavailable)
		return
	}

	b := newWorkMapBuilder()

	// 1. Automaton nodes (trace first, so their counts exist before the
	//    session pass may add Automata only a session tag named).
	for id, ro := range automatonRollups {
		n := b.node(id, workMapKindAutomaton, id)
		n.Events = ro.Events
		n.Errors = ro.Errors
		if n.Status == "" {
			if ro.Errors > 0 {
				n.Status = "error"
			} else {
				n.Status = "ok"
			}
		}
	}

	// 2. Task nodes, capped per Automaton, newest first. Tasks that trace
	//    never linked to an Automaton have no place in the map and are
	//    left out (their sessions still show, just higher up).
	tasksByAutomaton := map[string][]string{}
	for id := range taskRollups {
		tasksByAutomaton[taskToAutomaton[id]] = append(tasksByAutomaton[taskToAutomaton[id]], id)
	}
	sessionIDs := map[string]bool{}
	for _, sess := range sessions {
		sessionIDs[sess.id] = true
	}
	for automatonID, taskIDs := range tasksByAutomaton {
		sort.Slice(taskIDs, func(i, j int) bool {
			if !taskLastSeen[taskIDs[i]].Equal(taskLastSeen[taskIDs[j]]) {
				return taskLastSeen[taskIDs[i]].After(taskLastSeen[taskIDs[j]])
			}
			return taskIDs[i] < taskIDs[j]
		})
		omitted := 0
		kept := taskIDs
		if len(kept) > workMapMaxTasksPerAutomaton {
			omitted = len(kept) - workMapMaxTasksPerAutomaton
			kept = kept[:workMapMaxTasksPerAutomaton]
		}
		if automatonID != "" {
			automaton := b.node(automatonID, workMapKindAutomaton, automatonID)
			automaton.TasksOmitted += omitted
			if roll, ok := automatonRollups[automatonID]; ok && automaton.Events == 0 {
				automaton.Events = roll.Events
			}
		}
		for _, taskID := range kept {
			roll := taskRollups[taskID]
			n := b.node(taskID, workMapKindTask, taskID)
			n.Events = roll.Events
			n.Errors = roll.Errors
			if roll.Errors > 0 {
				n.Status = "error"
			} else if n.Status == "" {
				n.Status = "ok"
			}
			if automatonID != "" {
				n.Parent = automatonID
				b.edge(automatonID, taskID)
			}
			for _, sessID := range taskToSessions[taskID] {
				if sessionIDs[sessID] {
					b.edge(taskID, sessID)
				}
			}
		}
	}

	// 3. Session nodes with their placement containers.
	for _, sess := range sessions {
		n := b.node(sess.id, workMapKindSession, sess.label)
		n.Status = workMapStatusLabel(sess.state)
		n.Node = sess.axes["compute"]
		n.Lane = sess.axes["lane"]
		n.Role = sess.axes["role"]
		if roll, ok := sessionRollups[sess.id]; ok && n.Events == 0 {
			n.Events = roll.Events
			n.Errors = roll.Errors
		}
		// Container chain: project → automation → automaton → session,
		// built from the placement tags (with the trace as a fallback
		// Automaton link). Each container hangs under the one above it.
		automatonID := sess.axes["automaton"]
		if automatonID == "" {
			automatonID = sessionToAutomaton[sess.id]
		}
		automationID := sess.axes["automation"]
		projectID := sess.axes["project"]

		var projectNode, automationNode, automatonNode *workMapNode
		if projectID != "" {
			projectNode = b.node("project:"+projectID, workMapKindProject, projectID)
		}
		if automationID != "" {
			automationNode = b.node("automation:"+automationID, workMapKindAutomation, automationID)
			if projectNode != nil {
				automationNode.Parent = projectNode.ID
				b.edge(projectNode.ID, automationNode.ID)
			}
		}
		if automatonID != "" {
			automatonNode = b.node(automatonID, workMapKindAutomaton, automatonID)
			if automationNode != nil {
				automatonNode.Parent = automationNode.ID
				b.edge(automationNode.ID, automatonNode.ID)
			} else if projectNode != nil {
				automatonNode.Parent = projectNode.ID
				b.edge(projectNode.ID, automatonNode.ID)
			}
			n.Parent = automatonNode.ID
			b.edge(automatonNode.ID, sess.id)
		} else if automationNode != nil {
			n.Parent = automationNode.ID
			b.edge(automationNode.ID, sess.id)
		} else if projectNode != nil {
			n.Parent = projectNode.ID
			b.edge(projectNode.ID, sess.id)
		}
	}

	// 4. Container statuses aggregate their direct children.
	children := map[string][]string{}
	for _, n := range b.nodes {
		if n.Parent != "" {
			children[n.Parent] = append(children[n.Parent], n.Status)
		}
	}
	for id, n := range b.nodes {
		if n.Kind == workMapKindProject || n.Kind == workMapKindAutomation || n.Kind == workMapKindAutomaton {
			if sts := children[id]; len(sts) > 0 {
				n.Status = workMapNodeStatus(sts)
			} else if n.Status == "" {
				n.Status = "ok"
			}
		}
	}

	// 5. Deterministic order: kind (parent→child), then label.
	kindRank := map[string]int{
		workMapKindProject:    0,
		workMapKindAutomation: 1,
		workMapKindAutomaton:  2,
		workMapKindTask:       3,
		workMapKindSession:    4,
	}
	nodes := make([]workMapNode, 0, len(b.nodes))
	for _, n := range b.nodes {
		nodes = append(nodes, *n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		ri, rj := kindRank[nodes[i].Kind], kindRank[nodes[j].Kind]
		if ri != rj {
			return ri < rj
		}
		return nodes[i].Label < nodes[j].Label
	})
	edgeKeys := make([]string, 0, len(b.edges))
	for k := range b.edges {
		edgeKeys = append(edgeKeys, k)
	}
	sort.Strings(edgeKeys)
	edges := make([]workMapEdge, 0, len(edgeKeys))
	for _, k := range edgeKeys {
		edges = append(edges, b.edges[k])
	}

	// 6. The running strip: live sessions in an active state, newest
	//    first (the PWA rotates through it).
	active := []*workMapSession{}
	for _, sess := range sessions {
		if sess.state == "running" || sess.state == "waiting_input" {
			active = append(active, sess)
		}
	}
	sort.Slice(active, func(i, j int) bool {
		if !active[i].updatedAt.Equal(active[j].updatedAt) {
			return active[i].updatedAt.After(active[j].updatedAt)
		}
		return active[i].id < active[j].id
	})
	running := make([]workMapRunning, 0, len(active))
	for _, sess := range active {
		running = append(running, workMapRunning{
			ID:    sess.id,
			Label: sess.label,
			State: sess.state,
			Node:  sess.axes["compute"],
			Lane:  sess.axes["lane"],
			Role:  sess.axes["role"],
		})
	}

	counts := map[string]int{}
	for _, n := range nodes {
		counts[n.Kind]++
	}

	writeJSONOK(w, workMapResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Trace:       traceOn,
		Counts:      counts,
		Nodes:       nodes,
		Edges:       edges,
		Running:     running,
	})
}

// workMapSession is the per-session slice the map needs, decoupled from
// the session package so the builder is testable in isolation.
type workMapSession struct {
	id        string
	label     string
	state     string
	updatedAt time.Time
	axes      map[string]string
}

// sessionWorkMapLabel prefers the operator's session name and falls back
// to the session id.
func sessionWorkMapLabel(name, id string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return id
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
