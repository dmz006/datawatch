// Package channel manages the embedded MCP channel server for Claude Code.
// The channel server (channel.js) is embedded in the binary and extracted to
// ~/.datawatch/channel/ on first use.
package channel

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed embed/channel.js
var channelJS []byte

// packageJSON is the minimal package.json needed for npm install of the channel server deps.
var packageJSON = []byte(`{
  "name": "datawatch-channel",
  "version": "0.1.0",
  "type": "module",
  "dependencies": {
    "@modelcontextprotocol/sdk": "^1.10.0"
  }
}
`)

// EnsureExtracted extracts the embedded channel.js to dataDir/channel/channel.js
// if it does not exist or is outdated. Also ensures npm dependencies are installed.
// Returns the path to the extracted file.
func EnsureExtracted(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, "channel")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create channel dir: %w", err)
	}
	dst := filepath.Join(dir, "channel.js")

	// Write if missing or different size (simple staleness check).
	info, err := os.Stat(dst)
	if err != nil || info.Size() != int64(len(channelJS)) {
		if err := os.WriteFile(dst, channelJS, 0644); err != nil {
			return "", fmt.Errorf("write channel.js: %w", err)
		}
	}

	// Ensure node_modules exist — write package.json and run npm install if needed.
	nmDir := filepath.Join(dir, "node_modules", "@modelcontextprotocol")
	if _, err := os.Stat(nmDir); os.IsNotExist(err) {
		pkgPath := filepath.Join(dir, "package.json")
		if err := os.WriteFile(pkgPath, packageJSON, 0644); err != nil {
			return "", fmt.Errorf("write package.json: %w", err)
		}
		npmBin := findNPM()
		if npmBin == "" {
			return "", fmt.Errorf("npm not found — install Node.js (with npm) or run: cd %s && npm install", dir)
		}
		npmCmd := exec.Command(npmBin, "install", "--production", "--no-audit", "--no-fund")
		npmCmd.Dir = dir
		if out, err := npmCmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("npm install in %s: %w\n%s", dir, err, string(out))
		}
	}

	return dst, nil
}

// findNPM looks for npm in PATH and common install locations.
func findNPM() string {
	if p, err := exec.LookPath("npm"); err == nil {
		return p
	}
	// Check common locations when npm isn't in the daemon's PATH
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".nvm", "versions"), // nvm — need to find active version
		"/usr/local/bin/npm",
		"/usr/bin/npm",
		filepath.Join(home, ".local", "bin", "npm"),
	}
	// For nvm, find the latest installed version
	nvmDir := filepath.Join(home, ".nvm", "versions", "node")
	if entries, err := os.ReadDir(nvmDir); err == nil {
		for i := len(entries) - 1; i >= 0; i-- {
			npmPath := filepath.Join(nvmDir, entries[i].Name(), "bin", "npm")
			if _, err := os.Stat(npmPath); err == nil {
				return npmPath
			}
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	// Try corepack's npm shim as last resort
	shimPath := "/usr/share/nodejs/corepack/shims/npm"
	if _, err := os.Stat(shimPath); err == nil {
		return shimPath
	}
	return ""
}

// NodePath returns the path to the node binary, or an error if not found.
func NodePath() (string, error) {
	p, err := exec.LookPath("node")
	if err != nil {
		return "", fmt.Errorf("node not found in PATH: %w", err)
	}
	return p, nil
}

// RegisterMCP registers the channel server with claude mcp as "datawatch".
// channelJSPath is the absolute path to the extracted channel.js.
// env is additional KEY=VALUE pairs to pass as --env flags.
// This is idempotent — safe to call on every start.
func RegisterMCP(channelJSPath string, env map[string]string) error {
	return registerMCPNamed("datawatch", channelJSPath, env)
}

// RegisterSessionMCP registers a per-session MCP channel server as "datawatch-{sessionID}".
// The CLAUDE_SESSION_ID env var is set so the channel server can identify itself in callbacks.
// DATAWATCH_CHANNEL_PORT=0 makes each session use a random port to avoid conflicts.
func RegisterSessionMCP(sessionID, channelJSPath string, env map[string]string) error {
	name := "datawatch-" + sessionID
	merged := make(map[string]string, len(env)+2)
	for k, v := range env {
		merged[k] = v
	}
	merged["CLAUDE_SESSION_ID"] = sessionID
	merged["DATAWATCH_CHANNEL_PORT"] = "0"
	return registerMCPNamed(name, channelJSPath, merged)
}

// UnregisterSessionMCP removes the per-session MCP channel server registration.
func UnregisterSessionMCP(sessionID string) {
	name := "datawatch-" + sessionID
	exec.Command("claude", "mcp", "remove", name, "-s", "user").Run() //nolint:errcheck
}

// UnregisterGlobalMCP removes the legacy global "datawatch" MCP registration.
func UnregisterGlobalMCP() {
	exec.Command("claude", "mcp", "remove", "datawatch", "-s", "user").Run() //nolint:errcheck
}

// ChannelServerName returns the MCP server name for a given session.
func ChannelServerName(sessionID string) string {
	return "datawatch-" + sessionID
}

func registerMCPNamed(name, channelJSPath string, env map[string]string) error {
	nodePath, err := NodePath()
	if err != nil {
		return err
	}
	// Remove existing entry (ignore errors — may not exist).
	exec.Command("claude", "mcp", "remove", name, "-s", "user").Run() //nolint:errcheck

	args := []string{"mcp", "add", "--scope", "user", name, nodePath, channelJSPath}
	for k, v := range env {
		args = append(args, "--env", k+"="+v)
	}
	out, err := exec.Command("claude", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude mcp add %s: %w\n%s", name, err, string(out))
	}
	return nil
}
