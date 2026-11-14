// decompose_lint_hook.go — the decompose-time decompose-lint hook and
// the read-only LintPRD dry run.
//
// The hook (decomposeLintHook) runs between a successful decompose
// parse and the shared validateAndStorePlan tail, in both decompose
// paths (Decompose and decomposeStreamingCore — same call, kept in
// sync):
//
//   - off     : complete no-op — no template apply, no lint pass, no
//               record. A PRD decomposed while the check is off is
//               recorded exactly as before this feature.
//   - shadow  : apply task templates (idempotent), run one lint pass,
//               record the findings on the PRD (PRD.DecomposeLint).
//               The pass never blocks: the plan the decomposer wrote
//               is the plan that gets stored.
//   - enforce : as shadow, but while the pass carries block findings
//               and decompose_lint.max_retries budget remains, re-run
//               the decomposer with the current findings fed back into
//               its prompt; each retry result is re-parsed,
//               re-templated and re-linted. When the last retry is
//               spent (or a retry fails), the last good plan is what
//               gets stored, with whatever findings it still carries.
//
// The hook NEVER fails the decompose: findings are attached to the
// stored PRD, never fatal — a decomposition that produced them still
// reaches PRDNeedsReview, carrying its mode, lint-pass count and
// finding set as decompose_lint. A failed retry call, an unparseable
// retry plan or an empty retry plan all simply stop the retry loop
// with the previous plan intact.
//
// Task templates are applied in every recorded mode (shadow and
// enforce), before the first lint pass: ApplyTaskTemplates is
// idempotent, so re-applying it after a retry adds nothing, and the
// pack every planning run starts from is the built-in one merged with
// the operator's autonomous.task_templates (see task_templates.go).
// Mode off applies nothing.
//
// LintPRD is the operator's dry-run counterpart: the same engine
// (EffectiveRules + LintTasks) over a PRD's STORED stories — it
// computes no templates, writes nothing to the PRD and never calls
// the decomposer.
package autonomous

import (
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// decomposeLintSettings is the resolved decompose_lint snapshot for
// one hook run: canonical mode, clamped retry budget and the effective
// rule set the passes run.
type decomposeLintSettings struct {
	mode       string
	maxRetries int
	rules      []Rule
}

// decomposeLintSettingsSnapshot resolves the decompose_lint settings
// in effect (m.cfg.DecomposeLint): the mode canonical (empty — the
// shipped zero value — resolves to "enforce"; a value that fails
// normalization, which config.Load should already have refused but a
// hand-built manager config can carry, resolves to the safe "shadow"
// rather than "enforce"), the retry budget clamped to
// [0, DecomposeLintMaxRetriesCap], and the effective rule set (the
// built-in pack merged with the operator rules, disabled rules
// dropped — EffectiveRules).
func (m *Manager) decomposeLintSettingsSnapshot() decomposeLintSettings {
	m.mu.Lock()
	raw := m.cfg.DecomposeLint
	m.mu.Unlock()
	mode, err := config.NormalizeDecomposeLintMode(raw.Mode)
	if err != nil {
		mode = config.DecomposeLintModeShadow
	}
	return decomposeLintSettings{
		mode:       mode,
		maxRetries: config.NormalizeDecomposeLintMaxRetries(raw.MaxRetries),
		rules:      EffectiveRules(raw),
	}
}

// decomposeLintFeedback renders a finding set as the operator-style
// block appended to a retry attempt's prompt (enforce mode only).
// Block findings drive a retry, so they are rendered alone when any
// is present; a pass blocked by nothing renders its warn set.
// The block is appended AFTER PlanningPrompt's Sprintf formatting, so
// rule messages may carry any text — percent signs included.
func decomposeLintFeedback(findings []LintFinding) string {
	list := findings
	if s := SummaryOf(findings); s.Block > 0 {
		// Zero capacity: append allocates a fresh array so the caller's
		// findings slice (stored on the PRD record) is never mutated.
		list = list[:0:0]
		for _, f := range findings {
			if strings.ToLower(strings.TrimSpace(f.Severity)) == config.DecomposeLintSeverityBlock {
				list = append(list, f)
			}
		}
	}
	var b strings.Builder
	b.WriteString("\n\n--- Plan check findings from the previous plan ---\n")
	b.WriteString("The operator's decompose-lint check flagged the previous plan. " +
		"Re-plan so that every one of the following findings is fixed:\n")
	for _, f := range list {
		fmt.Fprintf(&b, "- [%s] %s (story %s, task %s): %s",
			strings.ToUpper(strings.TrimSpace(f.Severity)), strings.TrimSpace(f.RuleID), f.Story, f.Task, strings.TrimSpace(f.Message))
		if m := strings.TrimSpace(f.Matched); m != "" {
			fmt.Fprintf(&b, " [line: %s]", m)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// decomposeRetryStep re-runs the decomposer with feedback appended to
// the original attempt prompt's spec and returns that attempt's
// parsed outcome (title, raw response, stories). Any error — a failed
// decompose call, an unparseable plan, an empty story set — stops the
// retry loop; the hook keeps the last good plan.
type decomposeRetryStep func(feedback string) (title, raw string, stories []Story, err error)

// decomposeLintHook runs the decompose-lint pass over a freshly
// parsed plan and returns the final plan (title, raw and stories —
// unchanged in off mode; re-templated and possibly replaced by an
// enforce-mode retry otherwise) after attaching the pass record to
// prd. See this file's header for the full contract: off is a
// complete no-op, shadow records without retrying, enforce retries
// within the budget then records what remained — and the hook never
// empties stories, never errors and never fails the decompose.
func (m *Manager) decomposeLintHook(prd *PRD, title, raw string, stories []Story, retry decomposeRetryStep) (string, string, []Story) {
	st := m.decomposeLintSettingsSnapshot()
	if st.mode == config.DecomposeLintModeOff {
		return title, raw, stories
	}
	applyTemplates := func(s []Story) []Story {
		// A freshly parsed plan has no ids yet (the store assigns them
		// after validation), and a template task needs real ids: its own
		// id is built from its story's, and its depends_on names the
		// story's other tasks by id.
		ensurePlanIDs(s)
		s, _ = ApplyTaskTemplates(s, templateFeatureName(prd), prd.ProjectDir, m.decomposeLintTemplateCfg())
		return s
	}
	findings := func(s []Story) []LintFinding {
		f := LintTasksIn(prd.ProjectDir, m.daemonModeStories(s), st.rules)
		if f == nil {
			f = []LintFinding{}
		}
		return f
	}
	stories = applyTemplates(stories)
	findingsSet := findings(stories)
	attempts := 1
	for i := 0; st.mode == config.DecomposeLintModeEnforce && SummaryOf(findingsSet).Block > 0 && i < st.maxRetries; i++ {
		attempts++
		nextTitle, nextRaw, nextStories, rerr := retry(decomposeLintFeedback(findingsSet))
		if rerr != nil || len(nextStories) == 0 {
			// A failed, unparseable or empty retry keeps the last good
			// plan — decompose never fails because of the lint check.
			break
		}
		title, raw, stories = nextTitle, nextRaw, applyTemplates(nextStories)
		findingsSet = findings(stories)
	}
	prd.DecomposeLint = &DecomposeLintRecord{
		Findings: findingsSet,
		Mode:     st.mode,
		Attempts: attempts,
		LintedAt: time.Now(),
	}
	// Best-effort: validateAndStorePlan re-saves the same PRD row
	// moments later on the success path, so this save only makes the
	// record visible earlier (and on retry-terminal paths it is the
	// record's first write).
	_ = m.store.SavePRD(prd)
	return title, raw, stories
}

// decomposeLintTemplateCfg wraps the manager's task-templates mirror
// (m.cfg.TaskTemplates, the config.AutonomousConfig.TaskTemplates
// surface) into the *config.Config ApplyTaskTemplates expects. A zero
// mirror keeps Enabled false, so a template apply the operator has
// not turned on stays a no-op.
func (m *Manager) decomposeLintTemplateCfg() *config.Config {
	m.mu.Lock()
	tt := m.cfg.TaskTemplates
	m.mu.Unlock()
	return &config.Config{Autonomous: config.AutonomousConfig{TaskTemplates: tt}}
}

// retryDecomposeWithFeedback runs one enforce-mode retry attempt: the
// decomposer with the feedback block appended to the original prompt,
// parsed. Shared by both decompose twins' decomposeLintHook call.
func (m *Manager) retryDecomposeWithFeedback(prd *PRD, backend, model string, effort Effort, spec string) (string, string, []Story, error) {
	m.mu.Lock()
	timeout := m.cfg.PlanningTimeoutSeconds
	m.mu.Unlock()
	r, derr := m.decomposeAdmitted(prd, backend, model, DecomposeRequest{
		Spec:           spec,
		Backend:        backend,
		Effort:         effort,
		Model:          model,
		ProjectDir:     prd.ProjectDir,
		ScopeBlock:     ScopeBlock(prd),
		PRDID:          prd.ID,
		TimeoutSeconds: timeout,
	})
	if derr != nil {
		return "", "", nil, derr
	}
	t, s, perr := ParsePlanning(r)
	if perr != nil {
		return "", r, nil, perr
	}
	return t, r, s, nil
}

// LintPRD is the read-only decompose-lint dry run for one PRD: the
// same engine the decompose-time hook uses (EffectiveRules + LintTasks)
// over the PRD's STORED stories. It never mutates the PRD, applies no
// templates and never calls the decomposer — it answers "what would
// this check record right now", in whatever mode is configured (in
// off mode the report describes what a run would record).
func (m *Manager) LintPRD(prdID string) (*DecomposeLintReport, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("lint: prd %q not found", prdID)
	}
	st := m.decomposeLintSettingsSnapshot()
	findings := LintTasksIn(prd.ProjectDir, m.daemonModeStories(prd.Story), st.rules)
	if findings == nil {
		findings = []LintFinding{}
	}
	return &DecomposeLintReport{
		PRD:       prd.ID,
		Mode:      st.mode,
		RuleCount: len(st.rules),
		Findings:  findings,
		Summary:   SummaryOf(findings),
	}, nil
}

// templateFeatureName is what the task templates call the work: the
// Automaton's title, or the first line of its spec (bounded) when it has no
// title. The whole spec is never used, so words in a long spec cannot leak
// into the template task text and trip the plan checks.
func templateFeatureName(prd *PRD) string {
	name := strings.TrimSpace(prd.Title)
	if name == "" {
		name = strings.TrimSpace(strings.SplitN(strings.TrimSpace(prd.Spec), "\n", 2)[0])
	}
	const maxName = 120
	if r := []rune(name); len(r) > maxName {
		name = string(r[:maxName])
	}
	return name
}

// ensurePlanIDs gives every story and task of a parsed plan that has no id
// one, in place. The store keeps ids that are already set, so assigning them
// early changes nothing for the planner's own tasks and lets the task
// templates build ids and dependencies from real values.
func ensurePlanIDs(stories []Story) {
	for i := range stories {
		if stories[i].ID == "" {
			stories[i].ID = newID()
		}
		for j := range stories[i].Tasks {
			if stories[i].Tasks[j].ID == "" {
				stories[i].Tasks[j].ID = newID()
			}
		}
	}
}
