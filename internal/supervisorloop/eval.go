package supervisorloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultEvalCasesDir is where the eval cases of a job live, relative to the
// supervisorloop package: <dir>/<job>/*.json.
const DefaultEvalCasesDir = "internal/supervisorloop/testdata/eval"

// Expected validator outcomes of an eval case.
const (
	EvalExpectPass = "pass"
	EvalExpectFail = "fail"
)

// EvalCase is one case file: the job input, the validator outcome the case
// expects from a model's output, and an optional exact expected value.
type EvalCase struct {
	Name              string   `json:"name"`
	Input             string   `json:"input"`
	Candidates        []string `json:"candidates,omitempty"`
	ExpectedValidator string   `json:"expected_validator"`
	ExpectedValue     string   `json:"expected_value,omitempty"`
}

// EvalModelResult is the outcome of one model over every case.
type EvalModelResult struct {
	Model              string `json:"model"`
	Cases              int    `json:"cases"`
	Passed             int    `json:"passed"`
	ValidatorFailures  int    `json:"validator_failures"`
	Retries            int    `json:"retries"`
	MedianDurationMS   int64  `json:"median_duration_ms"`
	MaxDurationMS      int64  `json:"max_duration_ms"`
	Escalations        int    `json:"escalations"`
	CallErrors         int    `json:"call_errors"`
	Unreachable        bool   `json:"unreachable,omitempty"`
	UnreachableMessage string `json:"error,omitempty"`
}

// EvalReport is what EvalRun returns.
type EvalReport struct {
	Job    string            `json:"job"`
	Models []EvalModelResult `json:"models"`
}

// Evaluator runs the eval harness through an injected ModelClient.
type Evaluator struct {
	Client   ModelClient
	Settings Settings
	// Now supplies time; nil uses time.Now.
	Now func() time.Time
}

func (e *Evaluator) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// EvalValidator returns the validator a model job uses on its output.
func EvalValidator(job string, candidates []string) (Validator, error) {
	switch job {
	case "draft_spec":
		return ValidateSpecDraft, nil
	case "classify_ci_failure":
		return oneOf("class", CIFailureClasses), nil
	case "pick_backend":
		if len(candidates) == 0 {
			// No candidate can be right; every output is rejected.
			return func(out string) error {
				return fmt.Errorf("backend %q is not one of the (empty) candidates", strings.TrimSpace(out))
			}, nil
		}
		return oneOf("backend", candidates), nil
	}
	return nil, fmt.Errorf("unknown model job %q (use one of %s)", job, strings.Join(ModelJobNames, ", "))
}

// evalPrompt builds the job's prompt for one case.
func evalPrompt(job string, c EvalCase) string {
	switch job {
	case "draft_spec":
		return DraftSpecPrompt(c.Input)
	case "classify_ci_failure":
		return ClassifyCIFailurePrompt(c.Input)
	}
	return PickBackendPrompt(c.Input, c.Candidates)
}

// ResolveCasesDir turns the optional cases folder of an eval request into the
// folder to read. Empty means DefaultEvalCasesDir. Otherwise the text must name
// a subfolder that already exists directly under DefaultEvalCasesDir: the path
// is built from the directory listing, never from the request text, so a
// request cannot point the read anywhere else.
func ResolveCasesDir(requested string) (string, error) {
	if requested == "" {
		return DefaultEvalCasesDir, nil
	}
	entries, err := os.ReadDir(DefaultEvalCasesDir)
	if err != nil {
		return "", fmt.Errorf("cannot list the eval cases folder: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() == requested {
			return filepath.Join(DefaultEvalCasesDir, e.Name()), nil
		}
	}
	return "", fmt.Errorf("unknown cases set %q: it must name a folder under %s", requested, DefaultEvalCasesDir)
}

// evalJobFolder maps a job name that arrives in a request to the folder name of
// that model job. The name comes from the fixed list in the switch, never from
// the request text, so a request cannot steer the folder out of casesDir.
func evalJobFolder(job string) (string, error) {
	switch job {
	case "draft_spec":
		return "draft_spec", nil
	case "classify_ci_failure":
		return "classify_ci_failure", nil
	case "pick_backend":
		return "pick_backend", nil
	}
	return "", fmt.Errorf("unknown model job %q (use one of %s)", job, strings.Join(ModelJobNames, ", "))
}

// LoadEvalCases reads every *.json file of <casesDir>/<job>, sorted by file name.
func LoadEvalCases(job, casesDir string) ([]EvalCase, error) {
	name, err := evalJobFolder(job)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(casesDir, name)
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no eval cases found in %s", dir)
	}
	sort.Strings(files)
	var cases []EvalCase
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var c EvalCase
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("eval case %s: %w", f, err)
		}
		if c.Name == "" {
			c.Name = strings.TrimSuffix(filepath.Base(f), ".json")
		}
		if c.ExpectedValidator == "" {
			c.ExpectedValidator = EvalExpectPass
		}
		if c.ExpectedValidator != EvalExpectPass && c.ExpectedValidator != EvalExpectFail {
			return nil, fmt.Errorf("eval case %s: expected_validator must be %q or %q", f, EvalExpectPass, EvalExpectFail)
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// EvalRun runs the job's validator and model call for every case and every
// listed model. It returns an error naming each model when none is reachable;
// it never invents a result for a model that did not answer.
func (e *Evaluator) EvalRun(ctx context.Context, job string, models []string, casesDir string) (*EvalReport, error) {
	if len(models) == 0 {
		return nil, errors.New("at least one model is required")
	}
	if e.Client == nil {
		return nil, errors.New("no model client configured")
	}
	if _, err := EvalValidator(job, []string{"x"}); err != nil {
		return nil, err
	}
	if casesDir == "" {
		casesDir = DefaultEvalCasesDir
	}
	cases, err := LoadEvalCases(job, casesDir)
	if err != nil {
		return nil, err
	}
	rep := &EvalReport{Job: job}
	var down []string
	for _, m := range models {
		res := e.evalModel(ctx, job, m, cases)
		if res.Unreachable {
			down = append(down, fmt.Sprintf("%s (%s)", m, res.UnreachableMessage))
		}
		rep.Models = append(rep.Models, res)
	}
	if len(down) == len(models) {
		return nil, fmt.Errorf("no listed model is reachable: %s", strings.Join(down, "; "))
	}
	return rep, nil
}

func (e *Evaluator) evalModel(ctx context.Context, job, model string, cases []EvalCase) EvalModelResult {
	set := e.Settings.Jobs[job]
	res := EvalModelResult{Model: model, Cases: len(cases)}
	maxAttempts := 1 + e.Settings.ValidatorRetries
	var durs []int64
	var lastErr string
	for _, c := range cases {
		validate, _ := EvalValidator(job, c.Candidates)
		start := e.now()
		accepted, value, attempts, callErr, escalated := false, "", 0, error(nil), false
		for attempts < maxAttempts {
			out, err := e.Client.Ask(ctx, Request{Style: set.Style, Backend: set.Backend, Model: model, Prompt: evalPrompt(job, c)})
			attempts++
			if err != nil {
				callErr = err
				break
			}
			if verr := validate(out); verr == nil {
				accepted, value = true, strings.TrimSpace(out)
				break
			}
			res.ValidatorFailures++
			if e.Settings.EscalateAfter > 0 && attempts >= e.Settings.EscalateAfter {
				escalated = true
				break
			}
		}
		durs = append(durs, e.now().Sub(start).Milliseconds())
		if attempts > 1 {
			res.Retries += attempts - 1
		}
		if callErr != nil {
			res.CallErrors++
			lastErr = callErr.Error()
			continue
		}
		if escalated {
			res.Escalations++
		}
		want := c.ExpectedValidator == EvalExpectPass
		if accepted == want && (!accepted || c.ExpectedValue == "" || value == c.ExpectedValue) {
			res.Passed++
		}
	}
	if res.CallErrors == len(cases) {
		res.Unreachable, res.UnreachableMessage = true, lastErr
		res.Retries, res.ValidatorFailures, res.MedianDurationMS, res.MaxDurationMS = 0, 0, 0, 0
		return res
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	res.MaxDurationMS = durs[len(durs)-1]
	if n := len(durs); n%2 == 1 {
		res.MedianDurationMS = durs[n/2]
	} else {
		res.MedianDurationMS = (durs[n/2-1] + durs[n/2]) / 2
	}
	return res
}

// RenderEval writes the report as plain text, one line per model.
func RenderEval(w interface{ Write([]byte) (int, error) }, rep *EvalReport) {
	_, _ = fmt.Fprintf(w, "eval job %s\n", rep.Job)
	for _, m := range rep.Models {
		if m.Unreachable {
			_, _ = fmt.Fprintf(w, "  %s: unreachable (%s)\n", m.Model, m.UnreachableMessage)
			continue
		}
		_, _ = fmt.Fprintf(w, "  %s: passed %d/%d, validator failures %d, retries %d, escalations %d, median %dms, max %dms\n",
			m.Model, m.Passed, m.Cases, m.ValidatorFailures, m.Retries, m.Escalations, m.MedianDurationMS, m.MaxDurationMS)
	}
}
