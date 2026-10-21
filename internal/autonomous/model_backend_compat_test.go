package autonomous

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateModelForBackend(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		model   string
		wantErr bool
	}{
		{name: "empty model is valid", backend: "pi", model: "", wantErr: false},
		{name: "empty backend accepts a Claude alias", backend: "", model: "claude-haiku-5-5", wantErr: false},
		{name: "pi refuses bare Claude alias", backend: "pi", model: "claude-haiku-5-5", wantErr: true},
		{name: "pi-prompt refuses bare Claude alias", backend: "pi-prompt", model: "claude-sonnet-5-5", wantErr: true},
		{name: "pi accepts provider-qualified Claude", backend: "pi", model: "anthropic/claude-haiku-5-5", wantErr: false},
		{name: "pi accepts provider-qualified ollama model", backend: "pi", model: "ollama/qwen3.8:27b", wantErr: false},
		{name: "claude-code accepts bare Claude model", backend: "claude-code", model: "claude-opus-5-5", wantErr: false},
		{name: "claude-code refuses provider-qualified model", backend: "claude-code", model: "ollama/qwen3.8:27b", wantErr: true},
		{name: "ollama accepts Claude-looking name", backend: "ollama", model: "claude-haiku-5-5", wantErr: false},
		{name: "unrelated backend accepts provider-qualified name", backend: "opencode", model: "ollama/qwen3.8:27b", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModelForBackend(tt.backend, tt.model)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateModelForBackend(%q, %q) error = %v, wantErr %v", tt.backend, tt.model, err, tt.wantErr)
			}
			if err == nil {
				return
			}
			if !errors.Is(err, ErrModelBackendMismatch) {
				t.Errorf("error %v does not wrap ErrModelBackendMismatch", err)
			}
			if !strings.Contains(err.Error(), tt.model) || !strings.Contains(err.Error(), tt.backend) {
				t.Errorf("error %q should name model %q and backend %q", err.Error(), tt.model, tt.backend)
			}
		})
	}
}

func TestValidateModelForBackendErrorSuggestsFix(t *testing.T) {
	err := validateModelForBackend("pi", "claude-haiku-5-5")
	if err == nil {
		t.Fatal("expected refusal for bare Claude alias on pi")
	}
	for _, want := range []string{"anthropic/claude-haiku-5-5", "claude-code"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should suggest %q", err.Error(), want)
		}
	}
}
