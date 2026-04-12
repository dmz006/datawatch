package llm

import (
	"context"
	"testing"
)

type mockBackend struct {
	name    string
	version string
}

func (m *mockBackend) Name() string                  { return m.name }
func (m *mockBackend) Version() string               { return m.version }
func (m *mockBackend) SupportsInteractiveInput() bool { return false }
func (m *mockBackend) Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error {
	return nil
}

func TestRegisterAndGet(t *testing.T) {
	// Save and restore registry
	old := registry
	registry = map[string]Backend{}
	defer func() { registry = old }()

	Register(&mockBackend{name: "test-backend", version: "1.0"})

	b, err := Get("test-backend")
	if err != nil {
		t.Fatal(err)
	}
	if b.Name() != "test-backend" {
		t.Errorf("expected 'test-backend', got %q", b.Name())
	}
}

func TestGet_NotFound(t *testing.T) {
	old := registry
	registry = map[string]Backend{}
	defer func() { registry = old }()

	_, err := Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent backend")
	}
}

func TestNames(t *testing.T) {
	old := registry
	registry = map[string]Backend{}
	defer func() { registry = old }()

	Register(&mockBackend{name: "beta"})
	Register(&mockBackend{name: "alpha"})

	names := Names()
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d", len(names))
	}
	if names[0] != "alpha" {
		t.Errorf("expected sorted: alpha first, got %q", names[0])
	}
}
