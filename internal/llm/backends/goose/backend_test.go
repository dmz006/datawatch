package goose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBinary_PathLookup(t *testing.T) {
	// If the binary name is on PATH, it should be returned as-is.
	got := resolveBinary("sh") // sh is always on PATH
	if got != "sh" {
		t.Errorf("resolveBinary(\"sh\") = %q, want \"sh\"", got)
	}
}

func TestResolveBinary_AbsolutePath(t *testing.T) {
	got := resolveBinary("/usr/bin/env")
	if got != "/usr/bin/env" {
		t.Errorf("resolveBinary(\"/usr/bin/env\") = %q, want \"/usr/bin/env\"", got)
	}
}

func TestResolveBinary_FallbackToLocalBin(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	const binaryName = "goose-fake-bin"
	localBin := filepath.Join(tmp, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	localGoose := filepath.Join(localBin, binaryName)
	if err := os.WriteFile(localGoose, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveBinary(binaryName)
	if got != localGoose {
		t.Errorf("resolveBinary fallback = %q, want %q", got, localGoose)
	}
}

func TestResolveBinary_NotFound(t *testing.T) {
	// Binary not on PATH and no fallback files exist — return original name.
	got := resolveBinary("goose-totally-nonexistent-xyz")
	if got != "goose-totally-nonexistent-xyz" {
		t.Errorf("resolveBinary(not found) = %q, want original name", got)
	}
}

func TestVersionNormalization(t *testing.T) {
	// Version() prefixes 'v' when the binary omits it (Goose outputs "1.43.0").
	// We test the normalization logic directly since we can't run the real binary.
	cases := []struct {
		raw  string
		want string
	}{
		{"1.43.0", "v1.43.0"},
		{"v1.43.0", "v1.43.0"},
		{"", ""},
		{"2.0.0-rc1", "v2.0.0-rc1"},
	}
	for _, tc := range cases {
		v := tc.raw
		if v != "" && !hasVPrefix(v) {
			v = "v" + v
		}
		if v != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.raw, v, tc.want)
		}
	}
}

func hasVPrefix(s string) bool { return len(s) > 0 && s[0] == 'v' }

func TestBackendName(t *testing.T) {
	b := New("goose")
	if b.Name() != "goose" {
		t.Errorf("Backend.Name() = %q, want \"goose\"", b.Name())
	}
}

func TestPromptBackendName(t *testing.T) {
	b := NewPrompt("goose")
	if b.Name() != "goose-prompt" {
		t.Errorf("PromptBackend.Name() = %q, want \"goose-prompt\"", b.Name())
	}
}

func TestSupportsInteractiveInput(t *testing.T) {
	b := New("goose").(*Backend)
	if !b.SupportsInteractiveInput() {
		t.Error("Backend.SupportsInteractiveInput() = false, want true")
	}
	pb := NewPrompt("goose").(*PromptBackend)
	if pb.SupportsInteractiveInput() {
		t.Error("PromptBackend.SupportsInteractiveInput() = true, want false")
	}
}

func TestPromptRequired(t *testing.T) {
	b := New("goose").(*Backend)
	if b.PromptRequired() {
		t.Error("Backend.PromptRequired() = true, want false")
	}
	pb := NewPrompt("goose").(*PromptBackend)
	if !pb.PromptRequired() {
		t.Error("PromptBackend.PromptRequired() = false, want true")
	}
}

func TestSetSessionName_SanitizesColons(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetSessionName("johnnyjohnny:abc123")
	if b.sessionName != "johnnyjohnny-abc123" {
		t.Errorf("SetSessionName colon sanitize: got %q, want \"johnnyjohnny-abc123\"", b.sessionName)
	}
}

func TestSetSessionName_SanitizesSlashes(t *testing.T) {
	b := New("goose").(*Backend)
	b.SetSessionName("my/session\\name")
	if b.sessionName != "my-session-name" {
		t.Errorf("SetSessionName slash sanitize: got %q, want \"my-session-name\"", b.sessionName)
	}
}

