// B98 — opencode sessions never populated TokensIn/TokensOut/EstCostUSD
// for the same reason claude-code sessions didn't (see claude_usage.go):
// nothing in the real session lifecycle ever called AddUsage. Unlike
// claude-code, opencode has no predictable transcript path this daemon
// can compute in advance — its own session ID ("ses_...") is assigned
// internally at launch and is not something datawatch can pass in
// (confirmed in internal/llm/backends/opencode/backend.go's LaunchResume
// comment). Instead, `opencode session list --format json` exposes each
// session's `directory` and `created` (unix ms) fields, which this file
// uses to correlate opencode's own session ID to a datawatch Session by
// project directory + closest creation time — a one-time resolution,
// cached for the lifetime of the monitor goroutine.
package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// opencodeSessionIDs caches the resolved "ses_..." ID per datawatch
// FullID, so the (potentially large) `session list` scan only runs once
// per session instead of on every tick.
var opencodeSessionIDs sync.Map // FullID -> string

// opencodeLastTotals tracks the last cumulative input/output token
// totals reported for a session, since opencode's own `export` output
// reports lifetime-cumulative totals (info.tokens), not per-turn deltas.
// Same-process cache only (v9.0.8): the authoritative checkpoint is
// Session.UsageLastIn/UsageLastOut, persisted to disk, so a daemon
// restart resumes the delta from there instead of diffing against zero
// (and re-adding the session's whole lifetime total). See that field's
// doc comment in store.go for the incident this fixed.
var opencodeLastTotals sync.Map // FullID -> [2]int (in, out)

// opencodeResolveBinary mirrors opencode.resolveBinary. Reimplemented
// here rather than imported to keep this package's only dependency on
// the opencode backend package optional (no behavior here requires the
// llm.Backend interface, just the binary path convention).
func opencodeResolveBinary() string {
	binary := "opencode"
	if _, err := exec.LookPath(binary); err == nil {
		return binary
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{
		filepath.Join(home, ".opencode", "bin", binary),
		filepath.Join(home, ".local", "bin", binary),
		filepath.Join("/usr/local/bin", binary),
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return binary
}

type opencodeListEntry struct {
	ID        string `json:"id"`
	Directory string `json:"directory"`
	Created   int64  `json:"created"` // unix ms
}

type opencodeExportInfo struct {
	Info struct {
		Tokens struct {
			Input  int `json:"input"`
			Output int `json:"output"`
			// Reasoning tokens are deliberately excluded: it's not
			// confirmed whether opencode's "output" figure already
			// includes them or they're disjoint, and double-counting
			// would be worse than the conservative undercount (same
			// rationale as claude_usage.go's cache-token exclusion).
		} `json:"tokens"`
	} `json:"info"`
}

// resolveOpenCodeSessionID runs `opencode session list` once and picks
// the entry whose directory matches projectDir with created closest to
// createdAt. Returns "" if nothing matches (e.g. opencode hasn't
// written its own session record yet — try again next tick).
func resolveOpenCodeSessionID(ctx context.Context, binary, projectDir string, createdAt time.Time) string {
	if projectDir == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, binary, "session", "list", "--format", "json", "-n", "50").Output() // #nosec G204 -- binary resolved from a fixed candidate list, not user input
	if err != nil {
		return ""
	}
	var entries []opencodeListEntry
	if json.Unmarshal(out, &entries) != nil {
		return ""
	}
	targetMs := createdAt.UnixMilli()
	best := ""
	bestDelta := int64(-1)
	for _, e := range entries {
		if e.Directory != projectDir {
			continue
		}
		delta := e.Created - targetMs
		if delta < 0 {
			delta = -delta
		}
		if bestDelta == -1 || delta < bestDelta {
			bestDelta = delta
			best = e.ID
		}
	}
	return best
}

// trackOpenCodeUsage polls for this session's cumulative token totals
// and reports the delta since the last tick via reportFn. Best-effort:
// a resolution failure or export error just means no usage is reported
// this tick, matching trackClaudeCodeUsage's own never-hard-fail design.
func trackOpenCodeUsage(ctx context.Context, fullID, projectDir string, createdAt time.Time, initialLastIn, initialLastOut int, tick time.Duration, reportFn func(sessID string, tokensIn, tokensOut, curIn, curOut int)) {
	if reportFn == nil {
		return
	}
	opencodeLastTotals.Store(fullID, [2]int{initialLastIn, initialLastOut})
	defer opencodeSessionIDs.Delete(fullID)
	defer opencodeLastTotals.Delete(fullID)

	binary := opencodeResolveBinary()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanOpenCodeUsageOnce(ctx, binary, fullID, projectDir, createdAt, reportFn)
		}
	}
}

func scanOpenCodeUsageOnce(ctx context.Context, binary, fullID, projectDir string, createdAt time.Time, reportFn func(sessID string, tokensIn, tokensOut, curIn, curOut int)) {
	sesID, ok := opencodeSessionIDs.Load(fullID)
	if !ok {
		id := resolveOpenCodeSessionID(ctx, binary, projectDir, createdAt)
		if id == "" {
			return // not yet discoverable -- try again next tick
		}
		opencodeSessionIDs.Store(fullID, id)
		sesID = id
	}

	out, err := exec.CommandContext(ctx, binary, "export", sesID.(string)).Output() // #nosec G204 -- binary resolved from a fixed candidate list; sesID resolved server-side from opencode's own session list, not user input
	if err != nil {
		return
	}
	// `opencode export` prints a human-readable "Exporting session: <id>"
	// line before the JSON body -- skip to the first '{'.
	body := out
	if idx := strings.IndexByte(string(out), '{'); idx > 0 {
		body = out[idx:]
	}
	var export opencodeExportInfo
	if json.Unmarshal(body, &export) != nil {
		return
	}

	curIn, curOut := export.Info.Tokens.Input, export.Info.Tokens.Output
	lastIn, lastOut := 0, 0
	if v, ok := opencodeLastTotals.Load(fullID); ok {
		last := v.([2]int)
		lastIn, lastOut = last[0], last[1]
	}
	deltaIn, deltaOut := curIn-lastIn, curOut-lastOut
	opencodeLastTotals.Store(fullID, [2]int{curIn, curOut})
	if curIn != lastIn || curOut != lastOut {
		reportFn(fullID, max(deltaIn, 0), max(deltaOut, 0), curIn, curOut)
	}
}
