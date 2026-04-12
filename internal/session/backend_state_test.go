package session

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadBackendState(t *testing.T) {
	dir := t.TempDir()
	state := &BackendState{
		Backend:    "ollama",
		OllamaHost: "http://localhost:11434",
		OllamaModel: "llama3",
		ConversationHistory: []ChatMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		},
	}
	if err := SaveBackendState(dir, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadBackendState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Backend != "ollama" {
		t.Errorf("expected 'ollama', got %q", loaded.Backend)
	}
	if loaded.OllamaHost != "http://localhost:11434" {
		t.Errorf("expected host, got %q", loaded.OllamaHost)
	}
	if len(loaded.ConversationHistory) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded.ConversationHistory))
	}
	if loaded.ConversationHistory[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", loaded.ConversationHistory[0].Role)
	}
}

func TestLoadBackendState_NotExists(t *testing.T) {
	dir := t.TempDir()
	state, err := LoadBackendState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Error("expected nil for non-existent state")
	}
}

func TestLoadBackendState_EmptyDir(t *testing.T) {
	state, err := LoadBackendState("")
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Error("expected nil for empty dir")
	}
}

func TestRemoveBackendState(t *testing.T) {
	dir := t.TempDir()
	SaveBackendState(dir, &BackendState{Backend: "test"})

	RemoveBackendState(dir)

	state, _ := LoadBackendState(dir)
	if state != nil {
		t.Error("expected nil after remove")
	}
}

func TestSaveBackendState_ACPFields(t *testing.T) {
	dir := t.TempDir()
	state := &BackendState{
		Backend:      "opencode-acp",
		ACPBaseURL:   "http://localhost:54321",
		ACPSessionID: "sess-abc123",
		ACPPort:      54321,
	}
	SaveBackendState(dir, state)
	loaded, _ := LoadBackendState(dir)
	if loaded.ACPBaseURL != "http://localhost:54321" {
		t.Errorf("expected ACP URL, got %q", loaded.ACPBaseURL)
	}
	if loaded.ACPSessionID != "sess-abc123" {
		t.Errorf("expected ACP session ID, got %q", loaded.ACPSessionID)
	}
}

func TestSaveBackendState_OpenWebUIFields(t *testing.T) {
	dir := t.TempDir()
	state := &BackendState{
		Backend:          "openwebui",
		OpenWebUIBaseURL: "http://localhost:3000",
		OpenWebUIModel:   "llama3",
		OpenWebUIAPIKey:  "sk-test",
	}
	SaveBackendState(dir, state)
	loaded, _ := LoadBackendState(dir)
	if loaded.OpenWebUIBaseURL != "http://localhost:3000" {
		t.Errorf("expected URL, got %q", loaded.OpenWebUIBaseURL)
	}
}

func TestBackendStateFile(t *testing.T) {
	dir := t.TempDir()
	SaveBackendState(dir, &BackendState{Backend: "test"})
	path := filepath.Join(dir, backendStateFile)
	if backendStateFile != "backend_state.json" {
		t.Errorf("expected 'backend_state.json', got %q", backendStateFile)
	}
	_ = path
}
