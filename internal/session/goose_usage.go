// B98 — goose sessions never populated TokensIn/TokensOut/EstCostUSD,
// same root cause as claude-code/opencode (see claude_usage.go). Goose's
// `--name` flag is only ever passed at launch if Session.Name is
// non-empty (internal/llm/backends/goose/backend.go Launch, conditional
// on b.sessionName != ""); when it's empty, goose assigns its own
// date-based internal ID (e.g. "20261007_1") that this daemon has no
// way to predict or discover in advance, so that case is a deliberate
// no-op rather than a guess. When Name IS set, `goose session export
// --name <name> --format json` works directly as a lookup key (live-
// verified: no need to resolve goose's own internal ID at all) and
// returns a cumulative `accumulated_usage.{input_tokens,output_tokens}`
// for the session's lifetime -- tracked as a delta the same way as
// opencode's cumulative `info.tokens`.
package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// gooseLastTotals tracks the last cumulative input/output token totals
// reported for a session, keyed by FullID. Same-process cache only
// (v9.0.8): the authoritative checkpoint is Session.UsageLastIn/
// UsageLastOut, persisted to disk, so a daemon restart resumes the
// delta from there instead of diffing against zero. See that field's
// doc comment in store.go for the incident this fixed.
var gooseLastTotals sync.Map // FullID -> [2]int (in, out)

// gooseResolveBinary mirrors goose.resolveBinary (see opencodeResolveBinary
// for why this is reimplemented rather than imported).
func gooseResolveBinary() string {
	binary := "goose"
	if _, err := exec.LookPath(binary); err == nil {
		return binary
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{
		filepath.Join(home, ".local", "bin", binary),
		filepath.Join("/usr/local/bin", binary),
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return binary
}

type gooseExportInfo struct {
	AccumulatedUsage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"accumulated_usage"`
}

// trackGooseUsage polls the named session's export output every tick
// and reports the delta since the last tick via reportFn. No-op for
// the lifetime of ctx when sessionName is empty -- there is no reliable
// lookup key in that case (see package doc comment above).
func trackGooseUsage(ctx context.Context, fullID, sessionName string, initialLastIn, initialLastOut int, tick time.Duration, reportFn func(sessID string, tokensIn, tokensOut, curIn, curOut int)) {
	if sessionName == "" || reportFn == nil {
		return
	}
	gooseLastTotals.Store(fullID, [2]int{initialLastIn, initialLastOut})
	defer gooseLastTotals.Delete(fullID)

	binary := gooseResolveBinary()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanGooseUsageOnce(ctx, binary, fullID, sessionName, reportFn)
		}
	}
}

func scanGooseUsageOnce(ctx context.Context, binary, fullID, sessionName string, reportFn func(sessID string, tokensIn, tokensOut, curIn, curOut int)) {
	out, err := exec.CommandContext(ctx, binary, "session", "export", "--name", sessionName, "--format", "json").Output() // #nosec G204 -- binary resolved from a fixed candidate list; sessionName is this daemon's own Session.Name, not external input
	if err != nil {
		return // export not available yet (session may not have started), or name mismatch -- try again next tick
	}
	var export gooseExportInfo
	if json.Unmarshal(out, &export) != nil {
		return
	}

	curIn, curOut := export.AccumulatedUsage.InputTokens, export.AccumulatedUsage.OutputTokens
	lastIn, lastOut := 0, 0
	if v, ok := gooseLastTotals.Load(fullID); ok {
		last := v.([2]int)
		lastIn, lastOut = last[0], last[1]
	}
	deltaIn, deltaOut := curIn-lastIn, curOut-lastOut
	gooseLastTotals.Store(fullID, [2]int{curIn, curOut})
	if curIn != lastIn || curOut != lastOut {
		reportFn(fullID, max(deltaIn, 0), max(deltaOut, 0), curIn, curOut)
	}
}
