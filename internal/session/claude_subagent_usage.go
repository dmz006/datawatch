// Subagent token accounting. A Claude Code subagent (the Task or Agent tool)
// has no datawatch session: no lease, no record, nothing in the cost summary.
// Its usage is written by Claude Code to its own transcript next to the
// parent's, at <projects>/<escaped cwd>/<parent session uuid>/subagents/
// agent-<id>.jsonl, with the same assistant-message usage fields the parent
// transcript has. This file reads those transcripts and records the usage
// under the parent session, so the parent's totals, the cost summary, the
// spend ledger and the usage hook all include it. The daemon does this on
// its own; no model has to report anything.
package session

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// claudeSubagentState holds, per session FullID, the in-process scan state.
// The persisted checkpoint is Session.SubagentLinesRead; this map is the
// same-process cache plus the message and tool_use ids already counted.
var claudeSubagentState sync.Map // FullID -> *subagentScanState

type subagentScanState struct {
	mu    sync.Mutex
	lines map[string]int // agent id -> lines already counted
	seen  map[string]*claudeSeen
}

// ClaudeSubagentDir returns where Claude Code writes the session's subagent
// transcripts, or "" when the project directory is unknown.
func ClaudeSubagentDir(projectDir, fullID string) string {
	p := claudeTranscriptPath(projectDir, fullID)
	if p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), claudeSessionUUID(fullID), "subagents")
}

// ScanSubagentUsage reads every subagent transcript of the claude-code
// session sessID and records the usage not yet counted under that session.
// It is safe to call at any time and from several goroutines: the periodic
// tracker, the SubagentStop hook and the session end all use it. A session of
// another backend, or one with no subagent transcripts yet, is a no-op.
func (m *Manager) ScanSubagentUsage(sessID string) error {
	sess, ok := m.GetSession(sessID)
	if !ok {
		return ErrSessionNotFound
	}
	if sess.BackendFamily != "claude-code" {
		return nil
	}
	dir := ClaudeSubagentDir(sess.ProjectDir, sess.FullID)
	if dir == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(dir, "agent-*.jsonl"))
	if len(files) == 0 {
		return nil
	}
	sort.Strings(files)

	v, _ := claudeSubagentState.LoadOrStore(sess.FullID, &subagentScanState{
		lines: map[string]int{}, seen: map[string]*claudeSeen{},
	})
	st := v.(*subagentScanState)
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.lines) == 0 {
		for k, n := range sess.SubagentLinesRead {
			st.lines[k] = n
		}
	}

	var total ClaudeUsageDelta
	next := map[string]int{}
	changed := false
	for _, path := range files {
		agent := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "agent-"), ".jsonl")
		seen := st.seen[agent]
		if seen == nil {
			seen = &claudeSeen{msgs: map[string]struct{}{}, tools: map[string]struct{}{}}
			st.seen[agent] = seen
		}
		d, lines, read := scanClaudeTranscript(path, st.lines[agent], seen)
		if !read {
			continue
		}
		if lines > st.lines[agent] {
			st.lines[agent] = lines
			changed = true
		}
		next[agent] = st.lines[agent]
		total.In += d.In
		total.Out += d.Out
		total.CacheRead += d.CacheRead
		total.CacheCreation += d.CacheCreation
		total.ToolCalls += d.ToolCalls
	}
	if !changed {
		return nil
	}
	return m.addUsage(sess.FullID, total.In, total.Out, CostRate{}, func(s *Session) {
		s.CacheReadTokens += total.CacheRead
		s.CacheCreationTokens += total.CacheCreation
		s.ToolCalls += total.ToolCalls
		s.SubagentTokensIn += total.In
		s.SubagentTokensOut += total.Out
		s.SubagentToolCalls += total.ToolCalls
		merged := make(map[string]int, len(s.SubagentLinesRead)+len(next))
		for k, n := range s.SubagentLinesRead {
			merged[k] = n
		}
		for k, n := range next {
			merged[k] = n
		}
		s.SubagentLinesRead = merged
	})
}

// trackClaudeSubagentUsage scans the session's subagent transcripts every
// tick until ctx ends, then once more so usage written in the last interval
// is not lost when the session closes.
func (m *Manager) trackClaudeSubagentUsage(ctx context.Context, fullID string, tick time.Duration) {
	defer claudeSubagentState.Delete(fullID)
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = m.ScanSubagentUsage(fullID)
			return
		case <-t.C:
			_ = m.ScanSubagentUsage(fullID)
		}
	}
}
