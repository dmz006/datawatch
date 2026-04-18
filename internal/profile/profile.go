// Package profile holds the Project Profile and Cluster Profile types
// introduced in F10 sprint 2. A session at spawn time references one
// of each; the K8s/Docker driver (sprint 3+) composes a two-container
// Pod from the `(agent, sidecar)` pair described in the Project Profile,
// running against the cluster described in the Cluster Profile.
//
// Storage: JSON files under $DataDir/profiles/{projects,clusters}.json,
// transparently AES-256-GCM encrypted when --secure is active.
//
// Every field that configures runtime behaviour here is also reachable
// via the REST API, MCP, CLI, and comm-channel layers the project rules
// require; this package defines the data model and validation, and
// leaves the IO shape to the server + cobra + router packages.
package profile

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ── Names and validation ────────────────────────────────────────────────

// profileNameRE matches k8s-ish DNS-label names: lowercase letters,
// digits, and hyphens, 1-63 chars, can't start or end with hyphen.
// Using this pattern keeps Project Profile names safe to use as
// container labels, k8s Pod name prefixes, and memory namespaces
// without further escaping.
var profileNameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidateName returns nil if name is a valid Project/Cluster Profile
// identifier. Callers should use this in Create/Update validation.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > 63 {
		return fmt.Errorf("name %q exceeds 63 chars", name)
	}
	if !profileNameRE.MatchString(name) {
		return fmt.Errorf("name %q must match %s (lowercase dns label)", name, profileNameRE.String())
	}
	return nil
}

// ── Smoke test ──────────────────────────────────────────────────────────

// SmokeResult is what `Smoke(name)` returns for either a Project or
// Cluster Profile. A profile is "passing" iff Errors is empty.
// Warnings are surfaced in the UI but don't block spawn.
//
// Checks enumerated: the profile was built up cumulatively so callers
// can show operators where a partial validation got (e.g. "name ok,
// git reachable, cluster unreachable").
type SmokeResult struct {
	Name     string    `json:"name"`
	Checks   []string  `json:"checks"`            // ordered human-readable list
	Warnings []string  `json:"warnings,omitempty"`
	Errors   []string  `json:"errors,omitempty"`
	RanAt    time.Time `json:"ran_at"`
}

// Passed reports whether the smoke had zero errors. Warnings don't
// count as failure.
func (r *SmokeResult) Passed() bool { return len(r.Errors) == 0 }

// addCheck appends a check description; if err is non-nil it's routed
// to Errors so Passed() will be false.
func (r *SmokeResult) addCheck(desc string, err error) {
	r.Checks = append(r.Checks, desc)
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: %v", desc, err))
	}
}

func (r *SmokeResult) addWarning(msg string) {
	r.Warnings = append(r.Warnings, msg)
}

// ── Memory modes (Project Profile) ──────────────────────────────────────

// MemoryMode controls how a worker's episodic memory interacts with
// the parent's. One of:
//
//   "shared"     — worker connects directly to parent's pgvector;
//                  all writes immediately visible
//   "sync-back"  — worker has its own local memory; changes copy to
//                  parent on session end (or periodically)
//   "ephemeral"  — worker uses a tmpfs sqlite; nothing persists
//
// Default when unset in the profile: "sync-back" (safest: worker can
// survive parent outages; parent still sees results eventually).
type MemoryMode string

const (
	MemoryShared    MemoryMode = "shared"
	MemorySyncBack  MemoryMode = "sync-back"
	MemoryEphemeral MemoryMode = "ephemeral"
)

func (m MemoryMode) Valid() bool {
	switch m {
	case "", MemoryShared, MemorySyncBack, MemoryEphemeral:
		return true
	}
	return false
}

// effective returns the concrete mode, substituting the default when
// the stored value is empty.
func (m MemoryMode) effective() MemoryMode {
	if m == "" {
		return MemorySyncBack
	}
	return m
}

// ── Cluster kinds ────────────────────────────────────────────────────

// ClusterKind enumerates where a worker can be spawned. Only docker
// and k8s are implemented in F10 v1; cf (Cloud Foundry) is reserved
// per the plan's non-goal list.
type ClusterKind string

const (
	ClusterDocker ClusterKind = "docker"
	ClusterK8s    ClusterKind = "k8s"
	ClusterCF     ClusterKind = "cf" // placeholder; sprint 8+
)

func (k ClusterKind) Valid() bool {
	switch k {
	case ClusterDocker, ClusterK8s, ClusterCF:
		return true
	}
	return false
}

// ── Agent + Sidecar image names ─────────────────────────────────────────

// knownAgents is the list of agent-* images datawatch ships. A profile's
// ImagePair.Agent must match one (or be empty, for agent-less sessions
// like tools-driven operator work).
//
// Kept in code rather than reflecting harbor so that offline clusters
// can validate without a network round-trip.
var knownAgents = []string{
	"agent-claude",
	"agent-opencode",
	"agent-gemini",
	"agent-aider",
}

// knownSidecars lists valid sidecar images. Empty string is allowed
// (solo agent — no language or tools container in the Pod).
var knownSidecars = []string{
	"lang-go",
	"lang-node",
	"lang-python",
	"lang-rust",
	"lang-kotlin",
	"lang-ruby",
	"tools-ops",
}

// IsKnownAgent reports whether image is one of the agent-* variants
// this datawatch release ships.
func IsKnownAgent(image string) bool {
	for _, a := range knownAgents {
		if a == image {
			return true
		}
	}
	return false
}

// IsKnownSidecar reports whether image is one of the lang-*/tools-*
// variants. Empty string is valid (no sidecar).
func IsKnownSidecar(image string) bool {
	if image == "" {
		return true
	}
	for _, s := range knownSidecars {
		if s == image {
			return true
		}
	}
	return false
}

// KnownAgents returns a copy of the supported agent list for callers
// that need to populate UI dropdowns or CLI completions.
func KnownAgents() []string {
	out := make([]string, len(knownAgents))
	copy(out, knownAgents)
	return out
}

// KnownSidecars returns a copy of the supported sidecar list.
func KnownSidecars() []string {
	out := make([]string, len(knownSidecars))
	copy(out, knownSidecars)
	return out
}

// ── small helpers ───────────────────────────────────────────────────────

// deepCopyStrings returns an independent slice so callers can't mutate
// the store's internal state through a returned profile.
func deepCopyStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// deepCopyMap returns an independent map for the same reason.
func deepCopyMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// trimStrings maps strings.TrimSpace across a slice, dropping empties.
func trimStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
