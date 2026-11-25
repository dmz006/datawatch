package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/channel"
)

// The idle check must still know a session's own MCP servers now that its
// entry lives in the private launch folder and not in the project file:
// otherwise the session's own bridge counts as work and idle never fires.
func TestLaunch_IdleCheckKnowsServersFromThePrivateLaunchFile(t *testing.T) {
	f := newLaunchMCPFixture(t)
	project := t.TempDir()
	s := launchMCPSession("ffff", "claude-code", project, true)
	f.preLaunch(t, s, "token-f", map[string]channel.MCPServerSpec{"web_search": {Command: "/bin/datawatch", Args: []string{"mcp-search"}}})

	want := channel.LaunchMCPServers(s.TmuxSession, project)[channel.ProjectMCPEntryName]
	for _, spec := range sessionLaunchMCPSpecs(s.TmuxSession, project) {
		if spec.Command == want.Command && len(spec.Args) == len(want.Args) {
			return
		}
	}
	t.Fatalf("idle check does not know the session's own bridge %+v", want)
}
