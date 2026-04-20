// BL33 — plugins package unit tests.

package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
