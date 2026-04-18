package profile

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

// ── ClusterProfile ──────────────────────────────────────────────────────

// ClusterProfile describes WHERE a session runs — the kubectl context
// or docker endpoint, namespace, default resource limits, registry
// + pull secrets, and the CAs workers must trust for private-CA
// infrastructure.
//
// A Project Profile (WHAT) paired with a Cluster Profile (WHERE)
// uniquely determines a session's compose manifest.
type ClusterProfile struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	Kind        ClusterKind `json:"kind"`              // docker | k8s | cf
	Context     string      `json:"context,omitempty"` // kubectl context name
	Endpoint    string      `json:"endpoint,omitempty"` // explicit override for the API endpoint
	Namespace   string      `json:"namespace,omitempty"` // k8s namespace (default "default")

	// ImageRegistry overrides the global default REGISTRY from .env.build.
	// Format: host/project (e.g. harbor.dmzs.com/datawatch).
	ImageRegistry   string `json:"image_registry,omitempty"`
	ImagePullSecret string `json:"image_pull_secret,omitempty"`

	DefaultResources Resources `json:"default_resources,omitempty"`

	// TrustedCAs is a list of PEM-encoded certificates the worker Pod
	// should trust. Projected into the Pod as a ConfigMap mounted at
	// /etc/ssl/certs/extra-ca.pem and registered via update-ca-certs
	// by the worker's entrypoint. Essential for harbor/private-CA
	// setups where the cluster nodes themselves don't yet trust the CA.
	TrustedCAs []string `json:"trusted_cas,omitempty"`

	// CredsRef points at where git/registry/cloud credentials live.
	// In Sprint 1 only File + EnvVar are resolved; K8sSecret + Vault
	// are stubs reserved for Sprint 8.
	CredsRef CredsRef `json:"creds_ref,omitempty"`

	// NetworkPolicyRef, when set, names a pre-existing NetworkPolicy
	// in the same namespace that the worker Pod should be bound to.
	// Empty = no isolation (default).
	NetworkPolicyRef string `json:"network_policy_ref,omitempty"`

	// ParentCallbackURL lets operators force the URL workers call home
	// to. Empty = auto-detect from Server.PublicURL/Host.
	ParentCallbackURL string `json:"parent_callback_url,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Resources captures k8s-style cpu/mem requests + limits. Interpreted
// by the K8s driver; the Docker driver only honours the Mem fields.
type Resources struct {
	CPURequest string `json:"cpu_request,omitempty"` // e.g. "100m"
	CPULimit   string `json:"cpu_limit,omitempty"`
	MemRequest string `json:"mem_request,omitempty"` // e.g. "256Mi"
	MemLimit   string `json:"mem_limit,omitempty"`
}

// CredsRefProvider enumerates where secrets are loaded from at spawn.
type CredsRefProvider string

const (
	CredsFile    CredsRefProvider = "file"
	CredsEnvVar  CredsRefProvider = "env"
	CredsK8s     CredsRefProvider = "k8s-secret" // stub — sprint 8
	CredsVault   CredsRefProvider = "vault"       // stub — sprint 8
)

// CredsRef points at a credential bundle. The concrete resolution
// path depends on Provider: filesystem path, env-var name, k8s secret
// name, or vault path.
type CredsRef struct {
	Provider CredsRefProvider `json:"provider,omitempty"`
	Key      string           `json:"key,omitempty"`
}

// EffectiveNamespace returns the k8s namespace, defaulting to
// "default" when unset.
func (c *ClusterProfile) EffectiveNamespace() string {
	if c.Namespace != "" {
		return c.Namespace
	}
	return "default"
}

// ── Validation ──────────────────────────────────────────────────────────

func (c *ClusterProfile) Validate() error {
	var errs []string

	if err := ValidateName(c.Name); err != nil {
		errs = append(errs, err.Error())
	}

	if !c.Kind.Valid() {
		errs = append(errs, fmt.Sprintf("kind %q: must be docker|k8s|cf", c.Kind))
	} else if c.Kind == ClusterCF {
		// CF support deferred per the F10 plan. Accept at schema
		// level so profiles can be authored ahead of time, but warn.
	}

	// Kind-specific rules: k8s needs a context; docker can fall back
	// to the daemon's local socket.
	if c.Kind == ClusterK8s && c.Context == "" && c.Endpoint == "" {
		errs = append(errs, "k8s cluster profile requires either context or endpoint")
	}

	// Image registry format check: "host" or "host/project" only.
	// Port numbers allowed (host:5000/project).
	if c.ImageRegistry != "" {
		if strings.ContainsAny(c.ImageRegistry, " \t\n") {
			errs = append(errs, fmt.Sprintf("image_registry %q: must not contain whitespace", c.ImageRegistry))
		}
	}

	// Creds ref: if provider set, key must be set too.
	if c.CredsRef.Provider != "" && c.CredsRef.Key == "" {
		errs = append(errs, "creds_ref.key is required when creds_ref.provider is set")
	}
	switch c.CredsRef.Provider {
	case "", CredsFile, CredsEnvVar, CredsK8s, CredsVault:
		// ok
	default:
		errs = append(errs, fmt.Sprintf("creds_ref.provider %q: must be file|env|k8s-secret|vault|empty",
			c.CredsRef.Provider))
	}

	// PEM sanity: each TrustedCA must at least start with BEGIN CERTIFICATE.
	// Full x509 parse lives in the k8s driver when it mounts the bundle.
	for i, pem := range c.TrustedCAs {
		if !strings.Contains(pem, "BEGIN CERTIFICATE") {
			errs = append(errs, fmt.Sprintf("trusted_cas[%d]: not PEM-encoded", i))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid cluster profile: %s", strings.Join(errs, "; "))
	}
	return nil
}

// clone returns an independent copy.
func (c *ClusterProfile) clone() *ClusterProfile {
	if c == nil {
		return nil
	}
	out := *c
	out.TrustedCAs = deepCopyStrings(c.TrustedCAs)
	return &out
}

// ── ClusterStore ────────────────────────────────────────────────────────

// ClusterStore persists Cluster Profiles, mirroring ProjectStore.
type ClusterStore struct {
	mu       sync.Mutex
	path     string
	encKey   []byte
	profiles []*ClusterProfile
}

// NewClusterStore opens (or creates) a plaintext Cluster Profile store.
func NewClusterStore(path string) (*ClusterStore, error) {
	return newClusterStoreWithKey(path, nil)
}

// NewClusterStoreEncrypted opens (or creates) an AES-256-GCM encrypted
// Cluster Profile store.
func NewClusterStoreEncrypted(path string, key []byte) (*ClusterStore, error) {
	return newClusterStoreWithKey(path, key)
}

func newClusterStoreWithKey(path string, key []byte) (*ClusterStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create profile dir: %w", err)
	}
	s := &ClusterStore{path: path, encKey: key}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load cluster profiles: %w", err)
	}
	return s, nil
}

func (s *ClusterStore) load() error {
	data, err := secfile.ReadFile(s.path, s.encKey)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &s.profiles)
}

func (s *ClusterStore) save() error {
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cluster profiles: %w", err)
	}
	return secfile.WriteFile(s.path, data, 0600, s.encKey)
}

func (s *ClusterStore) List() []*ClusterProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ClusterProfile, len(s.profiles))
	for i, p := range s.profiles {
		out[i] = p.clone()
	}
	return out
}

func (s *ClusterStore) Get(name string) (*ClusterProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.profiles {
		if p.Name == name {
			return p.clone(), nil
		}
	}
	return nil, fmt.Errorf("cluster profile %q not found", name)
}

func (s *ClusterStore) Create(p *ClusterProfile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.profiles {
		if existing.Name == p.Name {
			return fmt.Errorf("cluster profile %q already exists", p.Name)
		}
	}
	now := time.Now().UTC()
	clone := p.clone()
	clone.CreatedAt = now
	clone.UpdatedAt = now
	s.profiles = append(s.profiles, clone)
	return s.save()
}

func (s *ClusterStore) Update(p *ClusterProfile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.profiles {
		if existing.Name == p.Name {
			clone := p.clone()
			clone.CreatedAt = existing.CreatedAt
			clone.UpdatedAt = time.Now().UTC()
			s.profiles[i] = clone
			return s.save()
		}
	}
	return fmt.Errorf("cluster profile %q not found", p.Name)
}

func (s *ClusterStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.profiles {
		if existing.Name == name {
			s.profiles = append(s.profiles[:i], s.profiles[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("cluster profile %q not found", name)
}

// Smoke validates + does cheap reachability checks. Deep probes
// (actually connecting to kubectl / docker API) land in Sprint 4
// when we wire the driver.
func (s *ClusterStore) Smoke(name string) (*SmokeResult, error) {
	p, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	r := &SmokeResult{Name: name, RanAt: time.Now().UTC()}

	r.addCheck("profile loaded", nil)

	if vErr := p.Validate(); vErr != nil {
		r.addCheck("validation", vErr)
		return r, nil
	}
	r.addCheck("validation", nil)

	if p.Kind == ClusterCF {
		r.addWarning("kind=cf: Cloud Foundry driver not implemented (F10 plan: sprint 8+)")
	}

	switch p.CredsRef.Provider {
	case CredsK8s:
		r.addWarning("creds_ref.provider=k8s-secret: resolver stub — sprint 8")
	case CredsVault:
		r.addWarning("creds_ref.provider=vault: resolver stub — sprint 8")
	}

	if len(p.TrustedCAs) == 0 && p.ImageRegistry != "" && strings.Contains(p.ImageRegistry, ".") {
		// Best-effort: non-localhost registry with no trusted CAs often
		// means the operator is relying on system CAs (fine). Note it.
		r.addCheck("registry reachability (deferred to spawn)", nil)
	}

	r.addCheck("smoke complete", nil)
	return r, nil
}
