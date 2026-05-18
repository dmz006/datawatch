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

	// v8.0 BL321 — new inference adapter kinds.
	KindGeminiAPI    Kind = "gemini-api"    // Google Generative Language v1beta
	KindOpenCodeAPI  Kind = "opencode-api"  // opencode HTTP inference API (OpenAI-compat)
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
	KindGeminiAPI, KindOpenCodeAPI,
}

// EnabledModel pairs one ComputeNode with one model name.
// Node is empty for SaaS kinds (claude, gemini).
type EnabledModel struct {
	Node  string `yaml:"node,omitempty" json:"node,omitempty"`
	Model string `yaml:"model" json:"model"`
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
	// Models is the per-node model list (alpha.37). Each element pairs a
	// ComputeNode name with a model name available on that node. For SaaS
	// kinds (claude, gemini) Node is empty. Replaces the single Model field.
	// Back-compat: on LoadSnapshot, if Models is nil and Model is non-empty,
	// it is expanded into Models using each ComputeNode.
	Models []EnabledModel `yaml:"models,omitempty" json:"models,omitempty"`
	// AutoAddModels — when true the model-refresh loop appends newly-discovered
	// models from the LLM's ComputeNodes automatically.
	AutoAddModels bool `yaml:"auto_add_models,omitempty" json:"auto_add_models,omitempty"`
	// APIKeyRef is for SaaS kinds (claude). May be a literal value
	// OR a `${secret:name}` reference resolved via the secrets store
	// at call time.
	APIKeyRef string `yaml:"api_key_ref,omitempty" json:"api_key_ref,omitempty"`
	// TimeoutSeconds overrides the adapter default (300s for ollama
	// per v5.26.9 cold-model latency observation; 60s for claude).
	TimeoutSeconds int      `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	Tags           []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	// AutoTags are daemon-applied internal markers. v7.0.0-alpha.23
	// (Q7): PWA strips these from the user-visible tag list.
	AutoTags       []string `yaml:"auto_tags,omitempty" json:"auto_tags,omitempty"`
	// CostPer1kTokens (USD) — for SaaS cost tracking. Ignored for
	// local kinds.
	CostPer1kTokensInput  float64 `yaml:"cost_per_1k_input,omitempty" json:"cost_per_1k_input,omitempty"`
	CostPer1kTokensOutput float64 `yaml:"cost_per_1k_output,omitempty" json:"cost_per_1k_output,omitempty"`

	CreatedAt   time.Time `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt   time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
	AutoCreated bool      `yaml:"auto_created,omitempty" json:"auto_created,omitempty"`

	// Disabled is the inverse of an "enabled" toggle — when true, the
	// dispatcher refuses to route to this LLM. Operator-spec'd #247
	// 2026-05-09: PWA replaces the 🧪 Test row-button with a ⚪ on/off
	// toggle that pre-tests before flipping to enabled.
	//
	// Inverse semantics: zero value = enabled (omitempty stays clean
	// for back-compat; existing JSON/YAML without the field reads as
	// enabled, preserving v7-alpha behavior).
	Disabled bool `yaml:"disabled,omitempty" json:"disabled,omitempty"`

	// Tombstoned marks an auto-created entry that the operator explicitly
	// deleted. Tombstoned entries are invisible to List/Get (treated as
	// ErrNotFound for all user-facing operations) but remain in the JSON
	// file so MigrateAllLegacyBackends skips re-creating them on restart.
	// Without this, deleting an auto-migrated LLM (aider, goose, gemini)
	// causes migration to recreate it from v6 cfg on every daemon start.
	Tombstoned bool `yaml:"tombstoned,omitempty" json:"tombstoned,omitempty"`

	// Session-backend fields. Only applicable when Kind is a session-backend
	// (claude-code, aider, goose, gemini, opencode, opencode-acp, opencode-prompt, shell).
	Binary      string `yaml:"binary,omitempty" json:"binary,omitempty"`
	ConsoleCols int    `yaml:"console_cols,omitempty" json:"console_cols,omitempty"`
	ConsoleRows int    `yaml:"console_rows,omitempty" json:"console_rows,omitempty"`
	OutputMode  string `yaml:"output_mode,omitempty" json:"output_mode,omitempty"`
	InputMode   string `yaml:"input_mode,omitempty" json:"input_mode,omitempty"`
	AutoGitInit   bool `yaml:"auto_git_init,omitempty" json:"auto_git_init,omitempty"`
	AutoGitCommit bool `yaml:"auto_git_commit,omitempty" json:"auto_git_commit,omitempty"`

	// Claude-code-specific settings. Clean move from SessionConfig.
	// Only meaningful when Kind == KindClaudeCode.
	SkipPermissions      bool     `yaml:"skip_permissions,omitempty" json:"skip_permissions,omitempty"`
	ChannelEnabled       bool     `yaml:"channel_enabled,omitempty" json:"channel_enabled,omitempty"`
	AutoAcceptDisclaimer bool     `yaml:"auto_accept_disclaimer,omitempty" json:"auto_accept_disclaimer,omitempty"`
	PermissionMode       string   `yaml:"permission_mode,omitempty" json:"permission_mode,omitempty"`
	DefaultEffort        string   `yaml:"default_effort,omitempty" json:"default_effort,omitempty"`
	FallbackChain        []string `yaml:"fallback_chain,omitempty" json:"fallback_chain,omitempty"`
}

// Enabled is the operator-facing accessor — handy for templates and
// UI code that wants the positive form. Inverse of Disabled.
func (l *LLM) Enabled() bool { return !l.Disabled }

// ApplyModelMigration expands the legacy single Model field into
// Models[] when Models is empty and Model is set. Called by REST
// POST/PUT handlers so file-load and REST create/update behave the same.
func (l *LLM) ApplyModelMigration() {
	if len(l.Models) > 0 || l.Model == "" {
		return
	}
	if len(l.ComputeNodes) > 0 {
		for _, cn := range l.ComputeNodes {
			l.Models = append(l.Models, EnabledModel{Node: cn, Model: l.Model})
		}
	} else {
		l.Models = []EnabledModel{{Model: l.Model}}
	}
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

// historicalAutoTags lists tag values the daemon has historically
// auto-applied to LLM entries. v7.0.0-alpha.23 Q7 — these move from
// Tags to AutoTags so the PWA stops showing them.
var historicalAutoTags = map[string]bool{
	"v7-cfg-migration": true,
}

// MigrateAutoTags is a one-time daemon-startup pass that moves
// historically auto-applied tags from Tags to AutoTags. Returns the
// count of LLMs touched. Idempotent.
func (r *Registry) MigrateAutoTags() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	touched := 0
	for _, l := range r.llms {
		var keep []string
		moved := false
		for _, t := range l.Tags {
			if historicalAutoTags[t] {
				if !containsTag(l.AutoTags, t) {
					l.AutoTags = append(l.AutoTags, t)
				}
				moved = true
				continue
			}
			keep = append(keep, t)
		}
		if moved {
			l.Tags = keep
			l.UpdatedAt = time.Now().UTC()
			touched++
		}
	}
	if touched > 0 && r.persistFn != nil {
		_ = r.persistFn()
	}
	return touched
}

func containsTag(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// List returns every non-tombstoned LLM, sorted by name.
func (r *Registry) List() []*LLM {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*LLM, 0, len(r.llms))
	for _, l := range r.llms {
		if l.Tombstoned {
			continue
		}
		cp := *l
		out = append(out, &cp)
	}
	sortLLMs(out)
	return out
}

// ResolveKind maps a named LLM to its adapter kind string.
// Returns ("", false) when the name is unknown or the entry is disabled.
// Satisfies the pipeline.KindResolver interface so the pipeline executor
// can route named LLMs to the correct adapter without importing this package.
func (r *Registry) ResolveKind(name string) (string, bool) {
	l, err := r.Get(name)
	if err != nil || l.Disabled {
		return "", false
	}
	return string(l.Kind), true
}

// Get returns a copy of the named LLM, or ErrNotFound.
// Tombstoned entries (operator-deleted auto-created entries) are treated
// as absent for all user-facing operations.
func (r *Registry) Get(name string) (*LLM, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.llms[name]
	if !ok || l.Tombstoned {
		return nil, ErrNotFound
	}
	cp := *l
	return &cp, nil
}

// Exists reports whether name is tracked in the registry, including
// tombstoned (operator-deleted) entries. Used by migration to avoid
// re-creating entries the operator explicitly removed.
func (r *Registry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.llms[name]
	return ok
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
// Auto-created entries are tombstoned (soft-deleted) instead of
// hard-deleted so that MigrateAllLegacyBackends skips re-creating
// them on the next daemon restart.
func (r *Registry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.llms[name]
	if !ok || l.Tombstoned {
		return ErrNotFound
	}
	if l.AutoCreated {
		l.Tombstoned = true
		l.Disabled = true
		l.UpdatedAt = time.Now().UTC()
		return r.persistLocked()
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
		// Back-compat alpha.37: expand legacy single-model field into Models[].
		if l.Models == nil && l.Model != "" {
			if len(l.ComputeNodes) > 0 {
				for _, cn := range l.ComputeNodes {
					l.Models = append(l.Models, EnabledModel{Node: cn, Model: l.Model})
				}
			} else {
				l.Models = []EnabledModel{{Model: l.Model}}
			}
		}
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
