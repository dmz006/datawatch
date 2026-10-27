// whisper.chunk_seconds — default and YAML loading.

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadWhisperChunkYAML(t *testing.T, yamlText string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlText), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestWhisperChunkSeconds_DefaultWithNoWhisperSection(t *testing.T) {
	if got := DefaultConfig().Whisper.ChunkSeconds; got != 120 {
		t.Fatalf("default ChunkSeconds = %d, want 120", got)
	}
	cfg := loadWhisperChunkYAML(t, "server:\n  host: 127.0.0.1\n")
	if cfg.Whisper.ChunkSeconds != 120 {
		t.Fatalf("ChunkSeconds with no whisper section = %d, want 120", cfg.Whisper.ChunkSeconds)
	}
}

func TestWhisperChunkSeconds_OldConfigWithoutKeyStillGets120(t *testing.T) {
	cfg := loadWhisperChunkYAML(t, "whisper:\n  enabled: true\n  model: small\n  venv_path: .venv\n")
	if cfg.Whisper.ChunkSeconds != 120 {
		t.Fatalf("ChunkSeconds without the key = %d, want 120", cfg.Whisper.ChunkSeconds)
	}
	if cfg.Whisper.Model != "small" {
		t.Fatalf("Model = %q, want small", cfg.Whisper.Model)
	}
}

func TestWhisperChunkSeconds_YAMLValueIsKept(t *testing.T) {
	cfg := loadWhisperChunkYAML(t, "whisper:\n  chunk_seconds: 45\n")
	if cfg.Whisper.ChunkSeconds != 45 {
		t.Fatalf("ChunkSeconds = %d, want 45", cfg.Whisper.ChunkSeconds)
	}
}

func TestWhisperChunkSeconds_ZeroStaysZero(t *testing.T) {
	cfg := loadWhisperChunkYAML(t, "whisper:\n  chunk_seconds: 0\n")
	if cfg.Whisper.ChunkSeconds != 0 {
		t.Fatalf("ChunkSeconds = %d, want 0 (chunking disabled)", cfg.Whisper.ChunkSeconds)
	}
}
