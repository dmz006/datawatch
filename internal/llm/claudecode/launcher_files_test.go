package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/llm/scratch"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// launchedPath returns the path the recorded launch command passes after flag
// (--mcp-config 'path') or inside the $(cat 'path') of --append-system-prompt.
func launchedPath(t *testing.T, cmd, flag string) string {
	t.Helper()
	var re *regexp.Regexp
	if flag == "--append-system-prompt" {
		re = regexp.MustCompile(`--append-system-prompt "\$\(cat '([^']+)'\)"`)
	} else {
		re = regexp.MustCompile(regexp.QuoteMeta(flag) + ` '([^']+)'`)
	}
	m := re.FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("launch command has no %s path: %s", flag, cmd)
	}
	return m[1]
}

func tokenProfileLaunch(t *testing.T, token string) WorkerProfileLaunch {
	t.Helper()
	p, ok := workerprofile.Builtin("minimal")
	if !ok {
		t.Fatal("builtin worker profile minimal not available")
	}
	p.MCP = workerprofile.MCPSelection{Mode: workerprofile.MCPDatawatch}
	return WorkerProfileLaunch{
		Profile: p,
		MCPServers: map[string]channel.MCPServerSpec{
			channel.ProjectMCPEntryName: {Command: "datawatch", Args: []string{"mcp"}, Env: map[string]string{"DATAWATCH_TOKEN": token}},
		},
		Starting: autonomous.StartingContextInput{Role: "worker", TaskSpec: "task"},
	}
}

// TestLauncherFiles_WrittenPerSessionWithOwnerOnlyMode: two sessions launched
// into the SAME project directory each get their own private launch folder
// (owner-only), the MCP config that holds the session token is owner-only,
// nothing is written into the project directory, the two sessions never share
// a file, and the session-end release removes the folder.
func TestLauncherFiles_WrittenPerSessionWithOwnerOnlyMode(t *testing.T) {
	project := t.TempDir()
	argv := startFakeTmux(t, false)
	b := backendOf()

	sessions := []struct{ tmux, token string }{
		{"cs-host-aaaa", "token-for-aaaa"},
		{"cs-host-bbbb", "token-for-bbbb"},
	}
	cfgs := map[string]string{}
	for _, s := range sessions {
		if err := b.LaunchWithProfile(context.Background(), tokenProfileLaunch(t, s.token), "", s.tmux, project, "/nonexistent/log"); err != nil {
			t.Fatalf("launch %s: %v", s.tmux, err)
		}
		cmd := recordedCmd(t, argv)
		cfgs[s.tmux] = launchedPath(t, cmd, "--mcp-config")
		ctx := launchedPath(t, cmd, "--append-system-prompt")

		if filepath.Dir(cfgs[s.tmux]) != filepath.Dir(ctx) {
			t.Errorf("%s: config and context are in different folders: %s vs %s", s.tmux, cfgs[s.tmux], ctx)
		}
		dir := filepath.Dir(cfgs[s.tmux])
		if rel, err := filepath.Rel(project, dir); err == nil && !strings.HasPrefix(rel, "..") {
			t.Errorf("%s: launch files are inside the project directory: %s", s.tmux, dir)
		}
		if st, err := os.Stat(dir); err != nil || st.Mode().Perm() != 0o700 {
			t.Errorf("%s: launch folder %s mode = %v (err %v), want 0700", s.tmux, dir, st, err)
		}
		for _, p := range []string{cfgs[s.tmux], ctx} {
			st, err := os.Stat(p)
			if err != nil {
				t.Fatalf("%s: %v", s.tmux, err)
			}
			if st.Mode().Perm() != 0o600 {
				t.Errorf("%s: %s mode = %v, want 0600", s.tmux, p, st.Mode().Perm())
			}
		}
		raw, _ := os.ReadFile(cfgs[s.tmux])
		if !strings.Contains(string(raw), s.token) {
			t.Errorf("%s: config does not hold its own token: %s", s.tmux, raw)
		}
		if !contains(scratch.Paths(s.tmux), dir) {
			t.Errorf("%s: launch folder %s not recorded for session-end release: %v", s.tmux, dir, scratch.Paths(s.tmux))
		}
	}
	if cfgs[sessions[0].tmux] == cfgs[sessions[1].tmux] {
		t.Errorf("two sessions share one config file: %s", cfgs[sessions[0].tmux])
	}
	// The project directory holds no launch file and no token.
	entries, _ := os.ReadDir(project)
	if len(entries) != 0 {
		t.Errorf("project directory not empty after launch: %v", entries)
	}
	// Session end removes exactly that session's folder.
	gone := filepath.Dir(cfgs[sessions[0].tmux])
	kept := filepath.Dir(cfgs[sessions[1].tmux])
	scratch.ReleaseSession(sessions[0].tmux)
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Errorf("session end left %s behind", gone)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("session end removed another session's folder %s: %v", kept, err)
	}
	scratch.ReleaseSession(sessions[1].tmux)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
