package profile

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// ── ProjectProfile ──────────────────────────────────────────────────────

// ProjectProfile describes WHAT work a session does — the repo to
// clone, which agent + sidecar images to compose, memory policy,
// spawn budgets, etc. It's stable, reusable, and orthogonal to the
// Cluster Profile (WHERE it runs).
//
// JSON tags use snake_case throughout for consistency with the rest of
// the datawatch config surface.
type ProjectProfile struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	Git GitSpec `json:"git"`

	ImagePair ImagePair `json:"image_pair"`

	// Env injects environment variables into both containers of the
	// composed Pod. Use for things like model-selection, feature flags,
	// or per-project API key overrides. NOT for secrets — those go
	// through the Cluster Profile's creds_ref.
	Env map[string]string `json:"env,omitempty"`

	Memory MemorySpec `json:"memory"`

	// IdleTimeout — if the session produces no activity for this long
	// the parent gracefully reaps it. Zero = no timeout.
	IdleTimeout time.Duration `json:"idle_timeout"`

	// Spawn budgets. When a worker tries to spawn children, the parent
	// enforces these caps so runaway recursion is impossible.
	AllowSpawnChildren    bool `json:"allow_spawn_children"`
	SpawnBudgetTotal      int  `json:"spawn_budget_total,omitempty"`
	SpawnBudgetPerMinute  int  `json:"spawn_budget_per_minute,omitempty"`

	// PostTaskHooks are shell commands executed after the AI finishes
	// its main task but before the session closes. Common uses: run
	// formatter, launch tests, post to slack. Executed inside the
	// sidecar container (since it holds the toolchain).
	PostTaskHooks []string `json:"post_task_hooks,omitempty"`

	// CommInheritance lists comm-channel backend names the spawned
	// worker should route its outbound alerts through (F10 S7.7).
	// Empty = worker uses no outbound comms (the parent shows the
	// worker's status via the proxy + WS broadcast, which is enough
	// for most profiles). Names match the parent's configured
	// messaging backends ("signal", "telegram", "slack", etc.).
	// Operator-tunable per-profile via every channel.
	CommInheritance []string `json:"comm_inheritance,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GitSpec is the project's git config. Gitlab will gain first-class
// support when the Sprint 5 git provider adapter lands; for now its
// Provider field is accepted so profiles can be authored ahead of time.
type GitSpec struct {
	Provider string `json:"provider"` // "github" | "gitlab" | "local" | ""
	URL      string `json:"url"`
	Branch   string `json:"branch,omitempty"` // defaults to the repo's default branch
	AutoPR   bool   `json:"auto_pr,omitempty"`
}

// ImagePair picks the two containers that get composed into the Pod.
// Agent is required; Sidecar is optional (solo-agent case).
type ImagePair struct {
	Agent   string `json:"agent"`             // agent-claude, agent-opencode, agent-gemini, agent-aider
	Sidecar string `json:"sidecar,omitempty"` // lang-go, lang-kotlin, lang-ruby, tools-ops, or ""
}

// MemorySpec controls the memory federation behaviour for a project.
type MemorySpec struct {
	Mode       MemoryMode `json:"mode"`                  // "" = sync-back default
	Namespace  string     `json:"namespace,omitempty"`   // derives from Name when empty
	SharedWith []string   `json:"shared_with,omitempty"` // cross-profile sharing opt-in
}

// EffectiveNamespace returns the memory namespace, deriving from the
// profile name when the field is empty.
func (p *ProjectProfile) EffectiveNamespace() string {
	if p.Memory.Namespace != "" {
		return p.Memory.Namespace
	}
	return "project-" + p.Name
}

// ── Validation ──────────────────────────────────────────────────────────

// Validate runs the full validation pass. Returns a joined error
// listing every problem so UIs can show the whole list rather than
// forcing a save-retry cycle.
func (p *ProjectProfile) Validate() error {
	var errs []string

	if err := ValidateName(p.Name); err != nil {
		errs = append(errs, err.Error())
	}

	if p.Git.URL == "" {
		errs = append(errs, "git.url is required")
	} else if _, err := url.Parse(p.Git.URL); err != nil {
		errs = append(errs, fmt.Sprintf("git.url %q: %v", p.Git.URL, err))
	}

	switch p.Git.Provider {
	case "", "github", "gitlab", "local":
		// ok
	default:
		errs = append(errs, fmt.Sprintf("git.provider %q: must be github|gitlab|local|empty", p.Git.Provider))
	}

	if p.ImagePair.Agent == "" {
		errs = append(errs, "image_pair.agent is required")
	} else if !IsKnownAgent(p.ImagePair.Agent) {
		errs = append(errs, fmt.Sprintf("image_pair.agent %q: not a known agent (expected one of %v)",
			p.ImagePair.Agent, knownAgents))
	}
	if !IsKnownSidecar(p.ImagePair.Sidecar) {
		errs = append(errs, fmt.Sprintf("image_pair.sidecar %q: not a known sidecar (expected one of %v or \"\")",
			p.ImagePair.Sidecar, knownSidecars))
	}

	if !p.Memory.Mode.Valid() {
		errs = append(errs, fmt.Sprintf("memory.mode %q: must be shared|sync-back|ephemeral|empty", p.Memory.Mode))
	}

	if p.IdleTimeout < 0 {
		errs = append(errs, "idle_timeout must be non-negative")
	}
	if p.SpawnBudgetTotal < 0 {
		errs = append(errs, "spawn_budget_total must be non-negative")
	}
	if p.SpawnBudgetPerMinute < 0 {
		errs = append(errs, "spawn_budget_per_minute must be non-negative")
	}

	// Recursion hygiene: if spawning children is disabled the budgets
	// are meaningless; flag as a consistency check.
	if !p.AllowSpawnChildren && (p.SpawnBudgetTotal > 0 || p.SpawnBudgetPerMinute > 0) {
		errs = append(errs, "spawn budgets set but allow_spawn_children is false — one or the other")
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid project profile: %s", strings.Join(errs, "; "))
	}
	return nil
}

// clone returns an independent copy so the store's internal state
// can't be mutated through a returned *ProjectProfile.
func (p *ProjectProfile) clone() *ProjectProfile {
	if p == nil {
		return nil
	}
	out := *p
	out.Env = deepCopyMap(p.Env)
	out.Memory.SharedWith = deepCopyStrings(p.Memory.SharedWith)
	out.PostTaskHooks = deepCopyStrings(p.PostTaskHooks)
	return &out
}

// ── ProjectStore ────────────────────────────────────────────────────────

// ProjectStore persists a set of Project Profiles to a single JSON
// file, transparently encrypted when an encryption key is supplied.
// Thread-safe.
type ProjectStore struct {
	mu       sync.Mutex
	path     string
	encKey   []byte
	profiles []*ProjectProfile
}

// NewProjectStore opens (or creates) a plaintext Project Profile store.
func NewProjectStore(path string) (*ProjectStore, error) {
	return newProjectStoreWithKey(path, nil)
}

// NewProjectStoreEncrypted opens (or creates) an AES-256-GCM encrypted
// Project Profile store. Pass the session encryption key derived by
// the --secure daemon startup.
func NewProjectStoreEncrypted(path string, key []byte) (*ProjectStore, error) {
	return newProjectStoreWithKey(path, key)
}

func newProjectStoreWithKey(path string, key []byte) (*ProjectStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create profile dir: %w", err)
	}
	s := &ProjectStore{path: path, encKey: key}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load project profiles: %w", err)
	}
	return s, nil
}

func (s *ProjectStore) load() error {
	data, err := secfile.ReadFile(s.path, s.encKey)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &s.profiles)
}

func (s *ProjectStore) save() error {
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project profiles: %w", err)
	}
	return secfile.WriteFile(s.path, data, 0600, s.encKey)
}

// List returns a defensive copy of every stored profile.
func (s *ProjectStore) List() []*ProjectProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ProjectProfile, len(s.profiles))
	for i, p := range s.profiles {
		out[i] = p.clone()
	}
	return out
}

// Get returns a clone of the named profile, or error if absent.
func (s *ProjectStore) Get(name string) (*ProjectProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.profiles {
		if p.Name == name {
			return p.clone(), nil
		}
	}
	return nil, fmt.Errorf("project profile %q not found", name)
}

// Create inserts a new profile. Validates first. Rejects duplicate names.
func (s *ProjectStore) Create(p *ProjectProfile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.profiles {
		if existing.Name == p.Name {
			return fmt.Errorf("project profile %q already exists", p.Name)
		}
	}
	now := time.Now().UTC()
	clone := p.clone()
	clone.CreatedAt = now
	clone.UpdatedAt = now
	s.profiles = append(s.profiles, clone)
	return s.save()
}

// Update replaces the profile identified by p.Name. Preserves CreatedAt.
func (s *ProjectStore) Update(p *ProjectProfile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.profiles {
		if existing.Name == p.Name {
			clone := p.clone()
			clone.CreatedAt = existing.CreatedAt // preserve
			clone.UpdatedAt = time.Now().UTC()
			s.profiles[i] = clone
			return s.save()
		}
	}
	return fmt.Errorf("project profile %q not found", p.Name)
}

// EffectiveNamespacesFor returns the union of namespaces a worker
// of profile `name` is allowed to read from. F10 S6.5 — gates
// cross-profile sharing on **mutual opt-in**: profile A only sees
// profile B's namespace when A.SharedWith contains B AND
// B.SharedWith contains A. Single-sided declarations are ignored
// (defence against operator misconfiguration leaking data the other
// project never agreed to expose).
//
// Always includes the requested profile's own namespace as the
// first element. Returns the single-namespace list when the profile
// has no sharing declarations or no peer reciprocates. Returns nil
// when the named profile doesn't exist (caller falls back to
// memory.DefaultNamespace).
func (s *ProjectStore) EffectiveNamespacesFor(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var self *ProjectProfile
	for _, p := range s.profiles {
		if p.Name == name {
			self = p
			break
		}
	}
	if self == nil {
		return nil
	}
	out := []string{self.EffectiveNamespace()}
	if len(self.Memory.SharedWith) == 0 {
		return out
	}

	for _, ref := range self.Memory.SharedWith {
		var peer *ProjectProfile
		for _, p := range s.profiles {
			if p.Name == ref {
				peer = p
				break
			}
		}
		if peer == nil {
			continue // missing peer — silently skip
		}
		// Mutual-opt-in check.
		mutual := false
		for _, back := range peer.Memory.SharedWith {
			if back == name {
				mutual = true
				break
			}
		}
		if mutual {
			out = append(out, peer.EffectiveNamespace())
		}
	}
	return out
}

// Delete removes the named profile.
func (s *ProjectStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.profiles {
		if existing.Name == name {
			s.profiles = append(s.profiles[:i], s.profiles[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("project profile %q not found", name)
}

// Smoke runs validation checks + stub reachability probes. Cheap and
// offline-friendly; full-depth checks (actually cloning the repo,
// talking to git provider) land in Sprint 5.
func (s *ProjectStore) Smoke(name string) (*SmokeResult, error) {
	p, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	r := &SmokeResult{Name: name, RanAt: time.Now().UTC()}

	r.addCheck("profile loaded", nil)

	if vErr := p.Validate(); vErr != nil {
		r.addCheck("validation", vErr)
		return r, nil // return result with errors, not Go-level error
	}
	r.addCheck("validation", nil)

	// Warn when sidecar choice looks obviously mismatched for known
	// agent→sidecar combinations. Not a hard error — operators may
	// know what they're doing.
	if p.ImagePair.Sidecar == "" {
		r.addWarning("no sidecar set — solo agent; toolchain access will be limited to agent-base")
	}

	// Shared_with cross-profile list currently can't be verified
	// without loading the referenced profiles; leave as TODO for
	// when the router resolves cross-profile memory access.
	for _, ref := range p.Memory.SharedWith {
		if err := ValidateName(ref); err != nil {
			r.addCheck(fmt.Sprintf("memory.shared_with[%s]", ref), err)
		}
	}

	r.addCheck("smoke complete", nil)
	return r, nil
}
