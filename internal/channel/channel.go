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
	"strings"
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

// ProbeResult describes whether the channel runtime (Node + npm + the
// MCP SDK) is ready to start. Used by the daemon startup warning and
// by `datawatch setup channel`.
type ProbeResult struct {
	Ready       bool
	NodePath    string
	NPMPath     string
	NodeModules bool
	Hint        string
}

// Probe checks for node, npm, and the extracted node_modules dir;
// also reports whether the native Go bridge binary (BL174) is on hand.
// Pure read — does not extract or install. dataDir is the daemon
// root (e.g. ~/.datawatch); the channel lives at <dataDir>/channel/.
func Probe(dataDir string) ProbeResult {
	res := ProbeResult{}
	// Native Go bridge takes precedence — when present the JS path is
	// irrelevant and Ready is true regardless of node/npm.
	if bin := BinaryPath(dataDir); bin != "" {
		res.Ready = true
		res.Hint = "native Go bridge (datawatch-channel) at " + bin
		return res
	}
	if p, err := NodePath(); err == nil {
		res.NodePath = p
	}
	res.NPMPath = findNPM()
	nmDir := filepath.Join(dataDir, "channel", "node_modules", "@modelcontextprotocol")
	if _, err := os.Stat(nmDir); err == nil {
		res.NodeModules = true
	}
	switch {
	case res.NodePath == "":
		res.Hint = "node not found in PATH — install Node.js (>= 18), set DATAWATCH_NODE_BIN, or drop the datawatch-channel Go bridge in <data_dir>/channel/"
	case res.NPMPath == "" && !res.NodeModules:
		res.Hint = "npm not found — install Node.js with npm, or run `datawatch setup channel` after installing"
	case !res.NodeModules:
		res.Hint = "channel deps not installed — run `datawatch setup channel` to pre-install"
	default:
		res.Ready = true
	}
	return res
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

// NodePath returns the path to the node binary, or an error if not
// found. Honours DATAWATCH_NODE_BIN as an explicit override (set by
// tests + by operators on hosts where `node` isn't on the daemon's
// PATH).
func NodePath() (string, error) {
	if override := os.Getenv("DATAWATCH_NODE_BIN"); override != "" {
		return override, nil
	}
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

// CleanupStaleMCP removes MCP registrations for sessions that no longer exist.
// sessionExists is called with the full session ID (hostname-id) to check if it's still tracked.
// Runs on daemon startup to prevent stale entries from accumulating.
func CleanupStaleMCP(sessionExists func(string) bool) {
	out, err := exec.Command("claude", "mcp", "list").Output()
	if err != nil {
		return
	}
	removed := 0
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "datawatch-") {
			continue
		}
		// Extract the server name (before the colon)
		name := line
		if idx := strings.IndexByte(line, ':'); idx > 0 {
			name = line[:idx]
		}
		// Extract the session ID from "datawatch-{hostname}-{id}"
		sessionID := strings.TrimPrefix(name, "datawatch-")
		if sessionID == "" {
			continue
		}
		// Check if this session still exists (active or completed)
		if sessionExists(sessionID) {
			continue // session exists, keep the registration
		}
		// Session doesn't exist — remove the stale registration
		exec.Command("claude", "mcp", "remove", name, "-s", "user").Run() //nolint:errcheck
		removed++
	}
	if removed > 0 {
		fmt.Printf("[channel] cleaned up %d stale MCP registration(s)\n", removed)
	}
}

// BinaryPath resolves the native datawatch-channel Go bridge (BL174).
// Returns "" when no binary is available — caller should fall back to
// the embedded channel.js path.
//
// Resolution order:
//  1. $DATAWATCH_CHANNEL_BIN explicit override
//  2. <dataDir>/channel/datawatch-channel (operator dropped one in)
//  3. datawatch-channel sibling of the running parent binary
//  4. datawatch-channel on PATH
func BinaryPath(dataDir string) string {
	if p := os.Getenv("DATAWATCH_CHANNEL_BIN"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if dataDir != "" {
		p := filepath.Join(dataDir, "channel", "datawatch-channel")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if exe, err := os.Executable(); err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		p := filepath.Join(filepath.Dir(exe), "datawatch-channel")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("datawatch-channel"); err == nil {
		return p
	}
	return ""
}

// LegacyJSArtifacts returns the paths of the JS-bridge files that are
// no longer needed once the native Go bridge is in use. Pure read.
// Returned paths are existing entries under <dataDir>/channel/:
//   - channel.js
//   - package.json
//   - package-lock.json
//   - node_modules (directory)
func LegacyJSArtifacts(dataDir string) []string {
	if dataDir == "" {
		return nil
	}
	dir := filepath.Join(dataDir, "channel")
	candidates := []string{
		filepath.Join(dir, "channel.js"),
		filepath.Join(dir, "package.json"),
		filepath.Join(dir, "package-lock.json"),
		filepath.Join(dir, "node_modules"),
	}
	var present []string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			present = append(present, p)
		}
	}
	return present
}

// RemoveLegacyJSArtifacts deletes the files reported by LegacyJSArtifacts.
// Returns the paths that were removed (best-effort — errors per path are
// swallowed so a partial cleanup still surfaces what worked). Idempotent.
func RemoveLegacyJSArtifacts(dataDir string) []string {
	paths := LegacyJSArtifacts(dataDir)
	var removed []string
	for _, p := range paths {
		if err := os.RemoveAll(p); err == nil {
			removed = append(removed, p)
		}
	}
	return removed
}

// CleanupStaleJSRegistrations (BL288, v5.4.0) — `claude mcp list` may
// still show pre-Go-bridge `datawatch*` entries pointing at
// `node + .channel.js`. Operator on v5.3.0 reported a fresh session
// spawning the legacy node process even though `[channel] using
// native Go bridge` was logged at boot — root cause was a leftover
// `.mcp.json` project-scoped registration.
//
// This walks each scope (`user`, `local`, `project`) and removes any
// MCP server named with the `datawatch` prefix whose command line
// resolves to `node` + a `channel.js` path. Returns the names that
// were removed; logs are the caller's job.
func CleanupStaleJSRegistrations() []string {
	var removed []string
	out, err := exec.Command("claude", "mcp", "list").CombinedOutput()
	if err != nil {
		return removed
	}
	for _, line := range strings.Split(string(out), "\n") {
		// Format: `<name>: <command...> - <status>`
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		if !strings.HasPrefix(name, "datawatch") {
			continue
		}
		rest := line[i+1:]
		if !strings.Contains(rest, "channel.js") {
			continue
		}
		// Try each scope until one removes it cleanly.
		for _, scope := range []string{"user", "local", "project"} {
			if err := exec.Command("claude", "mcp", "remove", name, "-s", scope).Run(); err == nil {
				removed = append(removed, name+"@"+scope)
				break
			}
		}
	}
	return removed
}

// channelBinPathForReg is set by RegisterSessionMCP / RegisterMCP to
// the dataDir-derived binary path. Empty means "fall back to JS".
var channelBinPathForReg string

// SetBinaryHint allows the parent daemon to pre-resolve the bridge
// binary once at startup; registerMCPNamed will prefer it over the
// JS path when set. Idempotent.
func SetBinaryHint(path string) { channelBinPathForReg = path }

// BridgeKind reports which bridge the daemon is currently configured
// to use: "go" when SetBinaryHint has been called, "js" otherwise.
// Pure-read; safe to call from any goroutine. v5.27.10.
func BridgeKind() string {
	if channelBinPathForReg != "" {
		return "go"
	}
	return "js"
}

// BridgePath returns the resolved Go bridge binary path when
// SetBinaryHint has been called, or "" when the JS path is in use.
// v5.27.10.
func BridgePath() string { return channelBinPathForReg }

func registerMCPNamed(name, channelJSPath string, env map[string]string) error {
	// Remove existing entry (ignore errors — may not exist).
	exec.Command("claude", "mcp", "remove", name, "-s", "user").Run() //nolint:errcheck

	var args []string
	if channelBinPathForReg != "" {
		// Native Go bridge — no node, no channel.js, no node_modules.
		args = []string{"mcp", "add", "--scope", "user", name, channelBinPathForReg}
	} else {
		nodePath, err := NodePath()
		if err != nil {
			return err
		}
		args = []string{"mcp", "add", "--scope", "user", name, nodePath, channelJSPath}
	}
	for k, v := range env {
		args = append(args, "--env", k+"="+v)
	}
	out, err := exec.Command("claude", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude mcp add %s: %w\n%s", name, err, string(out))
	}
	return nil
}
