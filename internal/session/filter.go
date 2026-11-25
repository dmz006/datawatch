package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// FilterAction identifies what a filter does when it matches.
type FilterAction string

const (
	// FilterActionSendInput sends the Value string as input to the session.
	FilterActionSendInput FilterAction = "send_input"
	// FilterActionAlert creates a system alert with the Value as the body.
	FilterActionAlert FilterAction = "alert"
	// FilterActionSchedule schedules the Value as a command when the session next asks for input.
	FilterActionSchedule FilterAction = "schedule"
	// FilterActionDetectPrompt marks the session as waiting for input immediately
	// (without waiting for the idle timeout). Value is unused.
	FilterActionDetectPrompt FilterAction = "detect_prompt"
)

// FilterPattern is a rule: when output matches Pattern, perform Action.
type FilterPattern struct {
	ID        string       `json:"id"`
	Pattern   string       `json:"pattern"` // regular expression matched against output lines
	Action    FilterAction `json:"action"`
	Value     string       `json:"value"` // input text, alert body, or scheduled command
	Enabled   bool         `json:"enabled"`
	Seeded    bool         `json:"seeded,omitempty"` // true for pre-populated patterns
	CreatedAt time.Time    `json:"created_at"`
	// DisabledReason says why the daemon turned the filter off.
	DisabledReason string `json:"disabled_reason,omitempty"`
	// PendingCleanup marks a filter the daemon turned off whose queued
	// input still has to be cancelled.
	PendingCleanup bool `json:"pending_cleanup,omitempty"`
}

// FilterStore is a persistent, thread-safe store for FilterPattern rules.
type FilterStore struct {
	mu      sync.Mutex
	path    string
	encKey  []byte
	filters []FilterPattern
}

// NewFilterStore creates a FilterStore backed by the given file path (no encryption).
func NewFilterStore(path string) (*FilterStore, error) {
	return newFilterStoreWithKey(path, nil)
}

// NewFilterStoreEncrypted creates a FilterStore with AES-256-GCM encryption at rest.
func NewFilterStoreEncrypted(path string, key []byte) (*FilterStore, error) {
	return newFilterStoreWithKey(path, key)
}

func newFilterStoreWithKey(path string, key []byte) (*FilterStore, error) {
	fs := &FilterStore{path: path, encKey: key}
	if err := fs.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load filter store: %w", err)
	}
	// Every start, so an upgrade turns off what an older version allowed.
	if fs.quarantineLocked() {
		if err := fs.save(); err != nil {
			return nil, fmt.Errorf("save filter store after turning off input-typing filters: %w", err)
		}
	}
	return fs, nil
}

func (fs *FilterStore) load() error {
	data, err := secfile.ReadFile(fs.path, fs.encKey)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &fs.filters)
}

func (fs *FilterStore) save() error {
	data, err := json.MarshalIndent(fs.filters, "", "  ")
	if err != nil {
		return err
	}
	return secfile.WriteFile(fs.path, data, 0600, fs.encKey)
}

// Add creates a new filter pattern. Returns error if the pattern regex is
// invalid. A filter that types input must pass ValidateInputFilter and starts
// disabled; every other filter starts enabled.
func (fs *FilterStore) Add(pattern string, action FilterAction, value string) (*FilterPattern, error) {
	if _, err := regexp.Compile(pattern); err != nil {
		return nil, fmt.Errorf("invalid pattern regex: %w", err)
	}
	if action.TypesInput() {
		if err := ValidateInputFilter(pattern, value); err != nil {
			return nil, err
		}
	}
	b := make([]byte, 2)
	rand.Read(b) //nolint:errcheck
	fp := FilterPattern{
		ID:        hex.EncodeToString(b),
		Pattern:   pattern,
		Action:    action,
		Value:     value,
		Enabled:   !action.TypesInput(),
		CreatedAt: time.Now(),
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.filters = append(fs.filters, fp)
	return &fp, fs.save()
}

// Delete removes a filter by ID.
func (fs *FilterStore) Delete(id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for i, f := range fs.filters {
		if f.ID == id {
			fs.filters = append(fs.filters[:i], fs.filters[i+1:]...)
			return fs.save()
		}
	}
	return fmt.Errorf("filter %q not found", id)
}

// SetEnabled enables or disables a filter by ID.
func (fs *FilterStore) SetEnabled(id string, enabled bool) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for i, f := range fs.filters {
		if f.ID == id {
			if enabled && f.Action.TypesInput() {
				if f.Seeded {
					return fmt.Errorf("%w: a built-in filter may not type input", ErrInputFilterRefused)
				}
				if err := ValidateInputFilter(f.Pattern, f.Value); err != nil {
					return err
				}
			}
			fs.filters[i].Enabled = enabled
			if enabled {
				fs.filters[i].DisabledReason = ""
			}
			return fs.save()
		}
	}
	return fmt.Errorf("filter %q not found", id)
}

// Update replaces mutable fields of an existing filter (pattern, action,
// value, enabled). An edit that leaves a filter typing input is validated and
// always leaves it disabled: turning it on is its own step.
func (fs *FilterStore) Update(id, pattern, action, value string, enabled bool) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for i, f := range fs.filters {
		if f.ID == id {
			next := f
			if pattern != "" {
				if _, err := regexp.Compile(pattern); err != nil {
					return fmt.Errorf("invalid pattern regex: %w", err)
				}
				next.Pattern = pattern
			}
			if action != "" {
				next.Action = FilterAction(action)
			}
			next.Value = value
			next.Enabled = enabled
			if next.Action.TypesInput() {
				if err := ValidateInputFilter(next.Pattern, next.Value); err != nil {
					return err
				}
				next.Enabled = false
				// The operator wrote this one now.
				next.Seeded = false
				next.DisabledReason = ""
			}
			fs.filters[i] = next
			return fs.save()
		}
	}
	return fmt.Errorf("filter %q not found", id)
}

// List returns all filters.
func (fs *FilterStore) List() []FilterPattern {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]FilterPattern, len(fs.filters))
	copy(out, fs.filters)
	return out
}

// Seed adds the given filter patterns if no pattern with the same Pattern string already exists.
func (fs *FilterStore) Seed(seeds []FilterPattern) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	changed := false
	for _, s := range seeds {
		if s.Action.TypesInput() {
			continue // a built-in filter may not type input
		}
		if _, err := regexp.Compile(s.Pattern); err != nil {
			continue // skip invalid patterns
		}
		found := false
		for _, f := range fs.filters {
			if f.Pattern == s.Pattern {
				found = true
				break
			}
		}
		if !found {
			b := make([]byte, 2)
			rand.Read(b) //nolint:errcheck
			s.ID = hex.EncodeToString(b)
			if s.CreatedAt.IsZero() {
				s.CreatedAt = time.Now()
			}
			s.Seeded = true
			s.Enabled = true
			fs.filters = append(fs.filters, s)
			changed = true
		}
	}
	if changed {
		return fs.save()
	}
	return nil
}

// ActionHandlers holds callbacks the FilterEngine uses when a filter matches.
type ActionHandlers struct {
	// MayType is asked before an input-typing filter does anything: it
	// answers for the daemon-wide switch, the filter and what the session
	// shows. A nil MayType means no filter types.
	MayType      func(sessID, filterID string) error
	SendInput    func(sessID, filterID, text string) error
	AddAlert     func(sessID, title, body string)
	AddSchedule  func(sessID, filterID, command string) error
	DetectPrompt func(sessID, matchedLine string) // marks session as waiting_input immediately
}

// FilterEngine applies enabled filters to output lines and fires actions.
type FilterEngine struct {
	store    *FilterStore
	handlers ActionHandlers
	compiled map[string]*regexp.Regexp // pattern -> compiled regex (lazy)
	mu       sync.Mutex
}

// NewFilterEngine creates a FilterEngine backed by the given store.
func NewFilterEngine(store *FilterStore, handlers ActionHandlers) *FilterEngine {
	return &FilterEngine{
		store:    store,
		handlers: handlers,
		compiled: make(map[string]*regexp.Regexp),
	}
}

// ProcessLine checks the given output line against all enabled filters for the session.
func (e *FilterEngine) ProcessLine(sess *Session, line string) {
	filters := e.store.List()
	e.mu.Lock()
	for _, f := range filters {
		if !f.Enabled {
			continue
		}
		// Keyed by the pattern text: an edited filter must never keep
		// matching with the regex it had before.
		re, ok := e.compiled[f.Pattern]
		if !ok {
			var err error
			re, err = regexp.Compile(f.Pattern)
			if err != nil {
				continue
			}
			e.compiled[f.Pattern] = re
		}
		if !re.MatchString(line) {
			continue
		}
		// Fire action (unlock first to avoid re-entrancy deadlock)
		action := f.Action
		value := f.Value
		id := f.ID
		e.mu.Unlock()
		if action.TypesInput() {
			// A line of a permission or confirmation prompt schedules and
			// types nothing, whatever the pattern says.
			if IsPromptLine(line) || e.handlers.MayType == nil || e.handlers.MayType(sess.FullID, id) != nil {
				e.mu.Lock()
				continue
			}
		}
		switch action {
		case FilterActionSendInput:
			if e.handlers.SendInput != nil {
				e.handlers.SendInput(sess.FullID, id, value) //nolint:errcheck
			}
		case FilterActionAlert:
			if e.handlers.AddAlert != nil {
				e.handlers.AddAlert(sess.FullID, fmt.Sprintf("[%s] filter match", sess.ID), value)
			}
		case FilterActionSchedule:
			if e.handlers.AddSchedule != nil {
				e.handlers.AddSchedule(sess.FullID, id, value) //nolint:errcheck
			}
		case FilterActionDetectPrompt:
			if e.handlers.DetectPrompt != nil {
				e.handlers.DetectPrompt(sess.FullID, line)
			}
		}
		e.mu.Lock()
	}
	e.mu.Unlock()
}

// InvalidateCache clears the compiled regex cache (call after filters are changed).
func (e *FilterEngine) InvalidateCache() {
	e.mu.Lock()
	e.compiled = make(map[string]*regexp.Regexp)
	e.mu.Unlock()
}
