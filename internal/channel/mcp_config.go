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
	if channelJSPath == "" {
		return fmt.Errorf("WriteProjectMCPConfig: channel.js path required")
	}
	nodePath, err := NodePath()
	if err != nil {
		return fmt.Errorf("WriteProjectMCPConfig: %w", err)
	}

	path := filepath.Join(projectDir, ".mcp.json")
	cfg := MCPProjectConfig{MCPServers: map[string]MCPServerSpec{}}
	if raw, readErr := os.ReadFile(path); readErr == nil {
		_ = json.Unmarshal(raw, &cfg) // best-effort merge; ignore parse errors
		if cfg.MCPServers == nil {
			cfg.MCPServers = map[string]MCPServerSpec{}
		}
	}

	cfg.MCPServers["datawatch"] = MCPServerSpec{
		Command: nodePath,
		Args:    []string{channelJSPath},
		Env:     env,
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal .mcp.json: %w", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
