package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	"github.com/dmz006/datawatch/internal/secfile"
	"github.com/fsnotify/fsnotify"
)

// ansiEscapeRe matches ANSI terminal escape sequences including:
// - CSI sequences: \x1b[...X
// - OSC sequences: \x1b]...(\x07|\x1b\\)
// - tmux passthrough: \x1bPtmux;...\x1b\\
// - DCS/PM/APC sequences: \x1bP...\x1b\\ , \x1b^...\x1b\\ , \x1b_...\x1b\\
// - Simple two-byte escapes: \x1bX
var ansiEscapeRe = regexp.MustCompile(`\x1b\][^\x07]*(?:\x07|\x1b\\)|\x1bP[^\x1b]*\x1b\\|\x1b_[^\x1b]*\x1b\\|\x1b\^[^\x1b]*\x1b\\|\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])`)

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
}

// Completion detection patterns
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
	maxSessions int
	store       *Store
	tmux        *TmuxManager
	idleTimeout time.Duration
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

	// onRawOutput is called for each new line of raw output (ANSI preserved, for xterm.js).
	onRawOutput func(sess *Session, rawLine string)

	// onSessionStart is called immediately after a session is successfully started.
	onSessionStart func(sess *Session)

	// onPreLaunch is called just before the LLM backend is launched (for per-session setup like MCP registration).
	onPreLaunch func(sess *Session)

	// onSessionEnd is called when a session reaches a terminal state (complete/failed/killed).
	onSessionEnd func(sess *Session)

	// cfg holds the full config reference for per-LLM settings lookup.
	cfg *config.Config

	// detection holds the active detection patterns (from config or defaults).
	detection config.DetectionConfig

	// schedStore is the schedule store for deferred sessions and timed commands.
	schedStore *ScheduleStore

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
		mcpRetryCounts: make(map[string]int),
		encKey:         key,
		monitors:       make(map[string]context.CancelFunc),
		trackers:       make(map[string]*Tracker),
	}, nil
}

// SetMCPMaxRetries sets the maximum MCP restart attempts per session.
func (m *Manager) SetMCPMaxRetries(n int) { m.mcpMaxRetries = n }

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

// BackfillOutputMode sets output_mode on existing sessions that don't have it,
// reading from the per-backend config. Call after SetConfig.
func (m *Manager) BackfillOutputMode() {
	if m.cfg == nil {
		return
	}
	for _, sess := range m.store.List() {
		if sess.OutputMode == "" {
			mode := m.cfg.GetOutputMode(sess.LLMBackend)
			if mode != "terminal" { // only backfill non-default
				sess.OutputMode = mode
				_ = m.store.Save(sess)
			}
		}
	}
}

// SetLLMBackend sets the active LLM backend name and launch function.
func (m *Manager) SetLLMBackend(name string, fn LaunchFunc) {
	m.llmBackend = name
	m.launchFn = fn
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
}

// Start creates a new AI coding session for the given task.
// projectDir optionally specifies the working directory; if empty the default is used.
// opts may be nil for defaults.
func (m *Manager) Start(ctx context.Context, task, groupID, projectDir string, opts ...*StartOptions) (*Session, error) {
	var opt *StartOptions
	if len(opts) > 0 && opts[0] != nil {
		opt = opts[0]
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

	// Resolve project directory
	if projectDir == "" {
		home, _ := os.UserHomeDir()
		projectDir = home
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
	_ = tracker.WriteSessionGuardrails(templatePath, sess)

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

	// Launch the LLM backend in the tmux session
	if launchFn != nil {
		var launchErr error
		if resumeID != "" {
			// Try the Resumable interface first (on the backend object if available)
			if rb, ok := backendObj.(llm.Resumable); ok {
				m.debugf("launching %q with resume=%q", backendName, resumeID)
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
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, ok := m.store.Get(fullID)
				if !ok || (current.State != StateRunning && current.State != StateWaitingInput && current.State != StateRateLimited) {
					return
				}
				capture, err := m.tmux.CapturePaneANSI(sess.TmuxSession)
				if err != nil {
					continue
				}
				// Only send if content changed
				if capture != lastCapture {
					lastCapture = capture
					if m.onRawOutput != nil {
						m.onRawOutput(sess, "\x1b[H\x1b[2J"+capture)
					}

					// State detection from captured screen content
					stripped := StripANSI(capture)
					lines := strings.Split(stripped, "\n")
					// Check last non-empty line for prompt patterns
					lastLine := ""
					for i := len(lines) - 1; i >= 0; i-- {
						l := strings.TrimSpace(lines[i])
						if l != "" {
							lastLine = l
							break
						}
					}
					if lastLine != "" && (current.State == StateRunning || current.State == StateWaitingInput) {
						for _, pat := range m.effectivePromptPatterns() {
							match := false
							if len(pat) <= 3 {
								match = strings.HasSuffix(lastLine, pat)
							} else {
								match = strings.HasSuffix(lastLine, pat) || strings.Contains(lastLine, pat)
							}
							if match {
								oldState := current.State
								if current.State != StateWaitingInput || current.LastPrompt != lastLine {
									current.State = StateWaitingInput
									current.LastPrompt = lastLine
									current.UpdatedAt = time.Now()
									_ = m.store.Save(current)
									if m.onStateChange != nil {
										m.onStateChange(current, oldState)
									}
								}
								break
							}
						}
					}
					// Check for completion
					for _, pat := range m.effectiveCompletionPatterns() {
						if strings.Contains(stripped, pat) {
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
					}
					// If state was waiting_input but screen now shows activity, go back to running
					if current.State == StateWaitingInput {
						// Check if the last line no longer matches any prompt pattern
						isStillPrompt := false
						for _, pat := range m.effectivePromptPatterns() {
							if len(pat) <= 3 {
								if strings.HasSuffix(lastLine, pat) { isStillPrompt = true; break }
							} else {
								if strings.HasSuffix(lastLine, pat) || strings.Contains(lastLine, pat) { isStillPrompt = true; break }
							}
						}
						if !isStillPrompt && lastLine != "" {
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
	// For opencode-acp sessions, route input via HTTP API if ACP session is active.
	if sess.LLMBackend == "opencode-acp" {
		if opencode.SendMessageACP(sess.TmuxSession, input) {
			m.debugf("SendInput routed via opencode ACP HTTP")
			return nil
		}
		m.debugf("SendInput ACP not active, falling back to tmux send-keys")
	}
	if err := m.tmux.SendKeys(sess.TmuxSession, input); err != nil {
		return fmt.Errorf("send input: %w", err)
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
		sess.UpdatedAt = time.Now()
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

	// Remove monitor and tracker references
	m.mu.Lock()
	delete(m.monitors, fullID)
	trackingDir := ""
	if t, ok := m.trackers[fullID]; ok {
		trackingDir = t.SessionDir()
		delete(m.trackers, fullID)
	}
	m.mu.Unlock()

	// Delete from store
	if err := m.store.Delete(fullID); err != nil {
		return fmt.Errorf("delete from store: %w", err)
	}

	// Optionally delete tracking directory
	if deleteData && trackingDir != "" {
		if err := os.RemoveAll(trackingDir); err != nil {
			fmt.Printf("[warn] delete session data %s: %v\n", trackingDir, err)
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
		var err error
		data, err = os.ReadFile(sess.LogFile)
		if err != nil {
			if os.IsNotExist(err) {
				return "(no output yet)", nil
			}
			return "", fmt.Errorf("read log: %w", err)
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
	if m.onNeedsInput != nil {
		m.onNeedsInput(sess, line)
	}
}

// KillAll terminates all running and waiting sessions on this host.
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
		// Resume tracker for this session
		tracker := ResumeTracker(m.dataDir, sess)
		m.mu.Lock()
		m.trackers[sess.FullID] = tracker
		m.mu.Unlock()

		// If rate limited, reschedule retry
		if sess.State == StateRateLimited {
			sessCopy := sess
			go func() {
				var waitDur time.Duration
				if sessCopy.RateLimitResetAt != nil {
					waitDur = time.Until(*sessCopy.RateLimitResetAt)
				}
				if waitDur < time.Minute {
					waitDur = 60 * time.Minute
				}
				select {
				case <-time.After(waitDur):
					m.ResumeRateLimitedSession(ctx, sessCopy.FullID)
				case <-ctx.Done():
				}
			}()
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
	"esc to interrupt", "Esc to interrupt",
	"processing", "Processing",
}

// matchPromptInLines checks the last N non-empty lines for a prompt pattern match.
// Returns the matched line or empty string.
func (m *Manager) matchPromptInLines(lines []string, n int) string {
	// First check if any active indicator is present — if so, LLM is working
	fullText := strings.Join(lines, "\n")
	for _, ind := range activeIndicators {
		if strings.Contains(fullText, ind) {
			return "" // actively working, not waiting for input
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
			if len(pat) <= 3 {
				match = strings.HasSuffix(l, pat)
			} else {
				match = strings.HasSuffix(l, pat) || strings.Contains(l, pat)
			}
			if match {
				return l
			}
		}
	}
	return ""
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
		ticker := time.NewTicker(30 * time.Second)
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
// Returns zero time if not parseable.
func parseRateLimitResetTime(line string) time.Time {
	// Look for "resets at <time>" pattern
	const marker = "resets at "
	idx := strings.Index(strings.ToLower(line), marker)
	if idx < 0 {
		return time.Time{}
	}
	timeStr := strings.TrimSpace(line[idx+len(marker):])
	if timeStr == "" || timeStr == "unknown" {
		return time.Time{}
	}
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return t
	}
	// Try date only
	if t, err := time.Parse("2006-01-02", timeStr); err == nil {
		return t
	}
	return time.Time{}
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
							current.State = StateWaitingInput
							current.LastPrompt = line
							current.UpdatedAt = time.Now()
							_ = m.store.Save(current)
							if m.onStateChange != nil {
								m.onStateChange(current, StateRunning)
							}
							if m.onNeedsInput != nil {
								m.onNeedsInput(current, line)
							}
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
				if m.onRawOutput != nil {
					m.onRawOutput(sess, string(drainTick[:n]))
				}
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

			// Capture-pane prompt detection for ALL backends — TUI apps may have
			// prompts that aren't in the raw log output but are visible on screen.
			if capture, capErr := m.tmux.CapturePaneANSI(sess.TmuxSession); capErr == nil && capture != "" {
				stripped := StripANSI(capture)
				capLines := strings.Split(stripped, "\n")
				if current, ok := m.store.Get(sess.FullID); ok && current.State == StateRunning {
					if matchedLine := m.matchPromptInLines(capLines, 10); matchedLine != "" {
						oldState := current.State
						current.State = StateWaitingInput
						current.LastPrompt = matchedLine
						current.UpdatedAt = time.Now()
						_ = m.store.Save(current)
						if m.onStateChange != nil { m.onStateChange(current, oldState) }
						if m.onNeedsInput != nil { m.onNeedsInput(current, matchedLine) }
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
						// Check last 5 non-empty lines for prompt patterns
						matchedLine := m.matchPromptInLines(capLines, 10)
						if matchedLine != "" {
							if current.State != StateWaitingInput || current.LastPrompt != matchedLine {
								oldState := current.State
								current.State = StateWaitingInput
								current.LastPrompt = matchedLine
								current.UpdatedAt = time.Now()
								_ = m.store.Save(current)
								if m.onStateChange != nil {
									m.onStateChange(current, oldState)
								}
							}
						}
						// Check for completion
						for _, pat := range m.effectiveCompletionPatterns() {
							if strings.Contains(stripped, pat) && (current.State == StateRunning || current.State == StateWaitingInput) {
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
						// If waiting but no prompt matches in last 5 lines, go back to running
						// Skip for ACP — its tmux screen shows server log, not interactive prompts
						if current.State == StateWaitingInput && matchedLine == "" && sess.LLMBackend != "opencode-acp" {
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
			if current.State == StateRunning && !lastOutputTime.IsZero() {
				// Use fast timeout (1s) if a prompt pattern was recently matched,
				// otherwise use the full configured idleTimeout.
				effectiveTimeout := m.idleTimeout
				if !lastPromptMatchTime.IsZero() && lastPromptMatchTime.After(lastOutputTime.Add(-time.Second)) {
					effectiveTimeout = 1 * time.Second
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
							// Use the last line as prompt (not entire buffer — avoids shell startup noise)
							prompt := lastLine
							oldState := current.State
							current.State = StateWaitingInput
							current.LastPrompt = prompt
							current.UpdatedAt = time.Now()
							_ = m.store.Save(current)

							tracker := getTracker()
							if tracker != nil {
								if err := tracker.RecordStateChange(oldState, StateWaitingInput); err != nil {
									fmt.Printf("[warn] tracker.RecordStateChange: %v\n", err)
								}
								if err := tracker.RecordNeedsInput(prompt); err != nil {
									fmt.Printf("[warn] tracker.RecordNeedsInput: %v\n", err)
								}
							}

							if m.onStateChange != nil {
								m.onStateChange(current, oldState)
							}
							if m.onNeedsInput != nil {
								m.onNeedsInput(current, prompt)
							}
							pendingLines = nil
						}
					}
				}
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
				if m.onRawOutput != nil {
					m.onRawOutput(sess, rawChunk)
				}
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
						if m.onRawOutput != nil {
							m.onRawOutput(sess, string(pb[:pn]))
						}
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

	// For structured channel backends (MCP/ACP), keep ACP-specific status detection
	// but skip generic terminal prompt patterns.
	if m.hasStructuredChannel(sess) {
		if len(*pendingLines) > 20 {
			*pendingLines = (*pendingLines)[len(*pendingLines)-20:]
		}
		if m.onOutput != nil {
			m.onOutput(sess, line)
		}
		if m.onRawOutput != nil {
			m.onRawOutput(sess, rawTrimmed)
		}
		// ACP status detection — these are explicit protocol messages, not terminal prompts
		if strings.Contains(line, "[opencode-acp]") {
			current, ok := m.store.Get(sess.FullID)
			if ok {
				if strings.Contains(line, "[opencode-acp] awaiting input") || strings.Contains(line, "[opencode-acp] ready") {
					if current.State == StateRunning {
						oldState := current.State
						current.State = StateWaitingInput
						current.LastPrompt = line
						current.UpdatedAt = time.Now()
						_ = m.store.Save(current)
						if m.onStateChange != nil { m.onStateChange(current, oldState) }
						if m.onNeedsInput != nil { m.onNeedsInput(current, line) }
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
		return
	}

	// Keep only the last 20 lines as context
	if len(*pendingLines) > 20 {
		*pendingLines = (*pendingLines)[len(*pendingLines)-20:]
	}
	if m.onOutput != nil {
		m.onOutput(sess, line)
	}
	// Send raw output (with ANSI) for xterm.js rendering
	if m.onRawOutput != nil {
		m.onRawOutput(sess, rawTrimmed)
	}

	// Check for rate limit patterns — only on short lines (< 200 chars) to avoid
	// false positives from code output that happens to contain rate limit keywords.
	// The DATAWATCH_RATE_LIMITED protocol pattern always matches regardless of length.
	lineLower := strings.ToLower(line)
	isRateLimit := false
	if strings.Contains(line, "DATAWATCH_RATE_LIMITED:") {
		isRateLimit = true
	} else if len(line) < 200 {
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
			}()

			// Schedule auto-resume after reset time
			fullID := current.FullID
			go func() {
				waitDur := time.Until(resetAt)
				if waitDur < time.Minute {
					waitDur = 60 * time.Minute
				}
				select {
				case <-time.After(waitDur):
					m.ResumeRateLimitedSession(ctx, fullID)
				case <-ctx.Done():
				}
			}()
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
				// Idle/ready → waiting_input
				if current.State == StateRunning {
					oldState := current.State
					current.State = StateWaitingInput
					current.LastPrompt = line
					current.UpdatedAt = time.Now()
					_ = m.store.Save(current)
					if m.onStateChange != nil {
						m.onStateChange(current, oldState)
					}
					if m.onNeedsInput != nil {
						m.onNeedsInput(current, line)
					}
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

	// Check for explicit completion pattern
	for _, pat := range m.effectiveCompletionPatterns() {
		if strings.Contains(line, pat) {
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

	// Check for explicit input needed pattern
	for _, pat := range m.effectiveInputNeededPatterns() {
		if strings.Contains(line, pat) {
			idx := strings.Index(line, pat)
			question := strings.TrimSpace(line[idx+len(pat):])
			current, ok := m.store.Get(sess.FullID)
			if ok && current.State == StateRunning {
				oldState := current.State
				current.State = StateWaitingInput
				current.LastPrompt = question
				current.UpdatedAt = time.Now()
				_ = m.store.Save(current)

				tracker := getTracker()
				if tracker != nil {
					if err := tracker.RecordStateChange(oldState, StateWaitingInput); err != nil {
						fmt.Printf("[warn] tracker.RecordStateChange: %v\n", err)
					}
					if err := tracker.RecordNeedsInput(question); err != nil {
						fmt.Printf("[warn] tracker.RecordNeedsInput: %v\n", err)
					}
				}

				if m.onStateChange != nil {
					m.onStateChange(current, oldState)
				}
				if m.onNeedsInput != nil {
					m.onNeedsInput(current, question)
				}
			}
			return
		}
	}

	// If we were waiting for input and see new output, transition back to running
	current, ok := m.store.Get(sess.FullID)
	if ok && current.State == StateWaitingInput {
		oldState := current.State
		current.State = StateRunning
		current.UpdatedAt = time.Now()
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
