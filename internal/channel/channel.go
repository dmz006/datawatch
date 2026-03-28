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

// EnsureExtracted extracts the embedded channel.js to dataDir/channel/channel.js
// if it does not exist or is outdated. Returns the path to the extracted file.
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
	return dst, nil
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
	nodePath, err := NodePath()
	if err != nil {
		return err
	}
	// Remove existing entry (ignore errors — may not exist).
	exec.Command("claude", "mcp", "remove", "datawatch").Run() //nolint:errcheck

	args := []string{"mcp", "add", "--scope", "user", "datawatch", nodePath, channelJSPath}
	for k, v := range env {
		args = append(args, "--env", k+"="+v)
	}
	out, err := exec.Command("claude", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude mcp add: %w\n%s", err, string(out))
	}
	return nil
}
