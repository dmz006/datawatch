package pipeline

import (
	"context"

	"github.com/dmz006/datawatch/internal/session"
)

// ManagerAdapter wraps session.Manager to implement SessionStarter.
type ManagerAdapter struct {
	mgr     *session.Manager
	backend string
}

// NewManagerAdapter creates an adapter for the session manager.
func NewManagerAdapter(mgr *session.Manager, defaultBackend string) *ManagerAdapter {
	return &ManagerAdapter{mgr: mgr, backend: defaultBackend}
}

func (a *ManagerAdapter) StartSession(task, projectDir, backend string) (string, error) {
	if backend == "" {
		backend = a.backend
	}
	opts := &session.StartOptions{Backend: backend}
	sess, err := a.mgr.Start(context.Background(), task, "", projectDir, opts)
	if err != nil {
		return "", err
	}
	return sess.FullID, nil
}

func (a *ManagerAdapter) GetSessionState(sessionID string) string {
	sess, ok := a.mgr.GetSession(sessionID)
	if !ok {
		return "unknown"
	}
	return string(sess.State)
}
