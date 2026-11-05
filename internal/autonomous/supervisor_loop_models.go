// Adapter from the daemon's LLM plumbing to supervisorloop.ModelClient.
// Oneshot calls go through the decision assistant's model call path and its
// timeout (DefaultDecisionModelTimeout); session style starts a long-running
// session through the daemon's existing session start path, supplied as a hook.

package autonomous

import (
	"context"
	"errors"
	"fmt"

	"github.com/dmz006/datawatch/internal/supervisorloop"
)

// SupervisorSessionFn starts a long-running session on backend/model with the
// prompt and returns the session's final answer. The daemon wires it to the
// existing session start path.
type SupervisorSessionFn func(ctx context.Context, backend, model, prompt string) (string, error)

// SupervisorModelClient implements supervisorloop.ModelClient.
type SupervisorModelClient struct {
	// Oneshot is the decision assistant's call path; defaults to the manager's.
	Oneshot func(ctx context.Context, backend, model, prompt string) (string, error)
	// Session starts a session; nil makes session style an error.
	Session SupervisorSessionFn
}

// NewSupervisorModelClient wires oneshot calls to the manager's decision
// assistant call path (callAssistantModel, with its timeout).
func NewSupervisorModelClient(m *Manager, session SupervisorSessionFn) *SupervisorModelClient {
	c := &SupervisorModelClient{Session: session}
	if m != nil {
		c.Oneshot = m.callAssistantModel
	}
	return c
}

// ErrSupervisorNoModel is returned when a request names no backend or model.
var ErrSupervisorNoModel = errors.New("no model configured; not falling back")

// Ask routes by style. A request with no backend and no model is refused: it
// never falls back to Claude or any default.
func (c *SupervisorModelClient) Ask(ctx context.Context, req supervisorloop.Request) (string, error) {
	if req.Backend == "" && req.Model == "" {
		return "", ErrSupervisorNoModel
	}
	switch req.Style {
	case supervisorloop.StyleSession:
		if c.Session == nil {
			return "", errors.New("session style is not wired")
		}
		return c.Session(ctx, req.Backend, req.Model, req.Prompt)
	case supervisorloop.StyleOneshot:
		if c.Oneshot == nil {
			return "", errors.New("no decision assistant model is wired")
		}
		return c.Oneshot(ctx, req.Backend, req.Model, req.Prompt)
	}
	return "", fmt.Errorf("unknown style %q", req.Style)
}

// SetSupervisorModelJobs stores the loop's model jobs so callers (CI triage,
// spec drafting, backend routing) reach them through the Manager.
func (m *Manager) SetSupervisorModelJobs(j *supervisorloop.ModelJobs) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.supervisorModelJobs = j
}

// SupervisorModelJobs returns the wired model jobs, or nil when none.
func (m *Manager) SupervisorModelJobs() *supervisorloop.ModelJobs {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.supervisorModelJobs
}
