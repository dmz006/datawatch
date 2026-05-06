// Package council (BL260, v6.11.0) implements the PAI Council Mode —
// structured multi-agent debate. 4–6 specialized personas critique a
// proposal across one or more rounds; a synthesizer combines the
// responses into a consensus recommendation with dissent notes.
//
// PAI source: docs/plans/2026-05-02-pai-comparison-analysis.md §4 +
// Recommendation M2.
//
// For v6.11.0 the LLM-driven response generation is stubbed — each
// persona returns a placeholder response derived from its system
// prompt + the proposal, marked clearly so operators see the
// framework + persona registry working without expecting real
// multi-LLM debate. Wiring real LLM calls (one per persona × N rounds
// + synthesizer) is a v6.11.x follow-up.
package council

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// Mode is the council execution mode.
type Mode string

const (
	ModeDebate Mode = "debate" // 3 rounds, 40-90s
	ModeQuick  Mode = "quick"  // 1 round
)

// Persona is one debater agent. Built-in personas live as YAML files
// in ~/.datawatch/council/personas/<name>.yaml; operators can add
// more.
type Persona struct {
	Name         string `yaml:"name" json:"name"`
	Role         string `yaml:"role,omitempty" json:"role,omitempty"`
	SystemPrompt string `yaml:"system_prompt" json:"system_prompt"`
	Model        string `yaml:"model,omitempty" json:"model,omitempty"`
}

// Round is one round of debate; each persona's response is captured.
type Round struct {
	Index     int                `json:"index"`
	Responses map[string]string  `json:"responses"` // persona name → response
}

// Run is one council execution.
type Run struct {
	ID           string    `json:"id"`
	Proposal     string    `json:"proposal"`
	Personas     []string  `json:"personas"`
	Mode         Mode      `json:"mode"`
	Rounds       []Round   `json:"rounds"`
	Consensus    string    `json:"consensus,omitempty"`
	Dissent      string    `json:"dissent,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
}

// DefaultPersonas returns the 12 built-in PAI-style personas.
//
// v6.12.1 (BL276) — operator added platform-engineer, network-engineer,
// data-architect, privacy. Operator's check: do existing personas cover
// these roles? Answer: no — security-skeptic touches privacy adjacent
// concerns but doesn't focus on PII/data-residency; ops-realist touches
// deployment but doesn't deeply consider platform-engineering or network
// load. The four new personas are distinct enough to merit standalone
// roles in a debate.
//
// These are loaded automatically when ~/.datawatch/council/personas/
// is empty; operators can override or extend by dropping their own
// YAML files at that path. The PWA Council card now exposes a "View /
// edit personas" affordance that opens the directory contents.
func DefaultPersonas() []Persona {
	return []Persona{
		{Name: "security-skeptic", Role: "Security review",
			SystemPrompt: "You are a paranoid security reviewer. For the proposal, identify attack vectors, privacy risks, supply-chain concerns, and weakest authentication / authorization assumptions. Cite specific scenarios."},
		{Name: "ux-advocate", Role: "User experience",
			SystemPrompt: "You are a UX advocate. Critique the proposal's effect on operator experience: cognitive load, error recovery, discoverability, mobile parity, accessibility, and the cost of operator confusion."},
		{Name: "perf-hawk", Role: "Performance",
			SystemPrompt: "You are a performance hawk. Analyze the proposal for latency, throughput, memory, and IO effects. Flag N+1 patterns, blocking calls, unbounded growth. Provide specific worst-case estimates."},
		{Name: "simplicity-advocate", Role: "Simplicity / minimalism",
			SystemPrompt: "You are a simplicity advocate. Push back against unnecessary complexity, premature abstractions, and feature creep. Propose the smallest correct version of the work."},
		{Name: "ops-realist", Role: "Operations",
			SystemPrompt: "You are an ops realist. Consider deployment, observability, debuggability, rollback, on-call burden, and dependencies on external infrastructure. Flag operational risks."},
		{Name: "contrarian", Role: "Devil's advocate",
			SystemPrompt: "You are the contrarian. Argue against the consensus assumptions of the proposal. Steel-man the alternative approach the proposer didn't consider. Be specific."},
		{Name: "platform-engineer", Role: "Platform / infrastructure",
			SystemPrompt: "You are a platform engineer responsible for the systems and operations of the running tech environment. Evaluate the proposal against host/runtime constraints, capacity planning, multi-tenancy, blast radius across other services, image/binary lifecycle, and the operational toil it introduces for the team."},
		{Name: "network-engineer", Role: "Networking / load",
			SystemPrompt: "You are a network engineer responsible for connectivity, load balancing, and trust boundaries. Evaluate the proposal for north-south + east-west traffic shape, link saturation risk, latency between zones/regions, ingress/egress costs, NAT/firewall complications, mTLS / overlay-network requirements, and impact on existing service-mesh policies."},
		{Name: "data-architect", Role: "Data / DBA",
			SystemPrompt: "You are an enterprise data architect / DBA. Consider implications of large data volumes, connected/joined data, retention policies, schema migration risk, indexing + query plan effects, transactional consistency vs eventual consistency tradeoffs, backup/restore impact, GDPR / data-residency, and downstream analytics pipelines."},
		{Name: "privacy", Role: "Privacy / PII",
			SystemPrompt: "You are a privacy reviewer (analogous to a security reviewer but focused on personal data). Identify which fields qualify as PII / PHI / sensitive personal data; consider consent, minimization, retention, anonymization quality, third-party processor risk, GDPR / CCPA / HIPAA exposure, and the operator's ability to honor data-subject rights."},
		{Name: "hacker", Role: "Adversarial security tester",
			SystemPrompt: "You are an adversarial security tester. Approach the proposal as someone trying to break it: enumerate misconfigurations, weak defaults, exposed surfaces, lateral-movement paths from one component to another. Consider supply-chain compromise, RCE / RCE-via-deserialization, server-side request forgery, secrets-leak via error messages, time-based and side-channel oracles, and operator phishing. Cite concrete exploit chains, not abstractions."},
		{Name: "app-hacker", Role: "Application security tester",
			SystemPrompt: "You are an application security tester. Focus specifically on the application layer (web / API / mobile / CLI surface) and the systems each runs on. For the proposal: enumerate input-handling weaknesses (XSS / injection / SSRF / IDOR / mass-assignment / path traversal), authn / authz bypasses, session fixation, CSRF, race conditions in state mutation, broken object-level authorization, and second-order injection through cached data. Then trace each downstream system the application talks to and consider how a foothold there could pivot back to the application. Be specific about HTTP verbs, endpoint paths, and parameter names."},
	}
}

// Orchestrator coordinates one council run end-to-end.
//
// LLMFn is the per-persona-round inference function injected by the
// daemon. When nil (as in v6.11.0 default), the orchestrator
// returns deterministic placeholder responses so the framework is
// usable end-to-end without an LLM backend.
type Orchestrator struct {
	mu        sync.Mutex
	dataDir   string
	personas  map[string]Persona
	LLMFn     func(persona Persona, proposal string, prior []Round) string
}

// NewOrchestrator constructs an Orchestrator rooted at dataDir
// (typically ~/.datawatch). Loads persona YAML files at
// dataDir/council/personas/; if the directory is empty, seeds with
// DefaultPersonas.
func NewOrchestrator(dataDir string) *Orchestrator {
	o := &Orchestrator{dataDir: dataDir, personas: map[string]Persona{}}
	o.loadOrSeed()
	return o
}

// PersonasDir returns the on-disk path holding persona YAML files.
func (o *Orchestrator) PersonasDir() string {
	return filepath.Join(o.dataDir, "council", "personas")
}

// RunsDir returns the path holding persisted Run JSON files.
func (o *Orchestrator) RunsDir() string {
	return filepath.Join(o.dataDir, "council", "runs")
}

func (o *Orchestrator) loadOrSeed() {
	dir := o.PersonasDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// fall back to in-memory defaults
		for _, p := range DefaultPersonas() {
			o.personas[p.Name] = p
		}
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		entries = nil
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !(strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		var p Persona
		if err := yaml.Unmarshal(b, &p); err != nil {
			continue
		}
		if p.Name == "" {
			continue
		}
		o.personas[p.Name] = p
		count++
	}
	if count == 0 {
		// seed defaults to disk so operators can edit them.
		for _, p := range DefaultPersonas() {
			b, _ := yaml.Marshal(&p)
			_ = os.WriteFile(filepath.Join(dir, p.Name+".yaml"), b, 0o644)
			o.personas[p.Name] = p
		}
		return
	}
	// v6.12.1 (BL276) — additive seed for default personas the operator
	// has never had on disk. A `.seeded` marker file tracks names the
	// daemon has previously written so we don't recreate ones the
	// operator deliberately deleted. New defaults (added in a future
	// release) are seeded the first time their name is missing from
	// both disk AND .seeded.
	markerPath := filepath.Join(dir, ".seeded")
	seededNames := map[string]bool{}
	if mb, err := os.ReadFile(markerPath); err == nil {
		for _, line := range strings.Split(string(mb), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				seededNames[line] = true
			}
		}
	}
	wrote := false
	for _, p := range DefaultPersonas() {
		if _, present := o.personas[p.Name]; present {
			seededNames[p.Name] = true
			continue
		}
		if seededNames[p.Name] {
			continue // operator deleted it deliberately
		}
		b, _ := yaml.Marshal(&p)
		if err := os.WriteFile(filepath.Join(dir, p.Name+".yaml"), b, 0o644); err == nil {
			o.personas[p.Name] = p
			seededNames[p.Name] = true
			wrote = true
		}
	}
	if wrote || len(seededNames) > 0 {
		var lines []string
		for name := range seededNames {
			lines = append(lines, name)
		}
		sort.Strings(lines)
		_ = os.WriteFile(markerPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}
}

// Personas returns the registered personas, sorted by name.
func (o *Orchestrator) Personas() []Persona {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Persona, 0, len(o.personas))
	for _, p := range o.personas {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AddPersona registers a new persona, persisting it to disk so it
// survives daemon restart. Operator-defined personas live alongside
// the built-in defaults; the .seeded marker is updated so the
// additive-seed path on next start treats this name as already known.
//
// v6.12.4 — backs the PWA "Add Persona" form + REST POST /api/council/personas.
func (o *Orchestrator) AddPersona(p Persona) error {
	if p.Name == "" {
		return fmt.Errorf("persona name required")
	}
	if p.SystemPrompt == "" {
		return fmt.Errorf("persona system_prompt required")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	dir := o.PersonasDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(&p)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, p.Name+".yaml"), b, 0o644); err != nil {
		return err
	}
	o.personas[p.Name] = p
	o.appendSeededLocked(p.Name)
	return nil
}

// RemovePersona deletes a persona from disk + memory and records the
// name in the .seeded marker so the additive-seed path doesn't
// resurrect it on the next daemon restart. Removing a built-in
// default this way "uninstalls" it durably.
//
// To reinstate a default after removal, call RestoreDefaultPersona.
func (o *Orchestrator) RemovePersona(name string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.personas[name]; !ok {
		return fmt.Errorf("persona %q not found", name)
	}
	dir := o.PersonasDir()
	for _, ext := range []string{".yaml", ".yml"} {
		_ = os.Remove(filepath.Join(dir, name+ext))
	}
	delete(o.personas, name)
	o.appendSeededLocked(name)
	return nil
}

// RestoreDefaultPersona resets a previously-removed default to its
// built-in form. No-op if the named persona isn't a built-in default.
func (o *Orchestrator) RestoreDefaultPersona(name string) error {
	var def Persona
	for _, p := range DefaultPersonas() {
		if p.Name == name {
			def = p
			break
		}
	}
	if def.Name == "" {
		return fmt.Errorf("%q is not a built-in default persona", name)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	dir := o.PersonasDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(&def)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, def.Name+".yaml"), b, 0o644); err != nil {
		return err
	}
	o.personas[def.Name] = def
	// Drop the name from .seeded so the additive-seed path treats it as
	// fresh again — symmetric to RemovePersona.
	o.removeSeededLocked(def.Name)
	return nil
}

// appendSeededLocked / removeSeededLocked maintain the .seeded marker.
// Caller must hold o.mu. Errors are best-effort logged via debugf.
func (o *Orchestrator) appendSeededLocked(name string) {
	dir := o.PersonasDir()
	markerPath := filepath.Join(dir, ".seeded")
	seededNames := map[string]bool{}
	if mb, err := os.ReadFile(markerPath); err == nil {
		for _, line := range strings.Split(string(mb), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				seededNames[line] = true
			}
		}
	}
	seededNames[name] = true
	var lines []string
	for n := range seededNames {
		lines = append(lines, n)
	}
	sort.Strings(lines)
	_ = os.WriteFile(markerPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func (o *Orchestrator) removeSeededLocked(name string) {
	dir := o.PersonasDir()
	markerPath := filepath.Join(dir, ".seeded")
	mb, err := os.ReadFile(markerPath)
	if err != nil {
		return
	}
	var lines []string
	for _, line := range strings.Split(string(mb), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != name {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		_ = os.Remove(markerPath)
		return
	}
	_ = os.WriteFile(markerPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// Run executes the council. names is the persona-name list; empty
// means "use all". mode chooses round count (debate=3, quick=1).
func (o *Orchestrator) Run(proposal string, names []string, mode Mode) (*Run, error) {
	if mode == "" {
		mode = ModeQuick
	}
	rounds := 1
	if mode == ModeDebate {
		rounds = 3
	}
	o.mu.Lock()
	if len(names) == 0 {
		for n := range o.personas {
			names = append(names, n)
		}
		sort.Strings(names)
	}
	chosen := make([]Persona, 0, len(names))
	for _, n := range names {
		p, ok := o.personas[n]
		if !ok {
			o.mu.Unlock()
			return nil, fmt.Errorf("unknown persona %q", n)
		}
		chosen = append(chosen, p)
	}
	o.mu.Unlock()

	run := &Run{
		ID:        uuid.NewString(),
		Proposal:  proposal,
		Personas:  names,
		Mode:      mode,
		StartedAt: time.Now().UTC(),
		Rounds:    make([]Round, 0, rounds),
	}
	for r := 0; r < rounds; r++ {
		round := Round{Index: r + 1, Responses: map[string]string{}}
		for _, p := range chosen {
			round.Responses[p.Name] = o.respond(p, proposal, run.Rounds)
		}
		run.Rounds = append(run.Rounds, round)
	}
	run.Consensus, run.Dissent = synthesize(run)
	run.FinishedAt = time.Now().UTC()
	if err := o.persistRun(run); err != nil {
		return run, err
	}
	return run, nil
}

func (o *Orchestrator) respond(p Persona, proposal string, prior []Round) string {
	if o.LLMFn != nil {
		return o.LLMFn(p, proposal, prior)
	}
	// Stubbed deterministic placeholder so the framework round-trips
	// without an LLM backend. Real LLM inference is a v6.11.x
	// follow-up.
	return fmt.Sprintf("[%s] STUB — proposal length %d chars, prior rounds=%d. (Real LLM debate ships in a v6.11.x follow-up.) System prompt: %s",
		p.Name, len(proposal), len(prior), strings.SplitN(p.SystemPrompt, ".", 2)[0])
}

// synthesize produces the (consensus, dissent) text from a Run's
// round responses. With the v6.11.0 stub, it concatenates the last
// round's responses; real synthesis with an LLM lives in v6.11.x.
func synthesize(run *Run) (consensus, dissent string) {
	if len(run.Rounds) == 0 {
		return "", ""
	}
	last := run.Rounds[len(run.Rounds)-1]
	var sb strings.Builder
	for name, resp := range last.Responses {
		fmt.Fprintf(&sb, "* %s: %s\n", name, resp)
	}
	consensus = "Council ran (stub mode). Aggregated last-round persona output:\n" + sb.String()
	dissent = "" // populated by real synthesizer in v6.11.x
	return
}

func (o *Orchestrator) persistRun(run *Run) error {
	dir := o.RunsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, run.ID+".json"), b, 0o644)
}

// LoadRun reads a persisted run by ID.
func (o *Orchestrator) LoadRun(id string) (*Run, error) {
	path := filepath.Join(o.RunsDir(), id+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var run Run
	if err := json.Unmarshal(b, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns returns persisted runs, most recent first; limit 0 = all.
func (o *Orchestrator) ListRuns(limit int) ([]*Run, error) {
	dir := o.RunsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []*Run{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		run, err := o.LoadRun(id)
		if err != nil {
			continue
		}
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
