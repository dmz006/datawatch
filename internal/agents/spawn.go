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
	"github.com/dmz006/datawatch/internal/secrets"
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

	// PQCKeys is set when the spawn was opted into PQC bootstrap
	// (Manager.PQCBootstrap or AgentsConfig.PQCBootstrap = true).
	// When non-nil ConsumeBootstrap accepts a PQC envelope as the
	// "token" arg in addition to the legacy UUID. The KEM private +
	// signing private bytes are injected into the worker container
	// via DATAWATCH_PQC_* env vars; the parent retains the public
	// counterparts here. Burned on bootstrap consume (BL95).
	PQCKeys *PQCKeys `json:"-"`

	// SessionIDs tracks which sessions the parent has bound to this
	// agent. Session lifecycle manages its own state; this list is
	// informational for the UI and for reaping decisions.
	SessionIDs      []string        `json:"session_ids,omitempty"`

	// Branch is the git working-branch this worker owns (F10 S7.3
	// workspace lock). Defaults to the Project Profile's Git.Branch
	// when not overridden at spawn time.
	Branch          string          `json:"branch,omitempty"`

	// ParentAgentID is set when this agent was spawned recursively by
	// another worker (F10 S7.4). Empty for top-level operator spawns.
	ParentAgentID   string          `json:"parent_agent_id,omitempty"`

	// Result is the worker's structured output captured via
	// POST /api/agents/{id}/result (F10 S7.2 fan-in). Empty when
	// the worker hasn't reported yet. The orchestrator (S7.1)
	// reads this to compose final session context.
	Result *AgentResult `json:"result,omitempty"`

	// LastActivityAt is the most-recent time the parent observed
	// activity on this agent (F10 S8.6 idle-timeout enforcement).
	// Activity = bootstrap, session input, memory write, agent log,
	// MCP call, peer message, etc. — anything that proves the
	// worker is doing something. The Manager's idle reaper compares
	// this against the profile's IdleTimeout and terminates stuck
	// workers. Zero means "no activity recorded yet" (the reaper
	// uses CreatedAt as the floor in that case).
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`

	// GitToken is a parent-minted, short-lived token (S5.1) the
	// worker uses to clone its Project Profile's repo and push back.
	// json:"-" — never leaked via the /api/agents JSON snapshot;
	// only delivered to the worker via the bootstrap response.
	GitToken string `json:"-"`

	project *profile.ProjectProfile
	cluster *profile.ClusterProfile
}

// AgentResult is the structured output a worker posts to
// /api/agents/{id}/result on session-end or task-complete (F10
// S7.2 fan-in). Status is "ok" / "fail" / "partial". Summary is a
// short operator-readable line; Artifacts is a free-form map for
// orchestrator-merge logic (PR URLs, file diffs, memory ids, etc.).
type AgentResult struct {
	Status     string                 `json:"status"`
	Summary    string                 `json:"summary,omitempty"`
	Artifacts  map[string]interface{} `json:"artifacts,omitempty"`
	ReportedAt time.Time              `json:"reported_at"`
}

// SpawnRequest is the canonical input to Manager.Spawn.
// Always constructed from Project + Cluster Profile names the daemon
// already knows about; callers resolve names to structs ahead of time.
type SpawnRequest struct {
	ProjectProfile string `json:"project_profile"`
	ClusterProfile string `json:"cluster_profile"`
	Task           string `json:"task,omitempty"`
	// ParentAgentID identifies the agent that triggered THIS spawn,
	// when the spawn is a recursive child (F10 S7.4). Empty for
	// operator-initiated top-level spawns. The Manager enforces
	// ProjectProfile.AllowSpawnChildren + SpawnBudget* against the
	// parent's profile when this is set.
	ParentAgentID string `json:"parent_agent_id,omitempty"`
	// Branch is the git working-branch name this spawn will own. F10
	// S7.3 — workspace lock rejects a second spawn on the same
	// (project_profile, branch) tuple. Empty = profile's default branch.
	Branch string `json:"branch,omitempty"`
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

// Discovery is the optional capability a Driver implements when it
// can enumerate running instances by label. BL112 uses this for the
// service-mode reconciler so workers labelled
// `datawatch.role=agent-worker` can be re-attached to the in-memory
// registry after a parent restart. Drivers without label discovery
// return ErrDiscoveryUnsupported and the reconciler skips that
// cluster kind.
//
// cluster is the ClusterProfile to query — needed so the K8sDriver
// can pick the right kubectl context. May be nil for drivers that
// have no per-cluster surface (Docker reads from $DOCKER_HOST).
type Discovery interface {
	// ListLabelled returns one entry per running instance whose
	// labels contain selector matches. Implementations should match
	// EVERY (k,v) in selector exactly (AND semantics).
	ListLabelled(ctx context.Context, cluster *profile.ClusterProfile, selector map[string]string) ([]DiscoveredInstance, error)
}

// DiscoveredInstance is the metadata the reconciler needs to
// reconstruct an Agent record from the driver's view of the world.
type DiscoveredInstance struct {
	// DriverInstance is the docker container ID or k8s "ns/podname".
	DriverInstance string
	// AgentID is the original agent ID, read back from the
	// `datawatch.agent_id` label.
	AgentID        string
	// ProjectProfile + ClusterProfile are read from the matching labels.
	ProjectProfile string
	ClusterProfile string
	// Branch + ParentAgentID are best-effort (label may be missing
	// on older spawns); empty is OK.
	Branch        string
	ParentAgentID string
	// Addr is the in-cluster address (`<ip>:<port>`) when known.
	Addr string
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

	// Auditor receives one AuditEvent per Manager mutation
	// (F10 S8.4). Optional — nil disables audit emission.
	Auditor Auditor

	// SecretsProvider (BL111) is consulted by ResolveCreds to read the
	// secret named by a ClusterProfile.CredsRef. Optional — when nil
	// ResolveCreds returns the literal Key string back so legacy
	// callers (kubeconfig path, gh CLI auth) keep working.
	SecretsProvider secrets.Provider

	// PQCBootstrap (BL95) — when true Spawn mints a fresh PQC keypair
	// and stores it on the Agent record; ConsumeBootstrap accepts a
	// PQC envelope as the "token" arg in addition to the legacy UUID.
	// Default false → legacy UUID flow only.
	PQCBootstrap bool

	// crashRetries tracks per-(project,branch,parent) respawn counts so
	// `respawn_once` is honoured across spawn calls and so
	// `respawn_with_backoff` doesn't restart from 1m on every failure.
	// Read+written under m.mu (BL106).
	crashRetries map[string]*crashState

	// S13 — observer peer registry. When non-nil, Spawn mints a peer
	// token alongside the bootstrap token; Terminate drops the peer.
	// Defined as an interface here to avoid an agents → observer
	// import cycle (observer is the implementation; agents only
	// needs the narrow surface).
	ObserverPeers ObserverPeerRegistry

	// observerPeerTokens holds the minted peer token for each agent
	// until ConsumeBootstrap reads it via GetObserverPeerTokenFor.
	// Cleared on Terminate. Map[agentID]token.
	observerPeerTokens map[string]string
}

// ObserverPeerRegistry is the narrow surface Manager needs from
// internal/observer.PeerRegistry — defined here to break the import
// cycle. Both methods match observer.PeerRegistry exactly.
type ObserverPeerRegistry interface {
	Register(name, shape, version string, hostInfo map[string]any) (token string, err error)
	Delete(name string) error
}

// crashState is the per-spawn-key respawn book-keeping for BL106.
type crashState struct {
	count   int       // how many respawns have already been attempted
	lastAt  time.Time // when the most recent respawn fired (backoff clock)
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
		agents:             map[string]*Agent{},
		drivers:            map[string]Driver{},
		projects:           projects,
		clusters:           clusters,
		TokenTTL:           5 * time.Minute,
		crashRetries:       map[string]*crashState{},
		observerPeerTokens: map[string]string{},
	}
}

// GetObserverPeerTokenFor returns the observer-peer bearer token
// minted for agentID at Spawn, or "" if observer-peer registration
// is disabled or has not run for this agent. Read by the bootstrap
// handler so it can hand the token to the worker. Single-read by
// design — the caller (handleAgentBootstrap) hands the token off
// to the worker once and never re-serves it.
func (m *Manager) GetObserverPeerTokenFor(agentID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.observerPeerTokens[agentID]
}

// GetProjectStore + GetClusterStore expose the underlying profile
// stores for tests + REST handlers that need to mutate profiles
// without dragging the store wiring through every constructor.
func (m *Manager) GetProjectStore() *profile.ProjectStore { return m.projects }
func (m *Manager) GetClusterStore() *profile.ClusterStore { return m.clusters }

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
	// F10 S8.3 — when the spawn request omits cluster_profile,
	// fall back to the project's DefaultClusterProfile. Lets a
	// project pin its preferred cluster while staying overridable
	// per spawn (operator can still pick a different cluster on
	// the request).
	clusterName := req.ClusterProfile
	if clusterName == "" {
		clusterName = proj.DefaultClusterProfile
	}
	if clusterName == "" {
		return nil, fmt.Errorf(
			"spawn: cluster_profile required (project %q has no default_cluster_profile)",
			proj.Name)
	}
	cluster, err := m.clusters.Get(clusterName)
	if err != nil {
		return nil, fmt.Errorf("cluster profile %q: %w", clusterName, err)
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

	// F10 S7.3 — workspace lock. Reject when an active agent already
	// owns the same (project_profile, branch) tuple. Branch defaults
	// to the profile's branch when not supplied by the caller.
	branch := req.Branch
	if branch == "" {
		branch = proj.Git.Branch
	}
	if err := m.checkWorkspaceLock(req.ProjectProfile, branch); err != nil {
		return nil, err
	}

	// F10 S7.4 — recursion gates. When this is a child spawn, enforce
	// the parent agent's profile budgets BEFORE creating the new
	// Agent record (so failed budget checks don't leave orphan
	// records to clean up).
	if req.ParentAgentID != "" {
		if err := m.checkRecursionBudget(req.ParentAgentID); err != nil {
			return nil, err
		}
	}

	a := &Agent{
		ID:             newAgentID(),
		ProjectProfile: proj.Name,
		ClusterProfile: cluster.Name,
		Task:           req.Task,
		State:          StatePending,
		CreatedAt:      time.Now().UTC(),
		BootstrapToken: newBootstrapToken(),
		Branch:         branch,
		ParentAgentID:  req.ParentAgentID,
		project:        proj,
		cluster:        cluster,
	}

	// BL95 — opt-in PQC envelope. When enabled the legacy UUID still
	// rides along (defence in depth + back-compat for workers that
	// haven't been rebuilt yet); the worker may present either form.
	if m.PQCBootstrap {
		keys, err := GeneratePQCKeys()
		if err != nil {
			return nil, fmt.Errorf("PQC key generation: %w", err)
		}
		a.PQCKeys = keys
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

	// S13 — mint an observer-peer token BEFORE the driver spawn so
	// the bootstrap response can hand it to the worker on first call.
	// Warn-only on failure: spawn proceeds without observer
	// peering (worker still functions, just no per-agent peer in the
	// parent's federated view).
	if m.ObserverPeers != nil {
		hostInfo := map[string]any{
			"hostname":        a.ID,
			"shape":           "agent",
			"project_profile": a.ProjectProfile,
			"cluster_profile": a.ClusterProfile,
			"parent_agent_id": a.ParentAgentID,
		}
		token, perr := m.ObserverPeers.Register(a.ID, "A", "", hostInfo)
		if perr != nil {
			fmt.Printf("[warn] observer peer register for %s: %v\n", a.ID, perr)
		} else {
			m.mu.Lock()
			m.observerPeerTokens[a.ID] = token
			m.mu.Unlock()
		}
	}

	if err := driver.Spawn(ctx, a); err != nil {
		m.mu.Lock()
		a.State = StateFailed
		a.FailureReason = err.Error()
		// S13 — clean up the orphan observer peer on spawn failure.
		// HandleCrash may produce a replacement agent that gets its
		// own peer registration; the failed agent's peer must go.
		delete(m.observerPeerTokens, a.ID)
		m.mu.Unlock()
		if m.ObserverPeers != nil {
			_ = m.ObserverPeers.Delete(a.ID)
		}
		emit(m.Auditor, "spawn_fail", a.ID, a.ProjectProfile, a.ClusterProfile,
			string(a.State), err.Error(), nil)

		// BL106 — consult OnCrash. If a respawn fires the caller still
		// gets the failure return so they can audit, but the
		// replacement agent is preferred for surfacing in callbacks.
		if replacement, retried, _ := m.HandleCrash(ctx, a); retried && replacement != nil {
			return replacement, fmt.Errorf("driver spawn (respawned as %s): %w", replacement.ID, err)
		}
		return a, fmt.Errorf("driver spawn: %w", err)
	}

	emit(m.Auditor, "spawn", a.ID, a.ProjectProfile, a.ClusterProfile,
		string(a.State), "", map[string]interface{}{
			"branch":          a.Branch,
			"parent_agent_id": a.ParentAgentID,
		})
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

	// S13 — drop the observer peer so it stops appearing in the
	// federated peers card. Best-effort.
	if m.ObserverPeers != nil {
		_ = m.ObserverPeers.Delete(a.ID)
	}
	m.mu.Lock()
	delete(m.observerPeerTokens, a.ID)
	m.mu.Unlock()

	m.mu.Lock()
	a.State = StateStopped
	a.StoppedAt = time.Now().UTC()
	m.mu.Unlock()
	emit(m.Auditor, "terminate", a.ID, a.ProjectProfile, a.ClusterProfile,
		string(a.State), "", nil)
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
//
// BL95 — when the Agent record holds PQCKeys the token arg is also
// accepted in PQC envelope form (<base64-ct>.<base64-sig>); the
// legacy UUID path keeps working alongside it so partial rollouts
// (newer parent + older worker image) still bootstrap.
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
	if a.State != StateStarting {
		return nil, fmt.Errorf("agent not in starting state (got %s)", a.State)
	}

	// PQC path — only attempted when keys exist on the record AND the
	// token looks like an envelope (contains ".") so legacy UUIDs
	// never accidentally trigger PQC verification.
	if a.PQCKeys != nil && strings.Contains(token, ".") {
		if _, err := VerifyPQCBootstrapToken(token, agentID, a.PQCKeys); err != nil {
			return nil, fmt.Errorf("PQC bootstrap verify: %w", err)
		}
	} else if a.BootstrapToken != token {
		return nil, fmt.Errorf("bootstrap token mismatch")
	}

	a.BootstrapToken = "" // burn UUID
	a.PQCKeys = nil       // burn PQC material — single use
	a.State = StateReady
	a.ReadyAt = time.Now().UTC()
	a.LastActivityAt = a.ReadyAt // first sign of life — F10 S8.6
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

// ActiveIDs returns the IDs of every agent the Manager is still
// tracking — including pending/starting/ready/running/failed (NOT
// stopped, since that's the terminal state where token cleanup
// should have already happened). Used by the F10 S5.5 sweeper to
// distinguish "alive worker, leave its token alone" from
// "orphaned token, sweep it".
func (m *Manager) ActiveIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.agents))
	for id, a := range m.agents {
		if a.State == StateStopped {
			continue
		}
		out = append(out, id)
	}
	return out
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

// checkWorkspaceLock (F10 S7.3) returns an error when an active
// (non-terminal) agent already owns the same (projectProfile,
// branch) tuple. Concurrent workers on the same workspace would
// race each other's git pushes — clean refusal at spawn time is
// the cheapest fix.
func (m *Manager) checkWorkspaceLock(projectProfile, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.agents {
		if a.State == StateStopped || a.State == StateFailed {
			continue
		}
		if a.ProjectProfile != projectProfile {
			continue
		}
		other := a.Branch
		if other == "" && a.project != nil {
			other = a.project.Git.Branch
		}
		if other == branch {
			return fmt.Errorf("workspace lock: agent %s already owns project %q branch %q (state=%s)",
				a.ID, projectProfile, branch, a.State)
		}
	}
	return nil
}

// checkRecursionBudget (F10 S7.4) enforces the parent agent's
// Project Profile spawn-budget when the spawn is recursive
// (req.ParentAgentID is set). Three checks:
//   1. AllowSpawnChildren must be true
//   2. SpawnBudgetTotal — count of all child agents whose
//      ParentAgentID == this parent's id, including stopped ones,
//      must be < the cap
//   3. SpawnBudgetPerMinute — count of children spawned in the
//      last 60 seconds must be < the cap
// All checks skip when the corresponding budget field is zero
// (= "no cap"). Operator-tunable via every channel through the
// existing ProjectProfile.SpawnBudget* fields.
func (m *Manager) checkRecursionBudget(parentAgentID string) error {
	m.mu.Lock()
	parent, ok := m.agents[parentAgentID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("recursion gate: parent agent %q not found", parentAgentID)
	}
	if parent.project == nil {
		m.mu.Unlock()
		return fmt.Errorf("recursion gate: parent agent %q missing profile reference", parentAgentID)
	}
	if !parent.project.AllowSpawnChildren {
		m.mu.Unlock()
		return fmt.Errorf("recursion gate: parent profile %q does not allow_spawn_children",
			parent.ProjectProfile)
	}
	totalCap := parent.project.SpawnBudgetTotal
	minuteCap := parent.project.SpawnBudgetPerMinute
	now := time.Now().UTC()
	cutoff := now.Add(-time.Minute)

	totalChildren := 0
	minuteChildren := 0
	for _, a := range m.agents {
		if a.ParentAgentID != parentAgentID {
			continue
		}
		totalChildren++
		if a.CreatedAt.After(cutoff) {
			minuteChildren++
		}
	}
	m.mu.Unlock()

	if totalCap > 0 && totalChildren >= totalCap {
		return fmt.Errorf("recursion gate: spawn_budget_total exhausted (%d/%d for parent %s)",
			totalChildren, totalCap, parentAgentID)
	}
	if minuteCap > 0 && minuteChildren >= minuteCap {
		return fmt.Errorf("recursion gate: spawn_budget_per_minute exhausted (%d/%d for parent %s)",
			minuteChildren, minuteCap, parentAgentID)
	}
	return nil
}

// NoteActivity bumps the agent's LastActivityAt to now. Called from
// every code path that observes a sign of life (bootstrap consume,
// MCP call, session input forward, memory write proxy, peer message,
// result post). Idempotent + cheap. F10 S8.6.
func (m *Manager) NoteActivity(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok {
		return
	}
	a.LastActivityAt = time.Now().UTC()
}

// ReapIdle terminates any active agent whose Project Profile has a
// non-zero IdleTimeout AND whose LastActivityAt (or CreatedAt
// floor) exceeds that timeout. Returns the IDs that were reaped.
// Safe to call from a background goroutine on a fixed cadence; the
// per-agent state checks all happen under m.mu.
//
// `now` is taken as a parameter so tests can drive deterministic
// expirations without sleeping.
func (m *Manager) ReapIdle(ctx context.Context, now time.Time) []string {
	type victim struct {
		id      string
		idle    time.Duration
		project string
	}
	var victims []victim
	m.mu.Lock()
	for id, a := range m.agents {
		if a.State == StateStopped || a.State == StateFailed {
			continue
		}
		if a.project == nil || a.project.IdleTimeout <= 0 {
			continue
		}
		// F10 S8.2 — service-mode workers are exempt from the idle
		// reaper. They terminate only on explicit operator call.
		if a.project.Mode == "service" {
			continue
		}
		floor := a.LastActivityAt
		if floor.IsZero() {
			floor = a.CreatedAt
		}
		if now.Sub(floor) > a.project.IdleTimeout {
			victims = append(victims, victim{id: id, idle: now.Sub(floor), project: a.project.Name})
		}
	}
	m.mu.Unlock()

	reaped := make([]string, 0, len(victims))
	for _, v := range victims {
		_ = m.Terminate(ctx, v.id) // Terminate emits its own audit event
		emit(m.Auditor, "idle_reap", v.id, v.project, "", string(StateStopped),
			fmt.Sprintf("idle %s exceeded profile.idle_timeout", v.idle.Round(time.Second)),
			nil)
		reaped = append(reaped, v.id)
	}
	return reaped
}

// RunIdleReaper (BL108) starts a background loop that calls ReapIdle
// every `interval` until ctx is cancelled. interval is clamped to a
// 10s minimum to keep the polling cost negligible. Returns
// immediately; the loop runs in its own goroutine.
//
// Wired from cmd/datawatch/main.go after Manager is fully configured;
// kept here so the cadence + clamping rules live next to ReapIdle
// itself rather than in the daemon entrypoint.
func (m *Manager) RunIdleReaper(ctx context.Context, interval time.Duration) {
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				m.ReapIdle(ctx, now)
			}
		}
	}()
}

// RecordResult attaches a structured worker-reported result to the
// agent record (F10 S7.2 fan-in). Idempotent within a sane window —
// re-posts overwrite. Returns an error when the agent doesn't exist;
// status defaults to "ok" when empty so workers can post a bare
// summary string.
func (m *Manager) RecordResult(agentID string, result *AgentResult) error {
	if agentID == "" {
		return fmt.Errorf("RecordResult: agent id required")
	}
	if result == nil {
		return fmt.Errorf("RecordResult: result required")
	}
	if result.Status == "" {
		result.Status = "ok"
	}
	if result.ReportedAt.IsZero() {
		result.ReportedAt = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[agentID]
	if !ok {
		return fmt.Errorf("RecordResult: agent %q not found", agentID)
	}
	cp := *result
	a.Result = &cp
	a.LastActivityAt = time.Now().UTC() // F10 S8.6 — fan-in is activity
	emit(m.Auditor, "result", agentID, a.ProjectProfile, a.ClusterProfile,
		string(a.State), result.Status, map[string]interface{}{
			"summary": result.Summary,
		})
	return nil
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

// ResolveCreds (BL111) returns the secret value for ref via the
// configured secrets.Provider. Empty Key short-circuits to ("", nil).
// Nil Provider returns the literal Key — preserves the legacy "the
// CredsRef IS the path" behaviour for callers that haven't migrated
// to the provider-backed model yet.
func (m *Manager) ResolveCreds(ref profile.CredsRef) (string, error) {
	if ref.Key == "" {
		return "", nil
	}
	if m.SecretsProvider == nil {
		return ref.Key, nil
	}
	val, err := m.SecretsProvider.Get(ref.Key)
	if err != nil {
		return "", fmt.Errorf("resolve creds %q via %s: %w",
			ref.Key, m.SecretsProvider.Kind(), err)
	}
	return val, nil
}
