package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
)

func ciGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestChecksShipIntegration drives the pre-start verifier (on) and the ship
// step (shadow) through a real Manager, then reads the recorded findings back
// through REST, the CLI, the MCP tools and the comm verbs. Everything lives in
// temporary directories; no daemon is started.
func TestChecksShipIntegration(t *testing.T) {
	autonomous.SetChecksConfig(config.AutonomousChecksConfig{PreStartVerifier: "on"})
	autonomous.SetShipConfig(config.AutonomousShipConfig{Push: "shadow", Tag: "shadow"})
	t.Cleanup(func() {
		autonomous.SetChecksConfig(config.AutonomousChecksConfig{})
		autonomous.SetShipConfig(config.AutonomousShipConfig{})
	})

	// A work repo with a local bare remote and a release version.
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	ciGit(t, root, "init", "--bare", "-b", "main", remote)
	ciGit(t, root, "init", "-b", "main", work)
	ciGit(t, work, "remote", "add", "origin", remote)
	if err := os.MkdirAll(filepath.Join(work, "cmd/datawatch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "cmd/datawatch/main.go"),
		[]byte("package main\n\nvar Version = \"1.2.3\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ciGit(t, work, "add", ".")
	ciGit(t, work, "commit", "-m", "release")
	// Publish main once so the remote has a real refs/heads/main to compare.
	ciGit(t, work, "push", "origin", "main")
	// refsOf lists every ref (name and object) on the remote and in the work
	// repo, so any push, tag or branch move shows up as a difference.
	refsOf := func() (string, string) {
		return ciGit(t, remote, "for-each-ref", "--format=%(refname) %(objectname)"),
			ciGit(t, work, "for-each-ref", "--format=%(refname) %(objectname)") + "\nHEAD " + ciGit(t, work, "rev-parse", "HEAD")
	}
	remoteBefore, workBefore := refsOf()

	m, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	spawns := 0
	spawn := func(_ context.Context, _ autonomous.SpawnRequest) (autonomous.SpawnResult, error) {
		spawns++
		return autonomous.SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *autonomous.PRD, _ *autonomous.Task) (autonomous.VerificationResult, error) {
		return autonomous.VerificationResult{OK: true, Summary: "ok"}, nil
	}
	newPRD := func(title, script string) *autonomous.PRD {
		prd, err := m.CreatePRD(title, work, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		task := autonomous.Task{Title: "t", Spec: "do it", VerificationScript: script}
		if err := m.Store().SetStories(prd.ID, []autonomous.Story{{Title: "S", Tasks: []autonomous.Task{task}}}); err != nil {
			t.Fatal(err)
		}
		prd, _ = m.Store().GetPRD(prd.ID)
		prd.Status = autonomous.PRDApproved
		prd.Git.Branch = "main"
		_ = m.Store().SavePRD(prd)
		return prd
	}

	// 1. A task whose verification script is missing is blocked before any
	// worker starts, and a triage record is written.
	blocked := newPRD("missing script", "scripts/verify-missing.sh")
	var seenMu sync.Mutex
	var blockedSeen []string
	cancelSub := m.Subscribe(autonomous.LifecycleFilter{PRDID: blocked.ID}, func(ev autonomous.LifecycleEvent) {
		if ev.NewStatus != "" && strings.HasPrefix(ev.Kind, "task.") {
			seenMu.Lock()
			blockedSeen = append(blockedSeen, ev.NewStatus)
			seenMu.Unlock()
		}
	})
	_ = m.Run(context.Background(), blocked.ID, spawn, verify)
	deadline := time.Now().Add(5 * time.Second)
	hasBlocked := func() bool {
		seenMu.Lock()
		defer seenMu.Unlock()
		for _, st := range blockedSeen {
			if st == string(autonomous.TaskBlocked) {
				return true
			}
		}
		return false
	}
	for !hasBlocked() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancelSub()
	if !hasBlocked() {
		t.Fatalf("task never reached status blocked; transitions %v", blockedSeen)
	}
	got, _ := m.Store().GetPRD(blocked.ID)
	finalTaskStatus := string(got.Story[0].Tasks[0].Status)
	snap, ok := m.Store().SnapshotPRD(blocked.ID)
	if !ok || snap.Triage == nil || len(snap.Triage.Notes) == 0 {
		t.Fatal("blocked task left no triage record")
	}
	// The gate marks the task blocked and records the triage entry before any
	// worker starts; the executor then settles the task's own final status.
	blockedRecords := 0
	if snap.Triage.Status == string(autonomous.TaskBlocked) && snap.Triage.TaskID == got.Story[0].Tasks[0].ID {
		blockedRecords = 1
	}
	if blockedRecords != 1 || spawns != 0 {
		t.Fatalf("blocked triage records = %d (want 1), worker spawns = %d (want 0), triage status %q", blockedRecords, spawns, snap.Triage.Status)
	}
	blockedTasks, blockedSpawns := blockedRecords, spawns

	// 2. A finished release runs the ship step in shadow mode.
	shipped := newPRD("ships", "")
	// Keep the separate push-on-complete branch push out of this run, so any
	// ref change below can only come from the ship step.
	if _, err := m.SetPRDGitPush(shipped.ID, "never"); err != nil {
		t.Fatal(err)
	}
	if err := m.Run(context.Background(), shipped.ID, spawn, verify); err != nil {
		t.Fatalf("run shipping PRD: %v", err)
	}
	// Shadow ship must run nothing: no ref on the remote (including
	// refs/heads/main and tags) or in the work repo may differ.
	remoteAfter, workAfter := refsOf()
	gitChanges := 0
	if remoteAfter != remoteBefore {
		gitChanges++
		t.Errorf("remote refs changed:\nbefore %q\nafter  %q", remoteBefore, remoteAfter)
	}
	if workAfter != workBefore {
		gitChanges++
		t.Errorf("work refs changed:\nbefore %q\nafter  %q", workBefore, workAfter)
	}
	if !strings.Contains(remoteAfter, "refs/heads/main ") || strings.Contains(remoteAfter, "refs/tags/") {
		t.Errorf("remote refs unexpected: %q", remoteAfter)
	}

	// 3. Serve the real handlers over httptest and point every surface at it.
	api := server.NewServer(server.NewHub(), nil, "h", "", nil, config.DefaultConfig(), filepath.Join(t.TempDir(), "config.yaml"))
	api.SetAutonomousAPI(autonomous.NewAPI(m))
	ts := httptest.NewServer(api.ChecksShipHandler())
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	port := u.Port()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := writeReloadTestConfig(home+"/.datawatch/config.yaml", "server:\n  port: "+port+"\n"); err != nil {
		t.Fatal(err)
	}
	var portNum int
	for _, c := range port {
		portNum = portNum*10 + int(c-'0')
	}

	mcpSrv := mcp.New("h", nil, &config.MCPConfig{}, t.TempDir(), mcp.Options{})
	mcpSrv.SetWebPort(portNum)
	rt := router.NewRouter("h", "", nil, nil, 0, nil)
	rt.SetWebPort(portNum)

	mcpText := func(tool, prd string) string {
		res, err := mcpSrv.CallTool(context.Background(), tool, map[string]any{"prd_id": prd})
		if err != nil || res == nil || res.IsError {
			t.Fatalf("mcp %s: %v %+v", tool, err, res)
		}
		var sb strings.Builder
		for _, c := range res.Content {
			if s, _ := c["text"].(string); s != "" {
				sb.WriteString(s)
			}
		}
		return sb.String()
	}
	get := func(path string) string {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close() //nolint:errcheck
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, rerr := resp.Body.Read(buf)
			sb.Write(buf[:n])
			if rerr != nil {
				break
			}
		}
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s = %d", path, resp.StatusCode)
		}
		return sb.String()
	}

	wantChecks := []string{"verifier_prestart", "does not exist"}
	wantShip := []string{
		"ship_step",
		"git push origin refs/heads/main:refs/heads/main",
		"git tag -a v1.2.3 -m Release 1.2.3",
		"git push origin refs/tags/v1.2.3:refs/tags/v1.2.3",
		"shadow: commands recorded, not run",
	}
	checksSurfaces := map[string]string{}
	shipSurfaces := map[string]string{}

	checksSurfaces["rest"] = get("/api/autonomous/checks?prd_id=" + blocked.ID)
	shipSurfaces["rest"] = get("/api/autonomous/ship?prd_id=" + shipped.ID)
	out, err := runLaneFillCmd(t, newAutonomousChecksCmd(), []string{"--prd", blocked.ID})
	if err != nil {
		t.Fatal(err)
	}
	checksSurfaces["cli"] = out
	if out, err = runLaneFillCmd(t, newAutonomousShipCmd(), []string{"--prd", shipped.ID}); err != nil {
		t.Fatal(err)
	}
	shipSurfaces["cli"] = out
	checksSurfaces["mcp"] = mcpText("checks_status", blocked.ID)
	shipSurfaces["mcp"] = mcpText("ship_status", shipped.ID)
	checksSurfaces["comm"] = strings.Join(rt.HandleTestMessage("autonomous checks "+blocked.ID), " ")
	shipSurfaces["comm"] = strings.Join(rt.HandleTestMessage("autonomous ship "+shipped.ID), " ")

	for name, text := range checksSurfaces {
		for _, w := range append([]string{"pre_start_verifier=on"}, wantChecks...) {
			if name == "rest" && strings.HasPrefix(w, "pre_start_verifier=") {
				w = `"pre_start_verifier":"on"`
			}
			if name == "mcp" && strings.HasPrefix(w, "pre_start_verifier=") {
				w = `"pre_start_verifier":"on"`
			}
			if !strings.Contains(text, w) {
				t.Errorf("checks via %s lacks %q:\n%s", name, w, text)
			}
		}
	}
	for name, text := range shipSurfaces {
		for _, w := range wantShip {
			if !strings.Contains(text, w) {
				t.Errorf("ship via %s lacks %q:\n%s", name, w, text)
			}
		}
	}
	recorded := 0
	for _, c := range wantShip[1:4] {
		if strings.Contains(shipSurfaces["rest"], c) {
			recorded++
		}
	}
	if recorded != 3 || gitChanges != 0 {
		t.Errorf("ship commands recorded = %d (want 3), git ref changes = %d (want 0)", recorded, gitChanges)
	}
	seenMu.Lock()
	transitions := strings.Join(blockedSeen, ">")
	seenMu.Unlock()
	t.Logf("COUNTS blocked_tasks=%d task_status_transitions=%s final_task_status=%s worker_spawns_for_blocked=%d triage_notes=%d checks_surfaces=%d ship_surfaces=%d ship_commands_recorded=%d git_changes=%d",
		blockedTasks, transitions, finalTaskStatus, blockedSpawns, len(snap.Triage.Notes), len(checksSurfaces), len(shipSurfaces), recorded, gitChanges)
}
