package supervisorloop

import "context"

// Kind says whether a job is plain code or a model call.
type Kind string

// Job kinds.
const (
	KindMechanical Kind = "mechanical"
	KindModel      Kind = "model"
)

// Style says how a model job reaches its model.
type Style string

// Job styles.
const (
	StyleOneshot Style = "oneshot"
	StyleSession Style = "session"
)

// Valid reports whether the style is a known one.
func (s Style) Valid() bool { return s == StyleOneshot || s == StyleSession }

// Validator checks a job output; a non-nil error rejects it.
type Validator func(output string) error

// Job is one unit of supervisor work. Style, Backend and Model are chosen per
// job at runtime (see JobSetting).
type Job struct {
	Name    string
	Kind    Kind
	Style   Style
	Backend string
	Model   string
	// Exec runs a mechanical job.
	Exec func(ctx context.Context, input string) (string, error)
	// Validator checks output; nil accepts anything.
	Validator Validator
}

// WithSetting returns a copy of the job using the runtime choices.
func (j Job) WithSetting(s JobSetting) Job {
	j.Style, j.Backend, j.Model = s.Style, s.Backend, s.Model
	return j
}
