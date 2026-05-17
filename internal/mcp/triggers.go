// Package mcp — BL302 S3: built-in sampling trigger registry.
//
// TriggerRegistry maps named trigger constants to their default prompt
// templates.  Subsystems (alert handler, gap-watcher, Council, automata)
// call SamplingDispatcher.Sample with one of these trigger names so the
// log viewer can identify the source.
//
// Templates use Go text/template syntax.  Available variables vary by
// trigger; see each template's comment for the expected context struct.
package mcp

// Built-in trigger name constants.
const (
	// TriggerAlertTriage fires when an alert with severity ≥ WARNING is created.
	// Template vars: .Alert (string summary).
	TriggerAlertTriage = "alert_triage"

	// TriggerAnomalyAnalysis fires when the gap-watcher detects an anomalous
	// session inactivity period.
	// Template vars: .SessionID (string), .Gap (duration string).
	TriggerAnomalyAnalysis = "anomaly_analysis"

	// TriggerMorningBriefing fires on the operator's configured daily briefing cron.
	// Template vars: .Sessions (int), .Alerts (int), .Memory (string summary).
	TriggerMorningBriefing = "morning_briefing"

	// TriggerCouncilDeliberation fires when Council `include_claude_code: true`
	// is set and a deliberation round begins.
	// Template vars: .Topic (string), .Personas ([]string).
	TriggerCouncilDeliberation = "council_deliberation"

	// TriggerAutomatonDecision fires when an automaton story hits a
	// per_story_approval gate and sampling is preferred over elicitation.
	// Template vars: .Name (automaton name), .Story (string).
	TriggerAutomatonDecision = "automaton_decision"
)

// TriggerTemplate holds the default prompt template for a named trigger.
type TriggerTemplate struct {
	// Name is the trigger constant (e.g. TriggerAlertTriage).
	Name string
	// PromptTemplate is the Go text/template string.
	PromptTemplate string
	// DefaultMaxTokens is the recommended max_tokens for this trigger.
	DefaultMaxTokens int
}

// TriggerRegistry maps trigger names to their default templates.
type TriggerRegistry struct {
	templates map[string]TriggerTemplate
}

// NewTriggerRegistry creates a registry pre-loaded with all five built-in triggers.
func NewTriggerRegistry() *TriggerRegistry {
	r := &TriggerRegistry{
		templates: make(map[string]TriggerTemplate),
	}
	r.registerBuiltins()
	return r
}

// Register adds or replaces a trigger template.
func (r *TriggerRegistry) Register(t TriggerTemplate) {
	r.templates[t.Name] = t
}

// Get returns the TriggerTemplate for name, or (zero, false) if not found.
func (r *TriggerRegistry) Get(name string) (TriggerTemplate, bool) {
	t, ok := r.templates[name]
	return t, ok
}

// Names returns all registered trigger names.
func (r *TriggerRegistry) Names() []string {
	out := make([]string, 0, len(r.templates))
	for k := range r.templates {
		out = append(out, k)
	}
	return out
}

func (r *TriggerRegistry) registerBuiltins() {
	builtins := []TriggerTemplate{
		{
			Name: TriggerAlertTriage,
			PromptTemplate: `New alert: {{.Alert}}
Classify severity, identify likely root cause, and recommend a concrete remediation action.
Be concise — one paragraph maximum.`,
			DefaultMaxTokens: 512,
		},
		{
			Name: TriggerAnomalyAnalysis,
			PromptTemplate: `Session {{.SessionID}} has been inactive for {{.Gap}}.
Is this expected for the current task? If not, recommend an action (check session, restart, escalate).
Be concise — one paragraph maximum.`,
			DefaultMaxTokens: 256,
		},
		{
			Name: TriggerMorningBriefing,
			PromptTemplate: `Generate a morning briefing.
Active sessions: {{.Sessions}}.
Open alerts: {{.Alerts}}.
Memory highlights since yesterday: {{.Memory}}.
Summarize key items and suggest today's priorities. Keep it under 200 words.`,
			DefaultMaxTokens: 512,
		},
		{
			Name: TriggerCouncilDeliberation,
			PromptTemplate: `You are a deliberation participant in the Council.
Topic: {{.Topic}}
Other personas: {{.Personas}}
Provide your perspective on the topic as a thoughtful AI assistant.
Be concise and focus on novel insights not covered by other personas.`,
			DefaultMaxTokens: 768,
		},
		{
			Name: TriggerAutomatonDecision,
			PromptTemplate: `Automaton "{{.Name}}" has reached a decision gate.
Story at gate: {{.Story}}
Should this automaton proceed (approve), stop (reject), or be modified?
Respond with a clear recommendation and brief rationale (2–3 sentences).`,
			DefaultMaxTokens: 256,
		},
	}
	for _, t := range builtins {
		r.templates[t.Name] = t
	}
}

// GlobalTriggerRegistry is the package-level registry populated at startup.
// It is safe to read concurrently; all writes happen at init time.
var GlobalTriggerRegistry = NewTriggerRegistry()
