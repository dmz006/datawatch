package autonomous

// The daemon wires *API (not *Manager) into the server, so the server's
// optional interfaces are satisfied only through these delegating methods.
// Without them gate repair, merge run, deploy run and the event preview answer
// 503, and release tags and the memory ceiling come back empty. The server
// holds each of them to its interface at compile time.

import (
	"context"
	"time"
)

// GateRepairerStatus returns the gate repair engine's read state.
func (a *API) GateRepairerStatus() GateRepairStatus { return a.M.GateRepairerStatus() }

// RunGateRepair runs gate repair for one failed task gate.
func (a *API) RunGateRepair(ctx context.Context, automatonID, taskID string) GateRepairOutcome {
	return a.M.RunGateRepair(ctx, automatonID, taskID)
}

// RunGateRepairPass runs gate repair for every failed task gate.
func (a *API) RunGateRepairPass(ctx context.Context) []GateRepairOutcome {
	return a.M.RunGateRepairPass(ctx)
}

// RecordMergeRun stores one merge pipeline run.
func (a *API) RecordMergeRun(run MergePipelineRun) { a.M.RecordMergeRun(run) }

// MergeBaseRepo is the repository merges and release tags are read from.
func (a *API) MergeBaseRepo() string { return a.M.MergeBaseRepo() }

// MergeQuestionStore is the operator question store a merge asks through.
func (a *API) MergeQuestionStore() *QuestionStore { return a.M.MergeQuestionStore() }

// AppendTriageNote adds a note to an Automaton's triage record.
func (a *API) AppendTriageNote(prdID, source, text string) error {
	return a.M.AppendTriageNote(prdID, source, text)
}

// AlertSink is the daemon's operator alert callback.
func (a *API) AlertSink() func(level, title, body string) string { return a.M.AlertSink() }

// RecentLifecycleEvents returns the recorded lifecycle events since since.
func (a *API) RecentLifecycleEvents(since time.Time) ([]LifecycleEvent, time.Time) {
	return a.M.RecentLifecycleEvents(since)
}

// MemoryCeiling reports the session memory ceiling in effect.
func (a *API) MemoryCeiling() MemoryCeilingStatus { return a.M.MemoryCeiling() }
