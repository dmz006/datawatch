// BL297 (v6.22.3) — persona-wizard drafts store.
//
// Operator-decided 2026-05-08 (interview Q9): bidirectional comm
// channels + PWA chat-style wizards need per-operator state across
// messages; CLI + one-way channels use one-shot endpoints. Both flows
// share this SQLite-backed drafts table.
//
// Operator-decided 2026-05-08 (interview Q-final-C): SQLite under
// ~/.datawatch/council/wizard-sessions.db with N-day GC + operator
// manual cleanup (selective + purge-all) + N-day retention as a fully
// configurable cfg option. Default retention 7 days.

package council

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DraftStatus is the lifecycle phase of a wizard draft.
type DraftStatus string

const (
	DraftInProgress DraftStatus = "in_progress"
	DraftDrafted    DraftStatus = "drafted"
	DraftSaved      DraftStatus = "saved"
	DraftAbandoned  DraftStatus = "abandoned"
)

// Draft is one in-flight persona-wizard session.
type Draft struct {
	ID            string      `json:"id"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	OperatorRef   string      `json:"operator_ref,omitempty"`
	ChannelRef    string      `json:"channel_ref,omitempty"`
	Status        DraftStatus `json:"status"`
	Backend       string      `json:"backend,omitempty"`
	Name          string      `json:"name"`
	Role          string      `json:"role"`
	Focus         string      `json:"focus"`
	Stance        string      `json:"stance"`
	Tone          string      `json:"tone"`
	AntiPatterns  string      `json:"anti_patterns"`
	Examples      string      `json:"examples"`
	DraftPersona  string      `json:"draft_persona,omitempty"`
	DraftTags     string      `json:"draft_tags,omitempty"`
	CurrentStep   string      `json:"current_step,omitempty"`
}

// DraftsStore wraps the SQLite-backed drafts table.
type DraftsStore struct {
	db *sql.DB
	mu sync.Mutex
}

// NewDraftsStore opens / creates the drafts SQLite at the given path.
func NewDraftsStore(dbPath string) (*DraftsStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("council drafts: mkdir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("council drafts: open: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS drafts (
			id            TEXT PRIMARY KEY,
			created_at    INTEGER NOT NULL,
			updated_at    INTEGER NOT NULL,
			operator_ref  TEXT NOT NULL DEFAULT '',
			channel_ref   TEXT NOT NULL DEFAULT '',
			status        TEXT NOT NULL,
			backend       TEXT NOT NULL DEFAULT '',
			name          TEXT NOT NULL DEFAULT '',
			role          TEXT NOT NULL DEFAULT '',
			focus         TEXT NOT NULL DEFAULT '',
			stance        TEXT NOT NULL DEFAULT '',
			tone          TEXT NOT NULL DEFAULT '',
			anti_patterns TEXT NOT NULL DEFAULT '',
			examples      TEXT NOT NULL DEFAULT '',
			draft_persona TEXT NOT NULL DEFAULT '',
			draft_tags    TEXT NOT NULL DEFAULT '',
			current_step  TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS drafts_updated_at_idx ON drafts(updated_at);
		CREATE INDEX IF NOT EXISTS drafts_operator_ref_idx ON drafts(operator_ref, channel_ref);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("council drafts: create table: %w", err)
	}
	return &DraftsStore{db: db}, nil
}

// Close releases the underlying DB.
func (s *DraftsStore) Close() error { return s.db.Close() }

// New starts a fresh draft (creates row in DB) and returns its ID.
func (s *DraftsStore) New(operatorRef, channelRef string) (*Draft, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	d := &Draft{
		ID:          id,
		CreatedAt:   now,
		UpdatedAt:   now,
		OperatorRef: operatorRef,
		ChannelRef:  channelRef,
		Status:      DraftInProgress,
		CurrentStep: "name_role",
	}
	if err := s.upsert(d); err != nil {
		return nil, err
	}
	return d, nil
}

// Get returns a draft by ID, or ErrNotFound.
func (s *DraftsStore) Get(id string) (*Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT
		id, created_at, updated_at, operator_ref, channel_ref, status, backend,
		name, role, focus, stance, tone, anti_patterns, examples,
		draft_persona, draft_tags, current_step
		FROM drafts WHERE id = ?`, id)
	d := &Draft{}
	var ca, ua int64
	if err := row.Scan(&d.ID, &ca, &ua, &d.OperatorRef, &d.ChannelRef, &d.Status, &d.Backend,
		&d.Name, &d.Role, &d.Focus, &d.Stance, &d.Tone, &d.AntiPatterns, &d.Examples,
		&d.DraftPersona, &d.DraftTags, &d.CurrentStep); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.CreatedAt = time.Unix(ca, 0).UTC()
	d.UpdatedAt = time.Unix(ua, 0).UTC()
	return d, nil
}

// Update persists changes to a draft (refreshes UpdatedAt).
func (s *DraftsStore) Update(d *Draft) error {
	d.UpdatedAt = time.Now().UTC()
	return s.upsert(d)
}

// FindActive returns the most-recent in-progress draft for an
// operator+channel pair, or nil.
func (s *DraftsStore) FindActive(operatorRef, channelRef string) (*Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`SELECT id FROM drafts
		WHERE operator_ref = ? AND channel_ref = ?
		AND status IN (?, ?)
		ORDER BY updated_at DESC LIMIT 1`,
		operatorRef, channelRef, string(DraftInProgress), string(DraftDrafted))
	var id string
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	s.mu.Unlock()
	out, err := s.Get(id)
	s.mu.Lock()
	return out, err
}

// List returns every draft (newest first).
func (s *DraftsStore) List() ([]*Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT
		id, created_at, updated_at, operator_ref, channel_ref, status, backend,
		name, role, focus, stance, tone, anti_patterns, examples,
		draft_persona, draft_tags, current_step
		FROM drafts ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Draft
	for rows.Next() {
		d := &Draft{}
		var ca, ua int64
		if err := rows.Scan(&d.ID, &ca, &ua, &d.OperatorRef, &d.ChannelRef, &d.Status, &d.Backend,
			&d.Name, &d.Role, &d.Focus, &d.Stance, &d.Tone, &d.AntiPatterns, &d.Examples,
			&d.DraftPersona, &d.DraftTags, &d.CurrentStep); err != nil {
			return nil, err
		}
		d.CreatedAt = time.Unix(ca, 0).UTC()
		d.UpdatedAt = time.Unix(ua, 0).UTC()
		out = append(out, d)
	}
	return out, nil
}

// Delete removes a single draft by ID.
func (s *DraftsStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM drafts WHERE id = ?`, id)
	return err
}

// Purge removes ALL drafts (operator-controlled cleanup).
func (s *DraftsStore) Purge() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM drafts`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// GC deletes drafts older than retentionDays. retentionDays<=0 disables
// GC (caller should guard via cfg). Returns count deleted.
func (s *DraftsStore) GC(retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM drafts WHERE updated_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// upsert (caller-controlled lock).
func (s *DraftsStore) upsert(d *Draft) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		INSERT INTO drafts (
			id, created_at, updated_at, operator_ref, channel_ref, status, backend,
			name, role, focus, stance, tone, anti_patterns, examples,
			draft_persona, draft_tags, current_step
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			updated_at    = excluded.updated_at,
			status        = excluded.status,
			backend       = excluded.backend,
			name          = excluded.name,
			role          = excluded.role,
			focus         = excluded.focus,
			stance        = excluded.stance,
			tone          = excluded.tone,
			anti_patterns = excluded.anti_patterns,
			examples      = excluded.examples,
			draft_persona = excluded.draft_persona,
			draft_tags    = excluded.draft_tags,
			current_step  = excluded.current_step`,
		d.ID, d.CreatedAt.Unix(), d.UpdatedAt.Unix(), d.OperatorRef, d.ChannelRef,
		string(d.Status), d.Backend,
		d.Name, d.Role, d.Focus, d.Stance, d.Tone, d.AntiPatterns, d.Examples,
		d.DraftPersona, d.DraftTags, d.CurrentStep)
	return err
}

// ErrNotFound is returned by Get when the ID is unknown.
var ErrNotFound = errors.New("council draft not found")

// newID returns a 16-byte hex token for draft IDs.
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SerializeFields returns the operator-supplied interview fields as a
// readable JSON-ish string for the LLM prompt. Order matches the
// interview sequence.
func (d *Draft) SerializeFields() string {
	var b strings.Builder
	if d.Name != "" {
		fmt.Fprintf(&b, "name: %s\n", d.Name)
	}
	if d.Role != "" {
		fmt.Fprintf(&b, "role (operator-supplied): %s\n", d.Role)
	}
	if d.Focus != "" {
		fmt.Fprintf(&b, "focus: %s\n", d.Focus)
	}
	if d.Stance != "" {
		fmt.Fprintf(&b, "stance: %s\n", d.Stance)
	}
	if d.Tone != "" {
		fmt.Fprintf(&b, "tone: %s\n", d.Tone)
	}
	if d.AntiPatterns != "" {
		fmt.Fprintf(&b, "anti_patterns: %s\n", d.AntiPatterns)
	}
	if d.Examples != "" {
		fmt.Fprintf(&b, "examples: %s\n", d.Examples)
	}
	return b.String()
}
