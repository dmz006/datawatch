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

// lockFileName is the sidecar advisory-lock file used by flockAcquire.
// A sidecar avoids locking the file being written (which secfile.WriteFile
// replaces via atomic rename, breaking any lock held on the old inode).
const lockFileName = "sessions.json.lock"

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
	// BackendFamily (v7.0.0-alpha.27 — renamed from LLMBackend) — the
	// backend FAMILY this session runs in: "claude-code", "ollama",
	// "opencode", "openwebui", "opencode-acp". Used by lifecycle/cleanup/
	// output-mode logic that needs the family classifier (not a specific
	// LLM entry). LLMRef (below) is the orthogonal v7 entry name.
	//
	// Migration note: legacy stored sessions use the JSON tag "llm_backend".
	// UnmarshalJSON in this package accepts both "backend_family" (current)
	// and "llm_backend" (legacy) so existing sessions/*.json load cleanly.
	BackendFamily string `json:"backend_family,omitempty"`
	// LLMRef (v7.0.0-alpha.21) — name of the v7 LLM registry entry
	// the session was launched against (resolves Kind + compute targets).
	// Empty when the session was started via the legacy v6 backend path
	// or before the v7 unified model existed.
	LLMRef string `json:"llm_ref,omitempty"`
	// ComputeNodeRef (v7.0.0-alpha.21) — name of the v7 ComputeNode
	// registry entry hosting the LLM (or session backend) for this run.
	// Empty for legacy sessions and for kinds that don't bind to a node
	// (e.g. claude-code on local host).
	ComputeNodeRef string `json:"compute_node_ref,omitempty"`
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

	// DiffSummary (BL10) holds the post-session git diff shortstat
	// (files changed, +ins/-del). Populated after PostSessionCommit
	// when project_dir is a git repo. Empty when session.auto_git_commit
	// is disabled or the project_dir is not a git repo.
	DiffSummary string `json:"diff_summary,omitempty"`

	// TokensIn / TokensOut / EstCostUSD (BL6) — running totals across
	// a session. Updated by per-backend usage parsers (initially
	// claude-code; other backends start at 0). EstCostUSD is computed
	// from per-backend rates in cost.go.
	TokensIn   int     `json:"tokens_in,omitempty"`
	TokensOut  int     `json:"tokens_out,omitempty"`
	EstCostUSD float64 `json:"est_cost_usd,omitempty"`

	// Effort (BL41) is an operator-supplied hint about thoroughness.
	// One of "quick", "normal", "thorough". Empty defaults to
	// session.default_effort (config). Backends that recognise the
	// value (e.g. claude-code) can map it to invocation flags;
	// others ignore it. Surfaces in REST + comm + MCP + UI for
	// per-session triage.
	Effort string `json:"effort,omitempty"`

	// EphemeralWorkspace (v5.26.26) marks ProjectDir as daemon-owned —
	// created by handleStartSession's project_profile clone path under
	// <data_dir>/workspaces/. When set, Manager.Delete reaps the
	// directory after killing the session. Operator-supplied
	// project_dirs are never reaped (flag is false by default).
	EphemeralWorkspace bool `json:"ephemeral_workspace,omitempty"`

	// LastChannelEventAt (BL266 v6.11.24) — wall time of the last channel
	// event (ACP/MCP message, chat_message, structured status, or
	// significant tmux pane change). Used by the channel-state watcher
	// to flip Running → WaitingInput after a configurable gap, and by
	// the PWA to render a "stale comms" indicator after ~2 s of silence
	// without changing state. Zero value = no events yet.
	LastChannelEventAt time.Time `json:"last_channel_event_at,omitempty"`

	// Skills (BL255 v6.7.0) — names of synced skills to inject into
	// <ProjectDir>/.datawatch/skills/<name>/ at session start (option C
	// of the BL255 design). Lifecycle hooks live in cmd/datawatch/main.go
	// (parallels the BL219 GitignoreCheckOnStart/CleanupArtifactsOnEnd
	// pattern). Option D — `skill_load` MCP tool — is always available
	// regardless of this field. Inherited from PRD.Skills when the
	// session is spawned by an automaton; settable directly via
	// /api/sessions/start for operator-launched sessions.
	Skills []string `json:"skills,omitempty"`

	// OneShot — when true, a DATAWATCH_COMPLETE: marker in the pane output
	// transitions the session to StateComplete (terminal). When false
	// (default for interactive sessions), the same marker transitions to
	// StateWaitingInput so the operator can continue sending input. Autonomous
	// task runs (PRD executors, agent spawns) set this to true so the session
	// self-terminates after the task is done.
	OneShot bool `json:"one_shot,omitempty"`

	// BL331 — federation peer that owns or originated this session via channel routing.
	OwnerPeer string `json:"owner_peer,omitempty"`
}

// UnmarshalJSON accepts both the v7.0.0-alpha.27 field name
// "backend_family" AND the legacy "llm_backend" so existing stored
// sessions/*.json from pre-alpha.27 daemons load cleanly. After load
// we always persist with the new name.
func (s *Session) UnmarshalJSON(data []byte) error {
	type alias Session
	aux := struct {
		*alias
		LegacyLLMBackend string `json:"llm_backend,omitempty"`
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if s.BackendFamily == "" && aux.LegacyLLMBackend != "" {
		s.BackendFamily = aux.LegacyLLMBackend
	}
	return nil
}

// EffortLevels enumerates the valid Session.Effort values.
var EffortLevels = []string{"quick", "normal", "thorough"}

// IsValidEffort reports whether s is a recognised effort level.
// Empty string is valid (means "use config default").
func IsValidEffort(s string) bool {
	if s == "" {
		return true
	}
	for _, v := range EffortLevels {
		if v == s {
			return true
		}
	}
	return false
}

// Store is a persistent JSON store for sessions.
type Store struct {
	mu       sync.RWMutex
	path     string
	lockPath string // advisory lock sidecar file (sessions.json.lock)
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
		lockPath: filepath.Join(filepath.Dir(path), lockFileName),
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
			// v6.15.1 (BL286) — operator post-mortem 2026-05-07: a non-
			// atomic write from a previous version left this file half-
			// flushed and the daemon crashed at startup. Recovery: walk
			// brace depth + string state, find the last fully-closed
			// top-level object, truncate there, append "]" to close the
			// array. Save the corrupted body to <path>.corrupted-<unix>
			// for forensics. Don't fail the daemon — the operator's
			// alternative is "manually edit JSON to boot the server".
			recovered, n, recErr := recoverSessionStore(data)
			if recErr != nil {
				return nil, fmt.Errorf("parse store %s: %w (recovery also failed: %v)", path, err, recErr)
			}
			ts := time.Now().Unix()
			corruptedPath := fmt.Sprintf("%s.corrupted-%d", path, ts)
			_ = os.WriteFile(corruptedPath, data, 0600)
			fmt.Printf("[warn] sessions store %s was corrupted (likely a non-atomic write killed mid-flush in a pre-v6.15.1 daemon). Recovered %d sessions from JSON prefix; original body saved to %s.\n", path, n, corruptedPath)
			sessions = recovered
		}
		for _, sess := range sessions {
			s.sessions[sess.FullID] = sess
		}
	}

	// v6.15.1 (BL286) — operator: "shouldn't auto repair also look at
	// the sessions like it does for recovering orphaned sessions?". Yes.
	// After loading whatever the JSON file gave us (possibly recovered),
	// walk the per-session subdirectories under <data_dir>/sessions/<id>/
	// and merge any session.json that's missing from the in-memory map.
	// This catches the tail of sessions that got truncated past the JSON
	// recovery point — each session subdirectory carries the full record
	// independently. Runtime-only state (in-memory caches, WS subscriptions)
	// is lost on any daemon restart, so subdir merge captures everything
	// that was persistable in the first place.
	if added := s.mergeFromSessionDirs(filepath.Dir(path)); added > 0 {
		fmt.Printf("[recovery] merged %d session(s) from per-session subdirectories not present in sessions.json\n", added)
	}

	return s, nil
}

// mergeFromSessionDirs walks <dataDir>/sessions/<id>/session.json and
// adds any session not already in the in-memory map. Returns the count
// added. Best-effort: per-subdir parse failures are logged + skipped,
// not surfaced as a fatal error.
func (s *Store) mergeFromSessionDirs(dataDir string) int {
	sessRoot := filepath.Join(dataDir, "sessions")
	entries, err := os.ReadDir(sessRoot)
	if err != nil {
		return 0
	}
	added := 0
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		fullID := ent.Name()
		if _, exists := s.sessions[fullID]; exists {
			continue
		}
		metaPath := filepath.Join(sessRoot, fullID, "session.json")
		raw, err := os.ReadFile(metaPath)
		if err != nil {
			continue // missing or unreadable — skip silently
		}
		var sess Session
		if err := json.Unmarshal(raw, &sess); err != nil {
			fmt.Printf("[recovery] skipping %s: parse %s: %v\n", fullID, metaPath, err)
			continue
		}
		if sess.FullID == "" {
			sess.FullID = fullID
		}
		s.sessions[sess.FullID] = &sess
		added++
	}
	return added
}

// recoverSessionStore walks corrupted JSON-array body; returns parsed
// sessions from the prefix that ends at the last fully-closed top-level
// object. Used only at boot when json.Unmarshal fails on the raw body.
func recoverSessionStore(data []byte) ([]*Session, int, error) {
	depth := 0
	inString := false
	escape := false
	sawOpenArray := false
	lastCompleteEnd := 0
	for i, b := range data {
		ch := rune(b)
		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inString {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '[' && depth == 0 && !sawOpenArray {
			sawOpenArray = true
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				j := i + 1
				for j < len(data) {
					c := data[j]
					if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' {
						j++
					} else {
						break
					}
				}
				lastCompleteEnd = j
			}
		}
	}
	if lastCompleteEnd == 0 || !sawOpenArray {
		return nil, 0, fmt.Errorf("no complete session objects found")
	}
	prefix := strings.TrimRight(string(data[:lastCompleteEnd]), " \t\n\r,")
	repaired := prefix + "\n]\n"
	var sessions []*Session
	if err := json.Unmarshal([]byte(repaired), &sessions); err != nil {
		return nil, 0, err
	}
	return sessions, len(sessions), nil
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

// Flush forces an immediate write of the current in-memory session
// set to disk. Save/Delete already write through synchronously so
// callers normally do not need to call Flush; it exists so external
// reconcilers (BL93/BL94) and shutdown hooks can guarantee the file
// reflects the in-memory state without performing another mutation.
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persist()
}

// persist writes the current sessions to disk.
// Must be called with mu held.
//
// BL294 — an exclusive advisory flock on sessions.json.lock serialises
// concurrent persist() calls from two Manager instances that briefly
// co-exist during the daemon re-exec path (capability elevation).
// The lock is held only for the duration of the atomic write and
// released immediately after. A 5-second timeout prevents deadlock:
// if the lock cannot be acquired within that window the persist is
// skipped (the ReconcileSessions boot call recovers any missed writes).
func (s *Store) persist() error {
	// Acquire cross-process advisory lock on the sidecar lock file.
	// flockAcquire returns (nil, nil) on timeout — caller skips the persist.
	lockFile, lockErr := flockAcquire(s.lockPath)
	if lockErr != nil {
		return fmt.Errorf("acquire sessions lock: %w", lockErr)
	}
	if lockFile == nil {
		// Timed out — skip this persist cycle rather than risk a deadlock.
		// The warning was already printed by flockAcquire.
		return nil
	}
	defer flockRelease(lockFile)

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
