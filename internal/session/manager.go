package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/backends/opencode"
	"github.com/dmz006/datawatch/internal/llm/backends/ollama"
	"github.com/dmz006/datawatch/internal/llm/backends/openwebui"
	"github.com/dmz006/datawatch/internal/secfile"
	"github.com/fsnotify/fsnotify"
)

// ansiEscapeRe matches ANSI terminal escape sequences including:
// - CSI sequences: \x1b[...X
// - OSC sequences: \x1b]...(\x07|\x1b\\)
// - tmux passthrough: \x1bPtmux;...\x1b\\
// - DCS/PM/APC sequences: \x1bP...\x1b\\ , \x1b^...\x1b\\ , \x1b_...\x1b\\
// - Simple two-byte escapes: \x1bX where X is in [@-Z\\-_]
// - DEC private two-byte escapes: \x1b7, \x1b8, \x1b=, \x1b> (cursor save/restore, keypad mode)
var ansiEscapeRe = regexp.MustCompile(`\x1b\][^\x07]*(?:\x07|\x1b\\)|\x1bP[^\x1b]*\x1b\\|\x1b_[^\x1b]*\x1b\\|\x1b\^[^\x1b]*\x1b\\|\x1b(?:[78=>]|[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])`)

// cursorForwardRe matches ANSI cursor-forward sequences: \x1b[Nc where N >= 1.
// TUI applications (e.g. claude-code) use these instead of literal space characters.
var cursorForwardRe = regexp.MustCompile(`\x1b\[(\d+)C`)

// StripANSI removes ANSI escape sequences from s, expanding cursor-forward
// sequences (\x1b[NC) into N literal space characters so word spacing is preserved.
func StripANSI(s string) string {
	// Replace cursor-forward with equivalent spaces before stripping other escapes.
	s = cursorForwardRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := cursorForwardRe.FindStringSubmatch(m)
		if len(sub) < 2 {
			return ""
		}
		var n int
		fmt.Sscanf(sub[1], "%d", &n)
		if n > 80 {
			n = 80 // cap to avoid runaway padding
		}
		result := make([]byte, n)
		for i := range result {
			result[i] = ' '
		}
		return string(result)
	})
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// Rate limit detection patterns
var rateLimitPatterns = []string{
	"DATAWATCH_RATE_LIMITED:",
	"You've hit your limit",
	"rate limit exceeded",
	"quota exceeded",
	// Modern claude-code rate-limit dialog (shows a 1/2/3 menu where
	// option 1 is "Stop and wait for limit to reset"). Detected here so
	// the auto-select + schedule-resume flow fires immediately.
	"usage limit will reset",
	"Approaching usage limit",
	"limit will reset at",
	// v5.27.4 — operator-reported: rate-limit detection broke when
	// claude-code switched to the "5-hour limit reached" / "weekly
	// limit reached" phrasing. Newer claude prints both forms; the
	// "Approaching usage limit" + "limit will reset at" patterns
	// catch the WARN dialog (~80% of capacity) but not the actual
	// HARD limit message that ends the session.
	"limit reached",         // "5-hour limit reached", "Weekly limit reached"
	"weekly usage limit",    // "Weekly usage limit reached"
	"hit weekly limit",      // "Hit weekly limit"
	"5-hour limit",          // 5-hour-window phrasing
	"opus limit reached",    // model-specific limit phrasings
	"sonnet limit reached",
	// BL240 (v6.2.0) — additional patterns for missed dialogs observed 2026-05-03.
	"claude usage limit",    // "Claude usage limits" header in newer dialogs
	"you've exceeded",       // "You've exceeded your Claude plan"
	"plan limit",            // "plan limit reached" variants
	"resets at",             // "Resets at 2pm" — reset time announcement (also feeds parser)
	"stop and wait",         // Part of the option text in some dialog formats
	"wait for limit to reset", // Option 1 text — appears when dialog auto-wraps to next line
	// BL262 (v6.11.3) — operator-reported 2026-05-05: "You're out of extra
	// usage · resets 11:50am (America/New_York)". The "out of extra usage"
	// phrasing isn't covered by any existing pattern; the existing
	// parseClaudeClockTime "resets " marker handles the time-extraction
	// half once the line is detected as rate-limited.
	"out of extra usage",    // "You're out of extra usage · resets ..."
	"you're out of",         // wider catch for "You're out of credits/quota/usage" phrasings
}

// Completion detection patterns. All matched against the trimmed
// prompt line via strings.HasPrefix.
//
// v6.11.7 — reverted v6.11.6's aggressive natural-language patterns.
// "Done!", "All done", and similar phrases appear constantly in
// claude-code prose, not just at task end. When the daemon restarted
// and replayed the last bit of pane buffer, these false-fired and
// marked sessions as complete prematurely → operator's PWA stopped
// reconnecting because the session was now in a terminal state.
//
// Reverted to the pre-v6.11.6 marker-only set. The right way to
// add natural-language detection is via configurable per-backend
// patterns in cfg.Detection.CompletionPatterns; that lets the
// operator opt in with project-specific phrasing and unit-test the
// matchers before they go live globally.
var completionPatterns = []string{
	"DATAWATCH_COMPLETE:",
}

// Input needed patterns (explicit protocol)
var inputNeededPatterns = []string{
	"DATAWATCH_NEEDS_INPUT:",
}

// MCP failure detection pattern — triggers auto-retry via /mcp command
var mcpFailedPattern = "MCP server failed"

// effectivePromptPatterns returns patterns from config or hardcoded fallback.
func (m *Manager) effectivePromptPatterns() []string {
	if len(m.detection.PromptPatterns) > 0 {
		return m.detection.PromptPatterns
	}
	return promptPatterns
}

func (m *Manager) effectiveRateLimitPatterns() []string {
	if len(m.detection.RateLimitPatterns) > 0 {
		return m.detection.RateLimitPatterns
	}
	return rateLimitPatterns
}

func (m *Manager) effectiveCompletionPatterns() []string {
	if len(m.detection.CompletionPatterns) > 0 {
		return m.detection.CompletionPatterns
	}
	return completionPatterns
}

func (m *Manager) effectiveInputNeededPatterns() []string {
	if len(m.detection.InputNeededPatterns) > 0 {
		return m.detection.InputNeededPatterns
	}
	return inputNeededPatterns
}

// promptPatterns detects when an LLM is waiting for user input (used in idle detection).
var promptPatterns = []string{
	"? ", "> ", "$ ", "# ", "[y/N]", "[Y/n]", "(y/n)", "[yes/no]",
	// claude-code permission prompts
	"Do you want to", "Allow ", "Deny ", "Trust ", "trust the files",
	"(y/n/always)", "(yes/no/always)", "Allow this action",
	"Would you like", "Proceed?", "[A]llow", "[D]eny",
	// claude-code folder trust (numbered menu)
	"Yes, I trust", "No, exit", "trust this folder", "Quick safety check",
	"Is this a project", "1. Yes", "2. No",
	// generic numbered menu
	"❯ 1.", "❯ 2.",
	// claude-code confirmation footer (appears in trust prompt and tool approval prompts)
	"Enter to confirm", "Esc to cancel",
	// claude-code --dangerously-load-development-channels consent prompt
	"I am using this for local development", "Loading development channels",
	// opencode-acp idle/ready state
	"[opencode-acp] awaiting input", "[opencode-acp] ready",
	// ollama interactive prompt
	">>> ",
	// opencode TUI prompt
	"Ask anything",
	// claude-code rate limit and other interactive prompts
	"What do you want to do?",
	"Esc to back", "Esc to go back",
	"↑↓ to navigate",
	// claude-code prompt (Unicode)
	"❯",
	// datawatch shell prompt (set by shell backend)
	"datawatch:",
}

// LaunchFunc is a function that launches an LLM backend in a tmux session.
type LaunchFunc func(ctx context.Context, task, tmuxSession, projectDir, logFile string) error

// Manager manages LLM coding sessions via tmux.
type Manager struct {
	hostname    string
	dataDir     string
	llmBin      string // fallback binary path (legacy; prefer launchFn)
	llmBackend  string      // active backend name
	launchFn    LaunchFunc  // active backend launch function
	backendObj  llm.Backend // backend object for optional interface dispatch
	maxSessions    int
	workspaceRoot  string // F10: container/PVC base for relative project_dirs
	store          *Store
	tmux           TmuxAPI
	idleTimeout    time.Duration
	autoGit        bool   // whether to auto-commit project dir
	autoGitInit    bool   // whether to git init project dir if needed
	verbose        bool   // enable debug logging
	mcpMaxRetries  int    // max MCP restart attempts per session (0 = disabled)
	encKey         []byte // AES-256 key for encrypting session logs (nil = plaintext)
	secureTracking string // "full" = encrypt tracker .md files too

	// mcpRetryCounts tracks per-session MCP retry attempts.
	mcpRetryCounts map[string]int

	// encFIFOs tracks encrypting FIFOs per session for cleanup.
	encFIFOs map[string]*secfile.EncryptingFIFO

	// streamPipes tracks real-time streaming pipes per session.
	streamPipes map[string]*StreamingPipe

	// onStateChange is called when a session changes state.
	// Used by the router to send Signal notifications.
	onStateChange func(sess *Session, oldState State)

	// onNeedsInput is called when a session needs user input.
	onNeedsInput func(sess *Session, prompt string)

	// onOutput is called for each new line of output from a session (ANSI stripped).
	onOutput func(sess *Session, line string)

	// onRawOutput is called for each new line of raw output (ANSI preserved, for log-mode sessions).
	onRawOutput func(sess *Session, rawLine string)

	// onScreenCapture is called with clean pane capture lines (for terminal-mode web display).
	onScreenCapture func(sess *Session, lines []string)

	// onSessionStart is called immediately after a session is successfully started.
	onSessionStart func(sess *Session)

	// onPreLaunch is called just before the LLM backend is launched (for per-session setup like MCP registration).
	onPreLaunch func(sess *Session)

	// onSessionEnd is called when a session reaches a terminal state (complete/failed/killed).
	onSessionEnd func(sess *Session)

	// onChatMessage is called when a chat-mode backend emits a structured message.
	onChatMessage func(sessionID, role, content string, streaming bool)

	// chatMemoryFn intercepts memory commands in chat-mode sessions.
	chatMemoryFn func(tmuxSession, text string) (string, bool)

	// onResponseCaptured is called when a session's last response is captured
	// (running→waiting_input transition). Used for alerts, memory, and web UI.
	onResponseCaptured func(sess *Session, response string)

	// onRateLimitFallback is called when a session hits a rate limit and fallback chain
	// is configured. The callback should start a new session with the next profile.
	onRateLimitFallback func(sess *Session)

	// cfg holds the full config reference for per-LLM settings lookup.
	cfg *config.Config

	// rawInputBuf accumulates typed characters per-session for input logging.
	// Flushed to session.LastInput on Enter key.
	rawInputBuf map[string]string

	// detection holds the active detection patterns (from config or defaults).
	detection config.DetectionConfig

	// schedStore is the schedule store for deferred sessions and timed commands.
	schedStore *ScheduleStore

	// scheduleSettleMs (B30) — when > 0, scheduled SendInput calls split
	// the text push and Enter into two tmux calls with this delay between
	// them. Fixes the 2nd-Enter bug for TUIs slow to reach phase-4 input
	// readiness after their prompt state transition fires.
	scheduleSettleMs int

	// defaultEffort (BL41) — applied to Session.Effort when neither the
	// caller's StartOptions.Effort nor a previous session's value is set.
	// Default "normal".
	defaultEffort string

	// cooldown (BL30) — global rate-limit pause state.
	cooldown *cooldownTracker

	// rateLimitGlobalPause (BL30) — operator opt-in: when true,
	// Start refuses while a cooldown is active.
	rateLimitGlobalPause bool

	// costRates (BL6) — backend → CostRate. nil = DefaultCostRates().
	costRates map[string]CostRate

	// promptDebounce tracks per-session prompt debounce state.
	// Key: fullID, Value: time when prompt was first detected in current window.
	promptFirstSeen map[string]time.Time
	// promptLastNotify tracks when the last needs-input notification was sent per session.
	promptLastNotify map[string]time.Time
	// promptOscillation tracks rapid state flips per session for backoff.
	// Key: fullID, Value: timestamps of recent running→waiting transitions.
	promptOscillation map[string][]time.Time

	mu       sync.Mutex
	monitors map[string]context.CancelFunc // fullID -> cancel func for monitor goroutine
	trackers map[string]*Tracker           // fullID -> Tracker
}

// NewManager creates a new session Manager.
// maxSessions limits concurrent active sessions (0 means no limit).
// An optional encKey (32 bytes) enables AES-256-GCM encryption of the session store.
// llmBin is the fallback binary path used only when no launchFn is configured (legacy claude-code path).
func NewManager(hostname, dataDir, llmBin string, idleTimeout time.Duration, encKey ...[]byte) (*Manager, error) {
	storePath := filepath.Join(dataDir, "sessions.json")
	var key []byte
	if len(encKey) > 0 {
		key = encKey[0]
	}
	var store *Store
	var err error
	if key != nil {
		store, err = NewStoreEncrypted(storePath, key)
	} else {
		store, err = NewStore(storePath)
	}
	if err != nil {
		return nil, fmt.Errorf("open session store: %w", err)
	}

	// Ensure the sessions directory exists (for tracking folders)
	if err := os.MkdirAll(filepath.Join(dataDir, "sessions"), 0755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	return &Manager{
		hostname:       hostname,
		dataDir:        dataDir,
		llmBin:         llmBin,
		llmBackend:     "claude-code",
		maxSessions:    10,
		store:          store,
		tmux:           &TmuxManager{},
		idleTimeout:    idleTimeout,
		mcpMaxRetries:  5,
		mcpRetryCounts:   make(map[string]int),
		encKey:           key,
		promptFirstSeen:   make(map[string]time.Time),
		promptLastNotify:  make(map[string]time.Time),
		promptOscillation: make(map[string][]time.Time),
		monitors:         make(map[string]context.CancelFunc),
		trackers:         make(map[string]*Tracker),
	}, nil
}

// DataDir returns the data directory the manager was initialised
// with. Used by external packages (server REST handlers, CLI) to
// resolve relative session paths under <dataDir>/sessions/.
func (m *Manager) DataDir() string { return m.dataDir }

// ReapOrphanWorkspaces removes <dataDir>/workspaces/ subdirectories
// that no live session references. Daemon-side closure for the
// crash-recovery gap left after v5.26.26: per-session reaping on
// Manager.Delete catches the happy path, but if the daemon dies
// mid-session (or someone removes a session record by hand), the
// cloned workspace persists with no Session.ProjectDir pointing at
// it. Call once at startup before new sessions can spawn.
//
// v5.26.27 — operator-asked follow-up to the per-session reaper.
// Intentionally conservative: only sweeps direct children of
// <dataDir>/workspaces/ and matches against ALL stored sessions'
// ProjectDirs (not just EphemeralWorkspace=true ones), so an
// operator who pointed project_dir under workspaces/ for some
// reason still keeps their tree.
func (m *Manager) ReapOrphanWorkspaces() (removed []string, err error) {
	wsRoot := filepath.Join(m.dataDir, "workspaces")
	entries, rerr := os.ReadDir(wsRoot)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, nil // nothing to reap
		}
		return nil, fmt.Errorf("read workspaces root: %w", rerr)
	}

	// Build set of in-use ProjectDirs across all stored sessions.
	inUse := make(map[string]struct{})
	for _, sess := range m.store.List() {
		if sess.ProjectDir != "" {
			inUse[filepath.Clean(sess.ProjectDir)] = struct{}{}
		}
	}

	for _, ent := range entries {
		if !ent.IsDir() {
			continue // skip stray files
		}
		full := filepath.Clean(filepath.Join(wsRoot, ent.Name()))
		if _, used := inUse[full]; used {
			continue
		}
		if rmErr := os.RemoveAll(full); rmErr != nil {
			fmt.Printf("[warn] reap orphan workspace %s: %v\n", full, rmErr)
			continue
		}
		removed = append(removed, full)
	}
	return removed, nil
}

// SetMCPMaxRetries sets the maximum MCP restart attempts per session.
func (m *Manager) SetMCPMaxRetries(n int) { m.mcpMaxRetries = n }

// SetRateLimitGlobalPause (BL30) toggles whether new session starts
// are blocked while a global cooldown is active.
func (m *Manager) SetRateLimitGlobalPause(on bool) {
	m.rateLimitGlobalPause = on
}

// RateLimitGlobalPause returns the current opt-in state.
func (m *Manager) RateLimitGlobalPause() bool { return m.rateLimitGlobalPause }

// SetDefaultEffort (BL41) configures the default Effort applied to
// new sessions when the caller doesn't supply one. Invalid values
// silently fall back to "normal".
func (m *Manager) SetDefaultEffort(effort string) {
	if !IsValidEffort(effort) || effort == "" {
		effort = "normal"
	}
	m.defaultEffort = effort
}

// DefaultEffort returns the configured default.
func (m *Manager) DefaultEffort() string {
	if m.defaultEffort == "" {
		return "normal"
	}
	return m.defaultEffort
}

// resolveEffort picks the effort string for a new session: explicit
// StartOptions.Effort wins, then manager default, then "normal".
func (m *Manager) resolveEffort(opt *StartOptions) string {
	if opt != nil && opt.Effort != "" && IsValidEffort(opt.Effort) {
		return opt.Effort
	}
	return m.DefaultEffort()
}

// SetScheduleSettleMs (B30) configures the two-step send delay for
// scheduled commands. 0 disables (legacy behaviour).
func (m *Manager) SetScheduleSettleMs(ms int) {
	if ms < 0 {
		ms = 0
	}
	m.scheduleSettleMs = ms
}

// ScheduleSettleMs returns the currently configured settle delay.
func (m *Manager) ScheduleSettleMs() int { return m.scheduleSettleMs }

// SetVerbose enables debug logging for session operations.
func (m *Manager) SetVerbose(v bool) { m.verbose = v }

// debugf logs a debug message if verbose mode is enabled.
func (m *Manager) debugf(format string, args ...interface{}) {
	if m.verbose {
		fmt.Printf("[session:debug] "+format+"\n", args...)
	}
}

// SetAutoGit configures automatic git commit behaviour for the project directory.
func (m *Manager) SetAutoGit(autoGit, autoGitInit bool) {
	m.autoGit = autoGit
	m.autoGitInit = autoGitInit
}

// SetSecureTracking sets the tracker encryption mode ("full" or "log_only").
func (m *Manager) SetSecureTracking(mode string) { m.secureTracking = mode }

// SetWorkspaceRoot configures the F10 container/PVC base path under which
// relative session project_dirs are resolved. Empty disables the rewrite
// (bare-metal back-compat).
func (m *Manager) SetWorkspaceRoot(root string) { m.workspaceRoot = root }

// BackfillOutputMode sets output_mode on existing sessions that don't have it,
// reading from the per-backend config. Call after SetConfig.
func (m *Manager) BackfillOutputMode() {
	if m.cfg == nil {
		return
	}
	for _, sess := range m.store.List() {
		changed := false
		if sess.OutputMode == "" {
			mode := m.cfg.GetOutputMode(sess.LLMBackend)
			if mode != "terminal" {
				sess.OutputMode = mode
				changed = true
			}
		}
		// Also backfill input_mode from config
		if sess.InputMode == "" {
			mode := m.cfg.GetInputMode(sess.LLMBackend)
			if mode != "tmux" { // only backfill non-default
				sess.InputMode = mode
				changed = true
			}
		}
		// Fix stale input_mode: if config says tmux but session says none, update
		if sess.InputMode == "none" {
			cfgMode := m.cfg.GetInputMode(sess.LLMBackend)
			if cfgMode == "tmux" {
				sess.InputMode = "tmux"
				changed = true
			}
		}
		if changed {
			_ = m.store.Save(sess)
		}
	}
}

// SetLLMBackend sets the active LLM backend name and launch function.
func (m *Manager) SetLLMBackend(name string, fn LaunchFunc) {
	m.llmBackend = name
	m.launchFn = fn
}

// SetLLMBackendName updates the default backend name without
// replacing the launch function. Used by POST /api/backends/active
// (v4.0.3) when the caller only wants to switch the default and
// reuse the existing launcher registry.
func (m *Manager) SetLLMBackendName(name string) {
	m.llmBackend = name
}

// SetLLMBackendObj stores the backend object for optional interface dispatch (e.g. Resumable).
func (m *Manager) SetLLMBackendObj(b llm.Backend) {
	m.backendObj = b
}

// ActiveBackend returns the name of the currently active LLM backend.
func (m *Manager) ActiveBackend() string {
	return m.llmBackend
}

// IsEncrypted returns true if the manager was initialized with an encryption key.
func (m *Manager) IsEncrypted() bool {
	return m.encKey != nil
}

// SetStateChangeHandler sets the callback invoked on session state transitions.
func (m *Manager) SetStateChangeHandler(fn func(*Session, State)) {
	m.onStateChange = fn
}

// SetNeedsInputHandler sets the callback invoked when a session waits for input.
func (m *Manager) SetNeedsInputHandler(fn func(*Session, string)) {
	m.onNeedsInput = fn
}

// StateChangeHandler returns the currently registered state-change callback (may be nil).
func (m *Manager) StateChangeHandler() func(*Session, State) {
	return m.onStateChange
}

// NeedsInputHandler returns the currently registered needs-input callback (may be nil).
func (m *Manager) NeedsInputHandler() func(*Session, string) {
	return m.onNeedsInput
}

// SetOutputHandler sets the callback invoked for each new output line from a session.
func (m *Manager) SetOutputHandler(fn func(*Session, string)) {
	m.onOutput = fn
}

// SetRawOutputHandler sets the callback for raw output (ANSI preserved, for xterm.js).
func (m *Manager) SetRawOutputHandler(fn func(*Session, string)) {
	m.onRawOutput = fn
}

// SetScreenCaptureHandler sets the callback for clean capture-pane lines (pane_capture WS).
func (m *Manager) SetScreenCaptureHandler(fn func(*Session, []string)) {
	m.onScreenCapture = fn
}

// SetOnSessionStart sets the callback invoked immediately after a session starts successfully.
func (m *Manager) SetOnSessionStart(fn func(*Session)) {
	m.onSessionStart = fn
}

// SetOnPreLaunch sets a callback invoked just before the LLM backend is launched.
func (m *Manager) SetOnPreLaunch(fn func(*Session)) {
	m.onPreLaunch = fn
}

// SetOnSessionEnd sets a callback invoked when a session reaches a terminal state.
func (m *Manager) SetOnSessionEnd(fn func(*Session)) {
	m.onSessionEnd = fn
}

// SetOnChatMessage sets the callback for structured chat messages from chat-mode backends.
func (m *Manager) SetOnChatMessage(fn func(sessionID, role, content string, streaming bool)) {
	m.onChatMessage = fn
}

// EmitChatMessage fires the chat message callback if registered.
// BL264 / v6.11.18 — also marks channel activity so chat-mode sessions
// transition WaitingInput → Running on assistant/user messages, same
// as MCP/ACP channel-bearing sessions.
func (m *Manager) EmitChatMessage(sessionID, role, content string, streaming bool) {
	if m.onChatMessage != nil {
		m.onChatMessage(sessionID, role, content, streaming)
	}
	// Don't bump on system/transient indicator chunks ("processing...",
	// "thinking...", "ready") — those don't represent real activity.
	// v6.11.19 — pass content so the classifier can detect completion /
	// input-needed signals from the message text itself.
	if role == "user" || role == "assistant" {
		m.MarkChannelActivityFromText(sessionID, content)
	}
}

// SetOnResponseCaptured sets the callback invoked when a session's last response is captured.
func (m *Manager) SetOnResponseCaptured(fn func(*Session, string)) {
	m.onResponseCaptured = fn
}

// CaptureResponse reads the last LLM response for a session. For claude-code,
// reads /tmp/claude/response.md. Falls back to the last N lines of tmux output.
func (m *Manager) CaptureResponse(sess *Session) string {
	// Try /tmp/claude/response.md first (Claude Code /copy output)
	if data, err := os.ReadFile("/tmp/claude/response.md"); err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data))
	}
	// Fallback: capture last 30 lines from tmux. v5.26.15 — operator-
	// reported: response capture should filter out animation spinners
	// and TUI status footers so the 📄 Response viewer shows useful
	// text only. stripResponseNoise drops single-glyph spinner lines,
	// status timers (e.g. "(7s · timeout 1m)"), and the standard
	// claude-code footer hints ("esc to interrupt", "shift+tab to
	// cycle", etc).
	tail, err := m.TailOutput(sess.FullID, 30)
	if err == nil && tail != "" {
		return stripResponseNoise(tail)
	}
	return ""
}

// stripResponseNoise filters lines from a captured response that are
// purely TUI animation / status decoration. Conservative — preserves
// any line that contains substantive prose; only drops lines that
// match the known noise patterns.
func stripResponseNoise(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		// Strip ANSI first so the noise check sees plain text.
		clean := StripANSI(raw)
		clean = strings.TrimRight(clean, " \t\r")
		trimmed := strings.TrimSpace(clean)
		if trimmed == "" {
			out = append(out, "") // preserve paragraph breaks
			continue
		}
		if isResponseNoiseLine(trimmed) {
			continue
		}
		out = append(out, clean)
	}
	// Collapse 3+ consecutive blank lines down to 1.
	collapsed := make([]string, 0, len(out))
	blanks := 0
	for _, l := range out {
		if l == "" {
			blanks++
			if blanks <= 1 {
				collapsed = append(collapsed, l)
			}
			continue
		}
		blanks = 0
		collapsed = append(collapsed, l)
	}
	return strings.TrimSpace(strings.Join(collapsed, "\n"))
}

// isResponseNoiseLine returns true for lines that are purely TUI
// animation / status decoration. Order matters — most specific tests
// first.
//
// v5.26.23 — operator-reported regression: "Last response now has
// only the animated stuff and not real response data." v5.26.15's
// pattern list caught box-drawing characters anywhere in the line,
// which killed real prose framed by claude's TUI borders (e.g.
// `│  Here is your answer  │`). Re-tightened so prose-with-decoration
// passes through; only PURE decoration is filtered. The new rule:
// a line is "noise" only when it has < 3 consecutive letters
// (substantive prose has at least one 3+-letter word) AND matches
// one of the structural shapes below.
func isResponseNoiseLine(s string) bool {
	// Pure-glyph spinner / progress / box-drawing lines.
	switch s {
	case "*", "·", "●", "○", "◯", "◉", "✢", "✶", "✺", "✻", "✽", "⏳",
		"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
		"❯", "│", "─", "•", "╭", "╮", "╯", "╰", "├", "┤", "┬", "┴", "┼":
		return true
	}
	// All-decoration multi-char border lines like "╭────────╮" or
	// "├─────┤" — every rune is a box-drawing char or whitespace.
	if isPureBoxDrawing(s) {
		return true
	}
	// v5.26.31 — TUI label borders like "──── datawatch claude ──"
	// (mostly ─/│ with a short embedded title). Real prose doesn't
	// look like this; the previous filter passed it through because
	// "datawatch claude" hits hasWord3.
	if isLabeledBorder(s) {
		return true
	}
	// Pure status-timer fragments: e.g. "(7s · timeout 1m)" or
	// "(5s)". Match BEFORE the hasWord3 gate because "timeout" /
	// "remaining" have 3+ letters but are still timer noise.
	if isPureStatusTimer(s) {
		return true
	}
	// v5.26.31 — embedded status-timer fragments inside a longer
	// progress/spinner line, e.g.
	//   `* Perambulating… (11m 42s · ↓ 22.2k tokens · thinking)`
	// The full line passes hasWord3 ("Perambulating") so the anchored-
	// footer rule kept it. Detect the timer signature directly.
	if hasEmbeddedStatusTimer(s) {
		return true
	}
	// v5.26.31 — single-spinner + digit/whitespace only. Lines like
	// `✻                                2` carry no prose but were
	// missed by the multi-spinner check (count=1) and the pure-glyph
	// switch (the digit broke it).
	if isSpinnerCounter(s) {
		return true
	}
	// v5.26.31 — bare digits + whitespace only ("                  2").
	// Claude pane-capture sometimes leaves the spinner column on the
	// previous line and only the counter on the next. Real prose has
	// at least one letter.
	if isPureDigitLine(s) {
		return true
	}
	// v5.26.31 — broaden the literal-footer match. Previous logic
	// only ran the anywhere-in-line check when hasWord3 was false,
	// so footer hints with leading TUI glyphs like
	//   `⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt`
	// snuck through. Now we apply it unconditionally — accept the
	// rare false-positive ("the doc says press esc to interrupt" in
	// real prose) for the much larger correct-positive volume.
	noisePatterns := []string{
		"esc to interrupt", "esc to back", "esc to go back",
		"shift+tab to cycle", "ctrl+b ctrl+b", "ctrl+b ", " ctrl+b",
		"↑↓ to navigate", "↑/↓", "enter to confirm", "esc to cancel",
		"· timeout", "· budget",
		"running in background", "(in background)",
		"to run in background",
		"bypass permissions",
		"press up to edit", "edit queued messages",
		"datawatch_complete:",
	}
	lower := strings.ToLower(s)
	for _, p := range noisePatterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	if !hasWord3(s) {
		// No prose AND none of the patterns hit — multi-spinner check
		// is the last gate.
		spinners := []rune{'✢', '✶', '✺', '✻', '✽', '●', '○', '◯', '◉', '*'}
		spinnerHits := 0
		for _, r := range s {
			for _, sp := range spinners {
				if r == sp {
					spinnerHits++
					break
				}
			}
		}
		if spinnerHits >= 2 {
			return true
		}
	}
	return false
}

// isLabeledBorder catches "──── label ──" style TUI section dividers
// where most of the line is `─` runs and there's a short embedded
// label. Threshold: ≥60% of non-space runes are box-drawing AND the
// line has at least 6 box-drawing runes total. v5.26.31.
func isLabeledBorder(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	box := 0
	nonSpace := 0
	for _, r := range t {
		if r == ' ' || r == '\t' {
			continue
		}
		nonSpace++
		switch r {
		case '╭', '╮', '╯', '╰', '├', '┤', '┬', '┴', '┼', '│', '─', '═',
			'╞', '╡', '╪':
			box++
		}
	}
	if nonSpace < 8 || box < 6 {
		return false
	}
	return float64(box)/float64(nonSpace) >= 0.60
}

// hasEmbeddedStatusTimer catches lines like
//   `* Perambulating… (11m 42s · ↓ 22.2k tokens · thinking)`
// where the parenthesized timer fragment sits inside a longer line.
// Signal: a digit immediately followed by `s `, `m `, or `ms` + a `·`
// somewhere on the line. v5.26.31.
func hasEmbeddedStatusTimer(s string) bool {
	if !strings.Contains(s, "·") {
		return false
	}
	// look for "<digit><unit><space>" or "<digit><unit><end>"
	r := []rune(s)
	for i := 0; i < len(r)-1; i++ {
		if r[i] >= '0' && r[i] <= '9' {
			// peek next char or two
			n := r[i+1]
			if n == 's' || n == 'm' || n == 'h' {
				// peek char after the unit; if absent, end-of-line, or
				// whitespace/punct — count it as a timer marker.
				if i+2 >= len(r) {
					return true
				}
				after := r[i+2]
				if after == ' ' || after == '\t' || after == ')' || after == 's' /* "ms" */ {
					return true
				}
			}
		}
	}
	return false
}

// isPureDigitLine catches lines that are only digits + whitespace,
// optionally with a trailing unit fragment (`10s`, `5ms`, `2h`,
// `10s)`). Real prose has at least 3 letters in a row; this rule
// catches stray status-timer fragments the v5.26.31 first pass
// missed (operator spotted "10s)" leaking through).
func isPureDigitLine(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	digits := 0
	letters := 0
	for _, r := range t {
		if r >= '0' && r <= '9' {
			digits++
			continue
		}
		if r == ' ' || r == '\t' {
			continue
		}
		// Allow timer unit chars + trailing punctuation typical of
		// fragments: s, m, h, ms, ), (, %, and middot.
		switch r {
		case 's', 'm', 'h', ')', '(', '%', '·', '↑', '↓', ',':
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letters++
			if letters >= 3 {
				return false // real word present
			}
			continue
		}
		return false
	}
	// Need at least one digit AND no real word. Pure-letter "abc"
	// shouldn't reach here, but defend against it.
	return digits >= 1 && letters < 3
}

// isSpinnerCounter catches single-spinner lines with a trailing
// counter, e.g. `✻                                2`. Real prose
// doesn't look like this. v5.26.31.
func isSpinnerCounter(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	spinnerRunes := map[rune]bool{
		'✢': true, '✶': true, '✺': true, '✻': true, '✽': true,
		'●': true, '○': true, '◯': true, '◉': true, '*': true,
		'⠋': true, '⠙': true, '⠹': true, '⠸': true, '⠼': true,
		'⠴': true, '⠦': true, '⠧': true, '⠇': true, '⠏': true,
		// v5.26.31 — claude uses '·' (middot) as a spinner frame too,
		// and '❯' appears as a queue-line marker prefix.
		'·': true, '❯': true,
	}
	spinnerCount := 0
	letterCount := 0
	for _, r := range t {
		if r == ' ' || r == '\t' {
			continue
		}
		if spinnerRunes[r] {
			spinnerCount++
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letterCount++
			if letterCount >= 3 {
				return false // real word present
			}
		}
		// other non-spinner / non-digit / non-letter — assume it's not
		// a pure spinner-counter line
		return false
	}
	return spinnerCount >= 1
}

// hasWord3 returns true when s contains a run of 3+ ASCII letters
// (case-insensitive). Cheap stand-in for "this looks like prose"
// that doesn't trip on numeric-only lines, spinner-only lines, or
// box-drawing lines.
func hasWord3(s string) bool {
	streak := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			streak++
			if streak >= 3 {
				return true
			}
		} else {
			streak = 0
		}
	}
	return false
}

// isPureBoxDrawing returns true for lines that are entirely box-
// drawing chars + whitespace (no prose, no numbers). Catches
// "╭────╮" / "├────┤" / "│   │" border-only lines.
func isPureBoxDrawing(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	saw := false
	for _, r := range t {
		switch r {
		case '╭', '╮', '╯', '╰', '├', '┤', '┬', '┴', '┼', '│', '─', '═',
			'╞', '╡', '╪':
			saw = true
		case ' ', '\t':
			// whitespace allowed
		default:
			return false
		}
	}
	return saw
}

// isPureStatusTimer returns true for parenthesized status-timer
// lines like "(7s · timeout 1m)" or "(5s)". The "(timeout" /
// "(elapsed" / "·" / "min" markers + a digit are the signal; the
// length cap rules out long prose that happens to be parenthesized.
func isPureStatusTimer(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) > 60 {
		return false
	}
	if !strings.HasPrefix(t, "(") || !strings.HasSuffix(t, ")") {
		return false
	}
	hasDigit := false
	for _, r := range t {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return false
	}
	// Match the well-known timer keywords. Without one, a generic
	// "(see note 1)" is allowed through.
	lower := strings.ToLower(t)
	for _, kw := range []string{
		"timeout", "elapsed", "remaining", "cooldown", "budget",
		"in background", "ctrl+b",
	} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	// Pure "(5s)" or "(123ms)" — second-or-millisecond duration in
	// parens with no other content. Conservative: require the s/ms
	// suffix.
	bare := strings.TrimSuffix(strings.TrimPrefix(t, "("), ")")
	bare = strings.TrimSpace(bare)
	if strings.HasSuffix(bare, "s") || strings.HasSuffix(bare, "ms") {
		// Quick check that the prefix is purely numeric.
		stripped := strings.TrimSuffix(strings.TrimSuffix(bare, "ms"), "s")
		stripped = strings.TrimSpace(stripped)
		if stripped != "" {
			allDigits := true
			for _, r := range stripped {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return true
			}
		}
	}
	return false
}

// lastResponseCache (BL291, v5.5.0) — short TTL cache so a flurry of
// /api/sessions/response or chat-channel reads doesn't trigger one
// CaptureResponse per call. CaptureResponse fans out to TailOutput
// which can read the entire encrypted session log (no seek-tail path
// for the encrypted reader); without this cache a chat reply burst on
// a multi-megabyte encrypted log measurably bloated daemon RSS.
//
// Bounded to lastResponseCacheMax entries so the sync.Map doesn't grow
// forever as new sessionIDs accumulate over a long-lived daemon. Eviction
// is best-effort: when the cache is at capacity an entry whose `at` is
// older than the TTL is dropped on every Store.
var lastResponseCache sync.Map // fullID → *cachedResponse
var lastResponseEvictAt time.Time

const lastResponseCacheMax = 256

type cachedResponse struct {
	at   time.Time
	body string
}

// evictStaleResponseCache walks the cache once and drops any entry whose
// `at` is older than 5×TTL. Cheap (size cap is small) and only invoked
// when the cache is at/over its bound.
func evictStaleResponseCache() {
	cutoff := time.Now().Add(-10 * time.Second)
	count := 0
	lastResponseCache.Range(func(k, v any) bool {
		count++
		if c, ok := v.(*cachedResponse); ok && c.at.Before(cutoff) {
			lastResponseCache.Delete(k)
		}
		return true
	})
	// If still over, drop a few arbitrary entries to claw back room.
	if count > lastResponseCacheMax {
		dropped := 0
		lastResponseCache.Range(func(k, _ any) bool {
			lastResponseCache.Delete(k)
			dropped++
			return dropped < count-lastResponseCacheMax
		})
	}
}

// GetLastResponse returns the most recent LLM response for a session.
// BL178 (v5.1.0 reopen) — for sessions that are still alive (running or
// waiting_input), re-capture from the live tmux pane on every read so a
// browser tab that's been open for days never sees a frozen reply.
// For terminated sessions the stored value is the last word and is
// returned as-is.
//
// BL291 (v5.5.0) — the live re-capture path is gated behind a 2-second
// TTL cache. See lastResponseCache above.
func (m *Manager) GetLastResponse(fullID string) string {
	sess, ok := m.store.Get(fullID)
	if !ok {
		// Try short ID
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return ""
		}
	}
	switch sess.State {
	case StateRunning, StateWaitingInput:
		if v, ok := lastResponseCache.Load(sess.FullID); ok {
			if c, ok := v.(*cachedResponse); ok && time.Since(c.at) < 2*time.Second {
				return c.body
			}
		}
		if fresh := m.CaptureResponse(sess); fresh != "" {
			now := time.Now()
			lastResponseCache.Store(sess.FullID, &cachedResponse{at: now, body: fresh})
			// Best-effort eviction so the cache doesn't grow forever.
			// Walk every ~10 s; cheap and bounded.
			if now.Sub(lastResponseEvictAt) > 10*time.Second {
				lastResponseEvictAt = now
				evictStaleResponseCache()
			}
			if fresh != sess.LastResponse {
				sess.LastResponse = fresh
				_ = m.store.Save(sess)
			}
			return fresh
		}
	}
	return sess.LastResponse
}

// SetChatMemoryHandler sets a handler for memory commands in chat-mode sessions.
func (m *Manager) SetChatMemoryHandler(fn func(tmuxSession, text string) (string, bool)) {
	m.chatMemoryFn = fn
}

// SetOnRateLimitFallback sets the callback for triggering fallback chain on rate limit.
func (m *Manager) SetOnRateLimitFallback(fn func(*Session)) {
	m.onRateLimitFallback = fn
}

// OutputHandler returns the currently registered output callback (may be nil).
func (m *Manager) OutputHandler() func(*Session, string) {
	return m.onOutput
}

// StartOptions holds optional parameters for starting a session.
type StartOptions struct {
	Name       string     // optional human-readable name
	Backend    string     // override LLM backend name (empty = use manager default)
	LaunchFn   LaunchFunc // override launch function (nil = use manager default)
	BackendObj llm.Backend // override backend object (for Resumable dispatch)
	ResumeID   string     // LLM session ID to resume (passed to Resumable backends)
	AutoGitCommit *bool   // per-session override for auto git commit (nil = use manager default)
	AutoGitInit   *bool   // per-session override for auto git init (nil = use manager default)
	Env        map[string]string // environment variable overrides (for profile-based launches)

	// Effort (BL41) — operator-supplied thoroughness hint:
	// "quick", "normal", "thorough". Empty falls through to
	// session.default_effort config.
	Effort string

	// v5.27.5 — claude-code per-session overrides forwarded to
	// `--permission-mode`, `--model`, `--effort`. Empty values mean
	// "use cfg.Session defaults". Most-specific-wins: per-task
	// (PRD executor) → per-PRD → per-session (these fields) →
	// global cfg.Session.{PermissionMode,Model,…}.
	PermissionMode string
	Model          string
	ClaudeEffort   string // separate from Effort (BL41 thoroughness) — this is claude's --effort enum

	// EphemeralWorkspace (v5.26.26) — set by handleStartSession when
	// it created ProjectDir via the project_profile clone path. Causes
	// Manager.Delete to reap the cloned tree after the session ends.
	// Default false; only set true at the clone site.
	EphemeralWorkspace bool
}

// Start creates a new AI coding session for the given task.
// projectDir optionally specifies the working directory; if empty the default is used.
// opts may be nil for defaults.
func (m *Manager) Start(ctx context.Context, task, groupID, projectDir string, opts ...*StartOptions) (*Session, error) {
	var opt *StartOptions
	if len(opts) > 0 && opts[0] != nil {
		opt = opts[0]
	}
	// BL30 — refuse new sessions while a global cooldown is active
	// AND the operator has opted in via session.rate_limit_global_pause.
	if m.rateLimitGlobalPause && m.inGlobalCooldown() {
		return nil, ErrGlobalCooldown
	}

	// Check max sessions (count only running/waiting sessions on this host)
	if m.maxSessions > 0 {
		all := m.store.List()
		active := 0
		for _, s := range all {
			if s.Hostname == m.hostname && (s.State == StateRunning || s.State == StateWaitingInput) {
				active++
			}
		}
		if active >= m.maxSessions {
			return nil, fmt.Errorf("max sessions (%d) reached", m.maxSessions)
		}
	}

	// Resolve project directory.
	//   - empty → user home (legacy default)
	//   - relative + workspaceRoot set → join under workspaceRoot (F10 container mode)
	//   - relative + no workspaceRoot → leave to caller (back-compat with the
	//     pre-F10 absolute-path expectation; many call sites already pass abs)
	//   - absolute → unchanged
	if projectDir == "" {
		home, _ := os.UserHomeDir()
		projectDir = home
	} else if !filepath.IsAbs(projectDir) && m.workspaceRoot != "" {
		projectDir = filepath.Join(m.workspaceRoot, projectDir)
	}

	// Ensure project dir exists
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	// Handle project dir git (per-session overrides take precedence)
	sessAutoGit := m.autoGit
	sessAutoGitInit := m.autoGitInit
	if opt != nil && opt.AutoGitCommit != nil {
		sessAutoGit = *opt.AutoGitCommit
	}
	if opt != nil && opt.AutoGitInit != nil {
		sessAutoGitInit = *opt.AutoGitInit
	}
	projGit := NewProjectGit(projectDir)
	if sessAutoGit {
		if sessAutoGitInit && !projGit.IsRepo() {
			if err := projGit.Init(); err != nil {
				fmt.Printf("[warn] git init %s: %v\n", projectDir, err)
			}
		}
		if projGit.IsRepo() {
			if err := projGit.PreSessionCommit("", task); err != nil {
				fmt.Printf("[warn] pre-session commit: %v\n", err)
			}
		}
	}

	// Generate unique short ID
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("generate session ID: %w", err)
	}
	fullID := fmt.Sprintf("%s-%s", m.hostname, id)
	tmuxSession := fmt.Sprintf("cs-%s-%s", m.hostname, id)

	backendName := m.llmBackend
	launchFn := m.launchFn
	backendObj := m.backendObj
	sessionName := ""
	resumeID := ""
	if opt != nil {
		if opt.Name != "" {
			sessionName = opt.Name
		}
		if opt.Backend != "" {
			backendName = opt.Backend
			// When a specific backend is requested, look it up in the registry
			// and wire its launch function (unless the caller supplied a custom one).
			if opt.LaunchFn == nil {
				if b, err := llm.Get(opt.Backend); err == nil {
					b2 := b // capture
					launchFn = func(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
						return b2.Launch(ctx, task, tmuxSession, projectDir, logFile)
					}
					backendObj = b2
					m.debugf("backend override: %q found in registry", opt.Backend)
				} else {
					m.debugf("backend override: %q not found in registry (%v), using manager default %q", opt.Backend, err, m.llmBackend)
				}
			}
		}
		if opt.LaunchFn != nil {
			launchFn = opt.LaunchFn
		}
		if opt.BackendObj != nil {
			backendObj = opt.BackendObj
		}
		if opt.ResumeID != "" {
			resumeID = opt.ResumeID
		}
	}

	sess := &Session{
		ID:          id,
		FullID:      fullID,
		Name:        sessionName,
		Task:        task,
		ProjectDir:  projectDir,
		TmuxSession: tmuxSession,
		LogFile:     "", // will be set after tracker creation
		State:       StateRunning,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Hostname:    m.hostname,
		GroupID:     groupID,
		LLMBackend:  backendName,
		Effort:      m.resolveEffort(opt),
	}
	if opt != nil && opt.EphemeralWorkspace {
		sess.EphemeralWorkspace = true
	}

	// Create the session tracker (git-tracked folder)
	tracker, err := NewTracker(m.dataDir, sess)
	if err != nil {
		return nil, fmt.Errorf("create session tracker: %w", err)
	}
	if m.encKey != nil && m.secureTracking == "full" {
		tracker.SetEncKey(m.encKey)
	}

	// Use tracker's output log path as the log file
	logFile := tracker.OutputLogPath()
	sess.LogFile = logFile
	sess.TrackingDir = tracker.SessionDir()

	// Store tracker in map
	m.mu.Lock()
	m.trackers[fullID] = tracker
	m.mu.Unlock()

	// Write session guardrails file (CLAUDE.md for claude-code, SESSION.md for others)
	templatePath := filepath.Join(filepath.Dir(m.dataDir), "templates", "session-guardrails.md")
	// Fall back to legacy CLAUDE.md template name, then well-known paths
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		templatePath = filepath.Join(filepath.Dir(m.dataDir), "templates", "session-CLAUDE.md")
	}
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		home, _ := os.UserHomeDir()
		templatePath = filepath.Join(home, ".local", "share", "datawatch", "templates", "session-guardrails.md")
	}
	guardrailOpts := GuardrailsOptions{}
	if m.cfg != nil {
		guardrailOpts.MemoryEnabled = m.cfg.Memory.Enabled
		guardrailOpts.RTKEnabled = m.cfg.RTK.Enabled
	}
	_ = tracker.WriteSessionGuardrails(templatePath, sess, guardrailOpts)

	// Create tmux session with per-LLM console size
	cols, rows := 80, 24
	if m.cfg != nil {
		cols, rows = m.cfg.GetConsoleSize(backendName)
	}
	sess.ConsoleCols = cols
	sess.ConsoleRows = rows
	if m.cfg != nil {
		sess.OutputMode = m.cfg.GetOutputMode(backendName)
		sess.InputMode = m.cfg.GetInputMode(backendName)
	}
	m.debugf("creating tmux session %q for backend=%q task=%q dir=%q size=%dx%d mode=%s", tmuxSession, backendName, task, projectDir, cols, rows, sess.OutputMode)
	if err := m.tmux.NewSessionWithSize(tmuxSession, cols, rows); err != nil {
		return nil, fmt.Errorf("create tmux session: %w", err)
	}

	// Pipe tmux output to tracker's output.log (encrypted if --secure)
	if m.encKey != nil {
		encLogPath := logFile + ".enc"
		fifo, err := secfile.NewEncryptingFIFO(logFile+".pipe", encLogPath, m.encKey)
		if err != nil {
			_ = m.tmux.KillSession(tmuxSession)
			return nil, fmt.Errorf("create encrypting FIFO: %w", err)
		}
		// tmux pipes to the FIFO; encrypted output goes to .enc file
		if err := m.tmux.PipeOutput(tmuxSession, fifo.FIFOPath()); err != nil {
			fifo.Close()
			_ = m.tmux.KillSession(tmuxSession)
			return nil, fmt.Errorf("pipe tmux output (encrypted): %w", err)
		}
		// Store FIFO reference for cleanup on session end
		m.mu.Lock()
		if m.encFIFOs == nil {
			m.encFIFOs = make(map[string]*secfile.EncryptingFIFO)
		}
		m.encFIFOs[fullID] = fifo
		m.mu.Unlock()
		m.debugf("tmux session %q piped to encrypted FIFO → %s", tmuxSession, encLogPath)
	} else {
		if err := m.tmux.PipeOutput(tmuxSession, logFile); err != nil {
			_ = m.tmux.KillSession(tmuxSession)
			return nil, fmt.Errorf("pipe tmux output: %w", err)
		}
		m.debugf("tmux session %q piped to %s", tmuxSession, logFile)
	}

	// Pre-launch hook (e.g. register per-session MCP channel for claude)
	if m.onPreLaunch != nil {
		m.onPreLaunch(sess)
	}

	// Set session name on the backend (for --name flag) if it supports it
	if nb, ok := backendObj.(llm.Nameable); ok && sess.Name != "" {
		nb.SetSessionName(sess.Name)
	}

	// v5.27.5 — per-session claude-code overrides (permission_mode,
	// model, effort) forwarded to the backend before launch. Each
	// setter is gated by an interface check so non-claude backends
	// silently no-op. PRD executor populates these from the
	// per-task fields (most-specific-wins fallthrough).
	if opt != nil {
		if opt.PermissionMode != "" {
			if pm, ok := backendObj.(interface{ SetPermissionMode(string) }); ok {
				pm.SetPermissionMode(opt.PermissionMode)
			}
		}
		if opt.Model != "" {
			if mm, ok := backendObj.(interface{ SetModel(string) }); ok {
				mm.SetModel(opt.Model)
			}
		}
		if opt.ClaudeEffort != "" {
			if em, ok := backendObj.(interface{ SetEffort(string) }); ok {
				em.SetEffort(opt.ClaudeEffort)
			}
		}
	}

	// Apply profile environment variables to tmux session if provided
	if opt != nil && len(opt.Env) > 0 {
		if err := m.tmux.SetEnvironment(tmuxSession, opt.Env); err != nil {
			fmt.Printf("[warn] set profile env for %s: %v\n", sess.ID, err)
		}
	}

	// Launch the LLM backend in the tmux session
	if launchFn != nil {
		var launchErr error
		if resumeID != "" {
			// Try the Resumable interface first (on the backend object if available)
			if rb, ok := backendObj.(llm.Resumable); ok {
				// If the backend supports naming, pass the session name as resumeID
				// so LaunchResume can derive the deterministic UUID.
				if sess.Name != "" && resumeID == sess.Name {
					m.debugf("launching %q with resume by name=%q", backendName, resumeID)
				} else {
					m.debugf("launching %q with resume=%q", backendName, resumeID)
				}
				launchErr = rb.LaunchResume(ctx, task, tmuxSession, projectDir, logFile, resumeID)
			} else {
				m.debugf("launching %q (no resume support, ignoring resumeID)", backendName)
				launchErr = launchFn(ctx, task, tmuxSession, projectDir, logFile)
			}
		} else {
			m.debugf("launching %q", backendName)
			launchErr = launchFn(ctx, task, tmuxSession, projectDir, logFile)
		}
		if launchErr != nil {
			_ = m.tmux.KillSession(tmuxSession)
			return nil, fmt.Errorf("launch LLM backend: %w", launchErr)
		}
		m.debugf("backend %q launched in tmux session %q", backendName, tmuxSession)
			// Persist backend state for reconnect after daemon restart
			m.saveBackendState(sess)
		} else {
		// Fallback: run LLM binary directly (legacy path, no configured backend)
		llmCmd := fmt.Sprintf("cd %s && NO_COLOR=1 %s --add-dir %s %q", projectDir, m.llmBin, projectDir, task)
		if err := m.tmux.SendKeys(tmuxSession, llmCmd); err != nil {
			_ = m.tmux.KillSession(tmuxSession)
			return nil, fmt.Errorf("send LLM command: %w", err)
		}
	}

	// Update pre-session commit to use proper session ID now that we have one
	if m.autoGit && projGit.IsRepo() {
		// The pre-session commit was already made above; nothing more to do here.
		// (projectDir git commit uses the ID for identification in post-session)
	}

	// Persist the session
	if err := m.store.Save(sess); err != nil {
		_ = m.tmux.KillSession(tmuxSession)
		return nil, fmt.Errorf("save session: %w", err)
	}

	// Start monitoring the session output
	monCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.monitors[fullID] = cancel
	m.mu.Unlock()

	// BL29 — record the pre-session checkpoint tag once the session
	// ID exists. Best-effort; failure does not block start.
	if sessAutoGit && projGit.IsRepo() {
		if err := projGit.TagCheckpoint("pre", sess.ID, task); err != nil {
			fmt.Printf("[warn] pre-checkpoint tag: %v\n", err)
		}
	}

	go m.monitorOutput(monCtx, sess, projGit)

	if m.onSessionStart != nil {
		m.onSessionStart(sess)
	}

	return sess, nil
}

// SendInput sends text input to a session that is waiting for input.
// source identifies the originator (e.g. "signal", "web", "mcp", "filter", "schedule").
// StartScreenCapture starts a goroutine that periodically captures the tmux pane
// and broadcasts it via onRawOutput. This provides reliable real-time terminal
// display without depending on fsnotify or FIFO pipes.
func (m *Manager) StartScreenCapture(ctx context.Context, fullID string, intervalMs int) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return
	}
	if intervalMs <= 0 {
		intervalMs = 200
	}
	go func() {
		ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
		defer ticker.Stop()
		var lastCapture string
		firstTick := true
		promptSeenCount := 0 // consecutive captures where prompt was detected
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, ok := m.store.Get(fullID)
				if !ok || (current.State != StateRunning && current.State != StateWaitingInput && current.State != StateRateLimited) {
					return
				}
				// v5.27.6 (BL211) — use the operator-scroll-aware visible
				// capture for the PWA display channel, but use the live
				// tail for state detection. Reading the same scrolled-up
				// view for both purposes was the cause of the
				// "session-still-running-but-claude-already-finished" bug:
				// when the operator scrolls up, the visible capture pins
				// to the scrolled view and the daemon's prompt + completion
				// checks run on stale content.
				displayCapture, err := m.tmux.CapturePaneVisible(sess.TmuxSession)
				if err != nil {
					continue
				}
				stateCapture, stErr := m.tmux.CapturePaneLiveTail(sess.TmuxSession)
				if stErr != nil {
					stateCapture = displayCapture // fall back to old behaviour on capture error
				}
				capture := displayCapture
				// Only send if content changed
				if capture != lastCapture {
					lastCapture = capture
					// BL266 / v6.11.24 — pane content changed counts as
					// "running" activity for the channel-state watcher.
					// Lets backends with no structural idle signal
					// (claude-code MCP) avoid spurious 15s WaitingInput
					// flips during long tool calls that DO update the
					// pane (most do).
					m.MarkChannelEvent(sess.FullID, EventRunning)
					// Send clean lines via pane_capture for terminal display
					if m.onScreenCapture != nil {
						lines := strings.Split(capture, "\n")
						m.onScreenCapture(sess, lines)
					}

					// Skip state detection on the first tick — this is the
					// initial baseline capture when a client connects/reconnects.
					// Running detection here would generate spurious alerts for
					// prompts that are already visible on screen.
					if firstTick {
						firstTick = false
						continue
					}

					// State detection from captured screen content.
					// v5.27.6 (BL211) — uses stateCapture (live tail) NOT
					// capture (display, may be scrolled view). Prompt /
					// completion patterns must read the live frame.
					stripped := StripANSI(stateCapture)
					capLines := strings.Split(stripped, "\n")

					// Check for prompt patterns in last 10 non-empty lines
					matchedLine, promptCtx := m.matchPromptInLines(capLines, 10)
					if matchedLine != "" {
						promptSeenCount++
						// Always update PromptContext
						if current.PromptContext != promptCtx {
							current.PromptContext = promptCtx
							_ = m.store.Save(current)
						}
						// For ChannelReady sessions, require the prompt to persist for
						// 50+ consecutive captures (~10s at 200ms interval) before
						// transitioning. Claude has multi-second pauses between tool
						// calls (thinking, planning, waiting for permission) where the
						// ❯ prompt is visible but it's still processing.
						minSeen := 1
						if current.ChannelReady {
							minSeen = 50
						}
						if promptSeenCount >= minSeen {
							wasRunning := current.State == StateRunning
							if m.tryTransitionToWaiting(fullID, matchedLine, "", nil) {
								// Capture last response on running→waiting_input
								if wasRunning {
									go func(s *Session) {
										resp := m.CaptureResponse(s)
										if resp != "" {
											s.LastResponse = resp
											_ = m.store.Save(s)
											if m.onResponseCaptured != nil {
												m.onResponseCaptured(s, resp)
											}
										}
									}(current)
								}
							}
						}
					} else {
						promptSeenCount = 0
						// v6.11.26 — operator-debugged 2026-05-05: this
						// "no prompt → revert" branch was undoing every
						// gap-watcher WaitingInput transition within 200 ms,
						// then bumping UpdatedAt and bypassing the watcher
						// for the next 15 s window. Only revert if pane
						// content changed AND BL266 LastChannelEventAt is
						// fresh (real recent activity). Inside the
						// `if capture != lastCapture` branch already
						// guarantees pane changed, so just gate on LCE
						// freshness — must be within the watcher's gap
						// window.
						lceFresh := !current.LastChannelEventAt.IsZero() && time.Since(current.LastChannelEventAt) < DefaultRunningToWaitingGap
						if current.State == StateWaitingInput && sess.LLMBackend != "opencode-acp" && lceFresh {
							// No prompt found AND we just had real activity — flip back to running
							oldState := current.State
							current.State = StateRunning
							current.UpdatedAt = time.Now()
							_ = m.store.Save(current)
							if m.onStateChange != nil {
								m.onStateChange(current, oldState)
							}
						}
					}

					// B5: Detect when opencode exits to shell — session should complete
					// If a shell prompt (datawatch: or $) appears but the backend is opencode,
					// it means the TUI exited and dropped to the parent shell.
					if sess.LLMBackend == "opencode" {
						lastLine := ""
						for i := len(capLines) - 1; i >= 0; i-- {
							if l := strings.TrimSpace(capLines[i]); l != "" {
								lastLine = l
								break
							}
						}
						if strings.HasSuffix(lastLine, "$") || strings.HasPrefix(lastLine, "datawatch:") {
							// Shell prompt detected after opencode — session is done
							if current.State == StateRunning || current.State == StateWaitingInput {
								oldState := current.State
								current.State = StateComplete
								current.UpdatedAt = time.Now()
								_ = m.store.Save(current)
								if m.onStateChange != nil {
									m.onStateChange(current, oldState)
								}
								if m.onSessionEnd != nil {
									m.onSessionEnd(current)
								}
								return
							}
						}
					}
					// Check for completion — only last 5 non-empty lines.
					// Use HasPrefix per-line to avoid false positives from command
					// echoes (e.g. "echo 'DATAWATCH_COMPLETE: ...'").
					completionDetected := false
					for i, checked := len(capLines)-1, 0; i >= 0 && checked < 5; i-- {
						l := strings.TrimSpace(capLines[i])
						if l == "" { continue }
						checked++
						for _, pat := range m.effectiveCompletionPatterns() {
							if strings.HasPrefix(l, pat) {
								completionDetected = true
								break
							}
						}
						if completionDetected { break }
					}
					if completionDetected {
						if current.State == StateRunning || current.State == StateWaitingInput {
							oldState := current.State
							current.State = StateComplete
							current.UpdatedAt = time.Now()
							_ = m.store.Save(current)
							if m.onStateChange != nil {
								m.onStateChange(current, oldState)
							}
							return
						}
					}
					// waiting→running flip is handled above (matchedLine == "" case)
				}
			}
		}
	}()
}

// KillTmuxSession terminates the tmux session for a datawatch session (e.g. after completion).
func (m *Manager) KillTmuxSession(fullID string) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return
	}
	_ = m.tmux.KillSession(sess.TmuxSession)
}

// SetState manually overrides a session's state. Used for fixing stuck sessions.
func (m *Manager) SetState(fullID string, newState State) error {
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return fmt.Errorf("session %s not found", fullID)
		}
	}
	oldState := sess.State
	sess.State = newState
	sess.UpdatedAt = time.Now()
	if newState == StateRateLimited {
		// Clear rate limit reset time
		sess.RateLimitResetAt = nil
	}
	_ = m.store.Save(sess)
	if m.onStateChange != nil {
		m.onStateChange(sess, oldState)
	}
	return nil
}

// ResizeTmux resizes a tmux pane to match the web terminal dimensions.
func (m *Manager) ResizeTmux(fullID string, cols, rows int) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return
		}
	}
	m.tmux.ResizePane(sess.TmuxSession, cols, rows)
}

// channelCompletionPatterns — natural-language phrases that signal task
// completion when emitted via a structured channel (MCP reply / ACP /
// chat_message). DIFFERENT from the global completionPatterns
// (which only matches the DATAWATCH_COMPLETE: protocol marker for tmux
// pane content). Per the v6.11.6 lesson, natural-language patterns
// are unsafe in tmux pane-buffer replay — but channel messages are
// per-message events with no replay risk, so we can match looser.
//
// All matching is case-insensitive on the trimmed message text.
var channelCompletionPatterns = []string{
	"task complete",
	"task completed",
	"task is complete",
	"task is now complete",
	"successfully completed",
	"all tasks complete",
	"all tasks completed",
	"i've completed the task",
	"i have completed the task",
	"completed the task",
	"finished the task",
	"the task is now complete",
	"the work is complete",
	"all done",
	"task done",
	"job complete",
	"job done",
	"work is done",
	"work is complete",
}

// channelInputNeededPatterns — natural-language phrases that signal the
// LLM is asking the operator for input. Triggers WaitingInput.
var channelInputNeededPatterns = []string{
	"should i proceed",
	"should i continue",
	"shall i proceed",
	"shall i continue",
	"do you want me to",
	"would you like me to",
	"please confirm",
	"please advise",
	"awaiting your input",
	"awaiting your response",
	"awaiting confirmation",
	"waiting for your",
	"waiting for input",
	"need your input",
	"need your decision",
	"need your guidance",
	"need clarification",
	"can you clarify",
	"could you clarify",
	"how would you like",
	"what would you like",
}

// channelBlockedPatterns — phrases that signal the session is stuck on
// something the operator might need to address. Doesn't transition to
// a terminal state (too risky from text alone) but logs and surfaces
// the situation. Could be wired to a future StateBlocked.
var channelBlockedPatterns = []string{
	"i'm blocked",
	"i am blocked",
	"i'm stuck",
	"i am stuck",
	"unable to proceed",
	"cannot proceed",
	"can't proceed",
	"hit an error",
	"encountered an error",
	"blocked on",
	"stuck on",
}

// detectChannelStateSignal classifies a channel message content into
// one of: "complete", "input", "blocked", or "" (just activity).
//
// v6.11.20 — tightened from v6.11.19's any-substring match. Operator
// reported false-positive completions: phrases like "task complete"
// appear mid-conversation in normal claude-code output (e.g. "OK, the
// auth task is complete, now starting on routing") and prematurely
// marked sessions Complete, freezing the PWA's pane_capture display.
//
// New rules:
//   - completion: pattern must appear at the END of the trimmed message
//     (with optional trailing `.`, `!`, or whitespace). Mid-message
//     occurrences are ignored.
//   - input: same pattern set but ALSO end-of-message; trailing-`?`
//     heuristic kept but constrained — message must be ≤ 200 chars
//     (long messages with trailing rhetorical `?` are usually narration,
//     not a real ask).
//   - blocked: substring match retained; only logs (no transition), so
//     false positives are harmless.
func detectChannelStateSignal(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	// v6.11.23 — operator: "not capturing running has ended". v6.11.20's
	// whole-message-suffix was too strict — typical claude-code closes look
	// like "I've completed the task. The file is at foo.go:42. Let me know
	// if you want anything else." — the completion phrase is the suffix of
	// an EARLY sentence, not the message as a whole. Switch to per-sentence
	// suffix matching (split on .!?), which keeps mid-sentence false-positive
	// safety from v6.11.20 ("the auth task is complete, now starting on
	// routing" stays a single sentence and still doesn't end with the
	// pattern) while picking up multi-sentence wrap-ups.
	sentences := splitSentencesForChannelClassifier(text)
	for _, s := range sentences {
		sLow := strings.ToLower(strings.TrimRight(strings.TrimSpace(s), ".!"))
		if sLow == "" {
			continue
		}
		for _, p := range channelCompletionPatterns {
			if strings.HasSuffix(sLow, p) {
				return "complete"
			}
		}
	}
	for _, s := range sentences {
		sLow := strings.ToLower(strings.TrimRight(strings.TrimSpace(s), ".!"))
		if sLow == "" {
			continue
		}
		for _, p := range channelInputNeededPatterns {
			if strings.HasSuffix(sLow, p) {
				return "input"
			}
		}
	}
	// Trailing "?" suggests asking — only on shortish messages where a
	// trailing question is likely the actual ask, not narration.
	if strings.HasSuffix(strings.TrimSpace(text), "?") && len(text) <= 200 {
		return "input"
	}
	low := strings.ToLower(text)
	for _, p := range channelBlockedPatterns {
		if strings.Contains(low, p) {
			return "blocked"
		}
	}
	return "" // generic activity
}

// splitSentencesForChannelClassifier breaks text on sentence-end
// punctuation (.!?) followed by whitespace or EOL. Conservative — empty
// segments are dropped by the caller.
func splitSentencesForChannelClassifier(text string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '.' || c == '!' || c == '?' {
			next := byte(' ')
			if i+1 < len(text) {
				next = text[i+1]
			}
			if i+1 == len(text) || next == ' ' || next == '\n' || next == '\t' || next == '\r' {
				out = append(out, text[start:i])
				start = i + 1
			}
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// MarkChannelActivity (BL264 / v6.11.18, content-aware in BL265 /
// v6.11.19) — operator-directed channel-based state detection. When a
// channel-bearing backend (claude-code MCP, opencode-acp ACP) emits a
// reply / notify / chat-message, the daemon treats that as
// authoritative evidence about session state.
//
// v6.11.18 used existence-only ("any channel event = Running"). Operator
// (BL265): "It didn't work for capturing session state, are you getting
// it from the actual message? Debug and get it working, this is one of
// the most important features, knowing when jobs are done or blocked
// and or input." v6.11.19 now parses the message content:
//
//   - completion phrase ("task complete" etc.) → StateComplete
//   - input-needed phrase ("should I proceed", trailing "?") → StateWaitingInput
//   - blocked phrase ("I'm stuck" etc.)            → keep current state, log it
//   - any other text                                → StateRunning (generic activity)
//
// Respects state locks:
//   - StateRateLimited: never override (operator-visible pause)
//   - StateComplete / StateFailed / StateKilled: terminal; only
//     completion signal can re-affirm Complete (no-op since already
//     Complete); other signals just touch UpdatedAt
func (m *Manager) MarkChannelActivity(fullID string) {
	m.MarkChannelActivityFromText(fullID, "")
}

// MarkChannelActivityFromText (post-BL266 / v6.11.24) — DEMOTED to
// advisory. The structural state engine (MarkChannelEvent / MarkACPEvent
// + StartChannelStateWatcher) is now the source of truth for state
// transitions. Text patterns no longer flip state on their own — they
// could only ever guess, and the guess kept being wrong (v6.11.18
// existence-only too eager; v6.11.19 substring too loose; v6.11.20
// whole-message-suffix too strict; v6.11.23 sentence-suffix still missed
// real claude wraps, see operator BL266 prompt 2026-05-05).
//
// What this still does:
//   - bumps LastChannelEventAt + UpdatedAt (so the gap watcher and the
//     PWA "stale comms" indicator both see the activity);
//   - if NLP detects a "complete" pattern AND state is Running/WaitingInput,
//     it's promoted to a structural EventComplete (covers claude-code MCP
//     replies that only mention completion in text, not in a marker line).
//
// Everything else — input-needed phrases, blocked phrases, generic activity
// → Running revival — is now handled by structural events from
// MarkChannelEvent / MarkACPEvent paths or by the gap watcher.
func (m *Manager) MarkChannelActivityFromText(fullID, text string) {
	if m == nil || m.store == nil {
		return
	}
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return
		}
	}
	// Always treat any channel text as activity (bump timestamp + revive
	// from WaitingInput → Running). Structural; same path as ACP busy.
	m.MarkChannelEvent(sess.FullID, EventRunning)

	// NLP advisory (v6.11.25, post-BL266): only the input-needed signal
	// still promotes through the structural engine. The completion
	// promotion was REMOVED — multi-sentence claude wraps like "Done.
	// Let me know if you want anything else." were promoting to
	// EventComplete via the per-sentence suffix matcher; Complete is
	// sticky and the PWA's pane_capture skip gate then dropped every
	// frame for 10 s, leaving the loading splash stuck. Complete should
	// only fire on REAL signals (ACP session.completed/message.completed,
	// MCP DATAWATCH_COMPLETE marker, operator-issued kill). The 15 s gap
	// watcher will still surface idleness as WaitingInput.
	if detectChannelStateSignal(text) == "input" {
		m.MarkChannelEvent(sess.FullID, EventIdle)
		m.debugf("MarkChannelActivityFromText: %s NLP-advisory input-needed → EventIdle", sess.FullID)
	}
}

// CapturePaneANSI captures the current visible content of a tmux pane with ANSI
// escape sequences preserved. Used after resize_term to give xterm.js a fresh
// snapshot at the correct column width (avoiding stale buffered output that was
// formatted for a different width).
func (m *Manager) CapturePaneANSI(fullID string) (string, error) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return "", fmt.Errorf("session not found: %s", fullID)
		}
	}
	return m.tmux.CapturePaneANSI(sess.TmuxSession)
}

// SendRawKeys sends literal bytes to the tmux session (for interactive terminal).
// Unlike SendInput, this does not append Enter and uses send-keys -l for literal mode.
func (m *Manager) SendRawKeys(fullID, data string) error {
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return fmt.Errorf("session %s not found", fullID)
		}
	}
	// Track typed input for alert logging — accumulate chars until Enter
	m.mu.Lock()
	if m.rawInputBuf == nil {
		m.rawInputBuf = make(map[string]string)
	}
	for _, ch := range data {
		if ch == '\r' || ch == '\n' {
			// Enter pressed — store accumulated input as LastInput
			buf := m.rawInputBuf[fullID]
			delete(m.rawInputBuf, fullID)
			if buf != "" {
				sess.LastInput = truncateStr(buf, 100)
			} else {
				sess.LastInput = "(enter)"
			}
			_ = m.store.Save(sess)
		} else if ch == '\x7f' || ch == '\b' {
			// Backspace — remove last char from buffer
			buf := m.rawInputBuf[fullID]
			if len(buf) > 0 {
				m.rawInputBuf[fullID] = buf[:len(buf)-1]
			}
		} else if ch >= ' ' {
			// Printable char — accumulate
			m.rawInputBuf[fullID] += string(ch)
		}
	}
	m.mu.Unlock()
	return m.tmux.SendKeysLiteral(sess.TmuxSession, data)
}

func (m *Manager) SendInput(fullID, input, source string) error {
	sess, ok := m.store.Get(fullID)
	if !ok {
		// Try short ID
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return fmt.Errorf("session %s not found", fullID)
		}
		fullID = sess.FullID
	}
	// Allow sending to running sessions too — the user may need to send input
	// before the idle detector fires (or when the session accepted the input
	// without transitioning states).
	if sess.State != StateWaitingInput && sess.State != StateRunning && sess.State != StateRateLimited {
		return fmt.Errorf("session %s cannot accept input (state: %s)", fullID, sess.State)
	}

	m.debugf("SendInput session=%s tmux=%s text=%q backend=%s", fullID, sess.TmuxSession, input, sess.LLMBackend)

	// For chat-mode sessions, intercept memory commands before sending to backend.
	// This works for any backend with output_mode=chat (Ollama, OpenWebUI, etc.)
	if sess.OutputMode == "chat" {
		sessionID := strings.TrimPrefix(sess.TmuxSession, "cs-")
		// Memory command interception
		if m.chatMemoryFn != nil {
			if response, handled := m.chatMemoryFn(sess.TmuxSession, input); handled {
				if m.onChatMessage != nil {
					m.onChatMessage(sessionID, "user", input, false)
					m.onChatMessage(sessionID, "system", response, false)
				}
				return nil
			}
		}
		// Emit user input as a chat message so it appears in the chat UI
		// (for inputs from comm channels, API, CLI — not just web UI)
		if m.onChatMessage != nil {
			m.onChatMessage(sessionID, "user", input, false)
		}
	}

	// For opencode-acp sessions, route input via HTTP API if ACP session is active.
	if sess.LLMBackend == "opencode-acp" {
		if opencode.SendMessageACP(sess.TmuxSession, input) {
			m.debugf("SendInput routed via opencode ACP HTTP")
			return nil
		}
		m.debugf("SendInput ACP not active, falling back to tmux send-keys")
	}
	// For openwebui sessions, route through Go HTTP conversation manager.
	if sess.LLMBackend == "openwebui" {
		if openwebui.SendMessageOWUI(sess.TmuxSession, input) {
			m.debugf("SendInput routed via openwebui Go conversation manager")
			return nil
		}
		m.debugf("SendInput openwebui conversation not active, falling back to tmux send-keys")
	}
	// For ollama chat-mode sessions, route through Go API conversation manager.
	if sess.LLMBackend == "ollama" && sess.OutputMode == "chat" {
		if ollama.SendMessageOllama(sess.TmuxSession, input) {
			m.debugf("SendInput routed via ollama Go conversation manager")
			return nil
		}
		m.debugf("SendInput ollama conversation not active, falling back to tmux send-keys")
	}
	// B30: for scheduled commands, split keys + Enter with a settle delay
	// so TUIs that are mid-render-settle (claude-code / ink) don't swallow
	// the Enter as part of prompt setup.
	var sendErr error
	if source == "schedule" && m.scheduleSettleMs > 0 {
		sendErr = m.tmux.SendKeysWithSettle(sess.TmuxSession, input,
			time.Duration(m.scheduleSettleMs)*time.Millisecond)
	} else {
		sendErr = m.tmux.SendKeys(sess.TmuxSession, input)
	}
	if sendErr != nil {
		return fmt.Errorf("send input: %w", sendErr)
	}
	m.debugf("SendInput OK")

	// Increment input counter
	sess.InputCount++

	// Record input in tracker
	m.mu.Lock()
	tracker := m.trackers[fullID]
	m.mu.Unlock()
	if tracker != nil {
		if err := tracker.RecordInputSent(input, source); err != nil {
			fmt.Printf("[warn] tracker.RecordInputSent: %v\n", err)
		}
	}

	// Transition back to running if we were waiting for input or rate limited
	if sess.State == StateWaitingInput || sess.State == StateRateLimited {
		oldState := sess.State
		sess.State = StateRunning
		sess.PendingInput = ""
		sess.RateLimitResetAt = nil
		sess.LastInput = truncateStr(input, 100) // for alert logging
		sess.UpdatedAt = time.Now()
		// BL266 / v6.11.24 — operator input counts as activity for the
		// channel-state watcher; without this the gap watcher would
		// flip Running → WaitingInput 15 s later if the LLM hasn't
		// produced any pane changes yet.
		sess.LastChannelEventAt = sess.UpdatedAt
		if err := m.store.Save(sess); err != nil {
			return fmt.Errorf("save session: %w", err)
		}
		if m.onStateChange != nil {
			m.onStateChange(sess, oldState)
		}
	}
	return nil
}

// Kill terminates a session by full ID.
func (m *Manager) Kill(fullID string) error {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return fmt.Errorf("session %s not found", fullID)
	}

	// Cancel the monitor goroutine and clean up encrypting FIFO
	m.mu.Lock()
	if cancel, ok := m.monitors[fullID]; ok {
		cancel()
		delete(m.monitors, fullID)
	}
	if fifo, ok := m.encFIFOs[fullID]; ok {
		fifo.Close()
		delete(m.encFIFOs, fullID)
	}
	if sp, ok := m.streamPipes[fullID]; ok {
		sp.Close()
		delete(m.streamPipes, fullID)
	}
	tracker := m.trackers[fullID]
	m.mu.Unlock()

	// Cancel any pending scheduled commands (e.g. rate-limit auto-resume)
	if m.schedStore != nil {
		if n := m.schedStore.CancelBySession(fullID); n > 0 {
			fmt.Printf("[session] cancelled %d scheduled command(s) for killed session %s\n", n, sess.ID)
		}
	}

	// Kill the tmux session
	_ = m.tmux.KillSession(sess.TmuxSession)

	oldState := sess.State
	sess.State = StateKilled
	sess.UpdatedAt = time.Now()
	if err := m.store.Save(sess); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	// Record in tracker
	if tracker != nil {
		if err := tracker.RecordComplete(StateKilled); err != nil {
			fmt.Printf("[warn] tracker.RecordComplete(killed): %v\n", err)
		}
	}

	if m.onStateChange != nil {
		m.onStateChange(sess, oldState)
	}
	if m.onSessionEnd != nil {
		m.onSessionEnd(sess)
	}
	return nil
}

// Restart relaunches a completed/failed/killed session in-place, reusing the
// same session ID and tmux session name. If the backend supports resuming, the
// Claude conversation is resumed via --resume; otherwise a fresh launch is done.
func (m *Manager) Restart(ctx context.Context, fullID string) (*Session, error) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return nil, fmt.Errorf("session %s not found", fullID)
		}
	}

	// Only allow restart of terminal-state sessions
	switch sess.State {
	case StateComplete, StateFailed, StateKilled:
		// OK to restart
	default:
		return nil, fmt.Errorf("cannot restart session in state %q (must be complete, failed, or killed)", sess.State)
	}

	// Cancel any existing monitor
	m.mu.Lock()
	if cancel, ok := m.monitors[sess.FullID]; ok {
		cancel()
		delete(m.monitors, sess.FullID)
	}
	if fifo, ok := m.encFIFOs[sess.FullID]; ok {
		fifo.Close()
		delete(m.encFIFOs, sess.FullID)
	}
	if sp, ok := m.streamPipes[sess.FullID]; ok {
		sp.Close()
		delete(m.streamPipes, sess.FullID)
	}
	m.mu.Unlock()

	// Kill any leftover tmux session
	_ = m.tmux.KillSession(sess.TmuxSession)

	// Resolve backend
	backendName := sess.LLMBackend
	if backendName == "" {
		backendName = m.llmBackend
	}
	var launchFn LaunchFunc = m.launchFn
	var backendObj llm.Backend = m.backendObj
	if backendName != "" {
		if b, err := llm.Get(backendName); err == nil {
			b2 := b
			launchFn = func(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
				return b2.Launch(ctx, task, tmuxSession, projectDir, logFile)
			}
			backendObj = b2
		}
	}

	// Reset session state
	oldState := sess.State
	sess.State = StateRunning
	sess.UpdatedAt = time.Now()
	sess.PendingInput = ""
	sess.LastPrompt = ""
	sess.PromptContext = ""
	sess.RateLimitResetAt = nil

	// Create tmux session
	cols, rows := sess.ConsoleCols, sess.ConsoleRows
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
		if m.cfg != nil {
			cols, rows = m.cfg.GetConsoleSize(backendName)
		}
	}
	if err := m.tmux.NewSessionWithSize(sess.TmuxSession, cols, rows); err != nil {
		return nil, fmt.Errorf("create tmux session: %w", err)
	}

	// Pipe output
	logFile := sess.LogFile
	if m.encKey != nil {
		encLogPath := logFile + ".enc"
		fifo, err := secfile.NewEncryptingFIFO(logFile+".pipe", encLogPath, m.encKey)
		if err != nil {
			_ = m.tmux.KillSession(sess.TmuxSession)
			return nil, fmt.Errorf("create encrypting FIFO: %w", err)
		}
		if err := m.tmux.PipeOutput(sess.TmuxSession, fifo.FIFOPath()); err != nil {
			fifo.Close()
			_ = m.tmux.KillSession(sess.TmuxSession)
			return nil, fmt.Errorf("pipe tmux output (encrypted): %w", err)
		}
		m.mu.Lock()
		if m.encFIFOs == nil {
			m.encFIFOs = make(map[string]*secfile.EncryptingFIFO)
		}
		m.encFIFOs[sess.FullID] = fifo
		m.mu.Unlock()
	} else {
		if err := m.tmux.PipeOutput(sess.TmuxSession, logFile); err != nil {
			_ = m.tmux.KillSession(sess.TmuxSession)
			return nil, fmt.Errorf("pipe tmux output: %w", err)
		}
	}

	// Pre-launch hook
	if m.onPreLaunch != nil {
		m.onPreLaunch(sess)
	}

	// Set session name on backend
	if nb, ok := backendObj.(llm.Nameable); ok && sess.Name != "" {
		nb.SetSessionName(sess.Name)
	}

	// Launch with resume support — use the session's fullID as resumeID
	// since Launch() now sets --session-id from the same derivation.
	resumeID := sess.FullID
	var launchErr error
	if rb, ok := backendObj.(llm.Resumable); ok {
		m.debugf("restarting %q with resume=%q", backendName, resumeID)
		launchErr = rb.LaunchResume(ctx, sess.Task, sess.TmuxSession, sess.ProjectDir, logFile, resumeID)
	} else if launchFn != nil {
		m.debugf("restarting %q (no resume support)", backendName)
		launchErr = launchFn(ctx, sess.Task, sess.TmuxSession, sess.ProjectDir, logFile)
	}
	if launchErr != nil {
		_ = m.tmux.KillSession(sess.TmuxSession)
		return nil, fmt.Errorf("relaunch LLM backend: %w", launchErr)
	}

	// Save updated session
	if err := m.store.Save(sess); err != nil {
		_ = m.tmux.KillSession(sess.TmuxSession)
		return nil, fmt.Errorf("save session: %w", err)
	}

	// Restart monitoring
	projGit := NewProjectGit(sess.ProjectDir)
	monCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.monitors[sess.FullID] = cancel
	m.mu.Unlock()
	go m.monitorOutput(monCtx, sess, projGit)

	if m.onStateChange != nil {
		m.onStateChange(sess, oldState)
	}
	if m.onSessionStart != nil {
		m.onSessionStart(sess)
	}

	return sess, nil
}

// Delete removes a session from the store and optionally deletes its tracking data on disk.
// If the session is running or waiting, it is killed first.
func (m *Manager) Delete(fullID string, deleteData bool) error {
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return fmt.Errorf("session %s not found", fullID)
		}
		fullID = sess.FullID
	}

	// Kill if still active
	if sess.State == StateRunning || sess.State == StateWaitingInput || sess.State == StateRateLimited {
		if err := m.Kill(fullID); err != nil {
			return fmt.Errorf("kill before delete: %w", err)
		}
	}

	// Remove all per-session in-memory references
	// BL292 (v5.6.0) — also drop promptOscillation / promptLastNotify
	// / promptFirstSeen + the lastResponseCache entry. These maps were
	// populated lazily on state transitions but never cleaned up on
	// session removal, so a long-lived daemon that ran thousands of
	// sessions accumulated thousands of dead entries.
	m.mu.Lock()
	delete(m.monitors, fullID)
	delete(m.mcpRetryCounts, fullID)
	delete(m.rawInputBuf, fullID)
	delete(m.promptFirstSeen, fullID)
	delete(m.promptLastNotify, fullID)
	delete(m.promptOscillation, fullID)
	trackingDir := ""
	if t, ok := m.trackers[fullID]; ok {
		trackingDir = t.SessionDir()
		delete(m.trackers, fullID)
	}
	m.mu.Unlock()
	lastResponseCache.Delete(fullID)

	// Cancel any pending scheduled commands for this session
	if m.schedStore != nil {
		if n := m.schedStore.CancelBySession(fullID); n > 0 {
			fmt.Printf("[session] cancelled %d scheduled command(s) for deleted session %s\n", n, sess.ID)
		}
	}

	// Delete from store
	if err := m.store.Delete(fullID); err != nil {
		return fmt.Errorf("delete from store: %w", err)
	}

	// v5.26.26 — reap daemon-owned workspaces. When handleStartSession
	// cloned a project_profile repo, ProjectDir lives under
	// <data_dir>/workspaces/ and is owned by the daemon. The session
	// is the only consumer, so deletion is the right time to drop the
	// clone tree. Operator-supplied project_dirs are never reaped
	// (EphemeralWorkspace is false by default). Always runs, even when
	// deleteData=false — the workspace is ephemeral by definition.
	if sess.EphemeralWorkspace && sess.ProjectDir != "" {
		expectedRoot := filepath.Join(m.dataDir, "workspaces")
		if rel, relErr := filepath.Rel(expectedRoot, sess.ProjectDir); relErr == nil &&
			!strings.HasPrefix(rel, "..") && rel != "." {
			if err := os.RemoveAll(sess.ProjectDir); err != nil {
				fmt.Printf("[warn] reap ephemeral workspace %s: %v\n", sess.ProjectDir, err)
			} else {
				fmt.Printf("[session] reaped ephemeral workspace %s\n", sess.ProjectDir)
			}
		} else {
			fmt.Printf("[warn] refusing to reap ProjectDir %s — not under %s\n", sess.ProjectDir, expectedRoot)
		}
	}

	// Optionally delete tracking directory
	if deleteData {
		// Prefer in-memory tracker path; fall back to session's persisted TrackingDir
		dir := trackingDir
		if dir == "" {
			dir = sess.TrackingDir
		}
		if dir != "" {
			if err := os.RemoveAll(dir); err != nil {
				fmt.Printf("[warn] delete session data %s: %v\n", dir, err)
			}
		}
	}
	return nil
}

// TailOutput returns the last n lines of a session's output log, with ANSI codes stripped.
func (m *Manager) TailOutput(fullID string, n int) (string, error) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		// Try short ID
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return "", fmt.Errorf("session %s not found", fullID)
		}
	}

	// Try encrypted log first, then plaintext
	var data []byte
	encPath := sess.LogFile + ".enc"
	if _, statErr := os.Stat(encPath); statErr == nil && m.encKey != nil {
		r, err := secfile.NewEncryptedLogReader(encPath, m.encKey)
		if err != nil {
			return "", fmt.Errorf("open encrypted log: %w", err)
		}
		data, err = r.ReadAll()
		r.Close()
		if err != nil {
			return "", fmt.Errorf("read encrypted log: %w", err)
		}
	} else {
		// Read only the tail of the file — seeking from end avoids loading entire
		// (potentially 100MB+) log files into memory. Read last 64KB which is enough
		// for hundreds of lines.
		f, err := os.Open(sess.LogFile)
		if err != nil {
			if os.IsNotExist(err) {
				return "(no output yet)", nil
			}
			return "", fmt.Errorf("read log: %w", err)
		}
		defer f.Close()
		const tailBytes = 64 * 1024
		fi, _ := f.Stat()
		offset := fi.Size() - tailBytes
		if offset < 0 {
			offset = 0
		}
		f.Seek(offset, 0) //nolint:errcheck
		data, err = io.ReadAll(f)
		if err != nil {
			return "", fmt.Errorf("read log tail: %w", err)
		}
		// If we seeked into the middle of the file, skip first partial line
		if offset > 0 {
			if idx := bytes.IndexByte(data, '\n'); idx >= 0 {
				data = data[idx+1:]
			}
		}
	}

	// Strip ANSI escape codes and carriage returns so messaging backends show clean text.
	// \r is produced by TUI applications doing in-place redraws (e.g. claude trust prompt).
	clean := StripANSI(string(data))
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	allLines := strings.Split(clean, "\n")
	// Filter out blank lines for clean messaging output
	var lines []string
	for _, l := range allLines {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

// TailRawOutput returns the last n bytes/lines of output WITH ANSI preserved.
// Used for xterm.js initial render. Returns raw bytes from the log file.
func (m *Manager) TailRawOutput(fullID string, n int) (string, error) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		sess, ok = m.store.GetByShortID(fullID)
		if !ok {
			return "", fmt.Errorf("session %s not found", fullID)
		}
	}

	data, err := os.ReadFile(sess.LogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	// For TUI apps, return the raw bytes (last ~16KB to keep it manageable)
	maxBytes := 16 * 1024
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	return string(data), nil
}

// ReadTimeline returns structured timeline lines for a session.
// Works for both active sessions (via in-memory tracker) and completed sessions
// (by reading timeline.md directly from the session tracking folder on disk).
func (m *Manager) ReadTimeline(id string) ([]string, error) {
	// Try in-memory tracker first (active sessions)
	m.mu.Lock()
	tracker := m.trackers[id]
	if tracker == nil {
		if sess, ok := m.store.GetByShortID(id); ok {
			tracker = m.trackers[sess.FullID]
		}
	}
	m.mu.Unlock()
	if tracker != nil {
		return tracker.ReadTimeline()
	}

	// Completed/not-in-memory session: resolve full ID and read from disk
	fullID := id
	if sess, ok := m.store.GetByShortID(id); ok {
		fullID = sess.FullID
	} else if _, ok := m.store.Get(id); ok {
		fullID = id
	}
	diskTracker := ResumeTracker(m.dataDir, &Session{FullID: fullID})
	return diskTracker.ReadTimeline()
}

// GetSession returns a session by full ID or short ID.
func (m *Manager) GetSession(id string) (*Session, bool) {
	if sess, ok := m.store.Get(id); ok {
		return sess, true
	}
	return m.store.GetByShortID(id)
}

// SaveSession persists the session state to the store.
func (m *Manager) SaveSession(sess *Session) error {
	return m.store.Save(sess)
}

// Rename sets a human-readable name for a session.
func (m *Manager) Rename(id, name string) error {
	sess, ok := m.GetSession(id)
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	sess.Name = name
	sess.UpdatedAt = time.Now()
	return m.store.Save(sess)
}

// SetAgentBinding records that this session lives inside the
// parent-spawned worker `agentID`. F10 sprint 3.6 — once bound,
// session API calls forward through /api/proxy/agent/{agentID}/...
// rather than touching the local tmux. Pass an empty agentID to
// unbind. Returns an error if the session does not exist.
func (m *Manager) SetAgentBinding(id, agentID string) error {
	sess, ok := m.GetSession(id)
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	sess.AgentID = agentID
	sess.UpdatedAt = time.Now()
	return m.store.Save(sess)
}

// MarkWaitingInput transitions a running session to StateWaitingInput immediately,
// using the provided line as the prompt text. Called by the filter engine when a
// detect_prompt filter fires, bypassing the idle-timeout-based detection.
func (m *Manager) MarkWaitingInput(fullID, line string) {
	sess, ok := m.GetSession(fullID)
	if !ok || (sess.State != StateRunning) {
		return
	}
	oldState := sess.State
	sess.State = StateWaitingInput
	sess.LastPrompt = line
	sess.UpdatedAt = time.Now()

	// Capture screen context around the prompt for richer alert bodies.
	if capture, err := m.tmux.CapturePaneVisible(sess.TmuxSession); err == nil && capture != "" {
		stripped := StripANSI(capture)
		screenLines := strings.Split(stripped, "\n")
		// Find the prompt line in screen content and extract surrounding context
		for i := len(screenLines) - 1; i >= 0; i-- {
			if strings.TrimSpace(screenLines[i]) != "" && strings.Contains(screenLines[i], line) {
				sess.PromptContext = extractPromptContext(screenLines, i, 10)
				break
			}
		}
		// If exact match not found, just grab the last meaningful lines
		if sess.PromptContext == "" {
			_, ctx := m.matchPromptInLines(screenLines, 10)
			sess.PromptContext = ctx
		}
	}

	_ = m.store.Save(sess)

	m.mu.Lock()
	tracker := m.trackers[fullID]
	m.mu.Unlock()
	if tracker != nil {
		_ = tracker.RecordStateChange(oldState, StateWaitingInput)
		_ = tracker.RecordNeedsInput(line)
	}
	if m.onStateChange != nil {
		m.onStateChange(sess, oldState)
	}
	// Respect notification cooldown to prevent floods
	m.mu.Lock()
	lastNotify := m.promptLastNotify[fullID]
	cooldown := m.notifyCooldownSeconds()
	canNotify := time.Since(lastNotify) >= cooldown
	if canNotify {
		m.promptLastNotify[fullID] = time.Now()
	}
	m.mu.Unlock()
	if canNotify && m.onNeedsInput != nil {
		m.onNeedsInput(sess, line)
	}
}

// KillAll terminates all running and waiting sessions on this host.
// ReconnectBackends restores in-memory backend state for running sessions after daemon restart.
// It reads backend_state.json from each session's tracking dir and calls the provided
// reconnect function with the session and state. The reconnect function is implemented
// in main.go where it has access to the backend packages.
func (m *Manager) ReconnectBackends(reconnectFn func(sess *Session, bs *BackendState)) {
	for _, sess := range m.store.List() {
		if sess.Hostname != m.hostname {
			continue
		}
		if sess.State != StateRunning && sess.State != StateWaitingInput && sess.State != StateRateLimited {
			continue
		}
		if sess.TrackingDir == "" {
			continue
		}
		// Verify tmux session still exists
		if !m.tmux.SessionExists(sess.TmuxSession) {
			m.debugf("reconnect: tmux session %s gone, marking complete", sess.TmuxSession)
			sess.State = StateComplete
			sess.UpdatedAt = time.Now()
			_ = m.store.Save(sess)
			continue
		}
		bs, err := LoadBackendState(sess.TrackingDir)
		if err != nil {
			m.debugf("reconnect: load state for %s: %v", sess.FullID, err)
			continue
		}
		if bs == nil {
			continue // no state saved — non-chat backend or legacy session
		}
		m.debugf("reconnect: restoring %s backend=%s", sess.FullID, bs.Backend)
		reconnectFn(sess, bs)
	}
}

// saveBackendState persists LLM backend connection state for reconnect after restart.
func (m *Manager) saveBackendState(sess *Session) {
	if sess.TrackingDir == "" || m.cfg == nil {
		return
	}
	var bs BackendState
	bs.Backend = sess.LLMBackend
	switch sess.LLMBackend {
	case "ollama":
		bs.OllamaHost = m.cfg.Ollama.Host
		bs.OllamaModel = m.cfg.Ollama.Model
	case "openwebui":
		bs.OpenWebUIBaseURL = m.cfg.OpenWebUI.URL
		bs.OpenWebUIModel = m.cfg.OpenWebUI.Model
		bs.OpenWebUIAPIKey = m.cfg.OpenWebUI.APIKey
	case "opencode-acp":
		// ACP state (port, sessionID) is set later by the backend's background goroutine.
		// We save what we know now; the backend will update via SaveACPState.
	default:
		// Non-chat backends don't need reconnect state
		return
	}
	if err := SaveBackendState(sess.TrackingDir, &bs); err != nil {
		fmt.Printf("[warn] save backend state: %v\n", err)
	}
}

func (m *Manager) KillAll() error {
	var errs []string
	for _, sess := range m.store.List() {
		if sess.Hostname != m.hostname {
			continue
		}
		if sess.State != StateRunning && sess.State != StateWaitingInput && sess.State != StateRateLimited {
			continue
		}
		if err := m.Kill(sess.FullID); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", sess.ID, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors killing sessions: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ListSessions returns all sessions.
func (m *Manager) ListSessions() []*Session {
	return m.store.List()
}

// ResumeRateLimitedSession resumes a session that was paused due to rate limiting.
// It sends a "continue" message to the tmux session and resets the state to running.
func (m *Manager) ResumeRateLimitedSession(ctx context.Context, fullID string) {
	m.mu.Lock()
	sess, ok := m.store.Get(fullID)
	if !ok || sess.State != StateRateLimited {
		m.mu.Unlock()
		return
	}
	oldState := sess.State
	sess.State = StateRunning
	sess.RateLimitResetAt = nil
	sess.UpdatedAt = time.Now()
	m.store.Save(sess) //nolint:errcheck
	m.mu.Unlock()

	// Get tracker
	tracker := m.getTracker(fullID)
	if tracker != nil {
		_ = tracker.RecordResume()
	}

	// Send resume prompt to tmux — reference the PAUSED.md file for context
	resumeMsg := "The rate limit has reset. Please read PAUSED.md in your working directory for context on what was in progress, then continue the task."
	_ = m.tmux.SendKeys(sess.TmuxSession, resumeMsg)

	if m.onStateChange != nil {
		m.onStateChange(sess, oldState)
	}
}

// getTracker returns the Tracker for the given fullID, or nil if not found.
func (m *Manager) getTracker(fullID string) *Tracker {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.trackers[fullID]
}

// ResumeMonitors restores monitoring for sessions that were running when the daemon last stopped.
func (m *Manager) ResumeMonitors(ctx context.Context) {
	for _, sess := range m.store.List() {
		if sess.Hostname != m.hostname {
			continue
		}
		if sess.State != StateRunning && sess.State != StateWaitingInput && sess.State != StateRateLimited {
			continue
		}
		// Check if tmux session still exists (retry once to handle transient failures)
		if !m.tmux.SessionExists(sess.TmuxSession) {
			time.Sleep(500 * time.Millisecond)
			if m.tmux.SessionExists(sess.TmuxSession) {
				goto resumeSession // tmux recovered
			}
			// Mark as failed — tmux session is confirmed gone
			oldState := sess.State
			sess.State = StateFailed
			sess.UpdatedAt = time.Now()
			_ = m.store.Save(sess)
			if m.onStateChange != nil {
				m.onStateChange(sess, oldState)
			}
			continue
		}

	resumeSession:
		// BL263 / v6.11.9 — re-establish the tmux pipe-pane bridge.
		// Operator: "When the server has restarted last few times i could
		// not connect to the session again, I've had to stop and restart
		// the session, like tmux or channel or something isn't working."
		//
		// Root cause: when the previous daemon died, the pipe-pane child
		// process tmux had spawned either died with the daemon (no pipe
		// in effect — output going nowhere) or kept running but writing
		// to a now-closed FD (output going nowhere either way). The new
		// daemon's monitorOutput goroutine watched the log file via
		// fsnotify, but no new lines ever arrived because tmux was no
		// longer piping. RepipeOutput unconditionally re-establishes
		// the pipe regardless of which case applied.
		//
		// Encrypted-FIFO sessions need the FIFO re-created too (the old
		// FIFO file is still on disk but nothing is reading from it).
		// We skip re-pipe for encrypted sessions for now; operator will
		// need to restart those manually until BL263 follow-up.
		if m.encKey == nil {
			if err := m.tmux.RepipeOutput(sess.TmuxSession, sess.LogFile); err != nil {
				fmt.Printf("[warn] re-pipe tmux output for %s: %v\n", sess.FullID, err)
			} else {
				m.debugf("re-piped tmux session %q → %s after daemon restart", sess.TmuxSession, sess.LogFile)
			}
		}
		// Resume tracker for this session
		tracker := ResumeTracker(m.dataDir, sess)
		m.mu.Lock()
		m.trackers[sess.FullID] = tracker
		m.mu.Unlock()

		// If rate limited, ensure a persisted schedule exists for auto-resume.
		// The scheduler (runScheduler) will fire the command at the right time.
		if sess.State == StateRateLimited {
			if m.schedStore != nil {
				// Check if there's already a pending resume schedule for this session
				pending := m.schedStore.PendingForSession(sess.FullID)
				hasResume := false
				for _, sc := range pending {
					if !sc.RunAt.IsZero() {
						hasResume = true
						break
					}
				}
				if !hasResume {
					resumeAt := time.Now().Add(60 * time.Minute)
					if sess.RateLimitResetAt != nil && time.Until(*sess.RateLimitResetAt) > time.Minute {
						resumeAt = *sess.RateLimitResetAt
					}
					resumeMsg := "The rate limit has reset. Please read PAUSED.md in your working directory for context on what was in progress, then continue the task."
					if _, err := m.schedStore.Add(sess.FullID, resumeMsg, resumeAt, ""); err != nil {
						fmt.Printf("[warn] schedule rate-limit resume for %s: %v\n", sess.FullID, err)
					} else {
						fmt.Printf("[rate-limit] re-scheduled auto-resume for %s at %s\n", sess.ID, resumeAt.Format(time.RFC3339))
					}
				}
			}
			continue
		}

		monCtx, cancel := context.WithCancel(ctx)
		m.mu.Lock()
		m.monitors[sess.FullID] = cancel
		m.mu.Unlock()

		projGit := NewProjectGit(sess.ProjectDir)
		go m.monitorOutput(monCtx, sess, projGit)
	}
}

// StartReconciler launches a background goroutine that periodically checks for
// orphaned sessions (marked running but tmux gone, or marked stopped but tmux alive).
// activeIndicators are screen content patterns that indicate the LLM is actively working.
// If ANY of these appear in the captured screen, the session is running (not waiting).
var activeIndicators = []string{
	"Forming", "Thinking", "Running", "Executing",
	"processing", "Processing",
	"Reading", "Writing", "Searching", "Analyzing",
	"Editing", "Creating", "Updating", "Checking",
	"Installing", "Building", "Compiling",
	"Crunching", "Finagling", "Reasoning", "Considering",
	"Crunched for", "Thought for", "Formed for", // past-tense with timing
}

// promptDebounceSeconds returns the configured prompt debounce duration.
func (m *Manager) promptDebounceSeconds() time.Duration {
	if m.detection.PromptDebounce > 0 {
		return time.Duration(m.detection.PromptDebounce) * time.Second
	}
	return 3 * time.Second
}

// notifyCooldownSeconds returns the configured notification cooldown duration.
func (m *Manager) notifyCooldownSeconds() time.Duration {
	if m.detection.NotifyCooldown > 0 {
		return time.Duration(m.detection.NotifyCooldown) * time.Second
	}
	return 15 * time.Second
}

// resetPromptDebounce clears the debounce timer for a session (call when new output arrives).
func (m *Manager) resetPromptDebounce(fullID string) {
	m.mu.Lock()
	delete(m.promptFirstSeen, fullID)
	m.mu.Unlock()
}

// tryTransitionToWaiting handles prompt detection with debouncing and notification cooldown.
// It returns true if the transition actually fired (debounce elapsed), false if still waiting.
// getTracker is a closure to retrieve the session tracker (may be nil).
// skipDebounce bypasses the debounce timer (for explicit protocol markers like DATAWATCH_NEEDS_INPUT).
func (m *Manager) tryTransitionToWaiting(fullID, matchedLine, promptCtx string, getTracker func() *Tracker, skipDebounce ...bool) bool {
	current, ok := m.store.Get(fullID)
	if !ok {
		return false
	}

	// Don't allow the prompt debounce to override an active rate-limit state.
	// Rate-limit detection routes through a separate handler; if we let
	// tryTransitionToWaiting proceed it can flip the session to waiting_input
	// ~3s after rate-limit detection and mask the rate-limit from the operator.
	if current.State == StateRateLimited {
		return false
	}

	// Already waiting with same prompt — check notification cooldown only
	if current.State == StateWaitingInput && current.LastPrompt == matchedLine {
		return false
	}

	// Debounce: first time seeing this prompt? Start the timer.
	// skipDebounce bypasses this for explicit protocol markers.
	skip := len(skipDebounce) > 0 && skipDebounce[0]
	if !skip {
		debounce := m.promptDebounceSeconds()
		// Oscillation backoff: if session has flipped >3 times in 60s,
		// increase debounce to 30s to prevent notification storms.
		m.mu.Lock()
		osc := m.promptOscillation[fullID]
		cutoff := time.Now().Add(-60 * time.Second)
		recent := osc[:0]
		for _, t := range osc {
			if t.After(cutoff) {
				recent = append(recent, t)
			}
		}
		m.promptOscillation[fullID] = recent
		if len(recent) >= 3 {
			debounce = 30 * time.Second
			m.debugf("oscillation backoff: session=%s flips=%d debounce=30s", fullID, len(recent))
		}
		m.mu.Unlock()
		m.mu.Lock()
		firstSeen, exists := m.promptFirstSeen[fullID]
		if !exists {
			m.promptFirstSeen[fullID] = time.Now()
			m.mu.Unlock()
			m.debugf("prompt debounce started session=%s prompt=%q (waiting %v)", fullID, matchedLine, debounce)
			return false
		}
		m.mu.Unlock()

		// Debounce period not yet elapsed
		if time.Since(firstSeen) < debounce {
			return false
		}
	}

	// Debounce elapsed (or skipped) — clear the timer and proceed with transition
	m.mu.Lock()
	delete(m.promptFirstSeen, fullID)
	m.mu.Unlock()

	// Track oscillation for backoff. BL292 (v5.6.0) — cap to last 100
	// timestamps so a session that bounces between running/waiting all
	// day doesn't grow this slice without bound. Backoff only needs
	// recent transitions to compute its window.
	m.mu.Lock()
	hist := append(m.promptOscillation[fullID], time.Now())
	if len(hist) > 100 {
		hist = hist[len(hist)-100:]
	}
	m.promptOscillation[fullID] = hist
	m.mu.Unlock()

	// Transition state
	oldState := current.State
	current.State = StateWaitingInput
	current.LastPrompt = matchedLine
	if promptCtx != "" {
		current.PromptContext = promptCtx
	}
	current.UpdatedAt = time.Now()
	_ = m.store.Save(current)

	if getTracker != nil {
		if tracker := getTracker(); tracker != nil {
			_ = tracker.RecordStateChange(oldState, StateWaitingInput)
			_ = tracker.RecordNeedsInput(matchedLine)
		}
	}

	if m.onStateChange != nil {
		m.onStateChange(current, oldState)
	}

	// Notification cooldown: only fire onNeedsInput if enough time has passed
	m.mu.Lock()
	lastNotify := m.promptLastNotify[fullID]
	cooldown := m.notifyCooldownSeconds()
	canNotify := time.Since(lastNotify) >= cooldown
	if canNotify {
		m.promptLastNotify[fullID] = time.Now()
	}
	m.mu.Unlock()

	if canNotify && m.onNeedsInput != nil {
		m.onNeedsInput(current, matchedLine)
	} else if !canNotify {
		m.debugf("prompt notification suppressed (cooldown %v) session=%s", cooldown, fullID)
	}

	return true
}

// matchPromptInLines checks the last N non-empty lines for a prompt pattern match.
// Returns the matched line and context (surrounding non-empty lines) or empty strings.
func (m *Manager) matchPromptInLines(lines []string, n int) (matched string, context string) {
	// Check the status bar (last few lines) for "esc to interrupt" — this is the
	// most reliable indicator that Claude is actively processing. When truly idle,
	// Claude shows "esc to go back" or just the prompt mode indicator.
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-3; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.Contains(l, "esc to interrupt") {
			return "", "" // Claude actively processing — status bar confirms it
		}
	}

	// Check whether Claude is actively processing by looking for tool execution
	// indicators in the lines just above the input area (❯ prompt). Claude's
	// layout: output/spinner above separator, ❯ prompt, separator, status bar.
	promptIdx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "❯" || strings.HasPrefix(l, "❯ ") {
			promptIdx = i
			break
		}
	}
	if promptIdx > 0 {
		// Check up to 10 content lines above the ❯ prompt for active indicators.
		checked := 0
		for i := promptIdx - 1; i >= 0 && checked < 10; i-- {
			l := strings.TrimSpace(lines[i])
			if l == "" || isAllSameChar(l) {
				continue
			}
			checked++
			// Tool execution: "⎿  Running…" or "⎿  Considering…"
			if strings.HasPrefix(l, "⎿") && strings.Contains(l, "…") {
				return "", "" // tool is actively running
			}
			// Spinner with ellipsis: "✢ Verb…" — active processing
			if strings.Contains(l, "…") {
				inner := strings.TrimLeft(l, "●✢✶✻✽·* ")
				if len(inner) > 0 && inner[0] >= 'A' && inner[0] <= 'Z' {
					return "", "" // spinner active
				}
			}
			// Spinner prefix with any status (including past-tense like "✻ Crunched for 52s")
			// These appear between tool calls when Claude has finished one step but not started the next.
			if len(l) > 0 {
				first := l[0]
				if first == 0xe2 { // UTF-8 multi-byte: ✻ ✢ ✶ ✽ ● etc.
					// Check for known active indicator words anywhere in the line
					for _, indicator := range activeIndicators {
						if strings.Contains(l, indicator) {
							return "", "" // active indicator found near prompt
						}
					}
				}
			}
			// Task list items (◻ or ◼ checkboxes) indicate active task execution
			if strings.HasPrefix(l, "◻") || strings.HasPrefix(l, "◼") || strings.HasPrefix(l, "☐") || strings.HasPrefix(l, "☑") {
				return "", "" // task list visible — still processing
			}
		}
	}

	count := 0
	for i := len(lines) - 1; i >= 0 && count < n; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		count++
		for _, pat := range m.effectivePromptPatterns() {
			match := false
			trimPat := strings.TrimRight(pat, " ")
			if len(pat) <= 3 {
				match = strings.HasSuffix(l, pat) || strings.HasSuffix(l, trimPat)
			} else {
				match = strings.HasSuffix(l, pat) || strings.HasSuffix(l, trimPat) || strings.Contains(l, pat)
			}
			if match {
				ctx := extractPromptContext(lines, i, 10)
				return l, ctx
			}
		}
	}
	return "", ""
}

// extractPromptContext collects up to maxLines meaningful lines above the
// matched prompt line to provide context about what is being asked.
func extractPromptContext(lines []string, matchIdx, maxLines int) string {
	var contextLines []string
	start := matchIdx - maxLines
	if start < 0 {
		start = 0
	}
	for i := start; i <= matchIdx; i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		// Skip separator lines (all dashes, box-drawing, equals)
		if isPromptContextNoise(l) {
			continue
		}
		contextLines = append(contextLines, l)
	}
	return strings.Join(contextLines, "\n")
}

// isAllSameChar returns true if the string is 3+ characters and all the same rune
// (e.g. separator lines like "────────").
func isAllSameChar(s string) bool {
	if len(s) < 3 {
		return false
	}
	runes := []rune(s)
	first := runes[0]
	for _, r := range runes[1:] {
		if r != first {
			return false
		}
	}
	return true
}

// isPromptContextNoise returns true for lines that are visual decoration
// or UI chrome rather than meaningful prompt context.
func isPromptContextNoise(line string) bool {
	// All-same-character separator lines
	if len(line) > 3 {
		first := rune(line[0])
		if first == '─' || first == '━' || first == '-' || first == '=' || first == '█' {
			allSame := true
			for _, r := range line {
				if r != first {
					allSame = false
					break
				}
			}
			if allSame {
				return true
			}
		}
	}
	// Spinner/progress lines: no letters, just symbols
	hasLetter := false
	for _, r := range line {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return true
	}
	// Status bar fragments and shell noise
	for _, p := range []string{
		"bypass permissions", "shift+tab to cycle", "esc to interrupt",
		"server listening on", "Tip Add $schema",
		"DATAWATCH_COMPLETE:", "claude done",
		"--dangerously-skip-permissions",
		"--dangerously-load-development-channels",
		"dangerously-load-development-channels",
		"opment-channels server:datawatch-",
		"channels you have downloaded",
		"Please use --channels",
		"Channels: server:",
	} {
		if strings.Contains(line, p) {
			return true
		}
	}
	// Shell prompt lines (PS1 patterns)
	if strings.Contains(line, ")$") || strings.HasSuffix(line, "$ ") {
		return true
	}
	// Launch command echoed by shell: cd '/...' && ...
	if strings.HasPrefix(line, "cd '") && strings.Contains(line, "&&") {
		return true
	}
	return false
}

// hasStructuredChannel returns true if the session's backend provides state
// signals via MCP or ACP, making terminal-based prompt detection unnecessary.
func (m *Manager) hasStructuredChannel(sess *Session) bool {
	switch sess.LLMBackend {
	case "opencode-acp":
		return true
	case "claude-code":
		// Claude uses MCP channel for structured state events
		return true
	}
	return false
}

// SetConfig stores a reference to the full config for per-LLM settings.
func (m *Manager) SetConfig(cfg *config.Config) {
	m.cfg = cfg
}

// SetDetection sets the detection patterns from config.
func (m *Manager) SetDetection(d config.DetectionConfig) {
	m.detection = d
}

// SetScheduleStore sets the schedule store for timed commands and deferred sessions.
func (m *Manager) SetScheduleStore(store *ScheduleStore) {
	m.schedStore = store
}

// GetScheduleStore returns the schedule store (may be nil if not set).
func (m *Manager) GetScheduleStore() *ScheduleStore {
	return m.schedStore
}

// StartScheduleTimer starts a background goroutine that checks for due scheduled
// commands and deferred sessions every 30 seconds.
func (m *Manager) StartScheduleTimer(ctx context.Context) {
	if m.schedStore == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.processScheduledItems(ctx)
			}
		}
	}()
}

func (m *Manager) processScheduledItems(ctx context.Context) {
	now := time.Now()

	// Process due timed commands (send to existing sessions)
	dueCmds := m.schedStore.DuePending(now)
	for _, sc := range dueCmds {
		sess, ok := m.store.Get(sc.SessionID)
		if !ok {
			// Try short ID match
			for _, s := range m.store.List() {
				if s.ID == sc.SessionID {
					sess = s
					ok = true
					break
				}
			}
		}
		if !ok {
			_ = m.schedStore.MarkDone(sc.ID, true) // session not found
			continue
		}
		if err := m.SendInput(sess.FullID, sc.Command, "schedule"); err != nil {
			_ = m.schedStore.MarkDone(sc.ID, true)
			fmt.Printf("[schedule] failed to send to %s: %v\n", sess.FullID, err)
		} else {
			_ = m.schedStore.MarkDone(sc.ID, false)
			fmt.Printf("[schedule] sent command to %s: %s\n", sess.FullID, sc.Command)
		}
		// Process any chained commands
		for _, after := range m.schedStore.AfterDone(sc.ID) {
			_ = after // will be picked up in next tick when their dependency is done
		}
	}

	// Process due deferred sessions (start new sessions)
	dueSessions := m.schedStore.DuePendingSessions(now)
	for _, sc := range dueSessions {
		if sc.DeferredSession == nil {
			_ = m.schedStore.MarkDone(sc.ID, true)
			continue
		}
		ds := sc.DeferredSession
		newSess, err := m.Start(ctx, ds.Task, "", ds.ProjectDir, &StartOptions{
			Name:    ds.Name,
			Backend: ds.Backend,
		})
		if err != nil {
			_ = m.schedStore.MarkDone(sc.ID, true)
			fmt.Printf("[schedule] failed to start deferred session %q: %v\n", ds.Name, err)
		} else {
			sc.SessionID = newSess.FullID
			_ = m.schedStore.MarkDone(sc.ID, false)
			fmt.Printf("[schedule] started deferred session %s (%s)\n", newSess.FullID, ds.Name)
		}
	}

	// Process waiting_input triggers for active sessions
	for _, sess := range m.store.List() {
		if sess.State != StateWaitingInput {
			continue
		}
		pending := m.schedStore.WaitingInputPending(sess.FullID)
		if len(pending) == 0 {
			pending = m.schedStore.WaitingInputPending(sess.ID) // try short ID
		}
		for _, sc := range pending {
			if err := m.SendInput(sess.FullID, sc.Command, "schedule"); err != nil {
				_ = m.schedStore.MarkDone(sc.ID, true)
			} else {
				_ = m.schedStore.MarkDone(sc.ID, false)
				fmt.Printf("[schedule] sent waiting_input command to %s: %s\n", sess.FullID, sc.Command)
			}
		}
	}
}

func (m *Manager) StartReconciler(ctx context.Context) {
	go func() {
		// v6.11.6 — was 30s; reduced to 10s so tmux-pane-exit
		// detection (claude-code /exit, process death) fires faster.
		// Operator report 2026-05-05: "still not capturing that
		// session has ended". 30s was too long when an operator
		// expected near-immediate session-end recognition.
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.reconcileSessions(ctx)
			}
		}
	}()
}

func (m *Manager) reconcileSessions(ctx context.Context) {
	for _, sess := range m.store.List() {
		if sess.Hostname != m.hostname {
			continue
		}
		tmuxAlive := m.tmux.SessionExists(sess.TmuxSession)
		isActive := sess.State == StateRunning || sess.State == StateWaitingInput || sess.State == StateRateLimited

		if isActive && !tmuxAlive {
			// Session thinks it's running but tmux is gone — verify with retry
			time.Sleep(500 * time.Millisecond)
			if m.tmux.SessionExists(sess.TmuxSession) {
				continue // transient
			}
			m.debugf("reconcile: session %s marked running but tmux gone — marking complete", sess.FullID)
			oldState := sess.State
			sess.State = StateComplete
			sess.UpdatedAt = time.Now()
			_ = m.store.Save(sess)
			if m.onStateChange != nil {
				m.onStateChange(sess, oldState)
			}
			if m.onSessionEnd != nil {
				m.onSessionEnd(sess)
			}
			// Cancel orphaned monitor if any
			m.mu.Lock()
			if cancel, ok := m.monitors[sess.FullID]; ok {
				cancel()
				delete(m.monitors, sess.FullID)
			}
			m.mu.Unlock()
		}

		if !isActive && tmuxAlive && (sess.State == StateComplete || sess.State == StateFailed) {
			// Session marked complete/failed but tmux is still alive — resume monitoring.
			// Cancel any stale monitor first (goroutine may have exited without cleanup).
			m.mu.Lock()
			if oldCancel, ok := m.monitors[sess.FullID]; ok {
				oldCancel()
				delete(m.monitors, sess.FullID)
			}
			m.mu.Unlock()
			{
				m.debugf("reconcile: session %s marked %s but tmux alive — resuming to running", sess.FullID, sess.State)
				oldState := sess.State
				sess.State = StateRunning
				sess.UpdatedAt = time.Now()
				_ = m.store.Save(sess)
				if m.onStateChange != nil {
					m.onStateChange(sess, oldState)
				}
				// Resume monitoring
				tracker := ResumeTracker(m.dataDir, sess)
				m.mu.Lock()
				m.trackers[sess.FullID] = tracker
				m.mu.Unlock()
				projGit := NewProjectGit(sess.ProjectDir)
				monCtx, cancel := context.WithCancel(ctx)
				m.mu.Lock()
				m.monitors[sess.FullID] = cancel
				m.mu.Unlock()
				go m.monitorOutput(monCtx, sess, projGit)
			}
		}
	}
}

// parseRateLimitResetTime attempts to parse a reset time from a rate limit line.
// Returns zero time if not parseable. Handles three families:
//
//  1. DATAWATCH protocol: "DATAWATCH_RATE_LIMITED: resets at <RFC3339|date>"
//  2. Claude prose: "...usage limit will reset at 9pm (America/Los_Angeles)"
//     or "...resets at 5:00 PM PST"
//  3. Relative: "...resets in 4h 23m" / "...try again in 30m"
func parseRateLimitResetTime(line string) time.Time {
	low := strings.ToLower(line)

	// Family 1 — DATAWATCH explicit marker.
	if t := parseAfterMarker(line, low, "resets at "); !t.IsZero() {
		return t
	}
	// Family 2 — claude prose "(will) reset at <time>" or the newer
	// "resets <time>" / "resets <time> (<zone>)" form (BL185 — operator
	// repro 2026-04-26: "You've hit your limit · resets 10pm (America/New_York)").
	for _, marker := range []string{"will reset at ", "reset at ", "resets at ", "resets "} {
		if t := parseClaudeClockTime(line, low, marker); !t.IsZero() {
			return t
		}
	}
	// Family 3 — relative ("in 4h 23m" / "in 30m" / "in 2 hours").
	for _, marker := range []string{"resets in ", "try again in ", "available in "} {
		if t := parseRelativeDuration(low, marker); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// parseAfterMarker handles the DATAWATCH protocol: tail of `line`
// after the marker is parsed as RFC3339 or a date.
func parseAfterMarker(line, low, marker string) time.Time {
	idx := strings.Index(low, marker)
	if idx < 0 {
		return time.Time{}
	}
	timeStr := strings.TrimSpace(line[idx+len(marker):])
	if timeStr == "" || strings.HasPrefix(strings.ToLower(timeStr), "unknown") {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", timeStr); err == nil {
		return t
	}
	return time.Time{}
}

// parseClaudeClockTime handles "<marker><clock-time>[ (zone)]" where
// clock-time is "9pm", "9:30pm", "21:00", "5:00 PM PST". Returns the
// next occurrence of that wall-clock time in local time.
func parseClaudeClockTime(line, low, marker string) time.Time {
	idx := strings.Index(low, marker)
	if idx < 0 {
		return time.Time{}
	}
	tail := strings.TrimSpace(line[idx+len(marker):])
	// Extract a clock token: leading digits, optional ":<digits>",
	// optional whitespace + am/pm. Anything after that (zone, period)
	// is discarded.
	clock := extractClockToken(tail)
	if clock == "" {
		return time.Time{}
	}
	hour, min, ok := parseClock(clock)
	if !ok {
		return time.Time{}
	}
	now := time.Now()
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
	if !candidate.After(now) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}

// extractClockToken pulls the leading clock substring from `s`, including
// an optional whitespace + am/pm suffix. Examples:
//
//	"5:30 PM PST." → "5:30 PM"
//	"9pm (US/PT)"  → "9pm"
//	"21:00."       → "21:00"
func extractClockToken(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return ""
	}
	if i < len(s) && s[i] == ':' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	// Optional whitespace + AM/PM (case-insensitive).
	j := i
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j+1 < len(s) {
		c0 := s[j] | 0x20
		c1 := s[j+1] | 0x20
		if (c0 == 'a' || c0 == 'p') && c1 == 'm' {
			return s[:j+2]
		}
	}
	return s[:i]
}

// parseRelativeDuration handles "<marker>4h 23m" / "<marker>30m" /
// "<marker>2 hours" → time.Now()+duration.
func parseRelativeDuration(low, marker string) time.Time {
	idx := strings.Index(low, marker)
	if idx < 0 {
		return time.Time{}
	}
	tail := strings.TrimSpace(low[idx+len(marker):])
	if tail == "" {
		return time.Time{}
	}
	end := 0
	for end < len(tail) {
		c := tail[end]
		if (c >= '0' && c <= '9') || c == 'h' || c == 'm' || c == 's' || c == ' ' || c == 'o' || c == 'u' || c == 'r' || c == 'i' || c == 'n' || c == 't' || c == 'e' {
			end++
			continue
		}
		break
	}
	tail = strings.TrimSpace(tail[:end])
	tail = strings.ReplaceAll(tail, " hours", "h")
	tail = strings.ReplaceAll(tail, " hour", "h")
	tail = strings.ReplaceAll(tail, " minutes", "m")
	tail = strings.ReplaceAll(tail, " minute", "m")
	tail = strings.ReplaceAll(tail, " ", "")
	d, err := time.ParseDuration(tail)
	if err != nil || d <= 0 {
		return time.Time{}
	}
	return time.Now().Add(d)
}

// isClockToken returns true for "9pm", "9:30pm", "21:00", "5:00pm",
// "5:00pmpst" (zone suffix tolerated).
func isClockToken(s string) bool {
	if s == "" {
		return false
	}
	if s[0] < '0' || s[0] > '9' {
		return false
	}
	hasDigit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	return hasDigit
}

// parseClock extracts (hour, minute) from common clock formats.
func parseClock(s string) (int, int, bool) {
	low := strings.ToLower(s)
	pm := strings.Contains(low, "pm")
	am := strings.Contains(low, "am")
	clean := low
	for _, suf := range []string{"pm", "am"} {
		if i := strings.Index(clean, suf); i >= 0 {
			clean = clean[:i]
		}
	}
	clean = strings.TrimSpace(clean)
	hour, min := 0, 0
	if strings.Contains(clean, ":") {
		parts := strings.SplitN(clean, ":", 2)
		var err error
		hour, err = parseInt(parts[0])
		if err != nil {
			return 0, 0, false
		}
		// Minute portion may have trailing non-digit zone chars; trim.
		mPart := parts[1]
		end := 0
		for end < len(mPart) && mPart[end] >= '0' && mPart[end] <= '9' {
			end++
		}
		if end == 0 {
			return 0, 0, false
		}
		min, err = parseInt(mPart[:end])
		if err != nil {
			return 0, 0, false
		}
	} else {
		var err error
		hour, err = parseInt(clean)
		if err != nil {
			return 0, 0, false
		}
	}
	if pm && hour < 12 {
		hour += 12
	}
	if am && hour == 12 {
		hour = 0
	}
	if hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, false
	}
	return hour, min, true
}

func parseInt(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not int")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// monitorOutput watches the log file for patterns indicating the session needs input or has completed.
func (m *Manager) monitorOutput(ctx context.Context, sess *Session, projGit *ProjectGit) {
	// Open log file and seek to end
	f, err := os.Open(sess.LogFile)
	if err != nil {
		return
	}
	defer f.Close()

	// For ACP sessions, scan existing content for status lines (may have been
	// written before monitor started due to race with ACP goroutine).
	if sess.LLMBackend == "opencode-acp" {
		existing, _ := os.ReadFile(sess.LogFile)
		if len(existing) > 0 {
			for _, line := range strings.Split(string(existing), "\n") {
				if strings.Contains(line, "[opencode-acp]") {
					current, ok := m.store.Get(sess.FullID)
					if ok && current.State == StateRunning {
						if strings.Contains(line, "[opencode-acp] awaiting input") || strings.Contains(line, "[opencode-acp] ready") {
							m.tryTransitionToWaiting(sess.FullID, line, "", nil, true)
						}
					}
				}
			}
		}
}

	// Seek to end for new content
	if _, err := f.Seek(0, 2); err != nil {
		return
	}

	reader := bufio.NewReaderSize(f, 64*1024) // 64KB buffer for TUI apps

	var lastOutputTime time.Time
	var pendingLines []string
	var lastPromptMatchTime time.Time // tracks when we last saw a prompt pattern
	var lastPartialDrain time.Time    // when we last drained partial (no-newline) data
	idleCheckTicker := time.NewTicker(2 * time.Second)
	defer idleCheckTicker.Stop()

	// Set up fsnotify watcher for interrupt-driven file monitoring.
	// Falls back to polling if watcher creation fails.
	watcher, watchErr := fsnotify.NewWatcher()
	var fileEvents <-chan fsnotify.Event
	if watchErr == nil {
		defer watcher.Close()
		// Watch the directory containing the log file (fsnotify requires dir-level watch)
		if err := watcher.Add(filepath.Dir(sess.LogFile)); err == nil {
			fileEvents = watcher.Events
		} else {
			watcher.Close()
			watcher = nil
		}
	}

	getTracker := func() *Tracker {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.trackers[sess.FullID]
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-idleCheckTicker.C:
			// Periodic drain: read any available data from the log file.
			// This catches TUI updates that fsnotify may miss (coalesced events).
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				m.processOutputLine(ctx, sess, projGit, line, &lastOutputTime, &pendingLines, &lastPromptMatchTime, getTracker)
			}
			drainTick := make([]byte, 64*1024)
			for {
				n, _ := reader.Read(drainTick)
				if n == 0 {
					break
				}
				lastOutputTime = time.Now()
				// Raw output from file monitor is NOT sent to xterm.js —
				// StartScreenCapture sends clean capture-pane snapshots instead.
				// Sending both causes garbled display (two sources fighting).
				stripped := StripANSI(strings.TrimRight(string(drainTick[:n]), "\r\n"))
				if stripped != "" {
					pendingLines = append(pendingLines, stripped)
					if len(pendingLines) > 20 {
						pendingLines = pendingLines[len(pendingLines)-20:]
					}
					if m.onOutput != nil {
						m.onOutput(sess, stripped)
					}
				}
			}

			// Check if tmux session is still alive — retry once to avoid false positives
			if !m.tmux.SessionExists(sess.TmuxSession) {
				time.Sleep(500 * time.Millisecond)
				if !m.tmux.SessionExists(sess.TmuxSession) {
					// Confirmed gone after retry
				} else {
					continue // transient failure, tmux is fine
				}
				current, ok := m.store.Get(sess.FullID)
				if ok && (current.State == StateRunning || current.State == StateWaitingInput) {
					oldState := current.State
					current.State = StateComplete
					current.UpdatedAt = time.Now()
					_ = m.store.Save(current)

					tracker := getTracker()
					if tracker != nil {
						if err := tracker.RecordComplete(StateComplete); err != nil {
							fmt.Printf("[warn] tracker.RecordComplete: %v\n", err)
						}
					}

					// Post-session project git commit
					if m.autoGit && projGit.IsRepo() {
						if err := projGit.PostSessionCommit(current.ID, current.Task, StateComplete); err != nil {
							fmt.Printf("[warn] post-session commit: %v\n", err)
						}
						// BL10 — capture diff summary for alerts/UI badge.
						if stat, _ := projGit.DiffStat(); !stat.IsZero() {
							current.DiffSummary = stat.Summary
							_ = m.SaveSession(current)
						}
						// BL29 — post-session checkpoint tag.
						if err := projGit.TagCheckpoint("post", current.ID, current.Task); err != nil {
							fmt.Printf("[warn] post-checkpoint tag: %v\n", err)
						}
					}

					if m.onStateChange != nil {
						m.onStateChange(current, oldState)
					}
					if m.onSessionEnd != nil {
						m.onSessionEnd(current)
					}
				}
				return
			}

			// Drain any buffered partial line (prompt without trailing newline, e.g. bash "$ ").
			// This handles interactive shells that write the prompt without a newline.
			if reader.Buffered() > 0 && time.Since(lastPartialDrain) > 500*time.Millisecond {
				lastPartialDrain = time.Now()
				peeked, _ := reader.Peek(reader.Buffered())
				if len(peeked) > 0 && !bytes.Contains(peeked, []byte{'\n'}) {
					// No newline in buffered data — treat as partial prompt line
					partial := make([]byte, reader.Buffered())
					n, _ := reader.Read(partial)
					if n > 0 {
						m.processOutputLine(ctx, sess, projGit, string(partial[:n]), &lastOutputTime, &pendingLines, &lastPromptMatchTime, getTracker)
					}
				}
			}

			// Capture-pane prompt detection for terminal backends — TUI apps may have
			// prompts that aren't in the raw log output but are visible on screen.
			// Skip for chat-mode sessions — they use the conversation manager, not tmux prompts.
			// Skip if recent output (velocity check) — LLM is still actively producing output.
			outputVelocityWindow := 5 * time.Second
			if sess.OutputMode != "chat" && (lastOutputTime.IsZero() || time.Since(lastOutputTime) >= outputVelocityWindow) {
				if capture, capErr := m.tmux.CapturePaneANSI(sess.TmuxSession); capErr == nil && capture != "" {
					stripped := StripANSI(capture)
					capLines := strings.Split(stripped, "\n")
					if _, ok := m.store.Get(sess.FullID); ok {
						if matchedLine, promptCtx := m.matchPromptInLines(capLines, 10); matchedLine != "" {
							m.tryTransitionToWaiting(sess.FullID, matchedLine, promptCtx, getTracker)
						}
					}
				}
			}

			// For structured channel backends (MCP/ACP), also handle tmux death and
			// waiting→running transitions via capture-pane.
			if m.hasStructuredChannel(sess) {
				// Check tmux alive
				if !m.tmux.SessionExists(sess.TmuxSession) {
					time.Sleep(500 * time.Millisecond)
					if !m.tmux.SessionExists(sess.TmuxSession) {
						current, ok := m.store.Get(sess.FullID)
						if ok && (current.State == StateRunning || current.State == StateWaitingInput) {
							oldState := current.State
							current.State = StateComplete
							current.UpdatedAt = time.Now()
							_ = m.store.Save(current)
							if m.onStateChange != nil {
								m.onStateChange(current, oldState)
							}
							if m.onSessionEnd != nil {
								m.onSessionEnd(current)
							}
						}
						return
					}
				}
				// Use capture-pane for state detection (every idle tick = 2s)
				capture, capErr := m.tmux.CapturePaneANSI(sess.TmuxSession)
				if capErr == nil && capture != "" {
					stripped := StripANSI(capture)
					capLines := strings.Split(stripped, "\n")
					current, ok := m.store.Get(sess.FullID)
					if ok {
						// Check last 10 non-empty lines for prompt patterns
						matchedLine, promptCtx := m.matchPromptInLines(capLines, 10)
						if matchedLine != "" {
							m.tryTransitionToWaiting(sess.FullID, matchedLine, promptCtx, getTracker)
						}
						// Check for completion — only check last 5 non-empty lines.
						// Use HasPrefix per-line to avoid false positives from command echoes.
						completionFound := false
						for ci, cc := len(capLines)-1, 0; ci >= 0 && cc < 5; ci-- {
							cl := strings.TrimSpace(capLines[ci])
							if cl == "" { continue }
							cc++
							for _, pat := range m.effectiveCompletionPatterns() {
								if strings.HasPrefix(cl, pat) {
									completionFound = true
									break
								}
							}
							if completionFound { break }
						}
						if completionFound && (current.State == StateRunning || current.State == StateWaitingInput) {
							oldState := current.State
							current.State = StateComplete
							current.UpdatedAt = time.Now()
							_ = m.store.Save(current)
							if m.onStateChange != nil {
								m.onStateChange(current, oldState)
							}
							if m.onSessionEnd != nil {
								m.onSessionEnd(current)
							}
							return
						}
						// If waiting but no prompt matches, go back to running.
						// Skip for ACP — its tmux shows server log, not interactive prompts.
						// v6.11.26 — gated on LCE freshness so this 2 s
						// monitor doesn't undo gap-watcher transitions.
						lceFresh := !current.LastChannelEventAt.IsZero() && time.Since(current.LastChannelEventAt) < DefaultRunningToWaitingGap
						if current.State == StateWaitingInput && matchedLine == "" && sess.LLMBackend != "opencode-acp" && lceFresh {
							oldState := current.State
							current.State = StateRunning
							current.UpdatedAt = time.Now()
							_ = m.store.Save(current)
							if m.onStateChange != nil {
								m.onStateChange(current, oldState)
							}
						}
					}
				}
				continue
			}
			current, ok := m.store.Get(sess.FullID)
			if !ok {
				return
			}
			if current.State == StateRunning && !lastOutputTime.IsZero() && sess.OutputMode != "chat" {
				// Output velocity check: if log had output within last 5s, LLM is still
				// active — skip the idle check entirely to prevent false positives.
				if time.Since(lastOutputTime) < 5*time.Second {
					// Recent output — don't check for prompts yet
				} else {
				// Use fast timeout (3s) if a prompt pattern was recently matched,
				// otherwise use the full configured idleTimeout.
				effectiveTimeout := m.idleTimeout
				if !lastPromptMatchTime.IsZero() && lastPromptMatchTime.After(lastOutputTime.Add(-time.Second)) {
					effectiveTimeout = 3 * time.Second // was 1s — too aggressive for Claude Code
				}
				if time.Since(lastOutputTime) >= effectiveTimeout {
					// Check if the last few lines look like a prompt
					if len(pendingLines) > 0 {
						lastLine := StripANSI(strings.TrimSpace(pendingLines[len(pendingLines)-1]))
						m.debugf("idle check session=%s lastLine=%q", sess.FullID, lastLine)
						isPrompt := false
						for _, pat := range m.effectivePromptPatterns() {
							// Short patterns ($ , # , > ) only match as suffix to avoid false positives
							// on lines like "cd /path && opencode" that contain "$ " mid-line.
							if len(pat) <= 3 {
								if strings.HasSuffix(lastLine, pat) {
									isPrompt = true
									m.debugf("prompt detected via suffix %q", pat)
									break
								}
							} else if strings.HasSuffix(lastLine, pat) || strings.Contains(lastLine, pat) {
								isPrompt = true
								m.debugf("prompt detected via pattern %q", pat)
								break
							}
						}
						if isPrompt {
							lastPromptMatchTime = time.Time{} // reset
							prompt := lastLine
							if m.tryTransitionToWaiting(sess.FullID, prompt, "", getTracker) {
								pendingLines = nil
							}
						}
					}
				}
			} // end else (velocity check passed)
			}
		case event, ok := <-fileEvents:
			// fsnotify: file was written to — drain all available lines.
			if !ok {
				fileEvents = nil // channel closed, fall through to ticker
				continue
			}
			if event.Name != sess.LogFile || event.Op&(fsnotify.Write|fsnotify.Chmod) == 0 {
				continue
			}
			// Drain all available data from the file.
			// First try line-by-line for proper state detection.
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				m.processOutputLine(ctx, sess, projGit, line, &lastOutputTime, &pendingLines, &lastPromptMatchTime, getTracker)
			}
			// Then drain ALL remaining bytes — critical for TUI apps that write
			// large chunks without newlines (opencode, htop, etc.).
			// Use a fixed buffer and keep reading until no more data.
			drainBuf := make([]byte, 64*1024)
			for {
				n, _ := reader.Read(drainBuf)
				if n == 0 {
					break
				}
				lastOutputTime = time.Now()
				rawChunk := string(drainBuf[:n])
				// Raw output from file monitor not sent to xterm — capture-pane handles display.
				stripped := StripANSI(strings.TrimRight(rawChunk, "\r\n"))
				if stripped != "" {
					pendingLines = append(pendingLines, stripped)
					if len(pendingLines) > 20 {
						pendingLines = pendingLines[len(pendingLines)-20:]
					}
					if m.onOutput != nil {
						m.onOutput(sess, stripped)
					}
				}
			}
			continue
		default:
			// Fallback: if fsnotify is not active, poll briefly.
			if fileEvents != nil {
				// fsnotify is active — block on the select instead of polling.
				// Use a short sleep to yield and retry select.
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// No fsnotify — poll the file directly.
			line, err := reader.ReadString('\n')
			if err != nil {
				// Drain any buffered partial data (TUI apps)
				if reader.Buffered() > 0 {
					pb := make([]byte, reader.Buffered())
					pn, _ := reader.Read(pb)
					if pn > 0 {
						lastOutputTime = time.Now()
						// Raw output from file monitor not sent to xterm — capture-pane handles display.
					}
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}
			m.processOutputLine(ctx, sess, projGit, line, &lastOutputTime, &pendingLines, &lastPromptMatchTime, getTracker)
			continue
		}
	}
}

// processOutputLine handles a single line of output from a session's log file.
// Extracted from monitorOutput so both fsnotify and polling paths share the same logic.
func (m *Manager) processOutputLine(ctx context.Context, sess *Session, projGit *ProjectGit, rawLine string, lastOutputTime *time.Time, pendingLines *[]string, lastPromptMatchTime *time.Time, getTracker func() *Tracker) {
	rawTrimmed := strings.TrimRight(rawLine, "\r\n")
	line := StripANSI(rawTrimmed)
	*lastOutputTime = time.Now()
	*pendingLines = append(*pendingLines, line)

	// New output arrived — reset prompt debounce timer (the LLM is still producing output)
	m.resetPromptDebounce(sess.FullID)

	// For structured channel backends (MCP/ACP), keep ACP-specific status detection
	// but skip generic terminal prompt patterns.
	if m.hasStructuredChannel(sess) {
		if len(*pendingLines) > 20 {
			*pendingLines = (*pendingLines)[len(*pendingLines)-20:]
		}
		if m.onOutput != nil {
			m.onOutput(sess, line)
		}
		if m.onRawOutput != nil && sess.OutputMode == "log" {
			m.onRawOutput(sess, rawTrimmed)
		}
		// ACP status detection — these are explicit protocol messages, not terminal prompts
		if strings.Contains(line, "[opencode-acp]") {
			current, ok := m.store.Get(sess.FullID)
			if ok {
				if strings.Contains(line, "[opencode-acp] awaiting input") || strings.Contains(line, "[opencode-acp] ready") {
					if current.State == StateRunning {
						m.tryTransitionToWaiting(sess.FullID, line, "", getTracker, true)
					}
				} else if strings.Contains(line, "[opencode-acp] processing") || strings.Contains(line, "[opencode-acp] thinking") {
					if current.State == StateWaitingInput {
						oldState := current.State
						current.State = StateRunning
						current.UpdatedAt = time.Now()
						_ = m.store.Save(current)
						if m.onStateChange != nil { m.onStateChange(current, oldState) }
					}
				}
			}
		}
		// Check for explicit completion pattern — applies to all backends, including
		// structured-channel ones (claude-code emits DATAWATCH_COMPLETE: when its
		// shell wrapper runs `echo` after claude exits).
		completionLine := strings.TrimSpace(line)
		for _, pat := range m.effectiveCompletionPatterns() {
			if strings.HasPrefix(completionLine, pat) {
				current, ok := m.store.Get(sess.FullID)
				if ok && (current.State == StateRunning || current.State == StateWaitingInput) {
					oldState := current.State
					current.State = StateComplete
					current.UpdatedAt = time.Now()
					_ = m.store.Save(current)
					tracker := getTracker()
					if tracker != nil {
						if err := tracker.RecordComplete(StateComplete); err != nil {
							fmt.Printf("[warn] tracker.RecordComplete: %v\n", err)
						}
					}
					if m.onStateChange != nil {
						m.onStateChange(current, oldState)
					}
					if m.onSessionEnd != nil {
						m.onSessionEnd(current)
					}
				}
				return
			}
		}
		return
	}

	// Keep only the last 20 lines as context
	if len(*pendingLines) > 20 {
		*pendingLines = (*pendingLines)[len(*pendingLines)-20:]
	}
	if m.onOutput != nil {
		m.onOutput(sess, line)
	}
	// Send raw output for log-mode sessions only (ACP etc).
	// Terminal-mode sessions use StartScreenCapture for display.
	if m.onRawOutput != nil && sess.OutputMode == "log" {
		m.onRawOutput(sess, rawTrimmed)
	}

	// Check for rate limit patterns. v5.27.6 (BL215) — operator hit
	// a real rate-limit on 2026-04-30 that datawatch missed. The
	// previous 200-char length gate was too tight: modern claude
	// rate-limit dialogs are paragraph-length with context (e.g.
	// "5-hour limit reached. Use the limit reset time to plan...
	//  Resets at 2pm. To continue working, ..."). Whole message
	// can be 400+ chars on a single line. Raised to 1024 to keep
	// the false-positive guard but cover the realistic message size.
	// DATAWATCH_RATE_LIMITED protocol pattern always matches regardless.
	lineLower := strings.ToLower(line)
	isRateLimit := false
	if strings.Contains(line, "DATAWATCH_RATE_LIMITED:") {
		isRateLimit = true
	} else if len(line) < 2048 {
		for _, pat := range m.effectiveRateLimitPatterns() {
			if pat == "DATAWATCH_RATE_LIMITED:" { continue } // already checked above
			if strings.Contains(lineLower, strings.ToLower(pat)) {
				isRateLimit = true
				break
			}
		}
	}
	if isRateLimit {
		resetAt := parseRateLimitResetTime(line)
		current, ok := m.store.Get(sess.FullID)
		if ok && current.State == StateRunning {
			oldState := current.State
			current.State = StateRateLimited
			if !resetAt.IsZero() {
				current.RateLimitResetAt = &resetAt
			}
			current.UpdatedAt = time.Now()
			_ = m.store.Save(current)

			tracker := getTracker()
			if tracker != nil {
				if err := tracker.RecordRateLimit(resetAt); err != nil {
					fmt.Printf("[warn] tracker.RecordRateLimit: %v\n", err)
				}
			}

			if m.onStateChange != nil {
				m.onStateChange(current, oldState)
			}

			// Auto-accept the default wait option ("1. Stop and wait for limit to reset")
			// by sending "1" + Enter to the tmux session after a brief delay.
			go func() {
				time.Sleep(2 * time.Second)
				_ = m.tmux.SendKeys(sess.TmuxSession, "1")
				time.Sleep(300 * time.Millisecond)
				_ = exec.Command("tmux", "send-keys", "-t", sess.TmuxSession, "Enter").Run() //nolint:errcheck
			}()

			// Schedule auto-resume via persisted ScheduleStore (survives daemon restart).
			resumeAt := resetAt
			if resumeAt.IsZero() || time.Until(resumeAt) < time.Minute {
				resumeAt = time.Now().Add(60 * time.Minute)
			}
			resumeMsg := "The rate limit has reset. Please read PAUSED.md in your working directory for context on what was in progress, then continue the task."
			if m.schedStore != nil {
				if _, err := m.schedStore.Add(current.FullID, resumeMsg, resumeAt, ""); err != nil {
					fmt.Printf("[warn] schedule rate-limit resume for %s: %v\n", current.FullID, err)
				} else {
					fmt.Printf("[rate-limit] scheduled auto-resume for %s at %s\n", current.ID, resumeAt.Format(time.RFC3339))
				}
			}

			// Trigger fallback chain if configured
			if m.onRateLimitFallback != nil {
				go m.onRateLimitFallback(current)
			}
		}
		return
	}

	// Check for MCP server failure — auto-retry by sending /mcp to restart MCP servers
	// MCP auto-retry: only retry if our session's MCP server specifically failed,
	// and only if output doesn't already show it recovered ("connected").
	if m.mcpMaxRetries > 0 && strings.Contains(line, mcpFailedPattern) {
		// Skip if line shows servers are connected (recovery already happened)
		if strings.Contains(line, "connected") {
			// MCP recovered — reset retry counter
			m.mu.Lock()
			m.mcpRetryCounts[sess.FullID] = 0
			m.mu.Unlock()
		} else {
			m.mu.Lock()
			count := m.mcpRetryCounts[sess.FullID]
			m.mu.Unlock()
			if count < m.mcpMaxRetries {
				m.mu.Lock()
				m.mcpRetryCounts[sess.FullID] = count + 1
				m.mu.Unlock()
				m.debugf("MCP server failed for %s (attempt %d/%d) — sending /mcp to retry", sess.FullID, count+1, m.mcpMaxRetries)
				go func() {
					time.Sleep(3 * time.Second)
					_ = m.tmux.SendKeys(sess.TmuxSession, "/mcp")
					time.Sleep(3 * time.Second)
					_ = exec.Command("tmux", "send-keys", "-t", sess.TmuxSession, "Enter").Run()
				}()
			} else {
				m.debugf("MCP server failed for %s — max retries (%d) exhausted", sess.FullID, m.mcpMaxRetries)
			}
		}
	}
	// Reset MCP retry counter when we see "connected" or "Listening for channel"
	if strings.Contains(line, "Listening for channel") || strings.Contains(line, "MCP channel connected") {
		m.mu.Lock()
		m.mcpRetryCounts[sess.FullID] = 0
		m.mu.Unlock()
	}

	// Immediate LLM status detection — ACP backend status messages trigger state
	// transitions without waiting for idle timeout. This provides instant UI feedback.
	if strings.Contains(line, "[opencode-acp]") {
		current, ok := m.store.Get(sess.FullID)
		if ok {
			if strings.Contains(line, "[opencode-acp] awaiting input") || strings.Contains(line, "[opencode-acp] ready") {
				// Idle/ready → waiting_input (explicit protocol, skip debounce)
				if current.State == StateRunning {
					m.tryTransitionToWaiting(sess.FullID, line, "", getTracker, true)
				}
				return
			} else if strings.Contains(line, "[opencode-acp] processing") || strings.Contains(line, "[opencode-acp] thinking") {
				// Active processing → ensure running
				if current.State == StateWaitingInput {
					oldState := current.State
					current.State = StateRunning
					current.UpdatedAt = time.Now()
					_ = m.store.Save(current)
					if m.onStateChange != nil {
						m.onStateChange(current, oldState)
					}
				}
				return
			}
		}
	}

	// Check for explicit completion pattern — must be at start of line
	// (not embedded in a command echo like: echo 'DATAWATCH_COMPLETE: ...')
	// `line` is already StripANSI'd at function entry (line 3731).
	completionLine := strings.TrimSpace(line)
	for _, pat := range m.effectiveCompletionPatterns() {
		if strings.HasPrefix(completionLine, pat) {
			current, ok := m.store.Get(sess.FullID)
			if ok && (current.State == StateRunning || current.State == StateWaitingInput) {
				oldState := current.State
				current.State = StateComplete
				current.UpdatedAt = time.Now()
				_ = m.store.Save(current)

				tracker := getTracker()
				if tracker != nil {
					if err := tracker.RecordComplete(StateComplete); err != nil {
						fmt.Printf("[warn] tracker.RecordComplete: %v\n", err)
					}
				}

				if m.autoGit && projGit.IsRepo() {
					if err := projGit.PostSessionCommit(current.ID, current.Task, StateComplete); err != nil {
						fmt.Printf("[warn] post-session commit: %v\n", err)
					}
					if stat, _ := projGit.DiffStat(); !stat.IsZero() {
						current.DiffSummary = stat.Summary
						_ = m.SaveSession(current)
					}
					if err := projGit.TagCheckpoint("post", current.ID, current.Task); err != nil {
						fmt.Printf("[warn] post-checkpoint tag: %v\n", err)
					}
				}

				if m.onStateChange != nil {
					m.onStateChange(current, oldState)
				}
				if m.onSessionEnd != nil {
					m.onSessionEnd(current)
				}
			}
			return
		}
	}

	// Check for explicit input needed pattern (no debounce — explicit protocol marker)
	// `line` is already StripANSI'd at function entry (line 3731).
	for _, pat := range m.effectiveInputNeededPatterns() {
		if strings.Contains(line, pat) {
			idx := strings.Index(line, pat)
			question := strings.TrimSpace(line[idx+len(pat):])
			// Explicit markers bypass debounce but still respect notification cooldown
			m.tryTransitionToWaiting(sess.FullID, question, "", getTracker, true)
			return
		}
	}

	// If we were waiting for input and see new output, transition back to running.
	// v6.11.26 — also bump LastChannelEventAt so the gap watcher sees the
	// fresh activity. Output arrival IS real evidence the LLM is producing.
	current, ok := m.store.Get(sess.FullID)
	if ok && current.State == StateWaitingInput {
		oldState := current.State
		current.State = StateRunning
		now := time.Now()
		current.UpdatedAt = now
		current.LastChannelEventAt = now
		_ = m.store.Save(current)

		tracker := getTracker()
		if tracker != nil {
			if err := tracker.RecordStateChange(oldState, StateRunning); err != nil {
				fmt.Printf("[warn] tracker.RecordStateChange: %v\n", err)
			}
		}

		if m.onStateChange != nil {
			m.onStateChange(current, oldState)
		}
	}

	// Immediate prompt detection: check if this line matches a prompt pattern.
	trimmedLine := strings.TrimSpace(line)
	if trimmedLine != "" {
		for _, pat := range m.effectivePromptPatterns() {
			if len(pat) <= 3 {
				if strings.HasSuffix(trimmedLine, pat) {
					*lastPromptMatchTime = time.Now()
					break
				}
			} else if strings.HasSuffix(trimmedLine, pat) || strings.Contains(trimmedLine, pat) {
				*lastPromptMatchTime = time.Now()
				break
			}
		}
	}
}

// generateID returns a random 4-char hex string (2 random bytes).
func generateID() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
