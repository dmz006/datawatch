// Package agents owns the lifecycle of ephemeral container-spawned
// workers (F10 sprint 3+).
//
// Architecture:
//
//   Driver     — plug-in per container platform (docker | k8s | cf).
//                Knows how to `Spawn` a worker container from a
//                (ProjectProfile, ClusterProfile) pair, `Status` it,
//                stream logs, and `Terminate` it.
//
//   Manager    — tracks every live agent keyed by worker ID (UUID).
//                Mediates between the REST API, the Driver, and the
//                bootstrap endpoint that the worker calls back to.
//
//   Worker ID  — UUIDv4 per spawn. Used as container/pod name, in
//                bootstrap token claims, for proxy URL routing, and
//                session binding.
//
// Sprint 3 ships Manager + Docker driver. K8s driver lands in sprint 4
// (S4.1); Cloud Foundry stays stubbed until sprint 8.

package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

// State enumerates the lifecycle of a spawned worker.
//
//   pending  — driver.Spawn has been called, container not yet created
//   starting — container created; worker hasn't called bootstrap yet
//   ready    — worker completed bootstrap + /readyz=200
//   running  — ready AND the parent has delegated at least one session
//   failed   — driver or bootstrap reported an unrecoverable error
//   stopped  — driver.Terminate returned; safe to reap from memory
type State string

const (
	StatePending  State = "pending"
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateRunning  State = "running"
	StateFailed   State = "failed"
	StateStopped  State = "stopped"
)

// Agent is the runtime representation of one spawned worker.
// Fields the Manager mutates are guarded by its own mutex; callers
// should treat values returned via Get/List as snapshots.
type Agent struct {
	ID              string          `json:"id"`             // UUIDv4
	ProjectProfile  string          `json:"project_profile"` // name
	ClusterProfile  string          `json:"cluster_profile"` // name
	Task            string          `json:"task"`
	State           State           `json:"state"`
	CreatedAt       time.Time       `json:"created_at"`
	ReadyAt         time.Time       `json:"ready_at,omitempty"`
	StoppedAt       time.Time       `json:"stopped_at,omitempty"`
	FailureReason   string          `json:"failure_reason,omitempty"`

	// DriverInstance is a driver-specific handle (docker container
	// ID, k8s pod name, etc). Opaque to the Manager.
	DriverInstance  string          `json:"driver_instance,omitempty"`

	// ContainerAddr is how the parent reaches the worker's HTTP API
	// for the reverse-proxy path. Usually "container-ip:8080" for
	// docker bridge, "pod-name.ns.svc:8080" for k8s.
	ContainerAddr   string          `json:"container_addr,omitempty"`

	// BootstrapToken is a single-use, never-logged secret minted at
	// spawn. The worker presents it to POST /api/agents/bootstrap to
	// retrieve its config. Burned on first call.
	BootstrapToken  string          `json:"-"`

	// SessionIDs tracks which sessions the parent has bound to this
	// agent. Session lifecycle manages its own state; this list is
	// informational for the UI and for reaping decisions.
	SessionIDs      []string        `json:"session_ids,omitempty"`

	// GitToken is a parent-minted, short-lived token (S5.1) the
	// worker uses to clone its Project Profile's repo and push back.
	// json:"-" — never leaked via the /api/agents JSON snapshot;
	// only delivered to the worker via the bootstrap response.
	GitToken string `json:"-"`

	project *profile.ProjectProfile
	cluster *profile.ClusterProfile
}

// SpawnRequest is the canonical input to Manager.Spawn.
// Always constructed from Project + Cluster Profile names the daemon
// already knows about; callers resolve names to structs ahead of time.
type SpawnRequest struct {
	ProjectProfile string `json:"project_profile"`
	ClusterProfile string `json:"cluster_profile"`
	Task           string `json:"task,omitempty"`
}

// Driver plugs container-platform-specific behaviour into the Manager.
// Implementations:
//
//   DockerDriver — local docker daemon (sprint 3)
//   K8sDriver    — in-cluster (sprint 4)
//   CFDriver     — Cloud Foundry stub (sprint 8+)
//
// Contract:
//
//   Spawn    — blocking: returns when the container exists in the
//              platform's state (NOT when it's reachable). Must set
//              DriverInstance on the passed *Agent.
//   Status   — cheap poll; returns the current State or "" if the
//              driver can't tell (e.g. container gone).
//   Logs     — streams up to `lines` tail lines.
//   Terminate — synchronous; must leave no zombie container behind.
type Driver interface {
	Kind() string
	Spawn(ctx context.Context, a *Agent) error
	Status(ctx context.Context, a *Agent) (State, error)
	Logs(ctx context.Context, a *Agent, lines int) (string, error)
	Terminate(ctx context.Context, a *Agent) error
}

// ── Manager ────────────────────────────────────────────────────────────

// Manager tracks live agents + dispatches to the right Driver.
//
// No persistence yet — an agent is considered lost if the parent
// daemon restarts. Sprint 7 will add reconciliation (query each driver
// for our labelled containers at startup and re-build state).
type Manager struct {
	mu       sync.Mutex
	agents   map[string]*Agent // keyed by ID
	drivers  map[string]Driver // keyed by Kind()
	projects *profile.ProjectStore
	clusters *profile.ClusterStore

	// CallbackURL is injected into the worker's env at spawn so it
	// knows where to call back for bootstrap. Usually the parent's
	// public URL; override per-cluster via ClusterProfile.ParentCallbackURL.
	CallbackURL string

	// TokenTTL is how long a bootstrap token stays valid before the
	// Manager's sweeper zeroes it out. Default 5 min.
	TokenTTL time.Duration

	// GitTokenMinter, when non-nil, mints + revokes git tokens
	// for spawned workers (F10 S5.1+S5.3). Manager calls
	// MintForWorker on Spawn (token rides along in the bootstrap
	// response) and RevokeForWorker on Terminate. Optional: when
	// nil, workers boot without git creds (legacy / read-only
	// sessions).
	GitTokenMinter GitTokenMinter
}

// GitTokenMinter is the narrow surface agents.Manager needs from
// auth.TokenBroker. Defined here to avoid an agents → auth import
// cycle (auth already depends on git, and we don't want a third
// edge).
type GitTokenMinter interface {
	MintForWorker(ctx context.Context, workerID, repo string, ttl time.Duration) (token string, err error)
	RevokeForWorker(ctx context.Context, workerID string) error
}

// NewManager builds a Manager bound to the supplied profile stores.
// Callers register Drivers via RegisterDriver before calling Spawn.
func NewManager(projects *profile.ProjectStore, clusters *profile.ClusterStore) *Manager {
	return &Manager{
		agents:   map[string]*Agent{},
		drivers:  map[string]Driver{},
		projects: projects,
		clusters: clusters,
		TokenTTL: 5 * time.Minute,
	}
}

// RegisterDriver wires a Driver under its Kind() name. Typically
// called once per process at startup from main.go.
func (m *Manager) RegisterDriver(d Driver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drivers[d.Kind()] = d
}

// driver returns the Driver matching the cluster profile's Kind,
// or an error when no driver is registered for it.
func (m *Manager) driver(c *profile.ClusterProfile) (Driver, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drivers[string(c.Kind)]
	if !ok {
		return nil, fmt.Errorf("no driver registered for cluster kind %q (registered: %v)",
			c.Kind, m.registeredKinds())
	}
	return d, nil
}

// registeredKinds lists available Driver.Kind() strings. Caller holds the mutex.
func (m *Manager) registeredKinds() []string {
	out := make([]string, 0, len(m.drivers))
	for k := range m.drivers {
		out = append(out, k)
	}
	return out
}

// Spawn resolves the profile pair, mints an agent + bootstrap token,
// and asks the appropriate Driver to bring the container up.
// Returns the Agent (with its ID and mint token) even when Driver.Spawn
// fails, so the caller can show the failure in the UI.
func (m *Manager) Spawn(ctx context.Context, req SpawnRequest) (*Agent, error) {
	if m.projects == nil || m.clusters == nil {
		return nil, fmt.Errorf("agent manager not wired with profile stores")
	}
	proj, err := m.projects.Get(req.ProjectProfile)
	if err != nil {
		return nil, fmt.Errorf("project profile %q: %w", req.ProjectProfile, err)
	}
	cluster, err := m.clusters.Get(req.ClusterProfile)
	if err != nil {
		return nil, fmt.Errorf("cluster profile %q: %w", req.ClusterProfile, err)
	}

	// Run validators again before we spend a docker pull — validate
	// protects against operators who edited the JSON directly.
	if err := proj.Validate(); err != nil {
		return nil, err
	}
	if err := cluster.Validate(); err != nil {
		return nil, err
	}

	driver, err := m.driver(cluster)
	if err != nil {
		return nil, err
	}

	a := &Agent{
		ID:             newAgentID(),
		ProjectProfile: proj.Name,
		ClusterProfile: cluster.Name,
		Task:           req.Task,
		State:          StatePending,
		CreatedAt:      time.Now().UTC(),
		BootstrapToken: newBootstrapToken(),
		project:        proj,
		cluster:        cluster,
	}

	m.mu.Lock()
	m.agents[a.ID] = a
	m.mu.Unlock()

	a.State = StateStarting

	// F10 S5.3 — mint a short-lived git token before the container
	// boots, so the bootstrap response can hand it to the worker.
	// MintForWorker failure is non-fatal (worker boots without git
	// creds — useful for read-only sessions or providers that aren't
	// implemented yet); the failure is recorded on the Agent record
	// for operator visibility.
	if m.GitTokenMinter != nil && proj.Git.URL != "" {
		repo := repoFromGitURL(proj.Git.URL)
		token, err := m.GitTokenMinter.MintForWorker(ctx, a.ID, repo, m.TokenTTL)
		if err != nil {
			a.FailureReason = "git token mint: " + err.Error()
		} else {
			a.GitToken = token
		}
	}

	if err := driver.Spawn(ctx, a); err != nil {
		m.mu.Lock()
		a.State = StateFailed
		a.FailureReason = err.Error()
		m.mu.Unlock()
		return a, fmt.Errorf("driver spawn: %w", err)
	}

	return a, nil
}

// repoFromGitURL extracts "owner/repo" from common GitHub URL forms:
//   https://github.com/owner/repo(.git)
//   git@github.com:owner/repo(.git)
// Falls back to the raw URL when nothing matches — broker callers
// then surface a "check token scope" error from gh which is
// actionable enough.
func repoFromGitURL(url string) string {
	// Strip trailing .git
	if strings.HasSuffix(url, ".git") {
		url = url[:len(url)-4]
	}
	// SSH form: git@host:owner/repo
	if i := strings.Index(url, ":"); i > 0 && strings.HasPrefix(url, "git@") {
		return url[i+1:]
	}
	// HTTPS form: https://host/owner/repo (take last 2 path segments)
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return url
}

// Get returns a snapshot of the named agent. Returns nil when unknown.
func (m *Manager) Get(id string) *Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return nil
	}
	return cloneAgent(a)
}

// List returns snapshots of every tracked agent in creation order.
func (m *Manager) List() []*Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Agent, 0, len(m.agents))
	for _, a := range m.agents {
		out = append(out, cloneAgent(a))
	}
	// Stable-ish order: creation time ascending. Callers that care
	// about other orderings sort themselves.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].CreatedAt.After(out[j].CreatedAt); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// Terminate asks the matching Driver to tear down the worker and
// marks the Agent Stopped in memory.
func (m *Manager) Terminate(ctx context.Context, id string) error {
	m.mu.Lock()
	a, ok := m.agents[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}

	driver, err := m.driver(a.cluster)
	if err != nil {
		return err
	}
	if err := driver.Terminate(ctx, a); err != nil {
		return fmt.Errorf("driver terminate: %w", err)
	}

	// F10 S5.3 — best-effort revoke of the worker's git token.
	// Failure is logged via the broker's audit and does NOT block
	// the Stopped transition (token sweeper is the safety net).
	if m.GitTokenMinter != nil {
		_ = m.GitTokenMinter.RevokeForWorker(ctx, a.ID)
	}

	m.mu.Lock()
	a.State = StateStopped
	a.StoppedAt = time.Now().UTC()
	m.mu.Unlock()
	return nil
}

// Logs forwards to the appropriate driver.
func (m *Manager) Logs(ctx context.Context, id string, lines int) (string, error) {
	m.mu.Lock()
	a, ok := m.agents[id]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("agent %q not found", id)
	}
	driver, err := m.driver(a.cluster)
	if err != nil {
		return "", err
	}
	return driver.Logs(ctx, a, lines)
}

// ConsumeBootstrap validates a bootstrap attempt: the token matches
// exactly one agent, and the agent is still in StateStarting (hasn't
// already bootstrapped). On success the token is zeroed so a second
// attempt with the same token fails.
func (m *Manager) ConsumeBootstrap(token, agentID string) (*Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", agentID)
	}
	if a.BootstrapToken == "" {
		return nil, fmt.Errorf("bootstrap token already consumed or missing")
	}
	if a.BootstrapToken != token {
		return nil, fmt.Errorf("bootstrap token mismatch")
	}
	if a.State != StateStarting {
		return nil, fmt.Errorf("agent not in starting state (got %s)", a.State)
	}
	a.BootstrapToken = "" // burn
	a.State = StateReady
	a.ReadyAt = time.Now().UTC()
	return cloneAgent(a), nil
}

// GetProjectFor returns the resolved Project Profile pointer for the
// named agent, or nil when unknown. Used by the server's bootstrap
// handler to populate BootstrapResponse.Git from the profile's Git
// spec without exposing the private profile pointer on the Agent
// struct itself.
func (m *Manager) GetProjectFor(agentID string) *profile.ProjectProfile {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok || a.project == nil {
		return nil
	}
	cp := *a.project
	return &cp
}

// GetGitTokenFor returns the parent-minted git token associated with
// the named agent (or "" when unknown / no broker / mint failed).
// Sensitive — server-only; never echoed in /api/agents snapshots.
func (m *Manager) GetGitTokenFor(agentID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok {
		return ""
	}
	return a.GitToken
}

// MarkSessionBound records that a session now lives on this agent.
// Drives the Running state transition + the UI session-agent badge.
func (m *Manager) MarkSessionBound(agentID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %q not found", agentID)
	}
	// Ignore duplicate binds (idempotent).
	for _, s := range a.SessionIDs {
		if s == sessionID {
			return nil
		}
	}
	a.SessionIDs = append(a.SessionIDs, sessionID)
	if a.State == StateReady {
		a.State = StateRunning
	}
	return nil
}

// ── token + id generation ──────────────────────────────────────────────

// newAgentID returns a 16-byte hex string. Not cryptographically
// significant beyond uniqueness — the bootstrap token is the auth
// secret.
func newAgentID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// newBootstrapToken returns a 32-byte hex token. It IS the auth secret
// for the first /api/agents/bootstrap call, so must stay unpredictable.
// PQC variant is a sprint 5 follow-up.
func newBootstrapToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// BootstrapTokenForTest returns the in-memory bootstrap token for
// the given agent. Exposed ONLY for package-external tests
// (internal/server/agent_api_test.go) that need to exercise the
// /api/agents/bootstrap endpoint end-to-end. Not used at runtime.
func (m *Manager) BootstrapTokenForTest(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return ""
	}
	return a.BootstrapToken
}

// cloneAgent returns a deep-ish copy so callers can't mutate the
// Manager's internal state. The profile pointers stay shared because
// they're already clones returned from the profile store.
func cloneAgent(a *Agent) *Agent {
	if a == nil {
		return nil
	}
	out := *a
	if a.SessionIDs != nil {
		out.SessionIDs = append([]string(nil), a.SessionIDs...)
	}
	// Never leak bootstrap token in snapshots returned outside of
	// ConsumeBootstrap; it's redacted via the json:"-" tag but zero
	// it here as a belt-and-braces measure for code paths that
	// don't go through JSON.
	out.BootstrapToken = ""
	return &out
}
