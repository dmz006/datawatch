package claudecode

import (
	"context"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// Claude Code accepts a channel only from a configured server whose name is
// exactly the one --channels names. A strict profile launch configures the
// bridge as "datawatch" and nothing else, so --channels must name that; a
// launch without a profile keeps the per-session name.
func TestLaunchChannelNameMatchesTheConfiguredServer(t *testing.T) {
	prof, ok := workerprofile.Builtin("datawatch-tools")
	if !ok {
		t.Fatal("builtin datawatch-tools is missing")
	}
	strict := WorkerProfileLaunch{Profile: prof, MCPServers: map[string]channel.MCPServerSpec{"datawatch": {Command: "bridge"}}}
	noEntry := WorkerProfileLaunch{Profile: prof}
	for _, tc := range []struct {
		name   string
		w      WorkerProfileLaunch
		resume bool
		want   string
	}{
		{"profile launch", strict, false, "server:datawatch"},
		{"profile resume", strict, true, "server:datawatch"},
		{"no profile", WorkerProfileLaunch{}, false, "server:datawatch-host-abcd"},
		{"no profile resume", WorkerProfileLaunch{}, true, "server:datawatch-host-abcd"},
		{"profile without a bridge entry", noEntry, false, "server:datawatch-host-abcd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argv := startFakeTmux(t, false)
			b := NewWithOptions("claude", false, true).(*Backend)
			var err error
			if tc.resume {
				err = b.LaunchResumeWithProfile(context.Background(), tc.w, "", "cs-host-abcd", t.TempDir(), "", "name")
			} else {
				err = b.LaunchWithProfile(context.Background(), tc.w, "", "cs-host-abcd", t.TempDir(), "")
			}
			if err != nil {
				t.Fatal(err)
			}
			cmd := recordedCmd(t, argv)
			for _, flag := range []string{"--channels ", "--dangerously-load-development-channels "} {
				if !strings.Contains(cmd, flag+tc.want+" ") {
					t.Errorf("want %s%s in:\n%s", flag, tc.want, cmd)
				}
			}
		})
	}
}
