package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startupDaemon is a real daemon in its own HOME, data, temp and tmux folders,
// with a stand-in `claude` and channel bridge first on PATH. The stand-in
// records every call; `claude mcp list` answers only once release() was called.
type startupDaemon struct {
	t         *testing.T
	calls     string
	released  string
	log       string
	healthURL string
}

const startupHostname = "dw-startup-test"

func startStartupDaemon(t *testing.T, mcpList string) *startupDaemon {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "datawatch")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build daemon: %v\n%s", err, out)
	}
	dirs := map[string]string{}
	for _, name := range []string{"home", "data", "tmp", "mux", "bin"} {
		dirs[name] = filepath.Join(root, name)
		if err := os.MkdirAll(dirs[name], 0o700); err != nil {
			t.Fatal(err)
		}
	}
	d := &startupDaemon{
		t:        t,
		calls:    filepath.Join(root, "claude-calls"),
		released: filepath.Join(root, "release-mcp-list"),
		log:      filepath.Join(root, "daemon.log"),
	}
	listFile := filepath.Join(root, "mcp-list")
	if err := os.WriteFile(listFile, []byte(mcpList), 0o600); err != nil {
		t.Fatal(err)
	}
	// The wait is bounded so a stand-in left behind ends on its own.
	claude := fmt.Sprintf(`#!/bin/sh
echo "$*" >> '%s'
if [ "$1 $2" = "mcp list" ]; then
  i=0
  while [ ! -e '%s' ] && [ "$i" -lt 1800 ]; do sleep 0.1; i=$((i+1)); done
  cat '%s'
fi
exit 0
`, d.calls, d.released, listFile)
	for name, body := range map[string]string{"claude": claude, "datawatch-channel": "#!/bin/sh\nexit 0\n"} {
		if err := os.WriteFile(filepath.Join(dirs["bin"], name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	freePort := func() int {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = l.Close() }()
		return l.Addr().(*net.TCPAddr).Port
	}
	httpPort, tlsPort := freePort(), freePort()
	d.healthURL = fmt.Sprintf("https://127.0.0.1:%d/api/health", tlsPort)
	cfg := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(cfg, []byte(fmt.Sprintf(`data_dir: %s
hostname: %s
server:
  enabled: true
  host: 127.0.0.1
  port: %d
  tls_enabled: true
  tls_port: %d
  tls_auto_generate: true
  token: ""
update:
  enabled: false
`, dirs["data"], startupHostname, httpPort, tlsPort)), 0o600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(d.log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	daemon := exec.Command(bin, "--config", cfg, "start", "--foreground")
	daemon.Dir = root
	daemon.Env = []string{
		"HOME=" + dirs["home"], "TMPDIR=" + dirs["tmp"], "TMUX_TMPDIR=" + dirs["mux"],
		"PATH=" + dirs["bin"] + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	daemon.Stdout, daemon.Stderr = logFile, logFile
	// Its own process group, so that nothing it started outlives the test.
	daemon.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := daemon.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = daemon.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		d.release()
		_ = daemon.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = daemon.Process.Kill()
			<-exited
		}
		_ = syscall.Kill(-daemon.Process.Pid, syscall.SIGKILL)
		for i := 0; i < 500 && syscall.Kill(-daemon.Process.Pid, 0) == nil; i++ {
			time.Sleep(10 * time.Millisecond)
		}
	})
	return d
}

func (d *startupDaemon) release() { _ = os.WriteFile(d.released, nil, 0o600) }

func (d *startupDaemon) callLog() string {
	b, _ := os.ReadFile(d.calls)
	return string(b)
}

// waitFor is a hang detector: the condition, not the bound, decides the test.
func (d *startupDaemon) waitFor(what string, cond func() bool) {
	d.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(d.log)
			if len(log) > 3000 {
				log = log[len(log)-3000:]
			}
			d.t.Fatalf("%s did not happen\nclaude calls:\n%sdaemon log tail:\n%s", what, d.callLog(), log)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (d *startupDaemon) healthy() bool {
	c := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- the test daemon's own self-signed certificate
	}}
	resp, err := c.Get(d.healthURL)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// `claude mcp list` contacts every MCP server the user has configured, so one
// unreachable server holds it for that client's whole connect timeout. The
// daemon's cleanup of old registrations must not hold the daemon's own start.
func TestDaemonStart_DoesNotWaitForClaudeMCPList(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real daemon")
	}
	d := startStartupDaemon(t, "")
	d.waitFor("a health answer while `claude mcp list` is still unanswered", d.healthy)
	d.waitFor("the cleanup's `claude mcp list` call", func() bool {
		return strings.Contains(d.callLog(), "mcp list")
	})
}

// claude's MCP list is shared by every daemon that runs as this user. A daemon
// knows only its own sessions, so it may remove only registrations named for
// them: another daemon's registration belongs to a session it cannot see.
func TestDaemonStart_LeavesOtherDaemonsMCPRegistrations(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real daemon")
	}
	other := "datawatch-otherhost-ab12"
	// Same leading text as this daemon's hostname, still another host's.
	longer := "datawatch-" + startupHostname + "-second-cd34"
	ownGone := "datawatch-" + startupHostname + "-ef56"
	d := startStartupDaemon(t, other+": x - ok\n"+longer+": x - ok\n"+ownGone+": x - ok\n")
	d.release()
	d.waitFor("a health answer", d.healthy)
	// The own registration is listed last, so once it is removed the other two
	// have been judged.
	d.waitFor("the removal of this daemon's own stale registration", func() bool {
		return strings.Contains(d.callLog(), "mcp remove "+ownGone+" -s user")
	})
	for _, name := range []string{other, longer} {
		if strings.Contains(d.callLog(), "mcp remove "+name+" ") {
			t.Errorf("the daemon removed %s, which is not its own\nclaude calls:\n%s", name, d.callLog())
		}
	}
}
