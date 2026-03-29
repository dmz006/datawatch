package session

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/backends/opencode"
)

// ansiEscapeRe matches ANSI terminal escape sequences.
var ansiEscapeRe = regexp.MustCompile(`\x1b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])`)

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

// promptPatterns detects when an LLM is waiting for user input (used in idle detection).
var promptPatterns = []string{
	"? ", "> ", "[y/N]", "[Y/n]", "(y/n)", "[yes/no]",
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
	autoGit     bool // whether to auto-commit project dir
	autoGitInit bool // whether to git init project dir if needed
	verbose     bool // enable debug logging

	// onStateChange is called when a session changes state.
	// Used by the router to send Signal notifications.
	onStateChange func(sess *Session, oldState State)

	// onNeedsInput is called when a session needs user input.
	onNeedsInput func(sess *Session, prompt string)

	// onOutput is called for each new line of output from a session.
	onOutput func(sess *Session, line string)

	// onSessionStart is called immediately after a session is successfully started.
	onSessionStart func(sess *Session)

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
		hostname:    hostname,
		dataDir:     dataDir,
		llmBin:      llmBin,
		llmBackend:  "claude-code",
		maxSessions: 10,
		store:       store,
		tmux:        &TmuxManager{},
		idleTimeout: idleTimeout,
		monitors:    make(map[string]context.CancelFunc),
		trackers:    make(map[string]*Tracker),
	}, nil
}

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

// SetOnSessionStart sets the callback invoked immediately after a session starts successfully.
func (m *Manager) SetOnSessionStart(fn func(*Session)) {
	m.onSessionStart = fn
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

	// Handle project dir git
	projGit := NewProjectGit(projectDir)
	if m.autoGit {
		if m.autoGitInit && !projGit.IsRepo() {
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

	// Create tmux session
	m.debugf("creating tmux session %q for backend=%q task=%q dir=%q", tmuxSession, backendName, task, projectDir)
	if err := m.tmux.NewSession(tmuxSession); err != nil {
		return nil, fmt.Errorf("create tmux session: %w", err)
	}

	// Pipe tmux output to tracker's output.log
	if err := m.tmux.PipeOutput(tmuxSession, logFile); err != nil {
		_ = m.tmux.KillSession(tmuxSession)
		return nil, fmt.Errorf("pipe tmux output: %w", err)
	}
	m.debugf("tmux session %q piped to %s", tmuxSession, logFile)

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
	if sess.State != StateWaitingInput && sess.State != StateRunning {
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

	// Record input in tracker
	m.mu.Lock()
	tracker := m.trackers[fullID]
	m.mu.Unlock()
	if tracker != nil {
		if err := tracker.RecordInputSent(input, source); err != nil {
			fmt.Printf("[warn] tracker.RecordInputSent: %v\n", err)
		}
	}

	// Transition back to running only if we were waiting for input
	if sess.State == StateWaitingInput {
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

	// Cancel the monitor goroutine
	m.mu.Lock()
	if cancel, ok := m.monitors[fullID]; ok {
		cancel()
		delete(m.monitors, fullID)
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

	data, err := os.ReadFile(sess.LogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "(no output yet)", nil
		}
		return "", fmt.Errorf("read log: %w", err)
	}

	// Strip ANSI escape codes and carriage returns so messaging backends show clean text.
	// \r is produced by TUI applications doing in-place redraws (e.g. claude trust prompt).
	clean := StripANSI(string(data))
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	lines := strings.Split(clean, "\n")
	// Remove empty trailing lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
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
		// Check if tmux session still exists
		if !m.tmux.SessionExists(sess.TmuxSession) {
			// Mark as failed — tmux session is gone
			oldState := sess.State
			sess.State = StateFailed
			sess.UpdatedAt = time.Now()
			_ = m.store.Save(sess)
			if m.onStateChange != nil {
				m.onStateChange(sess, oldState)
			}
			continue
		}

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

	// Seek to end
	if _, err := f.Seek(0, 2); err != nil {
		return
	}

	reader := bufio.NewReader(f)

	var lastOutputTime time.Time
	var pendingLines []string
	idleCheckTicker := time.NewTicker(2 * time.Second)
	defer idleCheckTicker.Stop()

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
			// Check if tmux session is still alive
			if !m.tmux.SessionExists(sess.TmuxSession) {
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
				}
				return
			}

			// Check for idle — no new output for idleTimeout
			current, ok := m.store.Get(sess.FullID)
			if !ok {
				return
			}
			if current.State == StateRunning && !lastOutputTime.IsZero() {
				if time.Since(lastOutputTime) >= m.idleTimeout {
					// Check if the last few lines look like a prompt
					if len(pendingLines) > 0 {
						lastLine := StripANSI(strings.TrimSpace(pendingLines[len(pendingLines)-1]))
						m.debugf("idle check session=%s lastLine=%q", sess.FullID, lastLine)
						isPrompt := false
						for _, pat := range promptPatterns {
							if strings.HasSuffix(lastLine, pat) || strings.Contains(lastLine, pat) {
								isPrompt = true
								m.debugf("prompt detected via pattern %q", pat)
								break
							}
						}
						if isPrompt {
							prompt := strings.Join(pendingLines, "\n")
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
		default:
			// Try to read a new line
			line, err := reader.ReadString('\n')
			if err != nil {
				// No new data; sleep briefly
				time.Sleep(50 * time.Millisecond)
				continue
			}
			line = strings.TrimRight(line, "\r\n")
			line = StripANSI(line)
			lastOutputTime = time.Now()
			pendingLines = append(pendingLines, line)
			// Keep only the last 20 lines as context
			if len(pendingLines) > 20 {
				pendingLines = pendingLines[len(pendingLines)-20:]
			}
			if m.onOutput != nil {
				m.onOutput(sess, line)
			}

			// Check for rate limit patterns
			lineLower := strings.ToLower(line)
			isRateLimit := false
			for _, pat := range rateLimitPatterns {
				if strings.Contains(lineLower, strings.ToLower(pat)) || strings.Contains(line, pat) {
					isRateLimit = true
					break
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
				continue
			}

			// Check for explicit completion pattern
			for _, pat := range completionPatterns {
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
					}
					break
				}
			}

			// Check for explicit input needed pattern
			for _, pat := range inputNeededPatterns {
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
					break
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
