package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// State represents the lifecycle state of a claude-code session.
type State string

const (
	StateRunning      State = "running"
	StateWaitingInput State = "waiting_input"
	StateComplete     State = "complete"
	StateFailed       State = "failed"
	StateKilled       State = "killed"
	StateRateLimited  State = "rate_limited"
)

// Session holds metadata about a single claude-code session.
type Session struct {
	ID          string    `json:"id"`           // 4-char hex
	FullID      string    `json:"full_id"`      // hostname-id
	Name        string    `json:"name,omitempty"` // optional human-readable name
	Task        string    `json:"task"`         // original task description
	ProjectDir  string    `json:"project_dir"`  // working directory for claude-code
	TmuxSession string    `json:"tmux_session"` // tmux session name
	LogFile     string    `json:"log_file"`     // path to output log
	State       State     `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Hostname    string    `json:"hostname"`
	GroupID     string    `json:"group_id"`
	LLMBackend  string    `json:"llm_backend,omitempty"` // which LLM backend was used
	// PendingInput is set when the session is waiting for user input
	PendingInput string `json:"pending_input,omitempty"`
	// LastPrompt is the last prompt text that triggered waiting_input
	LastPrompt string `json:"last_prompt,omitempty"`
	// PromptContext holds non-empty screen lines surrounding the detected prompt,
	// giving context about what the prompt is asking the user to confirm/decide.
	PromptContext string `json:"prompt_context,omitempty"`
	// RateLimitResetAt is set when the session is in rate_limited state.
	// The daemon will retry automatically after this time.
	RateLimitResetAt *time.Time `json:"rate_limit_reset_at,omitempty"`
	TrackingDir      string     `json:"tracking_dir"` // path to session git folder
	ConsoleCols      int        `json:"console_cols,omitempty"` // tmux terminal width
	ConsoleRows      int        `json:"console_rows,omitempty"` // tmux terminal height
	InputCount       int        `json:"input_count,omitempty"`  // number of inputs/prompts sent
	LastInput        string     `json:"last_input,omitempty"`   // last input sent (for alert logging, truncated)
	OutputMode       string     `json:"output_mode,omitempty"`  // "terminal" or "log" — controls web display
	InputMode        string     `json:"input_mode,omitempty"`   // "tmux" or "none" — controls input bar visibility
	ChannelReady     bool       `json:"channel_ready,omitempty"` // true when MCP channel is connected
	ChannelPort      int        `json:"channel_port,omitempty"`  // per-session MCP channel HTTP port
	ThreadIDs        map[string]string `json:"thread_ids,omitempty"` // per-backend thread IDs for threaded messaging
	Profile          string            `json:"profile,omitempty"`    // named profile used to launch this session
	FallbackOf       string            `json:"fallback_of,omitempty"` // session ID this is a fallback for
	// LastResponse holds the LLM's most recent response text, captured on
	// running→waiting_input transitions. Used for /copy, alerts, and memory.
	LastResponse     string            `json:"last_response,omitempty"`
	// AgentID is set when this session lives inside a parent-spawned
	// worker container (F10 sprint 3.6). The session API forwards
	// reads/writes for these sessions through /api/proxy/agent/{id}/...
	// so the parent UI sees one coherent session list. Empty for
	// sessions running directly on this host.
	AgentID string `json:"agent_id,omitempty"`
}

// Store is a persistent JSON store for sessions.
type Store struct {
	mu       sync.RWMutex
	path     string
	encKey   []byte // nil = no encryption
	sessions map[string]*Session // key: full ID
}

// NewStore creates a new Store backed by the file at path (no encryption).
// If the file does not exist, an empty store is created.
func NewStore(path string) (*Store, error) {
	return newStoreWithKey(path, nil)
}

// NewStoreEncrypted creates a Store with AES-256-GCM encryption at rest.
// key must be exactly 32 bytes (use config.DeriveKey to produce one).
func NewStoreEncrypted(path string, key []byte) (*Store, error) {
	return newStoreWithKey(path, key)
}

func newStoreWithKey(path string, key []byte) (*Store, error) {
	s := &Store{
		path:     path,
		encKey:   key,
		sessions: make(map[string]*Session),
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}

	data, err := secfile.ReadFile(path, key)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read store %s: %w", path, err)
	}
	if len(data) > 0 {
		var sessions []*Session
		if err := json.Unmarshal(data, &sessions); err != nil {
			return nil, fmt.Errorf("parse store %s: %w", path, err)
		}
		for _, sess := range sessions {
			s.sessions[sess.FullID] = sess
		}
	}

	return s, nil
}

// Save stores or updates a session.
func (s *Store) Save(sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.FullID] = sess
	return s.persist()
}

// Get returns a session by full ID.
func (s *Store) Get(fullID string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[fullID]
	return sess, ok
}

// GetByShortID returns a session by its 4-char hex ID.
// Matches if the session FullID ends with the shortID (hostname-shortID format).
func (s *Store) GetByShortID(shortID string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	shortID = strings.ToLower(shortID)
	for _, sess := range s.sessions {
		if sess.ID == shortID {
			return sess, true
		}
	}
	return nil, false
}

// List returns all sessions, unordered.
func (s *Store) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

// Delete removes a session from the store.
func (s *Store) Delete(fullID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, fullID)
	return s.persist()
}

// persist writes the current sessions to disk.
// Must be called with mu held.
func (s *Store) persist() error {
	sessions := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}

	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}

	if err := secfile.WriteFile(s.path, data, 0644, s.encKey); err != nil {
		return fmt.Errorf("write sessions %s: %w", s.path, err)
	}
	return nil
}
