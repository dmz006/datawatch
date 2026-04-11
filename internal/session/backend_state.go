package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BackendState persists LLM backend connection state so sessions can be
// reconnected after a daemon restart. Stored as backend_state.json in the
// session tracking directory.
type BackendState struct {
	// Backend name (e.g., "opencode-acp", "ollama", "openwebui")
	Backend string `json:"backend"`

	// ACP-specific
	ACPBaseURL   string `json:"acp_base_url,omitempty"`
	ACPSessionID string `json:"acp_session_id,omitempty"`
	ACPPort      int    `json:"acp_port,omitempty"`

	// Ollama-specific
	OllamaHost  string `json:"ollama_host,omitempty"`
	OllamaModel string `json:"ollama_model,omitempty"`

	// OpenWebUI-specific
	OpenWebUIBaseURL string `json:"openwebui_base_url,omitempty"`
	OpenWebUIModel   string `json:"openwebui_model,omitempty"`
	OpenWebUIAPIKey  string `json:"openwebui_api_key,omitempty"`

	// Conversation history (Ollama, OpenWebUI)
	ConversationHistory []ChatMessage `json:"conversation_history,omitempty"`
}

// ChatMessage is a role+content pair for conversation history persistence.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const backendStateFile = "backend_state.json"

// SaveBackendState writes the backend state to the session's tracking directory.
func SaveBackendState(trackingDir string, state *BackendState) error {
	if trackingDir == "" {
		return nil
	}
	path := filepath.Join(trackingDir, backendStateFile)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal backend state: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadBackendState reads backend state from the session's tracking directory.
// Returns nil, nil if the file doesn't exist.
func LoadBackendState(trackingDir string) (*BackendState, error) {
	if trackingDir == "" {
		return nil, nil
	}
	path := filepath.Join(trackingDir, backendStateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read backend state: %w", err)
	}
	var state BackendState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse backend state: %w", err)
	}
	return &state, nil
}

// RemoveBackendState deletes the backend state file.
func RemoveBackendState(trackingDir string) {
	if trackingDir == "" {
		return
	}
	os.Remove(filepath.Join(trackingDir, backendStateFile)) //nolint:errcheck
}
