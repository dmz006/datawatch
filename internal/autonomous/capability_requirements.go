// Capability minimum-backend-version enforcement + requirements report.
//
// A single minimum-version table keyed by capability lives in code
// (minimumOllamaByCapability). Today it holds exactly one floor: the
// classification-based verdict capability, whose /v1/systemone endpoint
// shipped in Ollama 0.35.0 — the documented minimum from
// docs/plans/2026-10-15-logprob-classification-guardrail-design.md
// (section "Backend note: Ollama").
//
// Operators override a per-capability minimum through the config key
// autonomous.requirements.min_ollama.<capability> (see
// config.AutonomousRequirementsConfig). An override of "" clears the
// built-in floor for that capability.
//
// Enforcement:
//   - enabling a capability on a node below the minimum is refused with a
//     message naming the node, the found version and the required version;
//   - a node that later falls below the minimum (an Ollama upgrade or
//     downgrade changing its version) is flagged, and the capability fails
//     closed — its status stays unavailable until a recheck finds the node
//     back at or above the minimum.
//
// The requirements report builder (RequirementsReport) states what is
// configured, what is missing, and the automation backend minimums: at
// least one execution backend plus a planning and a verification backend;
// Claude and the local model pool are each optional but at least one is
// required.

package autonomous

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// CapabilityClassificationVerdict is the capability key (used in
// autonomous.requirements.min_ollama.<capability>) for the
// classification-based logprob verdict feature that judges run outcomes
// through Ollama's /v1/systemone decision endpoint.
const CapabilityClassificationVerdict = "classification_verdict"

// minimumOllamaByCapability is the single in-code minimum-version table,
// keyed by capability. Values are minimum Ollama versions ("X.Y.Z").
var minimumOllamaByCapability = map[string]string{
	CapabilityClassificationVerdict: "0.35.0", // /v1/systemone shipped in 0.35.0
}

// KnownCapabilities lists the capabilities with a shipped minimum, sorted.
func KnownCapabilities() []string {
	out := make([]string, 0, len(minimumOllamaByCapability))
	for k := range minimumOllamaByCapability {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DefaultMinimumOllama returns the built-in Ollama floor for a capability.
// ok is false for unknown capabilities.
func DefaultMinimumOllama(capability string) (v string, ok bool) {
	v, ok = minimumOllamaByCapability[capability]
	return v, ok
}

// EffectiveMinimumOllama resolves the Ollama minimum for a capability:
// the operator override (autonomous.requirements.min_ollama.<capability>)
// when set — including an empty override, which clears the floor —
// otherwise the in-code table. ok is false for unknown capabilities.
func EffectiveMinimumOllama(overrides map[string]string, capability string) (v string, ok bool) {
	if overrides != nil {
		if val, set := overrides[capability]; set {
			return strings.TrimSpace(val), true
		}
	}
	d, set := minimumOllamaByCapability[capability]
	if !set {
		return "", false
	}
	return d, true
}

// dottedVersionRE matches the first MAJOR(.MINOR(.PATCH)) core in a
// version string, tolerant of banners ("Ollama version 0.40.5"), "v"
// prefixes and pre-release suffixes ("0.35.0-beta1").
var dottedVersionRE = regexp.MustCompile(`(\d+(?:\.\d+){1,2})`)

type versionTuple struct {
	major int
	minor int
	patch int
}

func parseDotted(s string) (versionTuple, bool) {
	m := dottedVersionRE.FindString(strings.TrimSpace(s))
	if m == "" {
		return versionTuple{}, false
	}
	seg := strings.Split(m, ".")
	part := func(i int) int {
		if i >= len(seg) {
			return 0
		}
		n := 0
		for _, r := range seg[i] {
			if r < '0' || r > '9' {
				return n
			}
			n = n*10 + int(r-'0')
		}
		return n
	}
	return versionTuple{major: part(0), minor: part(1), patch: part(2)}, true
}

// CompareVersions compares two dotted versions. cmp is -1 (found below
// required), 0 (equal) or +1 (found above). err is non-nil when either
// side is unparseable/unknown.
func CompareVersions(found, required string) (cmp int, err error) {
	f, fok := parseDotted(found)
	r, rok := parseDotted(required)
	if !fok || !rok {
		return 0, fmt.Errorf("unknown version: found %q, required %q", found, required)
	}
	pairs := [3][2]int{{f.major, r.major}, {f.minor, r.minor}, {f.patch, r.patch}}
	for _, p := range pairs {
		if p[0] != p[1] {
			if p[0] > p[1] {
				return 1, nil
			}
			return -1, nil
		}
	}
	return 0, nil
}

// CapabilityNodeCheck is the outcome of checking one node against one
// capability's Ollama minimum.
type CapabilityNodeCheck struct {
	Capability string // capability key
	Node       string // node name
	Found      string // version reported by the node (verbatim)
	Required   string // effective minimum in force
	Override   bool   // true when the minimum came from an operator override
	OK         bool   // true when the node satisfies the minimum
	Unknown    bool   // true when the found version is unparseable (failed closed)
	Reason     string // human-readable explanation when !OK
}

// CheckCapabilityNode evaluates node's reported Ollama version against the
// effective minimum for capability. It never calls I/O — the found version
// is supplied by the caller (a live probe, a node record, or a test fake).
func CheckCapabilityNode(overrides map[string]string, capability, node, found string) (CapabilityNodeCheck, error) {
	required, known := EffectiveMinimumOllama(overrides, capability)
	if !known {
		return CapabilityNodeCheck{Capability: capability, Node: node, Found: found},
			fmt.Errorf("unknown capability %q (known: %s)", capability, strings.Join(KnownCapabilities(), ", "))
	}
	chk := CapabilityNodeCheck{
		Capability: capability,
		Node:       node,
		Found:      found,
		Required:   required,
		Override:   overrides != nil,
	}
	// An empty effective minimum (cleared by an explicit "" override)
	// imposes no floor at all.
	if required == "" {
		chk.OK = true
		return chk, nil
	}
	cmp, err := CompareVersions(found, required)
	if err != nil {
		chk.OK = false
		chk.Unknown = true
		chk.Reason = fmt.Sprintf("node %s reports an unknown Ollama version (%q) but %s requires Ollama %s or later — failing closed",
			node, found, capability, required)
		return chk, nil
	}
	if cmp < 0 {
		chk.OK = false
		chk.Reason = fmt.Sprintf("node %s is on Ollama %s but %s requires Ollama %s or later",
			node, found, capability, required)
	} else {
		chk.OK = true
	}
	return chk, nil
}

// RequirementsConfig carries the autonomous.requirements.* overrides on the
// manager config (bridge type for config.AutonomousRequirementsConfig).
type RequirementsConfig struct {
	// MinOllama maps capability → minimum Ollama version override
	// (autonomous.requirements.min_ollama.<capability>).
	MinOllama map[string]string `json:"min_ollama,omitempty"`
}

// DefaultRequirementsConfig returns the shipped settings: no overrides;
// every capability reads its minimum from the in-code table.
func DefaultRequirementsConfig() RequirementsConfig {
	return RequirementsConfig{MinOllama: map[string]string{}}
}

// RequirementsConfigFromYAML bridges the config file block to the manager
// config.
func RequirementsConfigFromYAML(c config.AutonomousRequirementsConfig) RequirementsConfig {
	out := RequirementsConfig{MinOllama: map[string]string{}}
	for k, v := range c.MinOllama {
		out.MinOllama[k] = v
	}
	return out
}

// yaml returns the same settings as the config file type, so a manager
// Config → config.AutonomousConfig json round-trip preserves overrides.
func (c RequirementsConfig) yaml() config.AutonomousRequirementsConfig {
	out := config.AutonomousRequirementsConfig{MinOllama: map[string]string{}}
	for k, v := range c.MinOllama {
		out.MinOllama[k] = v
	}
	return out
}

// capabilityNodeState is one node's enablement record under a capability.
type capabilityNodeState struct {
	enabled   bool
	flagged   bool // fell below the minimum after a version change; fails closed
	lastFound string
}

// EnableCapability turns capability on for a node, refusing when the node's
// Ollama version is below (or unknown against) the effective minimum. The
// refusal message names the node, the found version and the required
// version.
func (m *Manager) EnableCapability(capability, node, found string) (CapabilityNodeCheck, error) {
	chk, err := CheckCapabilityNode(m.cfg.Requirements.MinOllama, capability, node, found)
	if err != nil {
		return chk, err
	}
	if !chk.OK {
		return chk, errors.New(chk.Reason)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureCapabilityStateLocked()
	if m.capabilityState[capability] == nil {
		m.capabilityState[capability] = map[string]capabilityNodeState{}
	}
	m.capabilityState[capability][node] = capabilityNodeState{enabled: true, lastFound: found}
	return chk, nil
}

// RecheckCapability re-evaluates an already-enabled node against the
// effective minimum — e.g. after an Ollama upgrade or downgrade changed the
// node's reported version. A node that now falls below the minimum (or
// reports an unknown version) is flagged, and the capability then fails
// closed: its state stays unavailable until a later recheck finds the node
// at or above the minimum, which clears the flag. Disabling (the node no
// longer enabled) is a no-op.
func (m *Manager) RecheckCapability(capability, node, found string) (CapabilityNodeCheck, error) {
	chk, err := CheckCapabilityNode(m.cfg.Requirements.MinOllama, capability, node, found)
	if err != nil {
		return chk, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureCapabilityStateLocked()
	st, enabled := m.capabilityState[capability][node]
	if !enabled {
		return chk, nil
	}
	if chk.OK {
		st.flagged = false
	} else {
		st.flagged = true
	}
	st.lastFound = found
	m.capabilityState[capability][node] = st
	return chk, nil
}

// DisableCapability turns capability off for a node (also clears its flag).
func (m *Manager) DisableCapability(capability, node string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.capabilityState[capability] != nil {
		delete(m.capabilityState[capability], node)
	}
}

func (m *Manager) ensureCapabilityStateLocked() {
	if m.capabilityState == nil {
		m.capabilityState = map[string]map[string]capabilityNodeState{}
	}
}

// CapabilityNodeState is the current state of one node under a capability.
type CapabilityNodeState struct {
	Capability string
	Node       string
	Enabled    bool
	Found      string // last recheck's reported version
	Required   string // effective minimum in force
	Flagged    bool   // fell below the minimum; capability fails closed
	OK         bool   // enabled && unflagged
	Reason     string // set when !OK and enabled
}

// CapabilityNodeState snapshots the enablement + flag state. A flagged node
// reports OK=false (capability fails closed) with a reason naming the node,
// last seen version and required version.
func (m *Manager) CapabilityNodeState(capability, node string) (CapabilityNodeState, error) {
	required, known := EffectiveMinimumOllama(m.cfg.Requirements.MinOllama, capability)
	if !known {
		return CapabilityNodeState{}, fmt.Errorf("unknown capability %q (known: %s)",
			capability, strings.Join(KnownCapabilities(), ", "))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, enabled := m.capabilityState[capability][node]
	out := CapabilityNodeState{
		Capability: capability,
		Node:       node,
		Enabled:    enabled,
		Found:      st.lastFound,
		Required:   required,
		Flagged:    st.flagged,
		OK:         enabled && !st.flagged,
	}
	if enabled && st.flagged {
		out.Reason = fmt.Sprintf("node %s fell below the %s minimum (last Ollama %s; requires Ollama %s or later) — capability fails closed until the node reaches the minimum again",
			node, capability, st.lastFound, required)
	}
	return out, nil
}

// AutomationBackends summarizes the backends configured for automation:
// the per-role backend lists plus the two optional providers (Claude and
// the local model pool), each of which is optional but at least one must
// be present.
type AutomationBackends struct {
	Execution    []string `json:"execution"`
	Planning     []string `json:"planning"`
	Verification []string `json:"verification"`
	// ClaudeConfigured is true when a Claude backend is configured.
	ClaudeConfigured bool `json:"claude_configured"`
	// LocalPoolConfigured is true when the local model pool is configured.
	LocalPoolConfigured bool `json:"local_pool_configured"`
}

// ReportCapabilityNode is one node's report row under a capability.
type ReportCapabilityNode struct {
	Name     string `json:"name"`
	Found    string `json:"found,omitempty"`
	Required string `json:"required"`
	Enabled  bool   `json:"enabled"`
	Flagged  bool   `json:"flagged"`
}

// ReportCapability is one capability's report block: its effective minimum
// (and whether it is an override), the nodes under it, and satisfaction.
type ReportCapability struct {
	Capability    string                 `json:"capability"`
	MinimumOllama string                 `json:"minimum_ollama"`
	Override      bool                   `json:"override"`
	Nodes         []ReportCapabilityNode `json:"nodes"`
	Satisfied     bool                   `json:"satisfied"` // at least one enabled, unflagged node
}

// RequirementsReport is the requirements report: what is configured
// (minimums + overrides + per-capability nodes), what is missing, and the
// automation backend minimums.
type RequirementsReport struct {
	GeneratedAt  time.Time          `json:"generated_at"`
	Capabilities []ReportCapability `json:"capabilities"`
	Backends     AutomationBackends `json:"backends"`
	Missing      []string           `json:"missing"` // every unmet requirement, human-readable
	OK           bool               `json:"ok"`      // true when Missing is empty
}

// RequirementsReportInput is everything a requirements report needs. It
// exists so any surface (REST, MCP, CLI, comm, tests with fakes) can build
// the report without a Manager: the builder is a pure function of this input.
type RequirementsReportInput struct {
	// Overrides is autonomous.requirements.min_ollama.* (nil = built-in table).
	Overrides map[string]string
	// Nodes maps capability → the node rows under it (already resolved and
	// sorted; typically from Manager.CapabilityNodeRows).
	Nodes map[string][]ReportCapabilityNode
	// Backends is the automation backend summary.
	Backends AutomationBackends
}

// BuildRequirementsReport builds the requirements report from the input.
// Pure: no I/O, no locks, no manager. Backend minimums enforced: at least
// one execution backend, a planning backend, a verification backend, and at
// least one of the two optional providers (Claude, local model pool).
func BuildRequirementsReport(in RequirementsReportInput) RequirementsReport {
	rep := RequirementsReport{
		GeneratedAt: time.Now().UTC(),
		Backends:    in.Backends,
		Missing:     []string{},
	}

	for _, capability := range KnownCapabilities() {
		required, _ := EffectiveMinimumOllama(in.Overrides, capability)
		overridden := false
		if min, set := in.Overrides[capability]; set {
			overridden = true
			required = strings.TrimSpace(min)
		}
		rc := ReportCapability{
			Capability:    capability,
			MinimumOllama: required,
			Override:      overridden,
		}
		nodes := in.Nodes[capability]
		if nodes == nil {
			nodes = []ReportCapabilityNode{}
		}
		rc.Nodes = nodes
		for _, n := range nodes {
			if n.Enabled && !n.Flagged {
				rc.Satisfied = true
			}
		}
		if !rc.Satisfied {
			rep.Missing = append(rep.Missing, fmt.Sprintf(
				"%s: no node enabled with Ollama %s or later (minimum: %s)",
				capability, required, minimumSource(overridden)))
		}
		rep.Capabilities = append(rep.Capabilities, rc)
	}

	if len(in.Backends.Execution) == 0 {
		rep.Missing = append(rep.Missing, "automation: no execution backend configured (at least one required)")
	}
	if len(in.Backends.Planning) == 0 {
		rep.Missing = append(rep.Missing, "automation: no planning backend configured")
	}
	if len(in.Backends.Verification) == 0 {
		rep.Missing = append(rep.Missing, "automation: no verification backend configured")
	}
	if !in.Backends.ClaudeConfigured && !in.Backends.LocalPoolConfigured {
		rep.Missing = append(rep.Missing,
			"automation: neither a Claude backend nor a local model pool is configured (each optional, at least one required)")
	}

	rep.OK = len(rep.Missing) == 0
	return rep
}

// CapabilityNodeRows snapshots the node rows under capability for the
// report (resolved and sorted by name, as BuildRequirementsReport expects).
func (m *Manager) CapabilityNodeRows(capability string) []ReportCapabilityNode {
	required, known := EffectiveMinimumOllama(m.cfg.Requirements.MinOllama, capability)
	if !known {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sts := m.capabilityState[capability]
	names := make([]string, 0, len(sts))
	for name := range sts {
		names = append(names, name)
	}
	sort.Strings(names)
	nodes := make([]ReportCapabilityNode, 0, len(sts))
	for _, name := range names {
		st := sts[name]
		nodes = append(nodes, ReportCapabilityNode{
			Name:     name,
			Found:    st.lastFound,
			Required: required,
			Enabled:  st.enabled,
			Flagged:  st.flagged,
		})
	}
	return nodes
}

// RequirementsReport builds the requirements report from the manager's
// requirements config, its capability enablement state and the caller's
// backend summary.
func (m *Manager) RequirementsReport(b AutomationBackends) RequirementsReport {
	in := RequirementsReportInput{
		Overrides: m.cfg.Requirements.MinOllama,
		Nodes:     map[string][]ReportCapabilityNode{},
		Backends:  b,
	}
	for _, capability := range KnownCapabilities() {
		in.Nodes[capability] = m.CapabilityNodeRows(capability)
	}
	return BuildRequirementsReport(in)
}

// Text renders the report for the text surfaces (CLI, comm channel, MCP
// text format). It states what is configured, what is missing, and the
// automation backend minimums.
func (r RequirementsReport) Text() string {
	var sb strings.Builder
	sb.WriteString("Automation requirements\n")
	for _, rc := range r.Capabilities {
		fmt.Fprintf(&sb, "%s\n  Ollama minimum: %s (%s)\n", rc.Capability, minimumVersionWord(rc.MinimumOllama), minimumSource(rc.Override))
		if len(rc.Nodes) == 0 {
			sb.WriteString("  nodes: none enabled\n")
		} else {
			for _, n := range rc.Nodes {
				state := "disabled"
				switch {
				case n.Enabled && !n.Flagged:
					state = "enabled, meets the minimum"
				case n.Enabled && n.Flagged:
					state = "enabled, FLAGGED: below the minimum (fails closed)"
				}
				fmt.Fprintf(&sb, "  node %s: Ollama %s, requires %s — %s\n", n.Name, n.Found, n.Required, state)
			}
		}
		sb.WriteString("  " + yesNo(rc.Satisfied, "satisfied", "not satisfied") + "\n")
	}
	sb.WriteString("\nAutomation backends\n")
	sb.WriteString("  execution: " + joinOrNone(r.Backends.Execution) + "\n")
	sb.WriteString("  planning: " + joinOrNone(r.Backends.Planning) + "\n")
	sb.WriteString("  verification: " + joinOrNone(r.Backends.Verification) + "\n")
	fmt.Fprintf(&sb, "  claude configured: %s\n", yesNo(r.Backends.ClaudeConfigured, "yes", "no"))
	fmt.Fprintf(&sb, "  local model pool configured: %s\n", yesNo(r.Backends.LocalPoolConfigured, "yes", "no"))
	sb.WriteString("\n")
	if r.OK {
		sb.WriteString("All requirements are met.\n")
	} else {
		fmt.Fprintf(&sb, "%d requirement(s) unmet:\n", len(r.Missing))
		for _, m := range r.Missing {
			fmt.Fprintf(&sb, "  - %s\n", m)
		}
	}
	return sb.String()
}

func minimumVersionWord(v string) string {
	if v == "" {
		return "(none — floor cleared)"
	}
	return v
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}

func yesNo(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

// minimumSource states where a capability's minimum comes from.
func minimumSource(overridden bool) string {
	if overridden {
		return "operator override"
	}
	return "built-in table"
}
