// Per-session launch MCP files.
//
// A session's datawatch entry carries its API token and its identity. It is
// kept in a private folder of its own, <data_dir>/launch/<tmux session>/
// (owner-only), never in the project folder, where sessions sharing the
// folder would read each other's identity and a commit could take the token.

package channel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/session"
)

const (
	launchDirName = "launch"
	// SessionMCPFileName is the MCP config file inside a session's private
	// launch folder. It has the "mcpServers" shape of .mcp.json.
	SessionMCPFileName = "session-mcp.json"
)

var (
	launchMu    sync.Mutex
	launchRoot  string                // data dir used for files written before a restart
	launchFiles = map[string]string{} // tmux session -> its file
)

// SetLaunchRoot tells the package the daemon's data directory, so the launch
// file of a session wired before a daemon restart is still found.
func SetLaunchRoot(dataDir string) {
	launchMu.Lock()
	defer launchMu.Unlock()
	launchRoot = dataDir
}

// sessionLaunchDir is the private launch folder of one tmux session. The name
// must be a single path element, so it cannot point anywhere else.
func sessionLaunchDir(dataDir, tmuxSession string) (string, error) {
	if dataDir == "" {
		return "", fmt.Errorf("session launch folder: data directory is required")
	}
	if tmuxSession == "" || tmuxSession == "." || tmuxSession == ".." ||
		strings.ContainsAny(tmuxSession, "/\\\x00") {
		return "", fmt.Errorf("session launch folder: bad tmux session name %q", tmuxSession)
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("session launch folder: %w", err)
	}
	return filepath.Join(abs, launchDirName, tmuxSession), nil
}

// WriteSessionMCPConfig writes the MCP config of one session: its datawatch
// bridge entry with env (token and identity) plus the configured extras. The
// folder is 0700, the file 0600, written atomically. Returns the file path.
func WriteSessionMCPConfig(dataDir, tmuxSession, channelJSPath string, env map[string]string, extras map[string]MCPServerSpec) (string, error) {
	dir, err := sessionLaunchDir(dataDir, tmuxSession)
	if err != nil {
		return "", err
	}
	spec, err := bridgeSpec(channelJSPath, env)
	if err != nil {
		return "", fmt.Errorf("session MCP config: %w", err)
	}
	cfg := MCPProjectConfig{MCPServers: map[string]MCPServerSpec{ProjectMCPEntryName: spec}}
	for name, extra := range extras {
		if name != ProjectMCPEntryName {
			cfg.MCPServers[name] = extra
		}
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("session MCP config: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("session MCP config: %w", err)
	}
	for _, d := range []string{filepath.Dir(dir), dir} {
		if err := os.Chmod(d, 0o700); err != nil { // #nosec G302 -- a folder needs the owner execute bit
			return "", fmt.Errorf("session MCP config: %w", err)
		}
	}
	tmp, err := os.CreateTemp(dir, "."+SessionMCPFileName+".tmp-*") // 0600
	if err != nil {
		return "", fmt.Errorf("session MCP config: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after the rename
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("session MCP config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("session MCP config: %w", err)
	}
	file := filepath.Join(dir, SessionMCPFileName)
	if err := os.Rename(tmp.Name(), file); err != nil {
		return "", fmt.Errorf("session MCP config: %w", err)
	}
	launchMu.Lock()
	launchFiles[tmuxSession] = file
	launchMu.Unlock()
	return file, nil
}

// RemoveSessionMCPConfig removes one session's launch file and its folder.
// Only that file is removed, and the folder only when nothing else is in it.
// A session without one is not an error.
func RemoveSessionMCPConfig(dataDir, tmuxSession string) error {
	dir, err := sessionLaunchDir(dataDir, tmuxSession)
	if err != nil {
		return err
	}
	launchMu.Lock()
	delete(launchFiles, tmuxSession)
	launchMu.Unlock()
	if err := os.Remove(filepath.Join(dir, SessionMCPFileName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SessionMCPServers returns the servers in one session's private launch file,
// or nil when it has none.
func SessionMCPServers(tmuxSession string) map[string]MCPServerSpec {
	launchMu.Lock()
	file, root := launchFiles[tmuxSession], launchRoot
	launchMu.Unlock()
	if file == "" {
		dir, err := sessionLaunchDir(root, tmuxSession)
		if err != nil {
			return nil
		}
		file = filepath.Join(dir, SessionMCPFileName)
	}
	return readMCPServers(file)
}

func readMCPServers(file string) map[string]MCPServerSpec {
	raw, err := os.ReadFile(file) // #nosec G304 -- a session launch file or a project .mcp.json
	if err != nil {
		return nil
	}
	var cfg MCPProjectConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.MCPServers
}

// LaunchMCPServers returns the MCP servers one session's launch may pick
// from: the operator's servers in the project's .mcp.json plus the session's
// own private file, which wins on a shared name. A daemon entry found in the
// project file belongs to some other or earlier launch and is never used.
func LaunchMCPServers(tmuxSession, projectDir string) map[string]MCPServerSpec {
	out := map[string]MCPServerSpec{}
	if projectDir != "" {
		for name, spec := range readMCPServers(filepath.Join(projectDir, ".mcp.json")) {
			if !session.IsDaemonMCPEntry(name, spec.Env) {
				out[name] = spec
			}
		}
	}
	for name, spec := range SessionMCPServers(tmuxSession) {
		out[name] = spec
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// RemoveProjectDaemonEntry removes the datawatch entry from the project's
// .mcp.json when it is one the daemon wrote and match accepts its env (a nil
// match accepts every daemon entry). Everything else in the file is kept as
// it was, including fields this package does not know. Returns true when an
// entry was removed.
func RemoveProjectDaemonEntry(projectDir string, match func(env map[string]string) bool) (bool, error) {
	if projectDir == "" {
		return false, nil
	}
	file := filepath.Join(projectDir, ".mcp.json")
	entry, ok := readMCPServers(file)[ProjectMCPEntryName]
	if !ok || !session.IsDaemonMCPEntry(ProjectMCPEntryName, entry.Env) || (match != nil && !match(entry.Env)) {
		return false, nil
	}
	raw, err := os.ReadFile(file) // #nosec G304 -- the project's .mcp.json
	if err != nil {
		return false, err
	}
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, err
	}
	servers := map[string]json.RawMessage{}
	if err := json.Unmarshal(doc["mcpServers"], &servers); err != nil {
		return false, err
	}
	delete(servers, ProjectMCPEntryName)
	if doc["mcpServers"], err = json.Marshal(servers); err != nil {
		return false, err
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(file, out, 0o600); err != nil {
		return false, err
	}
	return true, os.Chmod(file, 0o600)
}
