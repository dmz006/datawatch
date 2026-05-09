// Package inference — v7.0.0 S2 LLM-inference registry + dispatcher.
//
// An LLM is a named definition `(name, kind, model, [compute_node1,
// compute_node2, ...])`. Consumers (Council, /api/ask, BL297 wizard,
// agent spawn, session spawning) call Dispatcher.Call(llm_name, ctx,
// req) and the dispatcher walks the ordered ComputeNode list, calls
// the kind-specific adapter against the first reachable Node, and
// returns. Failover lands per BL295 Q11 (per-LLM ordered failover —
// no round-robin; operator-controlled placement).
//
// Adapters live in internal/inference/adapters/{ollama,openwebui,
// opencode,claude}.go. Each implements the Adapter interface.
//
// Operator-decided 2026-05-08 (BL295 design Q21): every currently-
// supported LLM kind must be in v7.0.0 — won't ship without. Today's
// kinds: ollama, openwebui, opencode, claude (claude-code Anthropic
// API).
//
// Naming distinction from existing internal/llm: that package is for
// "AI coding assistants" (Claude Code, aider) that own a tmux session
// end-to-end. This package is for one-shot inference calls.

package inference

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Kind enumerates supported LLM protocols.
//
// v7.0.0-alpha.14 (#228, operator-flagged 2026-05-09): expanded to
// cover every v6 backend identifier so the unified Compute model can
// migrate cfg.<Backend> blocks (alpha.15) into LLM registry entries
// without losing names. Some kinds (claude-code, opencode-acp, aider,
// goose, gemini, opencode-prompt, shell) are session-backend kinds —
// they describe a coding agent that runs in a tmux session, not a
// pure inference adapter. Their LLM registry entries hold the binary
// path, model selection, and console preferences; sessions resolve
// against them at start-time.
type Kind string

const (
	// Inference kinds — one-shot inference (Council, /api/ask, agent-spawn).
	KindOllama    Kind = "ollama"
	KindOpenWebUI Kind = "openwebui"
	KindOpenCode  Kind = "opencode" // ollama-protocol; opencode wraps ollama
	KindClaude    Kind = "claude"   // Anthropic API

	// Session-backend kinds — coding agents that run in tmux. Migrated
	// from cfg.<Backend> blocks in alpha.15. Adapter wiring for
	// inference is a no-op for these; they're resolvable LLM names
	// that sessions can target.
	KindClaudeCode     Kind = "claude-code"
	KindOpenCodeACP    Kind = "opencode-acp"
	KindOpenCodePrompt Kind = "opencode-prompt"
	KindAider          Kind = "aider"
	KindGoose          Kind = "goose"
	KindGemini         Kind = "gemini"
	KindShell          Kind = "shell"
)

// AllKinds is the set the operator can pick from + UI dropdowns.
//
// Order matters: inference-first (Council/agent-spawn use these),
// session-backends second (sessions use these). PWA renders this list
// as-is in the LLM Kind dropdown.
var AllKinds = []Kind{
	KindOllama, KindOpenWebUI, KindOpenCode, KindClaude,
	KindClaudeCode, KindOpenCodeACP, KindOpenCodePrompt,
	KindAider, KindGoose, KindGemini, KindShell,
}

// LLM is one named LLM definition in the registry.
type LLM struct {
	Name    string `yaml:"name" json:"name"`
	Kind    Kind   `yaml:"kind" json:"kind"`
	Model   string `yaml:"model,omitempty" json:"model,omitempty"`
	// ComputeNodes is the operator-ordered failover list of
	// ComputeNode names. Dispatcher walks left-to-right; first
	// reachable Node wins. Empty list means "default to whatever the
	// kind's adapter knows" (used by the v6.x cfg shim — see
	// MigrateLegacyConfig in main.go).
	ComputeNodes []string `yaml:"compute_nodes,omitempty" json:"compute_nodes,omitempty"`
	// APIKeyRef is for SaaS kinds (claude). May be a literal value
	// OR a `${secret:name}` reference resolved via the secrets store
	// at call time.
	APIKeyRef string `yaml:"api_key_ref,omitempty" json:"api_key_ref,omitempty"`
	// TimeoutSeconds overrides the adapter default (300s for ollama
	// per v5.26.9 cold-model latency observation; 60s for claude).
	TimeoutSeconds int      `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	Tags           []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// CostPer1kTokens (USD) — for SaaS cost tracking. Ignored for
	// local kinds.
	CostPer1kTokensInput  float64 `yaml:"cost_per_1k_input,omitempty" json:"cost_per_1k_input,omitempty"`
	CostPer1kTokensOutput float64 `yaml:"cost_per_1k_output,omitempty" json:"cost_per_1k_output,omitempty"`

	CreatedAt   time.Time `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt   time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
	AutoCreated bool      `yaml:"auto_created,omitempty" json:"auto_created,omitempty"`
}

// Validate returns the first reason this LLM is malformed, or nil.
func (l *LLM) Validate() error {
	if strings.TrimSpace(l.Name) == "" {
		return errors.New("llm: name required")
	}
	if !validName(l.Name) {
		return fmt.Errorf("llm: invalid name %q (use [a-z0-9._-]+)", l.Name)
	}
	if l.Kind == "" {
		return errors.New("llm: kind required")
	}
	if !validKind(l.Kind) {
		return fmt.Errorf("llm: unknown kind %q (allowed: %v)", l.Kind, AllKinds)
	}
	if l.TimeoutSeconds < 0 {
		return fmt.Errorf("llm: timeout_seconds must be >= 0")
	}
	return nil
}

// Registry is the in-memory + on-disk store for LLM definitions.
type Registry struct {
	mu   sync.RWMutex
	llms map[string]*LLM
	// persistFn is called after every mutation; nil = no persistence
	// (test stores). Real registries set it to a JSON-write closure.
	persistFn func() error
}

// NewRegistry returns an empty in-memory registry. Wire persistence
// via SetPersistFn (see NewRegistryFromFile for the JSON-backed form).
func NewRegistry() *Registry {
	return &Registry{llms: map[string]*LLM{}}
}

// SetPersistFn wires the after-mutate persistence callback.
func (r *Registry) SetPersistFn(fn func() error) { r.persistFn = fn }

// List returns every LLM, sorted by name.
func (r *Registry) List() []*LLM {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*LLM, 0, len(r.llms))
	for _, l := range r.llms {
		cp := *l
		out = append(out, &cp)
	}
	sortLLMs(out)
	return out
}

// Get returns a copy of the named LLM, or ErrNotFound.
func (r *Registry) Get(name string) (*LLM, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.llms[name]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *l
	return &cp, nil
}

// Add registers a new LLM. Returns ErrConflict if name exists.
func (r *Registry) Add(l *LLM) error {
	if err := l.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.llms[l.Name]; exists {
		return ErrConflict
	}
	now := time.Now().UTC()
	if l.CreatedAt.IsZero() {
		l.CreatedAt = now
	}
	l.UpdatedAt = now
	cp := *l
	r.llms[l.Name] = &cp
	return r.persistLocked()
}

// Update replaces the named LLM. Returns ErrNotFound if absent.
func (r *Registry) Update(l *LLM) error {
	if err := l.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.llms[l.Name]
	if !ok {
		return ErrNotFound
	}
	if old.CreatedAt.IsZero() {
		old.CreatedAt = time.Now().UTC()
	}
	l.CreatedAt = old.CreatedAt
	l.UpdatedAt = time.Now().UTC()
	cp := *l
	r.llms[l.Name] = &cp
	return r.persistLocked()
}

// Delete removes the named LLM. Returns ErrNotFound if absent.
func (r *Registry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.llms[name]; !ok {
		return ErrNotFound
	}
	delete(r.llms, name)
	return r.persistLocked()
}

// Snapshot returns the raw map for serialization (caller must not
// mutate). Used by the JSON-backed persist closure. Caller-locking
// variant: snapshotLocked is called from inside Add/Update/Delete
// which already hold the write lock; Snapshot is the public lock-
// acquiring version.
func (r *Registry) Snapshot() []*LLM {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshotLocked()
}

func (r *Registry) snapshotLocked() []*LLM {
	out := make([]*LLM, 0, len(r.llms))
	for _, l := range r.llms {
		cp := *l
		out = append(out, &cp)
	}
	sortLLMs(out)
	return out
}

// LoadSnapshot replaces the registry contents wholesale. Used at
// startup by the JSON loader.
func (r *Registry) LoadSnapshot(snap []LLM) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.llms = make(map[string]*LLM, len(snap))
	for i := range snap {
		l := snap[i]
		r.llms[l.Name] = &l
	}
}

func (r *Registry) persistLocked() error {
	if r.persistFn == nil {
		return nil
	}
	return r.persistFn()
}

// ErrNotFound is returned by Get / Update / Delete when name unknown.
var ErrNotFound = errors.New("llm not found")

// ErrConflict is returned by Add when name already exists.
var ErrConflict = errors.New("llm already exists")

// ErrNoBackend is returned by Dispatcher.Call when no ComputeNode in
// the LLM's failover list is reachable / allows the consumer.
var ErrNoBackend = errors.New("no reachable ComputeNode for this LLM")

func validName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func validKind(k Kind) bool {
	for _, x := range AllKinds {
		if x == k {
			return true
		}
	}
	return false
}

func sortLLMs(out []*LLM) {
	// Inline insertion sort — stable, tiny n.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
}
