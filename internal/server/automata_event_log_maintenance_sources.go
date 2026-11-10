package server

// Automata event log: maintenance (cleanup) sources.
//
// Two adapters turn the cleanup records the daemon already keeps into
// AutomataEvent values of kind cleanup for the operator event log: one event
// per branch or worktree in the safe-to-delete report, and one event for the
// cleanup status (both passes and the disk readings). The source data arrives
// through small function types, so the adapters are testable without a
// running daemon and nothing here reads the server's handlers. Automaton is
// left empty because the report names a branch, not an Automaton id, and it is
// never invented. Operator text says Automaton or Automata, never PRD.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// automataSafeDeleteReportSource returns the safe-to-delete report, the same
// data the safe-to-delete handler serves. An error means no report is available.
type automataSafeDeleteReportSource func() (autonomous.SafeDeleteReport, error)

// automataCleanupStatusSource returns the cleanup status, the same data the
// cleanup status handler serves.
type automataCleanupStatusSource func() autonomous.CleanupStatus

// automataSafeDeleteEvents converts a safe-to-delete report into cleanup
// events. Each safe branch is info. Each held branch is info, or warn when its
// worktree has uncommitted changes. All events share the report's computed_at
// time, so the same report always yields the same ids.
func automataSafeDeleteEvents(rep autonomous.SafeDeleteReport) ([]AutomataEvent, error) {
	at, err := time.Parse(time.RFC3339Nano, rep.ComputedAt)
	if err != nil {
		return nil, fmt.Errorf("automata event safe-to-delete source: computed_at %q: %w", rep.ComputedAt, err)
	}
	at = at.UTC()
	out := make([]AutomataEvent, 0, len(rep.Safe)+len(rep.Held))
	for _, s := range rep.Safe {
		detail := []string{fmt.Sprintf("%d commits not on base", s.Commits)}
		if s.Worktree != "" {
			detail = append(detail, "worktree "+s.Worktree)
		}
		if s.DirtyCheckSkipped {
			detail = append(detail, "uncommitted-changes check skipped while the Automaton runs")
		}
		out = append(out, AutomataEvent{
			ID:       fmt.Sprintf("cleanup:safe:%s:%d", s.Branch, at.UnixNano()),
			Time:     at,
			Kind:     AutomataEventCleanup,
			Summary:  automataOperatorText(fmt.Sprintf("Branch %s is safe to delete", s.Branch)),
			Detail:   automataOperatorText(strings.Join(detail, "; ")),
			Reason:   automataOperatorText(s.Reason),
			Severity: AutomataSeverityInfo,
		})
	}
	for _, h := range rep.Held {
		ev := AutomataEvent{
			ID:       fmt.Sprintf("cleanup:held:%s:%d", h.Branch, at.UnixNano()),
			Time:     at,
			Kind:     AutomataEventCleanup,
			Summary:  automataOperatorText(fmt.Sprintf("Branch %s is held, state %s", h.Branch, h.State)),
			Detail:   automataOperatorText(h.Worktree),
			Reason:   automataOperatorText(h.Note),
			Severity: AutomataSeverityInfo,
		}
		if h.Dirty {
			ev.Severity = AutomataSeverityWarn
			ev.Summary = automataOperatorText(fmt.Sprintf("Branch %s is held for uncommitted changes", h.Branch))
			ev.NextAction = "Review the uncommitted changes in the worktree before cleanup"
		}
		out = append(out, ev)
	}
	return out, nil
}

// automataCleanupDiskText describes one disk reading in operator words.
func automataCleanupDiskText(label string, d *autonomous.CleanupDiskCheck) string {
	if d == nil {
		return label + " not configured"
	}
	if d.Percent == nil {
		return fmt.Sprintf("%s %s unreadable", label, d.Path)
	}
	s := fmt.Sprintf("%s %s at %d%%", label, d.Path, *d.Percent)
	if d.Alert {
		s += " (alert)"
	}
	return s
}

// automataCleanupPassText describes one cleanup pass in operator words.
func automataCleanupPassText(label string, p autonomous.CleanupPassStatus) string {
	return fmt.Sprintf("%s: mode %s, removed %d, would remove %d, skipped %d, failed %d, freed %d bytes, owner removed %d, net removed %d",
		label, p.Mode, p.Removed, p.WouldRemove, p.Skipped, p.Failed, p.BytesFreed, p.OwnerRemoved, p.NetRemoved)
}

// automataCleanupStatusEvents converts the cleanup status into one cleanup
// event. The event time is the newest recorded time in the status (either
// pass's last run or the last disk alert), or the zero time when none is
// recorded. It is error when a pass recorded a failure, warn while the disk
// alert is on, and info otherwise.
func automataCleanupStatusEvents(st autonomous.CleanupStatus) []AutomataEvent {
	var at time.Time
	for _, t := range []*time.Time{st.Worktrees.LastRun, st.Temp.LastRun, st.LastDiskAlertAt} {
		if t != nil && t.After(at) {
			at = *t
		}
	}
	at = at.UTC()

	summary := fmt.Sprintf("Cleanup: worktrees removed %d, temp removed %d, disk alert %t",
		st.Worktrees.Removed, st.Temp.Removed, st.DiskAlert)
	detail := []string{
		automataCleanupPassText("worktrees", st.Worktrees),
		automataCleanupPassText("temp", st.Temp),
		automataCleanupDiskText("worktree disk", st.WorktreeDisk),
		automataCleanupDiskText("temp disk", st.TempDisk),
	}
	var reasons []string
	for _, e := range []string{st.Worktrees.LastError, st.Temp.LastError} {
		if e != "" {
			reasons = append(reasons, e)
		}
	}

	ev := AutomataEvent{
		ID:       fmt.Sprintf("cleanup:status:%d", at.UnixNano()),
		Time:     at,
		Kind:     AutomataEventCleanup,
		Summary:  automataOperatorText(summary),
		Detail:   automataOperatorText(strings.Join(detail, "; ")),
		Reason:   automataOperatorText(strings.Join(reasons, "; ")),
		Severity: AutomataSeverityInfo,
	}
	switch {
	case st.Worktrees.Failed > 0 || st.Temp.Failed > 0 || len(reasons) > 0:
		ev.Severity = AutomataSeverityError
	case st.DiskAlert:
		ev.Severity = AutomataSeverityWarn
		ev.NextAction = "Free disk space on the path that is over the alert threshold"
	}
	return []AutomataEvent{ev}
}

// errAutomataMaintenanceSourceNil is returned when an adapter is given no source.
var errAutomataMaintenanceSourceNil = errors.New("automata event maintenance source: source is nil")

// AutomataEventsFromSafeToDeleteReport reads the safe-to-delete report through
// src and returns one cleanup event per safe or held branch. A nil src is an
// error. A source error is returned, not turned into an empty log.
func AutomataEventsFromSafeToDeleteReport(src automataSafeDeleteReportSource) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataMaintenanceSourceNil
	}
	rep, err := src()
	if err != nil {
		return nil, fmt.Errorf("automata event safe-to-delete source: %w", err)
	}
	return automataSafeDeleteEvents(rep)
}

// AutomataEventsFromCleanupStatus reads the cleanup status through src and
// returns one cleanup event for both passes and the disk readings. A nil src
// is an error.
func AutomataEventsFromCleanupStatus(src automataCleanupStatusSource) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataMaintenanceSourceNil
	}
	return automataCleanupStatusEvents(src()), nil
}
