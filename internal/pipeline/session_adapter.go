package pipeline

import (
	"context"

	"github.com/dmz006/datawatch/internal/session"
)

// KindResolver maps a named LLM to its adapter kind.
// The inference.Registry satisfies this interface so the pipeline executor
// can resolve named LLMs (e.g. "ollama-datawatch" → "ollama") without a
// direct import of the inference package.
type KindResolver interface {
	ResolveKind(name string) (string, bool)
}

// ManagerAdapter wraps session.Manager to implement SessionStarter.
type ManagerAdapter struct {
	mgr      *session.Manager
	backend  string
	resolver KindResolver // optional; resolves named LLMs → adapter kind
}

// NewManagerAdapter creates an adapter for the session manager.
func NewManagerAdapter(mgr *session.Manager, defaultBackend string) *ManagerAdapter {
	return &ManagerAdapter{mgr: mgr, backend: defaultBackend}
}

// SetKindResolver wires an inference registry so that named LLMs
// (e.g. "ollama-datawatch") are resolved to their adapter kind
// before being passed to the session manager.
func (a *ManagerAdapter) SetKindResolver(r KindResolver) { a.resolver = r }

func (a *ManagerAdapter) StartSession(task, projectDir, backend string) (string, error) {
	if backend == "" {
		backend = a.backend
	}
	// BL309 — if the backend name is a named LLM from the inference registry
	// (not a raw adapter kind), resolve it to its kind so the session manager
	// can find the correct launch function via llm.Get.
	if a.resolver != nil {
		if kind, ok := a.resolver.ResolveKind(backend); ok {
			backend = kind
		}
	}
	opts := &session.StartOptions{Backend: backend, OneShot: true}
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
