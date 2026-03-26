package session

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Manager manages claude-code sessions via tmux.
type Manager struct {
	hostname    string
	dataDir     string
	claudeBin   string
	maxSessions int
	store       *Store
	tmux        *TmuxManager
	idleTimeout time.Duration

	// onStateChange is called when a session changes state.
	// Used by the router to send Signal notifications.
	onStateChange func(sess *Session, oldState State)

	// onNeedsInput is called when a session needs user input.
	onNeedsInput func(sess *Session, prompt string)

	mu       sync.Mutex
	monitors map[string]context.CancelFunc // fullID -> cancel func for monitor goroutine
}

// NewManager creates a new session Manager.
// maxSessions limits concurrent active sessions (0 means no limit).
func NewManager(hostname, dataDir, claudeBin string, idleTimeout time.Duration) (*Manager, error) {
	storePath := filepath.Join(dataDir, "sessions.json")
	store, err := NewStore(storePath)
	if err != nil {
		return nil, fmt.Errorf("open session store: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	return &Manager{
		hostname:    hostname,
		dataDir:     dataDir,
		claudeBin:   claudeBin,
		maxSessions: 10,
		store:       store,
		tmux:        &TmuxManager{},
		idleTimeout: idleTimeout,
		monitors:    make(map[string]context.CancelFunc),
	}, nil
}

// SetStateChangeHandler sets the callback invoked on session state transitions.
func (m *Manager) SetStateChangeHandler(fn func(*Session, State)) {
	m.onStateChange = fn
}

// SetNeedsInputHandler sets the callback invoked when a session waits for input.
func (m *Manager) SetNeedsInputHandler(fn func(*Session, string)) {
	m.onNeedsInput = fn
}

// Start creates a new claude-code session for the given task.
func (m *Manager) Start(ctx context.Context, task, groupID string) (*Session, error) {
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

	// Generate unique short ID
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("generate session ID: %w", err)
	}
	fullID := fmt.Sprintf("%s-%s", m.hostname, id)

	logFile := filepath.Join(m.dataDir, "logs", fullID+".log")
	tmuxSession := fmt.Sprintf("cs-%s-%s", m.hostname, id)

	sess := &Session{
		ID:          id,
		FullID:      fullID,
		Task:        task,
		TmuxSession: tmuxSession,
		LogFile:     logFile,
		State:       StateRunning,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Hostname:    m.hostname,
		GroupID:     groupID,
	}

	// Create the log file
	f, err := os.Create(logFile)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}
	f.Close()

	// Create tmux session
	if err := m.tmux.NewSession(tmuxSession); err != nil {
		return nil, fmt.Errorf("create tmux session: %w", err)
	}

	// Pipe tmux output to log file
	if err := m.tmux.PipeOutput(tmuxSession, logFile); err != nil {
		_ = m.tmux.KillSession(tmuxSession)
		return nil, fmt.Errorf("pipe tmux output: %w", err)
	}

	// Launch claude-code with the task
	claudeCmd := fmt.Sprintf("%s %q", m.claudeBin, task)
	if err := m.tmux.SendKeys(tmuxSession, claudeCmd); err != nil {
		_ = m.tmux.KillSession(tmuxSession)
		return nil, fmt.Errorf("send claude command: %w", err)
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

	go m.monitorOutput(monCtx, sess)

	return sess, nil
}

// SendInput sends text input to a session that is waiting for input.
func (m *Manager) SendInput(fullID, input string) error {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return fmt.Errorf("session %s not found", fullID)
	}
	if sess.State != StateWaitingInput {
		return fmt.Errorf("session %s is not waiting for input (state: %s)", fullID, sess.State)
	}

	if err := m.tmux.SendKeys(sess.TmuxSession, input); err != nil {
		return fmt.Errorf("send input: %w", err)
	}

	// Transition back to running
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
	m.mu.Unlock()

	// Kill the tmux session
	_ = m.tmux.KillSession(sess.TmuxSession)

	oldState := sess.State
	sess.State = StateKilled
	sess.UpdatedAt = time.Now()
	if err := m.store.Save(sess); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	if m.onStateChange != nil {
		m.onStateChange(sess, oldState)
	}
	return nil
}

// TailOutput returns the last n lines of a session's output log.
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

	lines := strings.Split(string(data), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

// GetSession returns a session by full ID or short ID.
func (m *Manager) GetSession(id string) (*Session, bool) {
	if sess, ok := m.store.Get(id); ok {
		return sess, true
	}
	return m.store.GetByShortID(id)
}

// ListSessions returns all sessions.
func (m *Manager) ListSessions() []*Session {
	return m.store.List()
}

// ResumeMonitors restores monitoring for sessions that were running when the daemon last stopped.
func (m *Manager) ResumeMonitors(ctx context.Context) {
	for _, sess := range m.store.List() {
		if sess.Hostname != m.hostname {
			continue
		}
		if sess.State != StateRunning && sess.State != StateWaitingInput {
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

		monCtx, cancel := context.WithCancel(ctx)
		m.mu.Lock()
		m.monitors[sess.FullID] = cancel
		m.mu.Unlock()
		go m.monitorOutput(monCtx, sess)
	}
}

// monitorOutput watches the log file for patterns indicating the session needs input or has completed.
func (m *Manager) monitorOutput(ctx context.Context, sess *Session) {
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

	// Patterns that indicate claude-code is waiting for input
	promptPatterns := []string{"? ", "> ", "[y/N]", "[Y/n]", "(y/n)", "[yes/no]"}

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
						lastLine := strings.TrimSpace(pendingLines[len(pendingLines)-1])
						isPrompt := false
						for _, pat := range promptPatterns {
							if strings.HasSuffix(lastLine, pat) || strings.Contains(lastLine, pat) {
								isPrompt = true
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
				time.Sleep(500 * time.Millisecond)
				continue
			}
			line = strings.TrimRight(line, "\r\n")
			lastOutputTime = time.Now()
			pendingLines = append(pendingLines, line)
			// Keep only the last 20 lines as context
			if len(pendingLines) > 20 {
				pendingLines = pendingLines[len(pendingLines)-20:]
			}

			// If we were waiting for input and see new output, transition back to running
			current, ok := m.store.Get(sess.FullID)
			if ok && current.State == StateWaitingInput {
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

// generateID returns a random 4-char hex string (2 random bytes).
func generateID() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
