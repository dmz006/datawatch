// B98 — claude-code sessions never populated TokensIn/TokensOut/
// EstCostUSD: AddUsage (cost.go) was only ever called from the manual
// POST /api/cost/usage endpoint, nothing in the real session lifecycle
// called it automatically. claude-code sessions run in full interactive
// TUI mode (no --print/--output-format json), so there's no structured
// per-turn usage on stdout to scrape -- but Claude Code already writes
// a JSONL transcript per session at a path this package can compute
// without any discovery step, since the session UUID used in
// --session-id is already deterministic (sha1 of the session's own
// FullID -- see claudecode.deriveSessionUUID, which this file
// reimplements identically so internal/session doesn't need to import
// internal/llm/claudecode and create a cycle). Each assistant message
// entry in that transcript has a real message.usage field.
package session

import (
	"bufio"
	"context"
	"crypto/sha1" // #nosec G505 -- not a security use, UUID v5 name-based derivation per RFC 4122 §4.3 (matches claudecode.deriveSessionUUID, which this reimplements to avoid an import cycle)
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// claudeUsageLinesRead tracks, per session FullID, how many JSONL lines
// have already been counted toward that session's TokensIn/TokensOut --
// a transcript only ever grows, so re-reading from the start every tick
// and re-summing everything would double-count every prior turn. This
// in-memory map is only a same-process cache now (v9.0.8): the
// authoritative checkpoint is Session.UsageLinesRead, persisted to disk,
// so a daemon restart resumes from there instead of re-counting (and
// re-adding on top of the already-persisted total) from line 0. See
// that field's doc comment in store.go for the incident this fixed.
var claudeUsageLinesRead sync.Map // FullID -> int

// claudeSessionUUID reproduces claudecode.deriveSessionUUID (UUID v5,
// RFC 4122 URL namespace, seeded with the session's FullID) so this
// package can locate the transcript Claude Code itself names using
// that exact value, without importing internal/llm/claudecode (which
// imports internal/session already -- a cycle the other direction).
func claudeSessionUUID(fullID string) string {
	ns := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	nsBytes, _ := hex.DecodeString(strings.ReplaceAll(ns, "-", ""))
	h := sha1.New() // #nosec G401 -- UUID v5 name-based derivation, not a security use
	h.Write(nsBytes)
	h.Write([]byte(fullID))
	sum := h.Sum(nil)
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// claudeTranscriptPath returns where Claude Code would write this
// session's JSONL transcript, given its project directory. Claude Code
// flattens the cwd into a directory name by replacing '/' and '.' with
// '-' (confirmed against real transcript directories on this machine,
// e.g. "/home/<user>/workspace/datawatch/.foo" -> "-home-user-workspace-
// datawatch--foo"); existing hyphens in the path are left alone.
func claudeTranscriptPath(projectDir, fullID string) string {
	if projectDir == "" {
		return ""
	}
	escaped := strings.NewReplacer("/", "-", ".", "-").Replace(projectDir)
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects", escaped, claudeSessionUUID(fullID)+".jsonl")
}

// claudeUsageEntry is the subset of one JSONL transcript line this
// package cares about. Unrelated line types (type != "assistant", or
// an assistant line with no usage, e.g. a tool-only turn) harmlessly
// decode to zero values and contribute nothing.
type claudeUsageEntry struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Usage struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		// Content is decoded loosely: it is a block list for assistant
		// lines but a plain string on some user lines.
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// ClaudeUsageDelta is the usage found in newly-read transcript lines.
// In is the non-cache input; cache tokens are reported separately.
type ClaudeUsageDelta struct {
	In, Out, CacheRead, CacheCreation, ToolCalls int
	LinesRead                                    int
}

// claudeUsageSeen holds, per session, the message ids and tool_use ids
// already counted, so a message written on several lines is counted once.
var claudeUsageSeen sync.Map // FullID -> *claudeSeen

type claudeSeen struct{ msgs, tools map[string]struct{} }

func countToolUse(raw json.RawMessage, seen map[string]struct{}) int {
	var blocks []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &blocks) != nil {
		return 0
	}
	n := 0
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		if b.ID != "" {
			if _, dup := seen[b.ID]; dup {
				continue
			}
			seen[b.ID] = struct{}{}
		}
		n++
	}
	return n
}

// trackClaudeCodeUsage polls sess's own JSONL transcript every tick and
// reports any newly-appeared usage since the last tick via reportFn
// (the caller passes m.AddUsage; kept as a plain func param so this
// file has no dependency on Manager's own definition order). Returns
// when ctx is cancelled -- same lifecycle as the monitorOutput
// goroutine this is started alongside.
//
// Best-effort by design: a missing/not-yet-created transcript, a mid-
// write partial line, or a path-escaping mismatch for some future
// Claude Code version just means no usage is reported this tick --
// never a hard failure, since the status quo this replaces is "always
// zero" and silently staying there is strictly better than crashing
// the session monitor over a usage-reporting nicety.
func trackClaudeCodeUsage(ctx context.Context, fullID, projectDir string, initialLinesRead int, tick time.Duration, reportFn func(sessID string, tokensIn, tokensOut, linesRead int)) {
	if reportFn == nil {
		return
	}
	trackClaudeCodeUsageDelta(ctx, fullID, projectDir, initialLinesRead, tick, func(id string, d ClaudeUsageDelta) {
		reportFn(id, d.In, d.Out, d.LinesRead)
	})
}

// trackClaudeCodeUsageDelta is trackClaudeCodeUsage reporting the full delta.
func trackClaudeCodeUsageDelta(ctx context.Context, fullID, projectDir string, initialLinesRead int, tick time.Duration, reportFn func(sessID string, d ClaudeUsageDelta)) {
	path := claudeTranscriptPath(projectDir, fullID)
	if path == "" || reportFn == nil {
		return
	}
	claudeUsageLinesRead.Store(fullID, initialLinesRead)
	defer claudeUsageLinesRead.Delete(fullID)
	defer claudeUsageSeen.Delete(fullID)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanClaudeUsageDeltaOnce(path, fullID, reportFn)
		}
	}
}

func scanClaudeUsageOnce(path, fullID string, reportFn func(sessID string, tokensIn, tokensOut, linesRead int)) {
	scanClaudeUsageDeltaOnce(path, fullID, func(id string, d ClaudeUsageDelta) {
		reportFn(id, d.In, d.Out, d.LinesRead)
	})
}

func scanClaudeUsageDeltaOnce(path, fullID string, reportFn func(sessID string, d ClaudeUsageDelta)) {
	f, err := os.Open(path) // #nosec G304 -- path is derived from this daemon's own session record, not external input
	if err != nil {
		return // transcript not written yet, or path guess was wrong -- try again next tick
	}
	defer f.Close() //nolint:errcheck

	alreadyRead := 0
	if v, ok := claudeUsageLinesRead.Load(fullID); ok {
		alreadyRead = v.(int)
	}
	var seen *claudeSeen
	if v, ok := claudeUsageSeen.Load(fullID); ok {
		seen = v.(*claudeSeen)
	} else {
		seen = &claudeSeen{msgs: map[string]struct{}{}, tools: map[string]struct{}{}}
		claudeUsageSeen.Store(fullID, seen)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // transcript lines can be large (big tool outputs)
	var d ClaudeUsageDelta
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= alreadyRead {
			continue // already counted on a prior tick
		}
		var entry claudeUsageEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue // mid-write partial line -- pick it up again next tick once it's complete
		}
		if entry.Type != "assistant" {
			continue
		}
		d.ToolCalls += countToolUse(entry.Message.Content, seen.tools)
		// Usage is repeated on every line of one message: count it once.
		id := entry.Message.ID
		if id == "" {
			id = entry.RequestID
		}
		if id != "" {
			if _, dup := seen.msgs[id]; dup {
				continue
			}
			seen.msgs[id] = struct{}{}
		}
		u := entry.Message.Usage
		d.In += u.InputTokens
		d.Out += u.OutputTokens
		d.CacheRead += u.CacheReadTokens
		d.CacheCreation += u.CacheCreationTokens
	}
	if lineNum > alreadyRead {
		claudeUsageLinesRead.Store(fullID, lineNum)
		// Persist the checkpoint even when nothing was found this tick.
		d.LinesRead = lineNum
		reportFn(fullID, d)
	}
}
