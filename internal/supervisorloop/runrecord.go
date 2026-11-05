package supervisorloop

import "time"

// Outcome is the result of a job run.
type Outcome string

// Outcomes.
const (
	OutcomeOK              Outcome = "ok"
	OutcomeValidatorFailed Outcome = "validator_failed"
	OutcomeEscalated       Outcome = "escalated"
	// OutcomeError is a job whose Exec or model call itself failed.
	OutcomeError Outcome = "error"
)

// RunRecord is one persisted job run.
type RunRecord struct {
	Job             string   `json:"job"`
	Style           Style    `json:"style"`
	Backend         string   `json:"backend"`
	Model           string   `json:"model"`
	Outcome         Outcome  `json:"outcome"`
	Attempts        int      `json:"attempts"`
	ValidatorErrors []string `json:"validator_errors,omitempty"`
	// Detail is a short plain-language account of what the run did or why it failed.
	Detail   string        `json:"detail,omitempty"`
	Started  time.Time     `json:"started"`
	Finished time.Time     `json:"finished"`
	Duration time.Duration `json:"duration"`
}
