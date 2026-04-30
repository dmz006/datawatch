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

// WriteProjectMCPConfig writes (or rewrites) `<projectDir>/.mcp.json`
// with a single "datawatch" server entry pointing at channelJSPath.
// Existing entries under other names are preserved so an operator can
// hand-add their own MCP servers without losing them on the next
// spawn.
//
// Returns nil with no side effect when projectDir is empty.
func WriteProjectMCPConfig(projectDir, channelJSPath string, env map[string]string) error {
	if projectDir == "" {
		return nil
	}

	// v5.27.10 — when the Go bridge is the active path, write the
	// project mcp.json pointing at the bridge binary directly. The JS
	// fallback fields are only emitted when no Go bridge is on hand.
	// Operator-flagged: ring-laptop had a stale `~/.mcp.json` pointing
	// at `node + ~/.datawatch/channel/channel.js` (a non-existent path)
	// because this writer used to hardcode the JS shape regardless.
	var spec MCPServerSpec
	if bin := BridgePath(); bin != "" {
		spec = MCPServerSpec{
			Command: bin,
			Args:    []string{},
			Env:     env,
		}
	} else {
		if channelJSPath == "" {
			return fmt.Errorf("WriteProjectMCPConfig: channel.js path required when Go bridge unavailable")
		}
		nodePath, err := NodePath()
		if err != nil {
			return fmt.Errorf("WriteProjectMCPConfig: %w", err)
		}
		spec = MCPServerSpec{
			Command: nodePath,
			Args:    []string{channelJSPath},
			Env:     env,
		}
	}

	path := filepath.Join(projectDir, ".mcp.json")
	cfg := MCPProjectConfig{MCPServers: map[string]MCPServerSpec{}}
	if raw, readErr := os.ReadFile(path); readErr == nil {
		_ = json.Unmarshal(raw, &cfg) // best-effort merge; ignore parse errors
		if cfg.MCPServers == nil {
			cfg.MCPServers = map[string]MCPServerSpec{}
		}
	}

	cfg.MCPServers["datawatch"] = spec

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
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
	spec, ok := cfg.MCPServers["datawatch"]
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
