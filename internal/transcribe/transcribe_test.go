package transcribe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNew_MissingVenv(t *testing.T) {
	_, err := New("/nonexistent/venv", "base", "en")
	if err == nil {
		t.Fatal("expected error for missing venv")
	}
	if got := err.Error(); !contains(got, "python3 not found") {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestNew_AutoLanguage(t *testing.T) {
	// Create a fake venv with python3 and whisper importable
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0o755)
	// Create a fake python3 that succeeds on "import whisper"
	os.WriteFile(filepath.Join(binDir, "python3"), []byte("#!/bin/sh\nexit 0\n"), 0o755)

	w, err := New(dir, "base", "auto")
	if err != nil {
		t.Fatal(err)
	}
	if w.Language != "" {
		t.Fatalf("expected empty language for auto, got %q", w.Language)
	}
}

func TestNew_ExplicitLanguage(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0o755)
	os.WriteFile(filepath.Join(binDir, "python3"), []byte("#!/bin/sh\nexit 0\n"), 0o755)

	w, err := New(dir, "small", "ja")
	if err != nil {
		t.Fatal(err)
	}
	if w.Language != "ja" {
		t.Fatalf("expected ja, got %q", w.Language)
	}
	if w.Model != "small" {
		t.Fatalf("expected small, got %q", w.Model)
	}
}

func TestSupportedLanguages(t *testing.T) {
	langs := SupportedLanguages()
	if len(langs) < 90 {
		t.Fatalf("expected 90+ languages, got %d", len(langs))
	}
	if langs["en"] != "English" {
		t.Fatalf("expected English for 'en', got %q", langs["en"])
	}
	if langs["zh"] != "Chinese" {
		t.Fatalf("expected Chinese for 'zh', got %q", langs["zh"])
	}
}

func TestTranscribe_Integration(t *testing.T) {
	// Skip if the project venv doesn't have whisper
	venv := filepath.Join(projectRoot(), ".venv")
	python := filepath.Join(venv, "bin", "python3")
	if _, err := os.Stat(python); err != nil {
		t.Skip("venv not found — skipping integration test")
	}

	// Check whisper is importable
	if out, err := exec.Command(python, "-c", "import whisper").CombinedOutput(); err != nil {
		t.Skipf("whisper not installed in venv — skipping: %s", string(out))
	}

	// Skip if ffmpeg is not available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found — skipping integration test")
	}

	w, err := New(venv, "tiny", "en")
	if err != nil {
		t.Fatal(err)
	}

	// Create a tiny silent WAV file (0.5s at 8000 Hz)
	wavPath := filepath.Join(t.TempDir(), "silence.wav")
	createSilentWAV(t, wavPath, 4000)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	text, err := w.Transcribe(ctx, wavPath)
	if err != nil {
		t.Fatalf("transcribe failed: %v", err)
	}
	// Silence should produce empty or very short text
	t.Logf("transcription of silence: %q", text)
}

func projectRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

func createSilentWAV(t *testing.T, path string, samples int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	dataSize := samples * 2
	fileSize := 36 + dataSize

	f.Write([]byte("RIFF"))
	writeLE32(f, uint32(fileSize))
	f.Write([]byte("WAVE"))
	f.Write([]byte("fmt "))
	writeLE32(f, 16)
	writeLE16(f, 1)
	writeLE16(f, 1)
	writeLE32(f, 8000)
	writeLE32(f, 16000)
	writeLE16(f, 2)
	writeLE16(f, 16)
	f.Write([]byte("data"))
	writeLE32(f, uint32(dataSize))
	f.Write(make([]byte, dataSize))
}

func writeLE32(f *os.File, v uint32) {
	f.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}

func writeLE16(f *os.File, v uint16) {
	f.Write([]byte{byte(v), byte(v >> 8)})
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
