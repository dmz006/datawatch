package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// launchMCPFixture is one pre-launch in a project folder: its own session,
// its own token, the real wiring.
type launchMCPFixture struct {
	dataDir string
	// sessionEnv records the variables set on each tmux session.
	sessionEnv map[string]map[string]string
}

func newLaunchMCPFixture(t *testing.T) *launchMCPFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &launchMCPFixture{dataDir: t.TempDir(), sessionEnv: map[string]map[string]string{}}
}

func (f *launchMCPFixture) preLaunch(t *testing.T, sess *session.Session, token string, extras map[string]channel.MCPServerSpec) {
	t.Helper()
	wireSessionMCPFiles(sess, launchMCPInput{
		DataDir:       f.dataDir,
		ChannelJSPath: filepath.Join(f.dataDir, "channel", "channel.js"),
		Env:           map[string]string{"DATAWATCH_API_URL": "http://127.0.0.1:1", "DATAWATCH_TOKEN": token},
		Extras:        extras,
		SetSessionEnv: func(tmuxSession, key, value string) error {
			if f.sessionEnv[tmuxSession] == nil {
				f.sessionEnv[tmuxSession] = map[string]string{}
			}
			f.sessionEnv[tmuxSession][key] = value
			return nil
		},
	})
}

func launchMCPSession(id, backend, projectDir string, profiled bool) *session.Session {
	s := &session.Session{
		ID: id, FullID: "host-" + id, TmuxSession: "cs-host-" + id,
		BackendFamily: backend, ProjectDir: projectDir,
	}
	if profiled {
		p, _ := workerprofile.Builtin("datawatch-tools")
		s.WorkerProfile = &p
	}
	return s
}

// filesHolding returns the files under dir whose bytes contain needle.
func filesHolding(t *testing.T, dir, needle string) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(raw), needle) {
			hits = append(hits, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return hits
}

func writeProjectMCP(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func projectMCPServers(t *testing.T, dir string) map[string]channel.MCPServerSpec {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var cfg channel.MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse project .mcp.json: %v", err)
	}
	return cfg.MCPServers
}

// Two profiled launches in one folder, the second wired before the first
// reads its servers: each must get its own identity, and neither token may
// be written anywhere under the project folder.
func TestLaunch_TwoProfiledSessionsInOneFolderKeepTheirOwnTokenAndWriteNoneInTheProject(t *testing.T) {
	f := newLaunchMCPFixture(t)
	project := t.TempDir()
	a := launchMCPSession("aaaa", "claude-code", project, true)
	b := launchMCPSession("bbbb", "claude-code", project, true)

	f.preLaunch(t, a, "token-of-session-a", nil)
	f.preLaunch(t, b, "token-of-session-b", nil)

	for _, tc := range []struct {
		sess  *session.Session
		token string
	}{{a, "token-of-session-a"}, {b, "token-of-session-b"}} {
		spec := channel.LaunchMCPServers(tc.sess.TmuxSession, project)[channel.ProjectMCPEntryName]
		if got := spec.Env["DATAWATCH_TOKEN"]; got != tc.token {
			t.Errorf("%s loads token %q, want its own %q", tc.sess.FullID, got, tc.token)
		}
		if got := spec.Env["CLAUDE_SESSION_ID"]; got != tc.sess.FullID {
			t.Errorf("%s loads identity %q, want its own", tc.sess.FullID, got)
		}
		if hits := filesHolding(t, project, tc.token); len(hits) > 0 {
			t.Errorf("token of %s written into the project folder: %v", tc.sess.FullID, hits)
		}
	}
}

// A backend other than claude-code gets its MCP file in the private launch
// folder (owner-only) and the path in its session environment. The project
// file keeps operator servers and the configured extras, and no token.
func TestLaunch_OtherBackendsWriteNoTokenIntoTheProject(t *testing.T) {
	for _, backend := range []string{"opencode", "opencode-acp", "pi", "pi-prompt", "shell", "aider", "goose", "gemini"} {
		t.Run(backend, func(t *testing.T) {
			f := newLaunchMCPFixture(t)
			project := t.TempDir()
			writeProjectMCP(t, project, `{"mcpServers":{"operator-tool":{"command":"/bin/tool","args":[]}}}`)
			s := launchMCPSession("cccc", backend, project, false)

			f.preLaunch(t, s, "token-of-session-c", map[string]channel.MCPServerSpec{"extra-tool": {Command: "/bin/extra"}})

			if hits := filesHolding(t, project, "token-of-session-c"); len(hits) > 0 {
				t.Fatalf("token written into the project folder: %v", hits)
			}
			got := projectMCPServers(t, project)
			if _, ok := got["operator-tool"]; !ok {
				t.Errorf("operator server lost: %+v", got)
			}
			if _, ok := got["extra-tool"]; !ok {
				t.Errorf("configured extra server missing from the project file: %+v", got)
			}
			path := f.sessionEnv[s.TmuxSession]["DATAWATCH_MCP_CONFIG"]
			if path == "" || strings.HasPrefix(path, project) || !strings.HasPrefix(path, f.dataDir) {
				t.Fatalf("DATAWATCH_MCP_CONFIG = %q, want a file in the daemon's launch folder", path)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("launch MCP file mode = %o, want 600", fi.Mode().Perm())
			}
			di, err := os.Stat(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if di.Mode().Perm() != 0o700 {
				t.Errorf("launch folder mode = %o, want 700", di.Mode().Perm())
			}
			servers := channel.LaunchMCPServers(s.TmuxSession, project)
			if servers[channel.ProjectMCPEntryName].Env["DATAWATCH_TOKEN"] != "token-of-session-c" {
				t.Errorf("session's own datawatch entry missing: %+v", servers)
			}
			for k, v := range f.sessionEnv[s.TmuxSession] {
				if strings.Contains(v, "token-of-session-c") {
					t.Errorf("token put into the tmux environment as %s", k)
				}
			}
		})
	}
}

// The end of one session removes its own launch folder and nothing else:
// the other session in the same project folder keeps its file.
func TestLaunch_SessionEndRemovesOnlyItsOwnLaunchFolder(t *testing.T) {
	f := newLaunchMCPFixture(t)
	project := t.TempDir()
	a := launchMCPSession("aaaa", "opencode", project, false)
	b := launchMCPSession("bbbb", "claude-code", project, true)
	f.preLaunch(t, a, "token-of-session-a", nil)
	f.preLaunch(t, b, "token-of-session-b", nil)
	if len(filesHolding(t, f.dataDir, "token-of-session-a")) != 1 {
		t.Fatal("session a has no launch file of its own")
	}

	endSessionMCPFiles(a, f.dataDir)

	if hits := filesHolding(t, f.dataDir, "token-of-session-a"); len(hits) > 0 {
		t.Errorf("ended session's token still on disk: %v", hits)
	}
	if len(filesHolding(t, f.dataDir, "token-of-session-b")) != 1 {
		t.Error("the live session's launch file was removed")
	}
	if got := channel.LaunchMCPServers(b.TmuxSession, project)[channel.ProjectMCPEntryName]; got.Env["DATAWATCH_TOKEN"] != "token-of-session-b" {
		t.Errorf("live session lost its entry after the other one ended: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "launch", a.TmuxSession)); !os.IsNotExist(err) {
		t.Errorf("ended session's launch folder still there: %v", err)
	}
}

// An entry an earlier daemon wrote into the project file (it carries a token
// or a session id) is removed at the next launch there. A datawatch entry
// without either is the operator's own and stays.
func TestLaunch_StaleDaemonEntryRemovedAndHandAddedEntryKept(t *testing.T) {
	stale := `{"mcpServers":{"datawatch":{"command":"/x/datawatch-channel","args":[],"env":{"DATAWATCH_API_URL":"http://127.0.0.1:1","DATAWATCH_TOKEN":"old-dead-token"}},"operator-tool":{"command":"/bin/tool","args":[]}}}`
	hand := `{"mcpServers":{"datawatch":{"command":"datawatch","args":["mcp"]}}}`
	for name, mk := range map[string]func(dir string) *session.Session{
		"other backend":   func(d string) *session.Session { return launchMCPSession("dddd", "opencode", d, false) },
		"profiled claude": func(d string) *session.Session { return launchMCPSession("eeee", "claude-code", d, true) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newLaunchMCPFixture(t)
			project := t.TempDir()
			writeProjectMCP(t, project, stale)
			f.preLaunch(t, mk(project), "new-token", nil)
			got := projectMCPServers(t, project)
			if _, ok := got["datawatch"]; ok {
				t.Errorf("stale daemon entry kept in the project file")
			}
			if _, ok := got["operator-tool"]; !ok {
				t.Errorf("operator server lost")
			}

			writeProjectMCP(t, project, hand)
			f.preLaunch(t, mk(project), "new-token", nil)
			if got := projectMCPServers(t, project); got["datawatch"].Command != "datawatch" {
				t.Errorf("hand-added datawatch entry changed: %+v", got["datawatch"])
			}
		})
	}
}
