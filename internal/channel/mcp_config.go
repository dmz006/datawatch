// BL109 — generic per-session MCP config writer.
//
// Originally only claude-code got an MCP server registered (via
// `claude mcp add ...` shell-out). Other backends (opencode, aider,
// goose, gemini, etc.) each have their own discovery convention; the
// most common one is a `.mcp.json` file at the project root in the
// "modelcontextprotocol/spec" shape:
//
//   {
//     "mcpServers": {
//       "datawatch": {
//         "command": "node",
//         "args":    ["/path/to/channel.js"],
//         "env":     {"KEY": "VAL", ...}
//       }
//     }
//   }
//
// Backends that read .mcp.json (claude-code 0.x with a project file,
// opencode "auto-discover" mode, gemini's experimental MCP support)
// will pick this up at startup. Backends with their own convention
// (aider's --mcp-config CLI flag) get a no-op for now; the per-
// backend writer registry below is the extension point.
//
// The function is idempotent + safe to call on every spawn — it
// rewrites the file in place.

package channel

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MCPProjectConfig is the on-disk shape of `.mcp.json`.
type MCPProjectConfig struct {
	MCPServers map[string]MCPServerSpec `json:"mcpServers"`
}

// MCPServerSpec mirrors the modelcontextprotocol stdio transport.
type MCPServerSpec struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// ProjectMCPEntryName is the shared/global MCP server name written into a
// project's (or the daemon's instance) .mcp.json, and the name used by the
// global claude mcp registration. Per-session channels use a different,
// per-session name — see ChannelServerName (internal/channel/channel.go).
// Single source of truth for every place that code writes or compares
// against that entry name.
const ProjectMCPEntryName = "datawatch"

// WriteProjectMCPConfig writes (or rewrites) `<projectDir>/.mcp.json`
// with a "datawatch" server entry pointing at channelJSPath plus any
// extras (BL344 / GH#118). Existing entries under other names are
// preserved so an operator can hand-add their own MCP servers without
// losing them on the next spawn.
//
// The daemon's own launches do not call this: a session's entry carries its
// token and goes to its private launch folder (WriteSessionMCPConfig).
//
// extras maps server-name → spec; nil/empty is a no-op for extras.
// Returns nil with no side effect when projectDir is empty.
func WriteProjectMCPConfig(projectDir, channelJSPath string, env map[string]string, extras map[string]MCPServerSpec) error {
	if projectDir == "" {
		return nil
	}

	spec, err := bridgeSpec(channelJSPath, env)
	if err != nil {
		return fmt.Errorf("WriteProjectMCPConfig: %w", err)
	}

	f, err := readMCPFile(filepath.Join(projectDir, ".mcp.json"))
	if err != nil {
		return fmt.Errorf("WriteProjectMCPConfig: %w", err)
	}
	f.set(ProjectMCPEntryName, spec)
	for name, extra := range extras {
		if name != ProjectMCPEntryName {
			f.set(name, extra)
		}
	}
	// HLLM-001 (docs/plans/historical-plans/2026-08-28-security-assessment-hostile-llm.md):
	// this file's "env" can carry DATAWATCH_TOKEN in plaintext, so it is
	// written 0600, which also heals a file that had a wider mode.
	return f.write()
}

// bridgeSpec is the datawatch bridge entry with env: the native bridge when
// there is one, else channel.js run by node.
//
// v5.27.10 — hardcoding the JS shape left stale entries pointing at a
// channel.js that did not exist.
func bridgeSpec(channelJSPath string, env map[string]string) (MCPServerSpec, error) {
	if bin := BridgePath(); bin != "" {
		return MCPServerSpec{Command: bin, Args: []string{}, Env: env}, nil
	}
	if channelJSPath == "" {
		return MCPServerSpec{}, fmt.Errorf("channel.js path required when Go bridge unavailable")
	}
	nodePath, err := NodePath()
	if err != nil {
		return MCPServerSpec{}, err
	}
	return MCPServerSpec{Command: nodePath, Args: []string{channelJSPath}, Env: env}, nil
}

// InjectExtrasIntoMCPConfig merges extra MCP server entries into
// <projectDir>/.mcp.json without touching any existing entries.
// Used for backends (e.g. claude-code) where the "datawatch" bridge
// is registered separately. No-op when projectDir is empty or extras
// is nil/empty.
func InjectExtrasIntoMCPConfig(projectDir string, extras map[string]MCPServerSpec) error {
	if projectDir == "" || len(extras) == 0 {
		return nil
	}
	f, err := readMCPFile(filepath.Join(projectDir, ".mcp.json"))
	if err != nil {
		return fmt.Errorf("extra MCP servers not added: %w", err)
	}
	changed := false
	for name, spec := range extras {
		if name == ProjectMCPEntryName {
			continue
		}
		f.set(name, spec)
		changed = true
	}
	if !changed {
		return nil
	}
	// HLLM-001 -- see WriteProjectMCPConfig's comment above.
	return f.write()
}

// WriteInstanceMCPConfig rewrites `<dataDir>/.mcp.json` so it tracks the
// current bridge (Go or JS). Idempotent — skips the write when the entry is
// already correct. Returns true when the file was rewritten. BL318.
func WriteInstanceMCPConfig(dataDir, channelJSPath string, env map[string]string) (bool, error) {
	if dataDir == "" {
		return false, fmt.Errorf("WriteInstanceMCPConfig: dataDir is required")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return false, fmt.Errorf("WriteInstanceMCPConfig: mkdir %s: %w", dataDir, err)
	}
	return sweepMCPConfigFile(filepath.Join(dataDir, ".mcp.json"), channelJSPath, env)
}

// SweepUserScopeMCPConfig checks the user-scope ~/.mcp.json and rewrites the
// "datawatch" entry if it is stale — either pointing at a JS bridge whose
// channel.js no longer exists, or pointing at node when the Go bridge is now
// on hand. Preserves all other entries. Idempotent. Returns true when the
// file was rewritten. v6.0.7 (BL218).
//
// Deprecated: use WriteInstanceMCPConfig with cfg.DataDir to avoid $HOME
// pollution when multiple daemon instances share a host (BL318).
func SweepUserScopeMCPConfig(channelJSPath string, env map[string]string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("SweepUserScopeMCPConfig: %w", err)
	}
	return sweepMCPConfigFile(filepath.Join(home, ".mcp.json"), channelJSPath, env)
}

// sweepMCPConfigFile rewrites the "datawatch" entry in path if it is stale.
// Shared by WriteInstanceMCPConfig and SweepUserScopeMCPConfig.
func sweepMCPConfigFile(path, channelJSPath string, env map[string]string) (bool, error) {
	f, err := readMCPFile(path)
	if err != nil {
		return false, fmt.Errorf("bridge entry not written: %w", err)
	}

	// Build the canonical entry for the current bridge.
	var want MCPServerSpec
	if bin := BridgePath(); bin != "" {
		want = MCPServerSpec{Command: bin, Args: []string{}, Env: env}
	} else {
		if channelJSPath == "" {
			return false, nil // nothing to write without a JS path
		}
		nodePath, nodeErr := NodePath()
		if nodeErr != nil {
			return false, nil // node not available — skip silently
		}
		want = MCPServerSpec{Command: nodePath, Args: []string{channelJSPath}, Env: env}
	}

	// Skip rewrite when the existing entry is already correct. Env is
	// compared too (nil and empty treated as equal): the env carries the
	// DATAWATCH_TOKEN, and with a rotating scoped token a stale token means
	// the entry is stale even when command/args match.
	if existing, ok := f.spec(ProjectMCPEntryName); ok {
		argsMatch := len(existing.Args) == len(want.Args) &&
			(len(existing.Args) == 0 || existing.Args[0] == want.Args[0])
		if existing.Command == want.Command && argsMatch && envMapEqual(existing.Env, want.Env) {
			return false, nil
		}
	}

	f.set(ProjectMCPEntryName, want)
	// HLLM-001 -- see WriteProjectMCPConfig's comment above.
	if err := f.write(); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveProjectMCPEntry removes a named server from <projectDir>/.mcp.json.
// Used to purge the legacy "datawatch" global entry that was written before
// per-session MCP registration replaced it. Idempotent; no-op when the file
// or the key doesn't exist.
func RemoveProjectMCPEntry(projectDir, name string) error {
	if projectDir == "" {
		return nil
	}
	return removeEntry(filepath.Join(projectDir, ".mcp.json"), name, false)
}

// IsStaleProjectMCPConfig reads <path> as a `.mcp.json` and reports
// whether its `datawatch` entry points at a JS bridge whose channel.js
// file no longer exists on disk. Used by `/api/channel/info` to flag
// stale operator workarounds and by the cleanup CLI subcommand.
// Returns (stale, channelJSPath, err) — stale=false on parse/missing
// errors so the caller can ignore unrelated mcp.json files.
// v5.27.10.
func IsStaleProjectMCPConfig(path string) (bool, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, "", err
	}
	var cfg MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return false, "", nil
	}
	spec, ok := cfg.MCPServers[ProjectMCPEntryName]
	if !ok {
		return false, "", nil
	}
	// Go-bridge entries have Args=[] (or a single non-.js arg). Stale
	// means "JS-shaped + the JS file is missing".
	if len(spec.Args) == 0 {
		return false, "", nil
	}
	jsPath := spec.Args[0]
	if filepath.Ext(jsPath) != ".js" {
		return false, "", nil
	}
	if _, err := os.Stat(jsPath); err == nil {
		return false, jsPath, nil // file exists, not stale
	}
	return true, jsPath, nil
}

// envMapEqual reports whether two env maps are equal, treating nil and
// empty maps as the same.
func envMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// RemoveInstanceMCPConfig removes the "datawatch" entry from
// <dataDir>/.mcp.json, preserving any operator-added servers (the rewrite
// keeps the existing 0600 + chmod discipline) and deleting the file when no
// servers remain. Idempotent; no error when the file is missing or
// unparsable.
func RemoveInstanceMCPConfig(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	return removeEntry(filepath.Join(dataDir, ".mcp.json"), ProjectMCPEntryName, true)
}

// ScrubInstanceMCPToken removes the "datawatch" entry from
// <dataDir>/.mcp.json when its env DATAWATCH_TOKEN equals any non-empty
// legacyTokens value (constant-time compare via crypto/subtle), deleting the
// file when no servers remain. Returns true when an entry was removed.
// Migration for files written by earlier daemons that held the daemon's
// long-lived server token in the file. No error when the file is missing or
// unparsable; the token value is never logged or returned.
func ScrubInstanceMCPToken(dataDir string, legacyTokens ...string) (bool, error) {
	if dataDir == "" {
		return false, nil
	}
	f, err := readMCPFile(filepath.Join(dataDir, ".mcp.json"))
	if err != nil {
		// Said, because a token in it stays on disk.
		fmt.Fprintf(os.Stderr, "[channel] old server token not looked for: %v\n", err)
		return false, nil
	}
	spec, ok := f.spec(ProjectMCPEntryName)
	if !ok {
		return false, nil
	}
	current := spec.Env["DATAWATCH_TOKEN"]
	match := false
	if current != "" {
		for _, legacy := range legacyTokens {
			if legacy == "" {
				continue
			}
			if subtle.ConstantTimeCompare([]byte(current), []byte(legacy)) == 1 {
				match = true
				break
			}
		}
	}
	if !match {
		return false, nil
	}
	if err := RemoveInstanceMCPConfig(dataDir); err != nil {
		return false, err
	}
	return true, nil
}
