package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
)

// The dependency update status tool is read-only: the three settings of
// the weekly dependency update Automation as they are in effect, the next
// scheduled run in the daemon's local time, and the last run (when, which
// Automaton, and the outcome).

// Outcomes a last run can report.
const (
	DependencyUpdateOutcomeUpdated        = "updated"
	DependencyUpdateOutcomeNothingToDo    = "nothing to update"
	DependencyUpdateOutcomeNeedsAttention = "needs attention"
)

// DependencyUpdateLastRun is the last-run record the weekly pass reports.
type DependencyUpdateLastRun struct {
	// At is when the run happened; the zero time means no run yet.
	At time.Time
	// AutomatonTitle is the title of the Automaton the run created.
	AutomatonTitle string
	// Outcome is one of the DependencyUpdateOutcome* values.
	Outcome string
}

// DependencyUpdateStateReader is satisfied by the weekly pass: it returns
// the state of its last run.
type DependencyUpdateStateReader interface {
	LastDependencyUpdateRun() DependencyUpdateLastRun
}

// SetDependencyUpdateState attaches the weekly pass's state accessor.
// While none is attached the last-run fields are empty.
func (s *Server) SetDependencyUpdateState(r DependencyUpdateStateReader) {
	s.dependencyUpdateState = r
}

type dependencyUpdateStatusBody struct {
	Enabled   bool   `json:"enabled"`
	Schedule  string `json:"schedule"`
	MaxMajors int    `json:"max_majors"`
	// NextRun is the next scheduled run in local time; empty when the
	// Automation is off or the schedule cannot be read.
	NextRun               string `json:"next_run"`
	LastRunAt             string `json:"last_run_at"`
	LastRunAutomatonTitle string `json:"last_run_automaton_title"`
	LastRunOutcome        string `json:"last_run_outcome"`
}

func (s *Server) toolDependencyUpdateStatus() mcpsdk.Tool {
	return mcpsdk.NewTool("dependency_update_status",
		mcpsdk.WithDescription("Read the weekly dependency update Automation status: the three settings as they are in effect (\"enabled\", \"schedule\" as 'weekday HH:MM', \"max_majors\"), the next scheduled run in local time (\"next_run\", empty while the Automation is off), the time and Automaton title of the last run (\"last_run_at\", \"last_run_automaton_title\") and its outcome (\"last_run_outcome\": \"updated\", \"nothing to update\" or \"needs attention\"). The last-run fields are empty until a run has happened. Read-only; nothing is run, changed or scheduled."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

// DependencyUpdateToolNames lists the dependency update tools, for the parity test.
func DependencyUpdateToolNames() []string {
	return []string{"dependency_update_status"}
}

// nextDependencyUpdateRun returns the first instant after now matching the
// 'weekday HH:MM' schedule, in now's location.
func nextDependencyUpdateRun(schedule string, now time.Time) (time.Time, bool) {
	wd, h, m, err := config.ParseDependencyUpdateSchedule(schedule)
	if err != nil {
		return time.Time{}, false
	}
	for i := 0; i <= 7; i++ {
		d := now.AddDate(0, 0, i)
		cand := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, now.Location())
		if strings.ToLower(cand.Weekday().String()) == wd && cand.After(now) {
			return cand, true
		}
	}
	return time.Time{}, false
}

func buildDependencyUpdateStatus(cfg config.DependencyUpdateConfig, st DependencyUpdateStateReader, now time.Time) dependencyUpdateStatusBody {
	now = now.Local()
	body := dependencyUpdateStatusBody{
		Enabled:   cfg.EffectiveEnabled(),
		Schedule:  cfg.EffectiveSchedule(),
		MaxMajors: cfg.EffectiveMaxMajors(),
	}
	if body.Enabled {
		if next, ok := nextDependencyUpdateRun(body.Schedule, now); ok {
			body.NextRun = next.Format(time.RFC3339)
		}
	}
	if st != nil {
		if lr := st.LastDependencyUpdateRun(); !lr.At.IsZero() {
			body.LastRunAt = lr.At.Local().Format(time.RFC3339)
			body.LastRunAutomatonTitle = lr.AutomatonTitle
			body.LastRunOutcome = lr.Outcome
		}
	}
	return body
}

func (s *Server) handleDependencyUpdateStatus(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	var cfg config.DependencyUpdateConfig
	if s.dependencyUpdateCfg != nil {
		cfg = s.dependencyUpdateCfg()
	}
	out, err := json.Marshal(buildDependencyUpdateStatus(cfg, s.dependencyUpdateState, time.Now()))
	if err != nil {
		return toolError("dependency_update_status", err), nil
	}
	return textOK(string(out)), nil
}
