// Package plugins (BL33, Sprint S7 → v3.11.0) loads subprocess
// plugins discovered under <data_dir>/plugins/. Each plugin ships a
// manifest.yaml + an executable; the daemon invokes the executable
// per-hook with a JSON line on stdin, reads one JSON line on stdout.
//
// Design doc: docs/plans/2026-04-20-bl33-plugin-framework.md.

package plugins

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// Hook is a typed discriminator for the contract. Arbitrary strings
// are accepted at runtime so plugins can register for future hooks
// without a datawatch rebuild; the constants here document the v1 set.
type Hook string

const (
	HookPreSessionStart      Hook = "pre_session_start"
	HookPostSessionOutput    Hook = "post_session_output"
	HookPostSessionComplete  Hook = "post_session_complete"
	HookOnAlert              Hook = "on_alert"
)

// Manifest mirrors the YAML manifest.yaml a plugin ships. Additional
// keys are tolerated so manifests can add features without a daemon
// update.
type Manifest struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Version     string   `yaml:"version,omitempty" json:"version,omitempty"`
	Entry       string   `yaml:"entry" json:"entry"`
	Hooks       []string `yaml:"hooks" json:"hooks"`
	TimeoutMs   int      `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	Mode        string   `yaml:"mode,omitempty" json:"mode,omitempty"` // "oneshot" (default) | "long-lived"
}

// Plugin is a loaded manifest with its invocation stats.
type Plugin struct {
	Manifest
	Dir          string    `json:"dir"`             // absolute path containing manifest.yaml
	Entry        string    `json:"resolved_entry"`  // absolute path to executable
	Enabled      bool      `json:"enabled"`
	LastInvokeAt time.Time `json:"last_invoke_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	InvokeCount  int       `json:"invoke_count"`
	ErrorCount   int       `json:"error_count"`
}

// Request is the JSON payload sent on stdin.
type Request struct {
	Hook    Hook           `json:"hook"`
	Session string         `json:"session_id,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
	// Flattened convenience fields (one of these is usually set):
	Line     string `json:"line,omitempty"`     // post_session_output
	Severity string `json:"severity,omitempty"` // on_alert
	Text     string `json:"text,omitempty"`     // on_alert
	Status   string `json:"status,omitempty"`   // post_session_complete
}

// Response is one JSON line on stdout.
type Response struct {
	// Action is "pass" | "drop" | "replace" | "block" | "" (treated as pass).
	Action string `json:"action,omitempty"`
	// Line is the replacement when Action=="replace".
	Line string `json:"line,omitempty"`
	// Fields allows the plugin to mutate pre_session_start inputs.
	Fields map[string]any `json:"fields,omitempty"`
	// OK is the fire-and-forget ack for notification hooks.
	OK bool `json:"ok,omitempty"`
	// Error lets the plugin report a non-fatal problem without
	// crashing the process — logged, hook treated as no-op.
	Error string `json:"error,omitempty"`
}

// Config mirrors the YAML plugins: block. Separate from Registry so
// config reloads can rebuild the Registry without invasive surgery.
type Config struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`
	Dir       string   `yaml:"dir,omitempty" json:"dir,omitempty"`
	TimeoutMs int      `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	Disabled  []string `yaml:"disabled,omitempty" json:"disabled,omitempty"`
}

// DefaultConfig — plugins disabled, conservative timeout.
func DefaultConfig() Config {
	return Config{Enabled: false, TimeoutMs: 2000}
}

// Registry holds discovered plugins keyed by Manifest.Name.
type Registry struct {
	mu     sync.Mutex
	cfg    Config
	byName map[string]*Plugin
}

// NewRegistry scans cfg.Dir and returns a registry. A nil return is
// never produced — an empty registry is fine.
func NewRegistry(cfg Config) (*Registry, error) {
	r := &Registry{cfg: cfg, byName: map[string]*Plugin{}}
	if !cfg.Enabled || strings.TrimSpace(cfg.Dir) == "" {
		return r, nil
	}
	return r, r.Discover()
}

// SetConfig replaces the config (used by /api/reload).
func (r *Registry) SetConfig(cfg Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = cfg
}

// Discover rescans the configured directory. Safe to call repeatedly.
func (r *Registry) Discover() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName = map[string]*Plugin{}
	if !r.cfg.Enabled || strings.TrimSpace(r.cfg.Dir) == "" {
		return nil
	}
	entries, err := os.ReadDir(r.cfg.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read plugin dir %s: %w", r.cfg.Dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pluginDir := filepath.Join(r.cfg.Dir, e.Name())
		manPath := filepath.Join(pluginDir, "manifest.yaml")
		data, err := os.ReadFile(manPath)
		if err != nil {
			continue // silently skip — not every subdir is a plugin
		}
		var m Manifest
		if err := yaml.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Name == "" {
			continue
		}
		entryPath := m.Entry
		if !filepath.IsAbs(entryPath) {
			entryPath = filepath.Join(pluginDir, entryPath)
		}
		p := &Plugin{
			Manifest: m,
			Dir:      pluginDir,
			Entry:    entryPath,
			Enabled:  !isDisabled(r.cfg.Disabled, m.Name),
		}
		r.byName[m.Name] = p
	}
	return nil
}

// List returns all discovered plugins sorted by name.
func (r *Registry) List() []*Plugin {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Plugin, 0, len(r.byName))
	for _, p := range r.byName {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns one plugin by name (or false).
func (r *Registry) Get(name string) (*Plugin, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byName[name]
	return p, ok
}

// SetEnabled toggles enabled/disabled state for a named plugin.
// Returns false if the plugin doesn't exist.
func (r *Registry) SetEnabled(name string, on bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byName[name]
	if !ok {
		return false
	}
	p.Enabled = on
	return true
}

// Invoke runs one oneshot hook. Returns the parsed Response or an
// error; a timeout or non-zero exit produces a Response{Action:"pass"}
// with an Error message and no Go error.
func (r *Registry) Invoke(ctx context.Context, name string, hook Hook, req Request) (Response, error) {
	p, ok := r.Get(name)
	if !ok {
		return Response{}, fmt.Errorf("plugin %q not found", name)
	}
	if !p.Enabled {
		return Response{Action: "pass"}, nil
	}
	timeout := time.Duration(r.cfg.TimeoutMs) * time.Millisecond
	if p.TimeoutMs > 0 {
		timeout = time.Duration(p.TimeoutMs) * time.Millisecond
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req.Hook = hook
	body, _ := json.Marshal(req)
	body = append(body, '\n')

	cmd := exec.CommandContext(cctx, p.Entry)
	cmd.Dir = p.Dir
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	r.recordInvoke(p, err, stderr.String(), start)
	if err != nil {
		return Response{Action: "pass", Error: err.Error()}, nil
	}
	line, _ := bufio.NewReader(&stdout).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return Response{Action: "pass"}, nil
	}
	var resp Response
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return Response{Action: "pass", Error: "parse: " + err.Error()}, nil
	}
	return resp, nil
}

// Fanout invokes every enabled plugin that declares the hook.
// For filter hooks (post_session_output), earlier replacements chain
// into the next plugin; a "drop" stops the chain.
func (r *Registry) Fanout(ctx context.Context, hook Hook, req Request) (Response, error) {
	final := Response{Action: "pass", Line: req.Line}
	for _, p := range r.List() {
		if !p.Enabled || !pluginDeclares(p, hook) {
			continue
		}
		resp, err := r.Invoke(ctx, p.Name, hook, req)
		if err != nil {
			return final, err
		}
		switch resp.Action {
		case "drop":
			return Response{Action: "drop"}, nil
		case "block":
			return resp, nil
		case "replace":
			req.Line = resp.Line
			final = resp
		}
	}
	return final, nil
}

// recordInvoke updates per-plugin stats.
func (r *Registry) recordInvoke(p *Plugin, err error, stderr string, start time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p.LastInvokeAt = start
	p.InvokeCount++
	if err != nil {
		p.ErrorCount++
		msg := err.Error()
		if stderr != "" {
			msg += " — " + strings.TrimSpace(stderr)
		}
		p.LastError = msg
	} else {
		p.LastError = ""
	}
}

// isDisabled reports whether name appears in the operator's disabled list.
// The caller uses Enabled = !isDisabled(...).
func isDisabled(list []string, name string) bool {
	for _, s := range list {
		if s == name {
			return true
		}
	}
	return false
}

func pluginDeclares(p *Plugin, hook Hook) bool {
	h := string(hook)
	for _, x := range p.Hooks {
		if x == h {
			return true
		}
	}
	return false
}

// Watch starts a background goroutine that re-runs Discover when the
// plugin directory changes (file created, renamed, or removed).
// Coalesces bursts with a 500 ms debounce so editor-save chatter
// doesn't trigger a storm of rescans. Exits when ctx is cancelled.
//
// No-op when the registry is disabled or the directory doesn't exist.
// Returns nil on a successful watcher start; errors are logged to
// stderr but don't block registry use.
func (r *Registry) Watch(ctx context.Context) error {
	if !r.cfg.Enabled || strings.TrimSpace(r.cfg.Dir) == "" {
		return nil
	}
	if _, err := os.Stat(r.cfg.Dir); err != nil {
		return nil // directory missing; discovery is already a no-op
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	if err := w.Add(r.cfg.Dir); err != nil {
		w.Close()
		return fmt.Errorf("watch %s: %w", r.cfg.Dir, err)
	}
	go func() {
		defer w.Close()
		var timer *time.Timer
		fire := func() {
			if err := r.Discover(); err != nil {
				fmt.Fprintf(os.Stderr, "[plugins] hot-reload: %v\n", err)
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				// Only react to create/remove/rename — write events on a
				// single manifest.yaml get their own rebuild via create
				// after editors do atomic-rename saves.
				if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(500*time.Millisecond, fire)
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				fmt.Fprintf(os.Stderr, "[plugins] watcher: %v\n", err)
			}
		}
	}()
	return nil
}
