// B98 — aider sessions never populated TokensIn/TokensOut/EstCostUSD,
// same root cause as every other backend covered in this file's
// siblings. Unlike claude-code/opencode/goose, aider has no separate
// transcript or export command to poll: datawatch's aider.Launch runs
// one `aider --yes --message '<task>'` invocation directly in the tmux
// pane (confirmed in internal/llm/backends/aider/backend.go), and aider
// itself prints its own per-turn usage line to that same pane after
// each turn -- "Tokens: <sent> sent, <received> received." (confirmed
// by reading aider 0.86.2's own source, aider/coders/base_coder.py
// ~line 2023, and by a live `aider --model ollama_chat/qwen3:1.7b
// --yes --message ...` run against a local Ollama model). This file
// polls the same sess.LogFile the existing per-session monitor already
// tails, instead of a separate command or transcript.
//
// aider formats both numbers via its own format_tokens() (aider/
// utils.py): plain integers below 1000, "X.Yk" (one decimal) from
// 1,000-9,999, and "Nk" (rounded, no decimal) at 10,000+. That's a real
// precision loss for high-volume sessions (e.g. "15k" could be anywhere
// from 14,500-15,499) -- but it's the only usage signal aider's one-shot
// CLI mode exposes at all (no JSON export, no API), so an approximate
// real number is still strictly better than the status quo's exact zero.
package session

import (
	"bufio"
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// aiderUsageLinesRead tracks, per session FullID, how many pane-log
// lines have already been scanned for a "Tokens:" report -- same
// never-double-count rationale as claudeUsageLinesRead. Same-process
// cache only (v9.0.8): the authoritative checkpoint is
// Session.UsageLinesRead, persisted to disk, so a daemon restart
// resumes from there instead of re-counting from line 0. See that
// field's doc comment in store.go for the incident this fixed.
var aiderUsageLinesRead sync.Map // FullID -> int

// aiderTokensRe matches aider's own per-turn usage line, tolerating the
// optional "cache write"/"cache hit" segments some providers add
// between "sent" and "received".
var aiderTokensRe = regexp.MustCompile(`Tokens: (\S+) sent(?:, \S+ cache write)?(?:, \S+ cache hit)?, (\S+) received\.`)

// parseAiderTokenCount parses one side of aider's format_tokens() output
// ("610", "1.2k", "15k") back into an approximate integer token count.
func parseAiderTokenCount(s string) (int, bool) {
	mult := 1.0
	if strings.HasSuffix(s, "k") {
		mult = 1000
		s = strings.TrimSuffix(s, "k")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return int(f*mult + 0.5), true
}

// trackAiderUsage polls sess's own tmux pane log every tick and reports
// any newly-appeared "Tokens:" lines since the last tick via reportFn.
// Best-effort, same never-hard-fail design as trackClaudeCodeUsage.
func trackAiderUsage(ctx context.Context, fullID, logFile string, initialLinesRead int, tick time.Duration, reportFn func(sessID string, tokensIn, tokensOut, linesRead int)) {
	if logFile == "" || reportFn == nil {
		return
	}
	aiderUsageLinesRead.Store(fullID, initialLinesRead)
	defer aiderUsageLinesRead.Delete(fullID)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanAiderUsageOnce(logFile, fullID, reportFn)
		}
	}
}

func scanAiderUsageOnce(logFile, fullID string, reportFn func(sessID string, tokensIn, tokensOut, linesRead int)) {
	f, err := os.Open(logFile) // #nosec G304 -- path is this daemon's own session record, not external input
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck

	alreadyRead := 0
	if v, ok := aiderUsageLinesRead.Load(fullID); ok {
		alreadyRead = v.(int)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var sumIn, sumOut, lineNum int
	for scanner.Scan() {
		lineNum++
		if lineNum <= alreadyRead {
			continue
		}
		line := StripANSI(scanner.Text())
		m := aiderTokensRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sent, ok1 := parseAiderTokenCount(m[1])
		received, ok2 := parseAiderTokenCount(m[2])
		if !ok1 || !ok2 {
			continue
		}
		sumIn += sent
		sumOut += received
	}
	if lineNum > alreadyRead {
		aiderUsageLinesRead.Store(fullID, lineNum)
		reportFn(fullID, sumIn, sumOut, lineNum)
	}
}
