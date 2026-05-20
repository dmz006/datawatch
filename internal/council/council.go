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
	"context"
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
	Cancelled    bool      `json:"cancelled,omitempty"` // v7.0.0 S3
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
// v7.0.0 S3 — InferenceFn replaces the v6.x LLMFn placeholder. When
// set (which is the default in v7), every persona response is a real
// LLM call routed through the dispatcher with ordered ComputeNode
// failover. When nil, Run() returns ErrNoInference (honest 503 per
// BL295 ASK-Q7) instead of emitting STUB strings — operators wire an
// LLM via cfg.Council.LLMRef + the v7 LLM registry.
//
// MaxParallel controls per-round persona concurrency (default 2 per
// BL295 Q2). When the chosen LLM's reachable ComputeNodes can't
// accommodate the requested parallelism, Run auto-serializes with a
// banner per ASK 27.
type Orchestrator struct {
	mu        sync.Mutex
	dataDir   string
	personas  map[string]Persona

	// v7.0.0 S3 — wired by daemon at startup. nil = honest 503.
	InferenceFn InferenceFn
	// LLMRef is the registry name the dispatcher resolves; see
	// internal/inference/dispatcher.go.
	LLMRef string
	// MaxParallel default 2 (BL295 Q2). 0 = serial.
	MaxParallel int

	// v7.0.0 S3 — cancellation registry. Run registers a cancel
	// func keyed on Run.ID so the cancel REST endpoint can stop
	// in-flight calls.
	cancels map[string]context.CancelFunc

	// v7.0.0 S4 — live-update fan-out. nil = no broadcast (silent
	// run). Daemon wires a closure that pushes to SSEHub + comm
	// channels.
	EventFn EventFn

	// v7.0.0 S4 — set by RunCtxWithID before calling RunCtx so the
	// new Run uses the supplied id (instead of generating a fresh
	// uuid). Cleared after Run() finishes. Internal.
	preallocatedRunID string

	// v7.0.0 S4.c (alpha.10) — per-persona session spawning. Daemon
	// wires SessionFn to a closure that creates a Session record per
	// persona invocation and returns its short id. SessionUpdateFn is
	// called when the persona's response (or error) is ready so the
	// session record can be filled in and marked complete.
	//
	// Both default to nil — when unset, no per-persona session is
	// created (preserves backward compat with v6.x clients that don't
	// know about Council).
	//
	// SpawnReal toggles between virtual transcript sessions (default,
	// cheap, read-only) and real coding-agent sessions (operator
	// opt-in via the run request). Daemon's SessionFn implementation
	// honours this flag.
	SessionFn       SessionFn
	SessionUpdateFn SessionUpdateFn

	// SpawnRealSessions, when true, asks SessionFn to spawn a real
	// coding-agent session per persona (tmux-attachable). Default
	// false = virtual transcript sessions (read-only entries in the
	// Sessions tab). Set per-run via StartAsyncWithOptions.
	SpawnRealSessions bool
}

// SessionFn is the per-persona session-creation contract. Daemon wires
// it; nil = no per-persona sessions. roundNum is 1-indexed.
//
// Returns the new session's short id (for SessionUpdateFn calls) plus
// any error. On error the caller continues without a session record;
// the persona inference still runs.
type SessionFn func(runID, persona, role string, roundNum int, prompt string, spawnReal bool) (sessionID string, err error)

// SessionUpdateFn closes out the persona session — fills in the response
// text + flips state to complete (or failed on error).
type SessionUpdateFn func(sessionID, response string, errMsg string)

// InferenceFn is the dispatcher contract from the daemon's
// perspective. Council doesn't import internal/inference directly
// (avoids package import cycle); the daemon wires a thin closure
// that calls inference.Dispatcher.Call.
type InferenceFn func(ctx context.Context, llmRef, systemPrompt, prompt, consumer string) (text string, usedNode string, err error)

// EventFn is the SSE/comm-push contract from the daemon's
// perspective. Council doesn't import internal/server (avoids cycle);
// the daemon wires a closure that calls SSEHub.Publish + comm push.
// runID is the topic; eventType + payload are JSON-encoded by the
// SSE writer.
//
// Event types emitted by Council per BL295 ASK 28 + § 4.4:
//   run_started        | {run_id, mode, personas, max_parallel}
//   round_started      | {run_id, round}
//   persona_responding | {run_id, round, persona}
//   persona_response   | {run_id, round, persona, text, used_node}
//   persona_error      | {run_id, round, persona, error}
//   round_completed    | {run_id, round}
//   synthesis_started  | {run_id}
//   run_completed      | {run_id, consensus, dissent, finished_at}
//   run_cancelled      | {run_id, finished_at}
type EventFn func(runID, eventType string, payload map[string]any)

// ErrNoInference is returned by Run when no InferenceFn is wired —
// honest fallback per BL295 ASK Q7. Operators see "council requires
// an LLM configured (set cfg.Council.LLMRef and add an LLM via
// `datawatch llm add ...`)" instead of stub responses.
var ErrNoInference = fmt.Errorf("council requires an LLM configured (set cfg.Council.LLMRef and add an LLM via 'datawatch llm add ...'); see docs/howto/council-mode.md")

// NewOrchestrator constructs an Orchestrator rooted at dataDir
// (typically ~/.datawatch). Loads persona YAML files at
// dataDir/council/personas/; if the directory is empty, seeds with
// DefaultPersonas.
func NewOrchestrator(dataDir string) *Orchestrator {
	o := &Orchestrator{
		dataDir:     dataDir,
		personas:    map[string]Persona{},
		cancels:     map[string]context.CancelFunc{},
		MaxParallel: 2, // v7.0.0 S3 default per BL295 Q2
	}
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
		if !strings.HasSuffix(n, ".yaml") && !strings.HasSuffix(n, ".yml") {
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

// GetPersona returns a single persona by name, or an error if not found.
//
// BL296 — backs REST GET /api/council/personas/{name} and the new
// council_personas_get MCP tool + CLI personas get <name>.
func (o *Orchestrator) GetPersona(name string) (Persona, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	p, ok := o.personas[name]
	if !ok {
		return Persona{}, fmt.Errorf("persona %q not found", name)
	}
	return p, nil
}

// UpdatePersona replaces an existing persona's mutable fields (role and
// system_prompt) in memory and on disk. The name is immutable; rename
// by removing the old one and adding a new one.
//
// BL296 — backs REST PUT /api/council/personas/{name} and the new
// council_personas_set MCP tool + CLI personas set <name> --prompt.
// This is the recommended path instead of the delete+re-POST pattern
// the PWA previously used.
func (o *Orchestrator) UpdatePersona(name string, update Persona) error {
	if name == "" {
		return fmt.Errorf("persona name required")
	}
	if update.SystemPrompt == "" {
		return fmt.Errorf("persona system_prompt required")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	existing, ok := o.personas[name]
	if !ok {
		return fmt.Errorf("persona %q not found", name)
	}
	if update.Role != "" {
		existing.Role = update.Role
	}
	existing.SystemPrompt = update.SystemPrompt
	dir := o.PersonasDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(&existing)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), b, 0o644); err != nil {
		return err
	}
	o.personas[name] = existing
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
//
// v7.0.0 S3 — every persona response is a real LLM call routed
// through the dispatcher with ordered ComputeNode failover. Per-round
// persona calls run concurrently up to MaxParallel (BL295 Q2).
// Cancellation is supported via Cancel(runID); ctx.Done short-
// circuits the remaining personas + synthesis.
func (o *Orchestrator) Run(proposal string, names []string, mode Mode) (*Run, error) {
	return o.RunCtx(context.Background(), proposal, names, mode)
}

// RunCtx is the ctx-aware variant — cancellation propagates to
// in-flight LLM calls.
func (o *Orchestrator) RunCtx(ctx context.Context, proposal string, names []string, mode Mode) (*Run, error) {
	if o.InferenceFn == nil {
		return nil, ErrNoInference
	}
	if strings.TrimSpace(o.LLMRef) == "" {
		return nil, fmt.Errorf("%w (cfg.Council.LLMRef is empty)", ErrNoInference)
	}
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

	maxPar := o.MaxParallel
	if maxPar < 1 {
		maxPar = 1
	}
	if maxPar > len(chosen) {
		maxPar = len(chosen)
	}
	runID := o.preallocatedRunID
	if runID == "" {
		runID = uuid.NewString()
	}
	run := &Run{
		ID:        runID,
		Proposal:  proposal,
		Personas:  names,
		Mode:      mode,
		StartedAt: time.Now().UTC(),
		Rounds:    make([]Round, 0, rounds),
	}

	// v7.0.0 S4 — emit lifecycle SSE events. Closure-style fan-out;
	// daemon wires EventFn to push to SSEHub + comm channels.
	emit := func(eventType string, payload map[string]any) {
		if o.EventFn == nil {
			return
		}
		if payload == nil {
			payload = map[string]any{}
		}
		payload["run_id"] = run.ID
		o.EventFn(run.ID, eventType, payload)
	}
	emit("run_started", map[string]any{
		"mode":         mode,
		"personas":     names,
		"max_parallel": maxPar,
		"rounds_total": rounds,
	})

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	o.mu.Lock()
	if o.cancels == nil {
		o.cancels = map[string]context.CancelFunc{}
	}
	o.cancels[run.ID] = cancel
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.cancels, run.ID)
		o.mu.Unlock()
	}()

	for r := 0; r < rounds; r++ {
		if err := runCtx.Err(); err != nil {
			run.Cancelled = true
			run.FinishedAt = time.Now().UTC()
			emit("run_cancelled", map[string]any{"finished_at": run.FinishedAt})
			_ = o.persistRun(run)
			return run, fmt.Errorf("council run cancelled: %w", err)
		}
		emit("round_started", map[string]any{"round": r + 1})
		round := Round{Index: r + 1, Responses: map[string]string{}}
		responses := o.runRoundWithEvents(runCtx, run.ID, chosen, proposal, run.Rounds, maxPar, emit, r+1)
		round.Responses = responses
		run.Rounds = append(run.Rounds, round)
		emit("round_completed", map[string]any{"round": r + 1})
	}
	if runCtx.Err() == nil {
		emit("synthesis_started", nil)
		run.Consensus, run.Dissent = o.synthesize(runCtx, run)
	}
	run.FinishedAt = time.Now().UTC()
	if run.Cancelled {
		emit("run_cancelled", map[string]any{"finished_at": run.FinishedAt})
	} else {
		emit("run_completed", map[string]any{
			"consensus":   run.Consensus,
			"dissent":     run.Dissent,
			"finished_at": run.FinishedAt,
		})
	}
	if err := o.persistRun(run); err != nil {
		return run, err
	}
	return run, nil
}

func (o *Orchestrator) runRoundWithEvents(ctx context.Context, runID string, chosen []Persona, proposal string, prior []Round, maxPar int, emit func(string, map[string]any), roundNum int) map[string]string {
	type result struct {
		name string
		text string
	}
	out := make(chan result, len(chosen))
	sem := make(chan struct{}, maxPar)
	var wg sync.WaitGroup
	for _, p := range chosen {
		wg.Add(1)
		go func(p Persona) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				out <- result{name: p.Name, text: fmt.Sprintf("[%s] cancelled before start", p.Name)}
				return
			}
			defer func() { <-sem }()
			if emit != nil {
				emit("persona_responding", map[string]any{"round": roundNum, "persona": p.Name})
			}
			// v7.0.0 S4.c — per-persona session record. Daemon's
			// SessionFn creates a virtual transcript entry (default)
			// or spawns a real coding session (operator opt-in).
			var sessID string
			if o.SessionFn != nil && runID != "" {
				prompt := buildPersonaPrompt(p, proposal, prior)
				if id, err := o.SessionFn(runID, p.Name, p.Role, roundNum, prompt, o.SpawnRealSessions); err == nil {
					sessID = id
				}
			}
			text := o.respond(ctx, p, proposal, prior)
			if o.SessionUpdateFn != nil && sessID != "" {
				if strings.HasPrefix(text, "["+p.Name+"] error:") {
					o.SessionUpdateFn(sessID, "", text)
				} else {
					o.SessionUpdateFn(sessID, text, "")
				}
			}
			if emit != nil {
				if strings.HasPrefix(text, "["+p.Name+"] error:") {
					emit("persona_error", map[string]any{"round": roundNum, "persona": p.Name, "error": text, "session_id": sessID})
				} else {
					emit("persona_response", map[string]any{"round": roundNum, "persona": p.Name, "text": text, "session_id": sessID})
				}
			}
			out <- result{name: p.Name, text: text}
		}(p)
	}
	wg.Wait()
	close(out)
	responses := map[string]string{}
	for r := range out {
		responses[r.name] = r.text
	}
	return responses
}

// respond issues one persona-round inference call. v7.0.0 S3 —
// always routes through InferenceFn; stub fallback removed per
// BL295 ASK Q7 (honest 503).
func (o *Orchestrator) respond(ctx context.Context, p Persona, proposal string, prior []Round) string {
	prompt := buildPersonaPrompt(p, proposal, prior)
	text, usedNode, err := o.InferenceFn(ctx, o.LLMRef, p.SystemPrompt, prompt, "council")
	if err != nil {
		return fmt.Sprintf("[%s] error: %s", p.Name, err.Error())
	}
	if usedNode != "" {
		// Lightweight provenance footer — operator can grep "via:
		// gpu-1" to spot Node distribution. Doesn't affect synthesis.
		text = strings.TrimSpace(text) + "\n\n_(via: " + usedNode + ")_"
	}
	return text
}

// buildPersonaPrompt assembles the user-facing prompt for one
// persona-round call. The persona's SystemPrompt is passed
// separately by respond().
func buildPersonaPrompt(p Persona, proposal string, prior []Round) string {
	var sb strings.Builder
	sb.WriteString("Proposal:\n")
	sb.WriteString(proposal)
	sb.WriteString("\n\n")
	if len(prior) > 0 {
		sb.WriteString("Prior rounds:\n")
		for _, r := range prior {
			fmt.Fprintf(&sb, "Round %d:\n", r.Index)
			for name, resp := range r.Responses {
				fmt.Fprintf(&sb, "* %s: %s\n", name, truncatePrior(resp))
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Your role: " + p.Role + "\n")
	sb.WriteString("Respond in 5-10 sentences. Be concrete and reference the proposal's actual surfaces. Highlight your top concern + your top recommendation.")
	return sb.String()
}

func truncatePrior(s string) string {
	const max = 600
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// synthesize produces the (consensus, dissent) text from a Run's
// round responses. v7.0.0 S3 — uses a moderator system_prompt + the
// final-round transcript. Falls back to a deterministic last-round
// concatenation when InferenceFn is nil (defensive — Run() rejects
// nil InferenceFn earlier, but synthesize might be called from
// re-load paths).
func (o *Orchestrator) synthesize(ctx context.Context, run *Run) (consensus, dissent string) {
	if len(run.Rounds) == 0 {
		return "", ""
	}
	last := run.Rounds[len(run.Rounds)-1]
	if o.InferenceFn == nil {
		var sb strings.Builder
		for name, resp := range last.Responses {
			fmt.Fprintf(&sb, "* %s: %s\n", name, resp)
		}
		return "Aggregated last-round persona output (no synthesis LLM):\n" + sb.String(), ""
	}
	var sb strings.Builder
	sb.WriteString("Council debate concluded. Final round:\n\n")
	for name, resp := range last.Responses {
		fmt.Fprintf(&sb, "%s:\n%s\n\n", name, resp)
	}
	systemPrompt := "You are the moderator synthesizing a multi-persona council debate. Produce TWO clearly-labeled sections:\n\n1. CONSENSUS (4-8 sentences) — the points all or most personas converged on. Be specific.\n2. DISSENT (3-6 sentences, may be empty) — the meaningful disagreements that warrant follow-up. If there are none, write 'No material dissent.'."
	moderatorText, _, err := o.InferenceFn(ctx, o.LLMRef, systemPrompt, sb.String(), "council")
	if err != nil {
		return fmt.Sprintf("Synthesis failed: %s\n\nLast-round responses preserved in run.Rounds.", err.Error()), ""
	}
	consensus, dissent = splitConsensusDissent(moderatorText)
	return
}

// splitConsensusDissent slices the moderator output into the two
// labeled sections. Falls back to "everything goes in consensus" when
// the moderator didn't follow the labeling instruction.
func splitConsensusDissent(text string) (string, string) {
	upper := strings.ToUpper(text)
	di := strings.Index(upper, "DISSENT")
	if di < 0 {
		return strings.TrimSpace(text), ""
	}
	return strings.TrimSpace(text[:di]), strings.TrimSpace(text[di:])
}

// RunOptions carries per-run knobs for StartAsyncWithOptions.
type RunOptions struct {
	// SpawnRealSessions toggles between virtual transcript sessions
	// (default, cheap) and real coding-agent sessions (operator
	// opt-in). v7.0.0 S4.c.
	SpawnRealSessions bool
}

// StartAsync is the v7.0.0 S4 async entry — POST /api/council/run
// returns immediately with the pre-allocated run id. The
// orchestrator runs in a goroutine; subscribers connect to
// /api/council/runs/{id}/events (SSE) for live updates. Final state
// is fetched via GET /api/council/runs/{id} after run_completed (or
// run_cancelled) fires.
//
// Returns the run id immediately (or an error for setup failures
// like ErrNoInference / unknown persona).
func (o *Orchestrator) StartAsync(proposal string, names []string, mode Mode) (string, error) {
	return o.StartAsyncWithOptions(proposal, names, mode, RunOptions{})
}

// StartAsyncWithOptions is StartAsync with per-run options.
func (o *Orchestrator) StartAsyncWithOptions(proposal string, names []string, mode Mode, opts RunOptions) (string, error) {
	if o.InferenceFn == nil {
		return "", ErrNoInference
	}
	if strings.TrimSpace(o.LLMRef) == "" {
		return "", fmt.Errorf("%w (cfg.Council.LLMRef is empty)", ErrNoInference)
	}
	// Pre-validate personas synchronously so the caller gets a
	// useful 400 instead of an SSE error event after the goroutine
	// starts.
	o.mu.Lock()
	if len(names) == 0 {
		for n := range o.personas {
			names = append(names, n)
		}
		sort.Strings(names)
	}
	for _, n := range names {
		if _, ok := o.personas[n]; !ok {
			o.mu.Unlock()
			return "", fmt.Errorf("unknown persona %q", n)
		}
	}
	o.mu.Unlock()
	// Pre-allocate id so the caller can subscribe before the
	// goroutine kicks off.
	preID := uuidNewSafe()
	// SpawnRealSessions is read by SessionFn in runRoundWithEvents.
	// Set BEFORE the goroutine starts so the inner closure sees the
	// per-run value. (No mutex — Council runs are not concurrent at
	// the Orchestrator level today; if that changes, move into Run.)
	o.SpawnRealSessions = opts.SpawnRealSessions
	go func() {
		_, _ = o.RunCtxWithID(context.Background(), preID, proposal, names, mode)
	}()
	return preID, nil
}

// RunCtxWithID is RunCtx with an externally-supplied run id.
// Internal — used by StartAsync. Tests should call Run / RunCtx.
func (o *Orchestrator) RunCtxWithID(ctx context.Context, runID, proposal string, names []string, mode Mode) (*Run, error) {
	o.preallocatedRunID = runID
	defer func() { o.preallocatedRunID = "" }()
	return o.RunCtx(ctx, proposal, names, mode)
}

// uuidNewSafe is a thin wrapper so we don't have to add the uuid
// import to package-level (already present via Run() — just reuse).
func uuidNewSafe() string { return uuid.NewString() }

// Cancel signals an in-flight Run to stop. No-op when the run has
// already finished (or was never registered).
func (o *Orchestrator) Cancel(runID string) bool {
	o.mu.Lock()
	cancel, ok := o.cancels[runID]
	o.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
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
