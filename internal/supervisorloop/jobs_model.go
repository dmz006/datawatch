package supervisorloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// CIFailureClasses is the fixed list classify_ci_failure chooses from.
var CIFailureClasses = []string{"infra", "flaky_test", "test_failure", "build_error", "lint", "dependency", "unknown"}

// ErrNoModel is returned when a model job has no backend or model configured.
// The job escalates at once; it never falls back to another model.
var ErrNoModel = errors.New("no model configured for job")

// trackerIDRe matches tracker identifiers such as BL123, GH#45, B87 or PRD-3.
var trackerIDRe = regexp.MustCompile(`(?i)\b(BL|GH#?|B|PRD-?)\d+\b`)

// SpecDraft is the decompose-schema JSON draft_spec returns.
type SpecDraft struct {
	Stories []SpecStory `json:"stories"`
}

// SpecStory is one story of a draft.
type SpecStory struct {
	Name  string     `json:"name"`
	Tasks []SpecTask `json:"tasks"`
}

// SpecTask is one task of a draft.
type SpecTask struct {
	Name  string   `json:"name"`
	Spec  string   `json:"spec"`
	Files []string `json:"files"`
}

// ValidateSpecDraft checks the output parses as the schema, every task has a
// spec and a files list, and no tracker ID appears in a name.
func ValidateSpecDraft(output string) error {
	var d SpecDraft
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(output)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return fmt.Errorf("not valid decompose JSON: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after the decompose JSON")
	}
	if len(d.Stories) == 0 {
		return errors.New("no stories")
	}
	for _, s := range d.Stories {
		if strings.TrimSpace(s.Name) == "" {
			return errors.New("story without a name")
		}
		if trackerIDRe.MatchString(s.Name) {
			return fmt.Errorf("tracker ID in story name %q", s.Name)
		}
		if len(s.Tasks) == 0 {
			return fmt.Errorf("story %q has no tasks", s.Name)
		}
		for _, t := range s.Tasks {
			if strings.TrimSpace(t.Name) == "" {
				return fmt.Errorf("task without a name in story %q", s.Name)
			}
			if trackerIDRe.MatchString(t.Name) {
				return fmt.Errorf("tracker ID in task name %q", t.Name)
			}
			if strings.TrimSpace(t.Spec) == "" {
				return fmt.Errorf("task %q has no spec", t.Name)
			}
			if len(t.Files) == 0 {
				return fmt.Errorf("task %q has no files list", t.Name)
			}
		}
	}
	return nil
}

// oneOf returns a validator accepting exactly one of the choices.
func oneOf(what string, choices []string) Validator {
	return func(out string) error {
		got := strings.TrimSpace(out)
		for _, c := range choices {
			if got == c {
				return nil
			}
		}
		return fmt.Errorf("%s %q is not one of %s", what, got, strings.Join(choices, ", "))
	}
}

// ModelJobs builds and runs the three model jobs from settings.
type ModelJobs struct {
	Engine *Engine
	// SettingsFn, when set, supplies the settings live on every call; nil
	// uses Engine.Settings.
	SettingsFn func() Settings
}

// engine returns the engine for one call, with live settings applied.
func (m *ModelJobs) engine() *Engine {
	if m.SettingsFn == nil {
		return m.Engine
	}
	e := *m.Engine
	e.Settings = m.SettingsFn()
	return &e
}

// DraftSpecPrompt builds the draft_spec prompt around a quoted operator answer.
func DraftSpecPrompt(answer string) string {
	return "Turn the operator's answer, quoted below, into a decompose-schema JSON object and reply with the JSON only.\n" +
		`Schema: {"stories":[{"name":"...","tasks":[{"name":"...","spec":"...","files":["path"]}]}]}` + "\n" +
		"Every task needs a non-empty spec and a files list. Keep tasks atomic. " +
		"Do not put tracker IDs (BL123, GH#45, B87) in any name.\n" +
		"Operator answer:\n> " + strings.ReplaceAll(strings.TrimSpace(answer), "\n", "\n> ")
}

// ClassifyCIFailurePrompt builds the classify_ci_failure prompt.
func ClassifyCIFailurePrompt(log string) string {
	return "Classify the CI failure log below into exactly one class from this list: " +
		strings.Join(CIFailureClasses, ", ") + ".\nReply with the class name only.\nLog:\n" + log
}

// PickBackendPrompt builds the pick_backend prompt.
func PickBackendPrompt(rule string, candidates []string) string {
	return "Routing rule: " + rule + "\nChoose exactly one backend from these candidates: " +
		strings.Join(candidates, ", ") + ".\nReply with the backend name only."
}

func (m *ModelJobs) job(name string, v Validator) Job {
	set := m.engine().Settings.Jobs[name]
	return Job{Name: name, Kind: KindModel, Validator: v}.WithSetting(set)
}

func (m *ModelJobs) run(ctx context.Context, j Job, prompt string) (string, error) {
	if j.Backend == "" || j.Model == "" {
		return "", m.refuse(ctx, j)
	}
	return m.engine().Run(ctx, j, prompt)
}

// refuse escalates a job that has no model; nothing is called.
func (m *ModelJobs) refuse(ctx context.Context, j Job) error {
	e := m.engine()
	rec := RunRecord{Job: j.Name, Style: j.Style, Started: e.Clock.Now(), Outcome: OutcomeEscalated, Detail: ErrNoModel.Error()}
	rec.Finished = e.Clock.Now()
	rec.ValidatorErrors = []string{ErrNoModel.Error()}
	if e.Store != nil {
		_ = e.Store.Append(rec)
	}
	if e.Escalator != nil {
		if err := e.Escalator.Escalate(ctx, Escalation{Job: j.Name, Attempts: []Attempt{{ValidatorError: ErrNoModel.Error()}}}); err != nil {
			return fmt.Errorf("%w: %v", ErrEscalated, err)
		}
	}
	return fmt.Errorf("%w: %w", ErrEscalated, ErrNoModel)
}

// DraftSpec turns a quoted operator answer into validated decompose JSON.
func (m *ModelJobs) DraftSpec(ctx context.Context, answer string) (string, error) {
	return m.run(ctx, m.job("draft_spec", ValidateSpecDraft), DraftSpecPrompt(answer))
}

// ClassifyCIFailure returns one class from CIFailureClasses.
func (m *ModelJobs) ClassifyCIFailure(ctx context.Context, log string) (string, error) {
	out, err := m.run(ctx, m.job("classify_ci_failure", oneOf("class", CIFailureClasses)), ClassifyCIFailurePrompt(log))
	return strings.TrimSpace(out), err
}

// PickBackend returns one of candidates.
func (m *ModelJobs) PickBackend(ctx context.Context, rule string, candidates []string) (string, error) {
	if len(candidates) == 0 {
		return "", errors.New("pick_backend: no candidates")
	}
	out, err := m.run(ctx, m.job("pick_backend", oneOf("backend", candidates)), PickBackendPrompt(rule, candidates))
	return strings.TrimSpace(out), err
}
