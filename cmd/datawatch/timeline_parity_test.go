package main

import (
	"bytes"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
)

// timelineParityNames maps a REST operation to its counterpart on each verb
// surface. There are no exemptions: every operation is on every surface.
var timelineParityNames = map[string]struct{ mcp, cli, comm string }{
	"timeline": {"autonomous_timeline", "timeline", "timeline"},
}

func TestTimelineParity(t *testing.T) {
	mcpTools := toSet(mcp.TimelineToolNames())
	commVerbs := toSet(router.TimelineCommVerbs())
	cliVerbs := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		cliVerbs[c.Name()] = true
	}

	ops := server.TimelineRESTOperations()
	if len(ops) == 0 {
		t.Fatal("the server lists no timeline REST operations")
	}
	seen := map[string]bool{}
	for _, op := range ops {
		seen[op.Name] = true
		names, ok := timelineParityNames[op.Name]
		if !ok {
			t.Errorf("REST operation %q has no entry in timelineParityNames; map it to its MCP tool, CLI subcommand and comm verb", op.Name)
			continue
		}
		if !mcpTools[names.mcp] {
			t.Errorf("REST operation %q has no MCP tool %q", op.Name, names.mcp)
		}
		if !cliVerbs[names.cli] {
			t.Errorf("REST operation %q has no CLI subcommand %q", op.Name, names.cli)
		}
		if !commVerbs[names.comm] {
			t.Errorf("REST operation %q has no comm verb %q", op.Name, names.comm)
		}
		if router.Parse(names.comm).Type != router.CmdTimeline {
			t.Errorf("comm verb %q does not parse as the timeline command", names.comm)
		}
		if op.Method != "GET" {
			t.Errorf("REST operation %q is %s; the timeline operations are read-only", op.Name, op.Method)
		}
	}
	for name := range timelineParityNames {
		if !seen[name] {
			t.Errorf("timelineParityNames entry %q matches no REST operation", name)
		}
	}
}

// timelineParityManager is the manager double behind the real handler: it
// supplies the record source the timeline is drawn from and nothing else.
type timelineParityManager struct {
	server.AutonomousAPI
	prds []*autonomous.PRD
}

func (m *timelineParityManager) TimelineRecords() []*autonomous.PRD { return m.prds }

// timelineParityDaemon runs the real timeline handler over a fake manager and a
// fixed clock, and returns the daemon's base URL.
func timelineParityDaemon(t *testing.T) *httptest.Server {
	t.Helper()
	mgr := &timelineParityManager{prds: timelineTextFixturePRDs()}
	srv := httptest.NewServer(server.NewTimelineHandler(mgr, func() time.Time { return timelineParityNow }))
	t.Cleanup(srv.Close)
	return srv
}

var timelineParityNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// timelineTextFixturePRDs is the chart the every-surface test draws: two
// Automata, one finished and one still running with a blocked member and a
// projected finish.
func timelineTextFixturePRDs() []*autonomous.PRD {
	h := func(d, hr int) *time.Time {
		tt := time.Date(2026, 10, d, hr, 0, 0, 0, time.UTC)
		return &tt
	}
	return []*autonomous.PRD{
		{ID: "aaaa0001", Title: "Fix the crash", Status: autonomous.PRDRunning,
			ProjectDir: "/home/<user>/projA", Kind: autonomous.PRDKindBug,
			Story: []autonomous.Story{{
				ID: "aaaa0001-s1", Title: "Find and fix it", Status: autonomous.StoryInProgress,
				Tasks: []autonomous.Task{
					{ID: "aaaa0001-t1", Title: "Reproduce the crash", Status: autonomous.TaskCompleted,
						StartedAt: h(20, 9), CompletedAt: h(20, 12)},
					{ID: "aaaa0001-t2", Title: "Land the fix", Status: autonomous.TaskCompleted,
						StartedAt: h(21, 8), CompletedAt: h(21, 12)},
				},
			}},
		},
		{ID: "bbbb0002", Title: "Add the widget", Status: autonomous.PRDRunning,
			ProjectDir: "/home/<user>/projB", Kind: autonomous.PRDKindFeature,
			Story: []autonomous.Story{{
				ID: "bbbb0002-s1", Title: "Wire it up", Status: autonomous.StoryInProgress,
				Tasks: []autonomous.Task{
					{ID: "bbbb0002-t1", Title: "Build the shell", Status: autonomous.TaskCompleted,
						StartedAt: h(21, 8), CompletedAt: h(21, 11)},
					{ID: "bbbb0002-t2", Title: "Paint it", Status: autonomous.TaskCompleted,
						StartedAt: h(22, 9), CompletedAt: h(22, 11)},
					{ID: "bbbb0002-t3", Title: "Fit the widget", Status: autonomous.TaskBlocked,
						StartedAt:  h(23, 8),
						UpdatedAt:  time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC),
						WaitReason: "waiting on the operator"},
					{ID: "bbbb0002-t4", Title: "Ship it", Status: autonomous.TaskPending},
				},
			}},
		},
	}
}

func TestTimelineSameObjectOnEverySurface(t *testing.T) {
	srv := timelineParityDaemon(t)
	mcpSrv := progressParityMCP(t, srv)

	t.Run("chart", func(t *testing.T) {
		rest := progressParityGet(t, srv.URL, "/api/autonomous/timeline")
		restObj := progressParityDecode(t, "REST body", rest)
		if restObj["projects"] == nil || restObj["today"] == nil {
			t.Fatalf("REST body is not a chart object: %s", rest)
		}
		cliObj := progressParityDecode(t, "CLI --json", progressParityCLIBody(t, srv, "timeline"))
		mcpObj := progressParityDecode(t, "MCP result", progressParityMCPBody(t, mcpSrv, "autonomous_timeline", nil))
		if !reflect.DeepEqual(restObj, cliObj) {
			t.Errorf("CLI --json differs from REST\nREST: %s", rest)
		}
		if !reflect.DeepEqual(restObj, mcpObj) {
			t.Errorf("MCP result differs from REST\nREST: %s", rest)
		}
		// The CLI --json output is the REST body byte for byte.
		if got := progressParityCLIBody(t, srv, "timeline"); !bytes.Equal(got, rest) {
			t.Errorf("CLI --json is not the REST body untouched")
		}

		// The text bars the CLI prints are the golden bars of this fixture.
		prev := serverURL
		serverURL = srv.URL
		t.Cleanup(func() { serverURL = prev })
		t.Setenv("DATAWATCH_TOKEN", "test-token")
		var out bytes.Buffer
		c := newAutonomousCmd()
		c.SetOut(&out)
		c.SetErr(&out)
		c.SetArgs([]string{"timeline"})
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		wantBars := `Timeline as of 2026-10-23: 2 projects, 2 Automata, 2 stages, 6 members, 1 blocked
span 2026-10-20 -> 2026-10-25   # work  X blocked  ~ waiting  T today  P projected finish

/home/<user>/projA
  Fix the crash (bug, running) — finished 2026-10-21
    Find and fix it (in_progress)
      Reproduce the crash              #                      T                
      Land the fix                            ##              T                
/home/<user>/projB
  Add the widget (feature, running) — projected 2026-10-25
    Wire it up (in_progress)
      Build the shell                         ##              T               P
      Paint it                                        #       T               P
      Fit the widget                                         XT               P
      Ship it                                                 T               P
`
		if out.String() != wantBars {
			t.Fatalf("the CLI text bars differ from the golden\n got:\n%s\nwant:\n%s", out.String(), wantBars)
		}

		// The comm reply holds the same summary lines as the shared renderer.
		res, err := autonomous.ParseTimeline(rest)
		if err != nil {
			t.Fatal(err)
		}
		if res.Counts.Automata != 2 || res.Counts.Blocked != 1 {
			t.Fatalf("fixture counts = %v; want 2 Automata, 1 blocked", res.Counts)
		}
		reply := progressParityCommReply(t, srv, "timeline")
		for _, line := range strings.Split(strings.TrimRight(autonomous.RenderTimelineSummary(res), "\n"), "\n") {
			if !strings.Contains(reply, line) {
				t.Errorf("comm reply lacks the line %q\nreply:\n%s", line, reply)
			}
		}
	})

	t.Run("chart with filters", func(t *testing.T) {
		q := "?automaton=Add%20the%20widget"
		rest := progressParityDecode(t, "REST body", progressParityGet(t, srv.URL, "/api/autonomous/timeline"+q))
		cli := progressParityDecode(t, "CLI --json", progressParityCLIBody(t, srv, "timeline", "--automaton", "Add the widget"))
		viaMCP := progressParityDecode(t, "MCP result", progressParityMCPBody(t, mcpSrv, "autonomous_timeline", map[string]any{"automaton": "Add the widget"}))
		if !reflect.DeepEqual(rest, cli) || !reflect.DeepEqual(rest, viaMCP) {
			t.Errorf("filtered objects differ\nREST: %v\nCLI: %v\nMCP: %v", rest, cli, viaMCP)
		}
		unfiltered := progressParityDecode(t, "REST body", progressParityGet(t, srv.URL, "/api/autonomous/timeline"))
		if reflect.DeepEqual(rest["projects"], unfiltered["projects"]) {
			t.Error("the filter changed nothing; the test would not catch a surface that drops it")
		}
	})
}
