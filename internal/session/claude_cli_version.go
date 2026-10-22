// Claude CLI version capture for the rate-limit drift guard. The effective
// rate-limit patterns are matched against claude's TUI output, so a Claude
// Code upgrade that rewords its limit line is a second drift axis beyond
// pattern edits. This file records what `claude --version` prints and feeds
// it into the recheck fingerprint (see patterns_version.go). Nothing here
// interprets the version text — it is treated as an opaque string.

package session

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// claudeCLIVersionUnknown is the recorded text when the claude binary is
// absent, the version command fails or times out, or it prints nothing.
// The digest logic treats it (and the empty string) as "no information",
// so an absent tool can never by itself mark the guard stale.
const claudeCLIVersionUnknown = "unknown"

// claudeCLIVersionTimeout bounds one `claude --version` run (start-up and
// output, not just scheduling). Five seconds covers a slow cold start while
// staying well under any UI poll cadence.
const claudeCLIVersionTimeout = 5 * time.Second

// claudeCLIVersionCacheTTL is how long a successful read of the version
// text stays fresh before ClaudeCLIVersion re-runs the command.
const claudeCLIVersionCacheTTL = 10 * time.Minute

// claudeCLIBinaryName is the command name the version runner invokes; it is
// resolved via PATH (exec.LookPath inside CommandContext), matching how the
// rest of the codebase finds the claude-code backend binary.
const claudeCLIBinaryName = "claude"

// CommandRunner runs one external command under ctx and returns its stdout.
// It mirrors the shape of os/exec's CommandContext so production can use
// exec.CommandContext directly and tests can count calls and return canned
// output without touching PATH.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultCommandRunner is the production CommandRunner.
func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// claudeCLIState holds the cached version text and the seams (runner,
// clock) for tests. The zero value is production-ready: nil runner means
// the default, nil clock means wall time, empty text means never read.
type claudeCLIState struct {
	mu     sync.Mutex
	runner CommandRunner    // nil = defaultCommandRunner
	text   string           // last recorded text; "" = never read
	readAt time.Time        // when text was last read
	clock  func() time.Time // nil = time.Now
}

// runClaudeCLIVersion runs `claude --version` once and returns the first
// trimmed line of the output, or claudeCLIVersionUnknown. It never blocks
// longer than claudeCLIVersionTimeout and never panics on failure modes.
func (m *Manager) runClaudeCLIVersion() string {
	st := &m.claudeCli
	st.mu.Lock()
	runner := st.runner
	if runner == nil {
		runner = defaultCommandRunner
	}
	st.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), claudeCLIVersionTimeout)
	defer cancel()
	out, err := runner(ctx, claudeCLIBinaryName, "--version")
	if err != nil {
		return claudeCLIVersionUnknown
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return claudeCLIVersionUnknown
	}
	return line
}

// setClaudeCLIVersionClock injects the clock ClaudeCLIVersion uses to judge
// cache freshness (nil restores wall time). Test seam only.
func (m *Manager) setClaudeCLIVersionClock(fn func() time.Time) {
	m.claudeCli.mu.Lock()
	m.claudeCli.clock = fn
	m.claudeCli.mu.Unlock()
}

// SetClaudeCLIVersionRunner swaps in a fake or custom runner (nil restores
// the default exec-based runner). The cache is not invalidated; call
// RefreshClaudeCLIVersion to read through the new runner immediately.
func (m *Manager) SetClaudeCLIVersionRunner(fn CommandRunner) {
	m.claudeCli.mu.Lock()
	m.claudeCli.runner = fn
	m.claudeCli.mu.Unlock()
}

// claudeCLINow is the clock behind the cache-freshness check: the injected
// clock when set, else time.Now.
func (m *Manager) claudeCLINow() time.Time {
	if fn := m.claudeCli.clock; fn != nil {
		return fn()
	}
	return time.Now()
}

// RefreshClaudeCLIVersion re-runs `claude --version`, stores the result in
// the manager cache, and returns the recorded text (claudeCLIVersionUnknown
// on any failure mode). It always runs the command; it does not consult the
// cache.
func (m *Manager) RefreshClaudeCLIVersion() string {
	text := m.runClaudeCLIVersion()
	st := &m.claudeCli
	st.mu.Lock()
	st.text = text
	st.readAt = m.claudeCLINow()
	st.mu.Unlock()
	return text
}

// ClaudeCLIVersion returns the cached claude CLI version text, re-running
// the command first when no read has happened yet or the last read is
// older than claudeCLIVersionCacheTTL. Concurrent callers may each run the
// command once while one is in flight; the command never runs while the
// cache mutex is held.
func (m *Manager) ClaudeCLIVersion() string {
	st := &m.claudeCli
	st.mu.Lock()
	fresh := st.text != "" && m.claudeCLINow().Sub(st.readAt) <= claudeCLIVersionCacheTTL
	text := st.text
	st.mu.Unlock()
	if fresh {
		return text
	}
	return m.RefreshClaudeCLIVersion()
}
