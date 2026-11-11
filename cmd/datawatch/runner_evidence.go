package main

import (
	"fmt"
	"strconv"
	"strings"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/session"
)

// formatRunnerProcessLine renders one process for the runner evidence: the pid,
// the kernel command name and the first two argv entries. A node-hosted runner
// shows its script this way, which the bare command name would hide. Empty
// parts are left out, so a process whose argv cannot be read prints as pid and
// comm only.
func formatRunnerProcessLine(pid int, comm string, argv []string) string {
	parts := []string{strconv.Itoa(pid)}
	if comm != "" {
		parts = append(parts, comm)
	}
	if len(argv) > 2 {
		argv = argv[:2]
	}
	parts = append(parts, argv...)
	return strings.Join(parts, " ")
}

// runnerProcessLines lists every descendant of the panes of tmuxSession, one
// formatted line each, in the order the walk found them. It returns nil when
// the process tree cannot be read (no tmux, no pgrep, the session is gone) and
// never an error: the evidence is best effort and must not get in the way of
// the stall handling that asks for it.
func runnerProcessLines(tmuxSession string) []string {
	roots, tree, ok := sessionProcessTree(tmuxSession)
	if !ok {
		return nil
	}
	var lines []string
	visited := make(map[int]bool, len(roots))
	for _, r := range roots {
		visited[r] = true
	}
	queue := append([]int(nil), roots...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, c := range tree[pid] {
			if visited[c] {
				continue
			}
			visited[c] = true
			lines = append(lines, formatRunnerProcessLine(c, readProcessCommName(c), readProcessCmdline(c)))
			queue = append(queue, c)
		}
	}
	return lines
}

// newRunnerEvidence builds the evidence for a pane capture and the processes
// under tmuxSession. The pane stays preferred when it has content. When it is
// empty (the pane is gone once the wrapper exited), the evidence comes from the
// session output log at logPath, carrying node and model; the process list is
// still attached either way.
func newRunnerEvidence(pane, tmuxSession, logPath, node, model string) *autonomouspkg.RunnerEvidence {
	procs := runnerProcessLines(tmuxSession)
	if strings.TrimSpace(pane) == "" {
		ev := autonomouspkg.RunnerEvidenceFromLog(logPath, node, model)
		ev.Processes = procs
		return &ev
	}
	ev := autonomouspkg.RunnerEvidenceFrom(pane, procs)
	return &ev
}

// withRunnerExitStatus appends the runner's exit status to a stall error when
// the evidence carries one. The original error stays wrapped, so
// errors.Is(err, ErrWorkerStalled) keeps holding.
func withRunnerExitStatus(err error, ev *autonomouspkg.RunnerEvidence) error {
	if err == nil || ev == nil || ev.ExitStatus == nil {
		return err
	}
	return fmt.Errorf("%w (pi exited with status %d)", err, *ev.ExitStatus)
}

// sessionEvidenceNode names the node a session ran on: its compute node when
// one is set, otherwise the host.
func sessionEvidenceNode(s *session.Session) string {
	if s.ComputeNodeRef != "" {
		return s.ComputeNodeRef
	}
	return s.Hostname
}
