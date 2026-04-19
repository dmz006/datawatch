// F10 sprint 3 — CmdAgent handler for chat-side spawn/list/kill/etc.

package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/agents"
)

// SetAgentManager wires the agent lifecycle manager for chat commands.
func (r *Router) SetAgentManager(m *agents.Manager) { r.agentMgr = m }

// handleAgent dispatches a parsed CmdAgent.
func (r *Router) handleAgent(cmd Command) {
	if r.agentMgr == nil {
		r.send(fmt.Sprintf("[%s] agent manager not configured", r.hostname))
		return
	}
	if cmd.AgentVerb == "" || cmd.Text != "" {
		r.send(agentHelpText(r.hostname, cmd.Text))
		return
	}
	switch cmd.AgentVerb {
	case AgentVerbList:
		r.agentList()
	case AgentVerbShow:
		r.agentShow(cmd.AgentID)
	case AgentVerbLogs:
		r.agentLogs(cmd.AgentID)
	case AgentVerbKill:
		r.agentKill(cmd.AgentID)
	case AgentVerbSpawn:
		r.agentSpawn(cmd.AgentProject, cmd.AgentClusterName, cmd.AgentTask)
	case AgentVerbAudit:
		r.agentAudit(cmd.AgentID)
	default:
		r.send(agentHelpText(r.hostname, "unknown verb: "+cmd.AgentVerb))
	}
}

// agentAudit (BL107) tails the audit JSON-lines file. The path is
// supplied via SetAgentAuditPath; when empty (or CEF-formatted) the
// command degrades to a friendly "not available" message.
func (r *Router) agentAudit(agentID string) {
	if r.agentAuditPath == "" {
		r.send(fmt.Sprintf("[%s] agent audit not enabled", r.hostname))
		return
	}
	if r.agentAuditCEF {
		r.send(fmt.Sprintf("[%s] audit file is CEF-formatted; query your SIEM", r.hostname))
		return
	}
	events, err := agents.ReadEvents(r.agentAuditPath,
		agents.ReadEventsFilter{AgentID: agentID}, 20)
	if err != nil {
		r.send(fmt.Sprintf("[%s] agent audit: %v", r.hostname, err))
		return
	}
	if len(events) == 0 {
		r.send(fmt.Sprintf("[%s] no audit events", r.hostname))
		return
	}
	lines := []string{fmt.Sprintf("[%s] audit (%d):", r.hostname, len(events))}
	for _, ev := range events {
		lines = append(lines, fmt.Sprintf("  %s  %-12s  %s  %s",
			ev.At.Format("15:04:05"), ev.Event,
			truncate(ev.AgentID, 8), truncate(ev.Note, 60)))
	}
	r.send(strings.Join(lines, "\n"))
}

// SetAgentAuditPath wires the audit file path used by `agent audit`.
func (r *Router) SetAgentAuditPath(path string, cef bool) {
	r.agentAuditPath = path
	r.agentAuditCEF = cef
}

func (r *Router) agentList() {
	list := r.agentMgr.List()
	lines := []string{fmt.Sprintf("[%s] %d agent(s):", r.hostname, len(list))}
	if len(list) == 0 {
		lines = append(lines, "  (none)")
	}
	for _, a := range list {
		lines = append(lines, fmt.Sprintf("  %s  %-10s  %s/%s  %s",
			a.ID[:8]+"…", a.State, a.ProjectProfile, a.ClusterProfile, truncate(a.Task, 40)))
	}
	r.send(strings.Join(lines, "\n"))
}

func (r *Router) agentShow(id string) {
	if id == "" {
		r.send(agentHelpText(r.hostname, "show requires an agent id"))
		return
	}
	a := r.agentMgr.Get(id)
	if a == nil {
		r.send(fmt.Sprintf("[%s] agent %q not found", r.hostname, id))
		return
	}
	lines := []string{
		fmt.Sprintf("[%s] agent: %s", r.hostname, a.ID),
		fmt.Sprintf("  state: %s", a.State),
		fmt.Sprintf("  profiles: %s / %s", a.ProjectProfile, a.ClusterProfile),
		fmt.Sprintf("  task: %s", truncate(a.Task, 200)),
		fmt.Sprintf("  created: %s", a.CreatedAt.Format("2006-01-02 15:04:05")),
	}
	if !a.ReadyAt.IsZero() {
		lines = append(lines, fmt.Sprintf("  ready: %s", a.ReadyAt.Format("2006-01-02 15:04:05")))
	}
	if a.FailureReason != "" {
		lines = append(lines, fmt.Sprintf("  failure: %s", a.FailureReason))
	}
	if a.DriverInstance != "" {
		lines = append(lines, fmt.Sprintf("  container: %s", a.DriverInstance))
	}
	if len(a.SessionIDs) > 0 {
		lines = append(lines, fmt.Sprintf("  sessions: %s", strings.Join(a.SessionIDs, ", ")))
	}
	r.send(strings.Join(lines, "\n"))
}

func (r *Router) agentLogs(id string) {
	if id == "" {
		r.send(agentHelpText(r.hostname, "logs requires an agent id"))
		return
	}
	// Tail 40 lines — chat backends cap message length so keep it small.
	out, err := r.agentMgr.Logs(context.Background(), id, 40)
	if err != nil {
		r.send(fmt.Sprintf("[%s] logs %s: %v", r.hostname, id, err))
		return
	}
	out = strings.TrimRight(out, "\n")
	if out == "" {
		out = "(no logs yet)"
	}
	r.send(fmt.Sprintf("[%s] logs %s:\n%s", r.hostname, id, out))
}

func (r *Router) agentKill(id string) {
	if id == "" {
		r.send(agentHelpText(r.hostname, "kill requires an agent id"))
		return
	}
	if err := r.agentMgr.Terminate(context.Background(), id); err != nil {
		r.send(fmt.Sprintf("[%s] kill %s: %v", r.hostname, id, err))
		return
	}
	r.send(fmt.Sprintf("[%s] agent %s terminated", r.hostname, id))
}

func (r *Router) agentSpawn(projectName, clusterName, task string) {
	a, err := r.agentMgr.Spawn(context.Background(), agents.SpawnRequest{
		ProjectProfile: projectName,
		ClusterProfile: clusterName,
		Task:           task,
	})
	if err != nil {
		// Agent record may still exist (driver failed after registration)
		if a != nil {
			r.send(fmt.Sprintf("[%s] spawn failed: agent %s state=%s reason=%s",
				r.hostname, a.ID[:8]+"…", a.State, a.FailureReason))
			return
		}
		r.send(fmt.Sprintf("[%s] spawn failed: %v", r.hostname, err))
		return
	}
	r.send(fmt.Sprintf("[%s] spawned agent %s  project=%s cluster=%s state=%s",
		r.hostname, a.ID, a.ProjectProfile, a.ClusterProfile, a.State))
}

func agentHelpText(hostname, note string) string {
	out := []string{fmt.Sprintf("[%s] agent usage:", hostname)}
	if note != "" {
		out = append(out, "  "+note)
	}
	out = append(out,
		"  agent list",
		"  agent show <id>",
		"  agent logs <id>",
		"  agent kill <id>",
		"  agent spawn <project> <cluster> [<task>]",
		"  agent audit [<id>]",
	)
	return strings.Join(out, "\n")
}

// truncate is defined in router.go; we reuse it.
