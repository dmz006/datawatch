// BL33 — plugins package unit tests.

package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dmz006/datawatch/internal/secrets"
)

// makePlugin writes a shell-script plugin manifest+entry into dir and
// returns the plugin directory. On Windows it skips.
func makePlugin(t *testing.T, root, name, entryScript string, hooks []string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	entry := filepath.Join(dir, "plugin.sh")
	if err := os.WriteFile(entry, []byte(entryScript), 0755); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	hooksStr := ""
	for _, h := range hooks {
		hooksStr += fmt.Sprintf("  - %s\n", h)
	}
	man := fmt.Sprintf(`name: %s
entry: ./plugin.sh
hooks:
%stimeout_ms: 1500
`, name, hooksStr)
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(man), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

func TestRegistry_Discover_FindsManifests(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "filter",
		"#!/bin/sh\nread line\necho '{\"action\":\"pass\"}'\n",
		[]string{"post_session_output"})
	r, err := NewRegistry(Config{Enabled: true, Dir: root})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got := r.List()
	if len(got) != 1 || got[0].Name != "filter" {
		t.Fatalf("expected one plugin 'filter', got %+v", got)
	}
}

func TestRegistry_Discover_SkipsNonPluginDirs(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "not-a-plugin"), 0755)
	_ = os.WriteFile(filepath.Join(root, "not-a-plugin", "README.md"),
		[]byte("hi"), 0644)
	r, _ := NewRegistry(Config{Enabled: true, Dir: root})
	if len(r.List()) != 0 {
		t.Fatalf("expected empty registry: %+v", r.List())
	}
}

func TestRegistry_Invoke_ReplacesLine(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "redact",
		`#!/bin/sh
# Read one line; respond with a redacted replacement.
read line
echo '{"action":"replace","line":"[redacted]"}'
`,
		[]string{"post_session_output"})
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})
	resp, err := r.Invoke(context.Background(), "redact",
		HookPostSessionOutput, Request{Line: "sk-abcd1234"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Action != "replace" || resp.Line != "[redacted]" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestRegistry_Invoke_TimeoutReturnsPass(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "hang",
		"#!/bin/sh\nsleep 5\necho '{\"action\":\"pass\"}'\n",
		[]string{"on_alert"})
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 200})
	resp, err := r.Invoke(context.Background(), "hang",
		HookOnAlert, Request{Text: "anything"})
	if err != nil {
		t.Fatalf("Invoke returned hard error: %v", err)
	}
	if resp.Action != "pass" || resp.Error == "" {
		t.Fatalf("expected pass+error, got %+v", resp)
	}
}

func TestRegistry_Fanout_ChainReplacements(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "aaa-upper",
		`#!/bin/sh
read j
# crude: echo a fixed replacement
echo '{"action":"replace","line":"STAGE1"}'
`,
		[]string{"post_session_output"})
	makePlugin(t, root, "bbb-suffix",
		`#!/bin/sh
read j
echo '{"action":"replace","line":"STAGE1/STAGE2"}'
`,
		[]string{"post_session_output"})
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})
	resp, err := r.Fanout(context.Background(), HookPostSessionOutput,
		Request{Line: "orig"})
	if err != nil {
		t.Fatalf("Fanout: %v", err)
	}
	if resp.Action != "replace" || resp.Line != "STAGE1/STAGE2" {
		t.Fatalf("chain broken: %+v", resp)
	}
}

func TestRegistry_DisabledList_HonoredAtDiscovery(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "keep",
		"#!/bin/sh\nread j\necho '{}'\n",
		[]string{"on_alert"})
	makePlugin(t, root, "skip-me",
		"#!/bin/sh\nread j\necho '{}'\n",
		[]string{"on_alert"})
	r, _ := NewRegistry(Config{
		Enabled: true, Dir: root, Disabled: []string{"skip-me"},
	})
	plugins := r.List()
	if len(plugins) != 2 {
		t.Fatalf("expected 2 discovered: %+v", plugins)
	}
	for _, p := range plugins {
		if p.Name == "skip-me" && p.Enabled {
			t.Fatalf("skip-me should have Enabled=false: %+v", p)
		}
		if p.Name == "keep" && !p.Enabled {
			t.Fatalf("keep should have Enabled=true: %+v", p)
		}
	}
}

func TestRegistry_SetEnabled(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "toggle",
		"#!/bin/sh\nread j\necho '{}'\n",
		[]string{"on_alert"})
	r, _ := NewRegistry(Config{Enabled: true, Dir: root})
	if !r.SetEnabled("toggle", false) {
		t.Fatalf("SetEnabled: plugin not found")
	}
	p, _ := r.Get("toggle")
	if p.Enabled {
		t.Fatalf("toggle should be disabled")
	}
}

func TestRegistry_DisabledGlobal_NoDiscovery(t *testing.T) {
	root := t.TempDir()
	makePlugin(t, root, "x",
		"#!/bin/sh\nread j\necho '{}'\n",
		[]string{"on_alert"})
	r, _ := NewRegistry(Config{Enabled: false, Dir: root})
	if got := r.List(); len(got) != 0 {
		t.Fatalf("disabled registry should skip discovery: %+v", got)
	}
}

// mockPluginStore implements secrets.Store for plugin env injection tests.
type mockPluginStore struct {
	data   map[string]string
	scopes map[string][]string
}

func (m *mockPluginStore) List() ([]secrets.Secret, error) { return nil, nil }
func (m *mockPluginStore) Get(name string) (secrets.Secret, error) {
	v, ok := m.data[name]
	if !ok {
		return secrets.Secret{}, secrets.ErrSecretNotFound
	}
	sec := secrets.Secret{Name: name, Value: v, Backend: "mock"}
	if m.scopes != nil {
		sec.Scopes = m.scopes[name]
	}
	return sec, nil
}
func (m *mockPluginStore) Set(n, v string, tags []string, desc string, scopes []string) error {
	return nil
}
func (m *mockPluginStore) Delete(name string) error { return nil }
func (m *mockPluginStore) Exists(name string) (bool, error) {
	_, ok := m.data[name]
	return ok, nil
}

// writeManifest writes a manifest.yaml into dir, overwriting any existing one.
func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func TestRegistry_Invoke_EnvInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	root := t.TempDir()
	// Plugin echoes the MY_SECRET env var back as the replacement line.
	makePlugin(t, root, "env-test",
		"#!/bin/sh\nread j\nprintf '{\"action\":\"replace\",\"line\":\"%s\"}\\n' \"$MY_SECRET\"\n",
		[]string{"post_session_output"})
	// Overwrite the manifest to include an env block.
	writeManifest(t, filepath.Join(root, "env-test"), `name: env-test
entry: ./plugin.sh
hooks:
  - post_session_output
timeout_ms: 2000
env:
  MY_SECRET: "${secret:plugin-key}"
`)
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})
	r.SetSecretsStore(&mockPluginStore{data: map[string]string{"plugin-key": "injected-value"}})

	resp, err := r.Invoke(context.Background(), "env-test", HookPostSessionOutput, Request{Line: "orig"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Line != "injected-value" {
		t.Fatalf("expected injected env var, got %q", resp.Line)
	}
}

func TestRegistry_Invoke_EnvInjection_ScopeDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	root := t.TempDir()
	// Plugin echoes MY_SECRET; if scope is denied, the var should be absent.
	makePlugin(t, root, "env-scoped",
		"#!/bin/sh\nread j\nprintf '{\"action\":\"replace\",\"line\":\"%s\"}\\n' \"${MY_SECRET:-ABSENT}\"\n",
		[]string{"post_session_output"})
	writeManifest(t, filepath.Join(root, "env-scoped"), `name: env-scoped
entry: ./plugin.sh
hooks:
  - post_session_output
timeout_ms: 2000
env:
  MY_SECRET: "${secret:locked-key}"
`)
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})
	r.SetSecretsStore(&mockPluginStore{
		data:   map[string]string{"locked-key": "secret-value"},
		scopes: map[string][]string{"locked-key": {"agent:only"}}, // not "plugin:*"
	})

	resp, err := r.Invoke(context.Background(), "env-scoped", HookPostSessionOutput, Request{Line: "orig"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	// Scope denied → env var not injected → plugin sees ABSENT
	if resp.Line != "ABSENT" {
		t.Fatalf("expected ABSENT (scope denied), got %q", resp.Line)
	}
}

func TestRegistry_Invoke_PlainEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping subprocess plugin test on Windows")
	}
	root := t.TempDir()
	makePlugin(t, root, "plain-env",
		"#!/bin/sh\nread j\nprintf '{\"action\":\"replace\",\"line\":\"%s\"}\\n' \"$PLAIN_VAR\"\n",
		[]string{"post_session_output"})
	writeManifest(t, filepath.Join(root, "plain-env"), `name: plain-env
entry: ./plugin.sh
hooks:
  - post_session_output
timeout_ms: 2000
env:
  PLAIN_VAR: "hello-world"
`)
	// No secrets store wired — plain values still injected.
	r, _ := NewRegistry(Config{Enabled: true, Dir: root, TimeoutMs: 2000})

	resp, err := r.Invoke(context.Background(), "plain-env", HookPostSessionOutput, Request{Line: "orig"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Line != "hello-world" {
		t.Fatalf("expected plain env var injected, got %q", resp.Line)
	}
}
