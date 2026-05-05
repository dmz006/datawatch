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

// DefaultPersonas returns the 6 built-in PAI-style personas.
//
// These are loaded automatically when ~/.datawatch/council/personas/
// is empty; operators can override or extend by dropping their own
// YAML files.
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
