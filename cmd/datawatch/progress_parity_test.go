package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

// progressParityNames maps a REST operation to its counterpart on each verb
// surface. There are no exemptions: every operation is on every surface.
var progressParityNames = map[string]struct{ mcp, cli, comm string }{
	"progress":       {"autonomous_progress", "progress", "progress"},
	"progress_trend": {"autonomous_progress_trend", "progress-trend", "progress trend"},
}

func TestProgressParity(t *testing.T) {
	mcpTools := toSet(mcp.ProgressToolNames())
	commVerbs := toSet(router.ProgressCommVerbs())
	cliVerbs := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		cliVerbs[c.Name()] = true
	}

	ops := server.ProgressRESTOperations()
	if len(ops) == 0 {
		t.Fatal("the server lists no progress REST operations")
	}
	seen := map[string]bool{}
	for _, op := range ops {
		seen[op.Name] = true
		names, ok := progressParityNames[op.Name]
		if !ok {
			t.Errorf("REST operation %q has no entry in progressParityNames; map it to its MCP tool, CLI subcommand and comm verb", op.Name)
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
		if router.Parse(names.comm).Type != router.CmdProgress {
			t.Errorf("comm verb %q does not parse as the progress command", names.comm)
		}
		if op.Method != http.MethodGet {
			t.Errorf("REST operation %q is %s; the progress operations are read-only", op.Name, op.Method)
		}
	}
	for name := range progressParityNames {
		if !seen[name] {
			t.Errorf("progressParityNames entry %q matches no REST operation", name)
		}
	}
}

// progressParityManager is the manager double behind the real handler: it
// supplies the record source and the snapshot store and nothing else.
type progressParityManager struct {
	server.AutonomousAPI
	prds  []*autonomous.PRD
	snaps *autonomous.ProgressSnapshotStore
}

type progressParitySource struct{ prds []*autonomous.PRD }

func (s progressParitySource) ListPRDs() []*autonomous.PRD { return s.prds }

func (m *progressParityManager) ProgressSource() autonomous.ProgressSource {
	return progressParitySource{prds: m.prds}
}
func (m *progressParityManager) ProgressSnapshots() *autonomous.ProgressSnapshotStore {
	return m.snaps
}

var progressParityNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func progressParityPRDs() []*autonomous.PRD {
	h := func(d, hr int) *time.Time {
		t := time.Date(2026, 10, d, hr, 0, 0, 0, time.UTC)
		return &t
	}
	task := func(id string, st autonomous.TaskStatus, a, b *time.Time) autonomous.Task {
		return autonomous.Task{ID: id, Status: st, StartedAt: a, CompletedAt: b}
	}
	return []*autonomous.PRD{
		{ID: "bbbb0001", Title: "Fix the crash", Status: autonomous.PRDCompleted, CreatedAt: *h(12, 8), Kind: autonomous.PRDKindBug,
			Story: []autonomous.Story{{Tasks: []autonomous.Task{task("t1", autonomous.TaskCompleted, h(12, 9), h(12, 12))}}}},
		{ID: "bbbb0002", Title: "Add the widget", Status: autonomous.PRDCompleted, CreatedAt: *h(18, 8), Kind: autonomous.PRDKindFeature,
			Story: []autonomous.Story{{Tasks: []autonomous.Task{task("t2", autonomous.TaskCompleted, h(18, 9), h(18, 13))}}}},
		{ID: "bbbb0003", Title: "Add the gadget", Status: autonomous.PRDRunning, CreatedAt: *h(21, 8), Kind: autonomous.PRDKindFeature,
			Story: []autonomous.Story{{Tasks: []autonomous.Task{
				task("t3", autonomous.TaskCompleted, h(22, 9), h(22, 10)),
				task("t4", autonomous.TaskPending, nil, nil)}}}},
	}
}

// progressParityDaemon runs the real progress handler over a fake manager and a
// fixed clock, and returns the daemon's base URL.
func progressParityDaemon(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	backlog := filepath.Join(dir, "README.md")
	rows := "| 1 | Add the gadget `bbbb0003` | Running |\n| 2 | AB42 Write the migration guide | Not started |\n"
	if err := os.WriteFile(backlog, []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	snapDir := filepath.Join(dir, "snapshots")
	if err := os.MkdirAll(snapDir, 0o700); err != nil {
		t.Fatal(err)
	}
	snaps, _, err := autonomous.NewProgressSnapshotStore(snapDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Autonomous.Progress.BacklogPath = backlog
	mgr := &progressParityManager{prds: progressParityPRDs(), snaps: snaps}
	if err := snaps.RecordDaily(progressParityNow.Add(-24*time.Hour), autonomous.CollectProgress(
		progressParitySource{prds: mgr.prds}, nil, nil, autonomous.ProgressOptions{Now: progressParityNow.Add(-24 * time.Hour)})); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.NewProgressHandler(mgr, cfg, func() time.Time { return progressParityNow }))
	t.Cleanup(srv.Close)
	return srv
}

func progressParityGet(t *testing.T, base, path string) []byte {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, resp.StatusCode, body)
	}
	return body
}

func progressParityDecode(t *testing.T, label string, body []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("%s is not a JSON object: %v\n%s", label, err, body)
	}
	return v
}

func progressParityMCP(t *testing.T, srv *httptest.Server) *mcp.Server {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	allowTestTemp(mgr)
	mgr.WithFakeTmux()
	s := mcp.New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, mcp.Options{Version: "test"})
	s.SetWebPort(port)
	return s
}

func progressParityMCPBody(t *testing.T, s *mcp.Server, tool string, args map[string]any) []byte {
	t.Helper()
	res, err := s.CallTool(context.Background(), tool, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || len(res.Content) == 0 {
		t.Fatalf("%s failed: %+v", tool, res)
	}
	text, _ := res.Content[0]["text"].(string)
	return []byte(text)
}

func progressParityCLIBody(t *testing.T, srv *httptest.Server, args ...string) []byte {
	t.Helper()
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	t.Setenv("DATAWATCH_TOKEN", "test-token")
	var out bytes.Buffer
	c := newAutonomousCmd()
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append(args, "--json"))
	if err := c.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return out.Bytes()
}

func TestProgressSameObjectOnEverySurface(t *testing.T) {
	srv := progressParityDaemon(t)
	mcpSrv := progressParityMCP(t, srv)

	t.Run("metrics", func(t *testing.T) {
		rest := progressParityGet(t, srv.URL, "/api/autonomous/progress")
		restObj := progressParityDecode(t, "REST body", rest)
		if restObj["generated_at"] == nil || restObj["totals"] == nil {
			t.Fatalf("REST body is not a metrics object: %s", rest)
		}
		cliObj := progressParityDecode(t, "CLI --json", progressParityCLIBody(t, srv, "progress"))
		mcpObj := progressParityDecode(t, "MCP result", progressParityMCPBody(t, mcpSrv, "autonomous_progress", nil))
		if !reflect.DeepEqual(restObj, cliObj) {
			t.Errorf("CLI --json differs from REST\nREST: %s", rest)
		}
		if !reflect.DeepEqual(restObj, mcpObj) {
			t.Errorf("MCP result differs from REST\nREST: %s", rest)
		}
		// The CLI --json output is the REST body byte for byte.
		if got := progressParityCLIBody(t, srv, "progress"); !bytes.Equal(got, rest) {
			t.Errorf("CLI --json is not the REST body untouched")
		}

		// The comm reply holds the same totals as the shared text renderer.
		m, err := autonomous.ParseProgressMetrics(rest)
		if err != nil {
			t.Fatal(err)
		}
		if m.Totals.PRDs != 3 || m.Totals.Done != 2 {
			t.Fatalf("fixture totals = %d records, %d done; want 3 and 2", m.Totals.PRDs, m.Totals.Done)
		}
		reply := progressParityCommReply(t, srv, "progress")
		for _, line := range strings.Split(strings.TrimRight(autonomous.RenderProgressText(m), "\n"), "\n") {
			if !strings.Contains(reply, line) {
				t.Errorf("comm reply lacks the line %q\nreply:\n%s", line, reply)
			}
		}
		if strings.Contains(reply, "AB42") {
			t.Errorf("a tracker token reached the comm reply:\n%s", reply)
		}
	})

	t.Run("metrics with filters", func(t *testing.T) {
		q := "?since=2026-10-15&until=2026-10-22"
		rest := progressParityDecode(t, "REST body", progressParityGet(t, srv.URL, "/api/autonomous/progress"+q))
		cli := progressParityDecode(t, "CLI --json", progressParityCLIBody(t, srv, "progress", "--since", "2026-10-15", "--until", "2026-10-22"))
		viaMCP := progressParityDecode(t, "MCP result", progressParityMCPBody(t, mcpSrv, "autonomous_progress", map[string]any{"since": "2026-10-15", "until": "2026-10-22"}))
		if !reflect.DeepEqual(rest, cli) || !reflect.DeepEqual(rest, viaMCP) {
			t.Errorf("filtered objects differ\nREST: %v\nCLI: %v\nMCP: %v", rest, cli, viaMCP)
		}
		unfiltered := progressParityDecode(t, "REST body", progressParityGet(t, srv.URL, "/api/autonomous/progress"))
		if reflect.DeepEqual(rest["totals"], unfiltered["totals"]) {
			t.Error("the date filter changed nothing; the test would not catch a surface that drops it")
		}
	})

	t.Run("trend", func(t *testing.T) {
		rest := progressParityGet(t, srv.URL, "/api/autonomous/progress/trend?days=7")
		restObj := progressParityDecode(t, "REST body", rest)
		if pts, _ := restObj["points"].([]any); len(pts) != 1 {
			t.Fatalf("fixture should hold one snapshot: %s", rest)
		}
		cli := progressParityDecode(t, "CLI --json", progressParityCLIBody(t, srv, "progress-trend", "--days", "7"))
		viaMCP := progressParityDecode(t, "MCP result", progressParityMCPBody(t, mcpSrv, "autonomous_progress_trend", map[string]any{"days": float64(7)}))
		if !reflect.DeepEqual(restObj, cli) || !reflect.DeepEqual(restObj, viaMCP) {
			t.Errorf("trend objects differ\nREST: %s", rest)
		}
		tb, err := autonomous.ParseProgressTrend(rest)
		if err != nil {
			t.Fatal(err)
		}
		reply := progressParityCommReply(t, srv, "progress trend 7")
		for _, line := range strings.Split(strings.TrimRight(autonomous.RenderTrendReply(tb.Days, tb.Direction, tb.Points), "\n"), "\n") {
			if !strings.Contains(reply, line) {
				t.Errorf("comm reply lacks the line %q\nreply:\n%s", line, reply)
			}
		}
	})
}

// progressParityCommReply sends text through a router whose REST loopback is
// the test daemon and returns everything it replied.
func progressParityCommReply(t *testing.T, srv *httptest.Server, text string) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	r := router.NewRouter("testhost", "", nil, nil, 0, nil)
	r.SetWebPort(port)
	return strings.Join(r.HandleTestMessage(text), "\n")
}
