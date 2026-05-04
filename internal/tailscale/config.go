// BL243 Phase 1 — Tailscale k8s sidecar: config types.

package tailscale

// Config drives the Tailscale integration globally and per-cluster.
// Fields that accept ${secret:name} references are resolved by
// secrets.ResolveConfig at daemon startup (same mechanism as BL242
// Phase 4 config refs).
type Config struct {
	// Enabled activates sidecar injection for all F10 agent pods by
	// default when true. Per-cluster override via ClusterProfile.Tailscale.
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// CoordinatorURL is the headscale server base URL, e.g.
	// "https://headscale.example.com". When empty, commercial Tailscale
	// is used (api.tailscale.com).
	CoordinatorURL string `yaml:"coordinator_url,omitempty" json:"coordinator_url,omitempty"`

	// AuthKey is the pre-auth key injected into spawned sidecars via
	// TS_AUTHKEY. Supports ${secret:name} references (BL242).
	AuthKey string `yaml:"auth_key,omitempty" json:"auth_key,omitempty"`

	// APIKey is the headscale admin API key used by the daemon for
	// status/nodes/ACL-push calls. Supports ${secret:name} references.
	APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty"`

	// Image is the tailscale sidecar image. Default:
	// "ghcr.io/tailscale/tailscale:latest".
	Image string `yaml:"image,omitempty" json:"image,omitempty"`

	// Tags is the list of tailscale ACL tags assigned to spawned
	// agent sidecars, e.g. ["tag:dw-agent"]. Defaults to
	// ["tag:dw-agent"] when empty.
	Tags []string `yaml:"tags,omitempty" json:"tags,omitempty"`

	// ACL holds the policy generation options for headscale.
	ACL ACLConfig `yaml:"acl,omitempty" json:"acl,omitempty"`
}

// ACLConfig controls the incremental ACL policy pushed to headscale.
type ACLConfig struct {
	// AllowedPeers lists tailscale node names/tags that should have
	// access to the datawatch agent mesh. These are merged into the
	// generated policy without breaking existing services.
	AllowedPeers []string `yaml:"allowed_peers,omitempty" json:"allowed_peers,omitempty"`

	// ManagedTags lists ACL tags managed by datawatch. Only entries in
	// this list will be overwritten on ACL push; existing rules for
	// other tags are preserved. Default: ["tag:dw-agent",
	// "tag:dw-research", "tag:dw-software", "tag:dw-operational"].
	ManagedTags []string `yaml:"managed_tags,omitempty" json:"managed_tags,omitempty"`
}

// SidecarImage returns the effective sidecar image reference.
func (c *Config) SidecarImage() string {
	if c.Image != "" {
		return c.Image
	}
	return "ghcr.io/tailscale/tailscale:latest"
}

// EffectiveTags returns the effective ACL tags for agent sidecars.
func (c *Config) EffectiveTags() []string {
	if len(c.Tags) > 0 {
		return c.Tags
	}
	return []string{"tag:dw-agent"}
}

// DefaultManagedTags returns the default managed ACL tags.
func DefaultManagedTags() []string {
	return []string{
		"tag:dw-agent",
		"tag:dw-research",
		"tag:dw-software",
		"tag:dw-operational",
	}
}
