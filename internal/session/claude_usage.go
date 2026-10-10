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
// e.g. "/home/dmz/workspace/datawatch/.foo" -> "-home-dmz-workspace-
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
	Type    string `json:"type"`
	Message struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			// Cache tokens (cache_creation_input_tokens / cache_read_
			// input_tokens) are deliberately NOT counted here. Anthropic
			// bills them at very different rates than base input tokens
			// (cache writes ~1.25x, cache reads ~0.1x) and CostRate
			// (cost.go) only has one InPerK for all input tokens --
			// folding cache tokens in at the standard rate would produce
			// a wildly inflated dollar figure (this session's own
			// transcript sample had 30,745 cache-creation tokens against
			// 2 real input tokens), which is worse than the current
			// honest zero. Needs a CostRate schema extension to do
			// properly; tracked as a known gap rather than guessed at.
		} `json:"usage"`
	} `json:"message"`
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
	path := claudeTranscriptPath(projectDir, fullID)
	if path == "" || reportFn == nil {
		return
	}
	claudeUsageLinesRead.Store(fullID, initialLinesRead)
	defer claudeUsageLinesRead.Delete(fullID)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanClaudeUsageOnce(path, fullID, reportFn)
		}
	}
}

func scanClaudeUsageOnce(path, fullID string, reportFn func(sessID string, tokensIn, tokensOut, linesRead int)) {
	f, err := os.Open(path) // #nosec G304 -- path is derived from this daemon's own session record, not external input
	if err != nil {
		return // transcript not written yet, or path guess was wrong -- try again next tick
	}
	defer f.Close() //nolint:errcheck

	alreadyRead := 0
	if v, ok := claudeUsageLinesRead.Load(fullID); ok {
		alreadyRead = v.(int)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // transcript lines can be large (big tool outputs)
	var sumIn, sumOut, lineNum int
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
		u := entry.Message.Usage
		sumIn += u.InputTokens
		sumOut += u.OutputTokens
	}
	if lineNum > alreadyRead {
		claudeUsageLinesRead.Store(fullID, lineNum)
		// Persist the new checkpoint even when this tick found no usage
		// (sumIn/sumOut both 0 -- e.g. a tool-only turn) so the
		// already-scanned lines aren't re-scanned after a restart.
		if sumIn > 0 || sumOut > 0 {
			reportFn(fullID, sumIn, sumOut, lineNum)
		} else {
			reportFn(fullID, 0, 0, lineNum)
		}
	}
}
