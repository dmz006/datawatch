package transcribe

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeVenv builds a venv layout whose python3 exits 0, which is all New()
// needs to accept it.
func fakeVenv(t *testing.T) string {
	t.Helper()
	venv := t.TempDir()
	bin := filepath.Join(venv, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "python3"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return venv
}

func TestNewFromConfig_ChunkWrapsLocalWhisper(t *testing.T) {
	tr, err := NewFromConfig(BackendConfig{VenvPath: fakeVenv(t), ChunkSeconds: 120})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := tr.(*ChunkedTranscriber)
	if !ok {
		t.Fatalf("got %T, want *ChunkedTranscriber", tr)
	}
	if c.Chunk != 120*time.Second {
		t.Fatalf("Chunk = %v, want 2m", c.Chunk)
	}
	if _, ok := c.Inner.(*WhisperTranscriber); !ok {
		t.Fatalf("Inner is %T, want *WhisperTranscriber", c.Inner)
	}
}

func TestNewFromConfig_ChunkZeroLeavesWhisperBare(t *testing.T) {
	tr, err := NewFromConfig(BackendConfig{Backend: "whisper", VenvPath: fakeVenv(t), ChunkSeconds: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.(*WhisperTranscriber); !ok {
		t.Fatalf("got %T, want *WhisperTranscriber", tr)
	}
}

func TestNewFromConfig_ChunkNeverWrapsHTTPBackends(t *testing.T) {
	for _, backend := range []string{"openai", "openai_compat", "openai-compat", "openwebui", "ollama"} {
		tr, err := NewFromConfig(BackendConfig{Backend: backend, Endpoint: "http://127.0.0.1:1", ChunkSeconds: 120})
		if err != nil {
			t.Fatalf("%s: %v", backend, err)
		}
		if _, wrapped := tr.(*ChunkedTranscriber); wrapped {
			t.Fatalf("%s backend was wrapped in ChunkedTranscriber", backend)
		}
	}
}
