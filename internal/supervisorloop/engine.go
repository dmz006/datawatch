package supervisorloop

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Request is one model call.
type Request struct {
	Style   Style
	Backend string
	Model   string
	Prompt  string
}

// ModelClient talks to a model, one-shot or via a session.
type ModelClient interface {
	Ask(ctx context.Context, req Request) (string, error)
}

// Attempt is one try of a job with the validator's verdict.
type Attempt struct {
	Output         string
	ValidatorError string
}

// Escalation is what the operator is shown instead of the loop acting.
type Escalation struct {
	Job      string
	Attempts []Attempt
}

// Summary renders the attempts as text.
func (e Escalation) Summary() string {
	s := fmt.Sprintf("job %s failed %d attempts", e.Job, len(e.Attempts))
	for i, a := range e.Attempts {
		s += fmt.Sprintf("\nattempt %d: output=%q validator_error=%q", i+1, a.Output, a.ValidatorError)
	}
	return s
}

// Escalator asks the operator for help.
type Escalator interface {
	Escalate(ctx context.Context, e Escalation) error
}

// Clock supplies time.
type Clock interface{ Now() time.Time }

// Store persists run records.
type Store interface {
	Append(RunRecord) error
	List() ([]RunRecord, error)
}

// Engine runs jobs.
type Engine struct {
	Settings  Settings
	Client    ModelClient
	Escalator Escalator
	Clock     Clock
	Store     Store
}

// ErrEscalated is returned when the job was handed to the Escalator.
var ErrEscalated = errors.New("escalated to operator")

// ErrValidatorFailed is returned when retries ran out without escalation.
var ErrValidatorFailed = errors.New("validator rejected every attempt")

// Run executes the job on input. Output that fails the validator is re-asked
// up to ValidatorRetries times; after EscalateAfter failed attempts the
// Escalator is called with an attempt summary and the output is not returned.
func (e *Engine) Run(ctx context.Context, job Job, input string) (string, error) {
	rec := RunRecord{Job: job.Name, Style: job.Style, Backend: job.Backend, Model: job.Model, Started: e.Clock.Now()}
	var attempts []Attempt
	out, err := e.loop(ctx, job, input, &rec, &attempts)
	rec.Finished = e.Clock.Now()
	rec.Duration = rec.Finished.Sub(rec.Started)
	rec.Attempts = len(attempts)
	if e.Store != nil {
		if serr := e.Store.Append(rec); serr != nil && err == nil {
			err = serr
		}
	}
	return out, err
}

func (e *Engine) loop(ctx context.Context, job Job, input string, rec *RunRecord, attempts *[]Attempt) (string, error) {
	maxAttempts := 1 + e.Settings.ValidatorRetries
	for len(*attempts) < maxAttempts {
		out, err := e.exec(ctx, job, input)
		if err != nil {
			*attempts = append(*attempts, Attempt{Output: out, ValidatorError: err.Error()})
			rec.ValidatorErrors = append(rec.ValidatorErrors, err.Error())
			rec.Outcome = OutcomeError
			rec.Detail = err.Error()
			return "", err
		}
		var verr error
		if job.Validator != nil {
			verr = job.Validator(out)
		}
		if verr == nil {
			*attempts = append(*attempts, Attempt{Output: out})
			rec.Outcome = OutcomeOK
			rec.Detail = out
			return out, nil
		}
		*attempts = append(*attempts, Attempt{Output: out, ValidatorError: verr.Error()})
		rec.ValidatorErrors = append(rec.ValidatorErrors, verr.Error())
		if job.Kind != KindModel {
			break
		}
		if len(*attempts) >= e.Settings.EscalateAfter {
			rec.Outcome = OutcomeEscalated
			if e.Escalator != nil {
				if eerr := e.Escalator.Escalate(ctx, Escalation{Job: job.Name, Attempts: *attempts}); eerr != nil {
					return "", fmt.Errorf("%w: %v", ErrEscalated, eerr)
				}
			}
			return "", ErrEscalated
		}
	}
	rec.Outcome = OutcomeValidatorFailed
	return "", ErrValidatorFailed
}

func (e *Engine) exec(ctx context.Context, job Job, input string) (string, error) {
	if job.Kind == KindMechanical {
		if job.Exec == nil {
			return "", errors.New("mechanical job has no Exec")
		}
		return job.Exec(ctx, input)
	}
	if e.Client == nil {
		return "", errors.New("no model client configured")
	}
	return e.Client.Ask(ctx, Request{Style: job.Style, Backend: job.Backend, Model: job.Model, Prompt: input})
}
