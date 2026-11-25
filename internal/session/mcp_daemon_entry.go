package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The daemon's own entry in an .mcp.json, and the two env keys that make a
// copy of it matter: the API token and the session identity.
const (
	daemonMCPEntryName  = "datawatch"
	daemonMCPTokenKey   = "DATAWATCH_TOKEN"
	mcpProjectFileName  = ".mcp.json"
	daemonMCPAPIURLKey  = "DATAWATCH_API_URL"
	daemonMCPSessionKey = "CLAUDE_SESSION_ID"
)

// literalDaemonToken reports whether env carries an API token written out in
// full. A ${VAR} reference holds no secret.
func literalDaemonToken(env map[string]string) bool {
	tok := env[daemonMCPTokenKey]
	return tok != "" && !strings.Contains(tok, "${")
}

// IsDaemonMCPEntry reports whether an .mcp.json server entry is one the daemon
// wrote for a session: the shared entry name, the daemon's API URL, and a
// token or a session id. An entry without those is the operator's own.
func IsDaemonMCPEntry(name string, env map[string]string) bool {
	return name == daemonMCPEntryName && env[daemonMCPAPIURLKey] != "" &&
		(literalDaemonToken(env) || env[daemonMCPSessionKey] != "")
}

// MCPFileHoldsDaemonEntry reports whether the bytes of an .mcp.json hold a
// daemon entry or a written-out daemon token under any name. A file that
// cannot be parsed but mentions the token key counts as holding one.
func MCPFileHoldsDaemonEntry(raw []byte) bool {
	var cfg struct {
		MCPServers map[string]struct {
			Env map[string]any `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return bytes.Contains(raw, []byte(daemonMCPTokenKey))
	}
	for name, spec := range cfg.MCPServers {
		env := make(map[string]string, len(spec.Env))
		for k, v := range spec.Env {
			if s, ok := v.(string); ok {
				env[k] = s
			}
		}
		if literalDaemonToken(env) || IsDaemonMCPEntry(name, env) {
			return true
		}
	}
	return false
}

// IsMCPFileWithDaemonEntry reports whether the file rel inside dir is an
// .mcp.json holding a daemon entry. No automatic commit or snapshot may take
// such a file. The file is read through a root opened on dir, so a parent
// folder that is, or is swapped for, a link leaving dir is not followed; git
// does not add a file through such a link either.
func IsMCPFileWithDaemonEntry(dir, rel string) bool {
	if path.Base(strings.ReplaceAll(rel, "\\", "/")) != mcpProjectFileName {
		return false
	}
	if dir == "" {
		dir = "." // git runs in the working directory too
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	name := filepath.FromSlash(rel)
	fi, err := root.Lstat(name)
	if err != nil || !fi.Mode().IsRegular() {
		return false // git stores a link as its target path, not its content
	}
	raw, err := root.ReadFile(name)
	if err != nil {
		return false
	}
	return MCPFileHoldsDaemonEntry(raw)
}

// unstageDaemonMCPFiles takes every staged .mcp.json that holds a daemon entry
// out of the index, so a commit cannot carry one a worker staged itself. The
// working file is not touched. An error means a token may still be staged, so
// the caller must not commit.
func (g *ProjectGit) unstageDaemonMCPFiles() error {
	if !g.IsRepo() {
		return nil // nothing can be staged
	}
	staged, err := gitOutput(g.dir, "diff", "--cached", "--name-only", "-z", "--diff-filter=d")
	if err != nil {
		return fmt.Errorf("check staged files for a daemon token: %w", err)
	}
	for _, rel := range strings.Split(staged, "\x00") {
		rel = strings.Trim(rel, "\n")
		if path.Base(rel) != mcpProjectFileName {
			continue
		}
		blob, berr := gitOutputOperands(g.dir, []string{"show"}, ":"+rel)
		if berr != nil || !MCPFileHoldsDaemonEntry([]byte(blob)) {
			continue
		}
		if rerr := runGitEnv(g.dir, nil, []string{"reset", "-q"}, ":(top,literal)"+rel); rerr != nil {
			return fmt.Errorf("unstage %s holding a daemon token: %w", rel, rerr)
		}
		fmt.Printf("[datawatch] warning: %s: unstaged %s — it holds a daemon MCP entry with a token\n", g.dir, rel)
	}
	return nil
}
