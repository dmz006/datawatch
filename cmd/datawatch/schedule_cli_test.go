package main

// The schedule commands go through the running daemon. They touch the file
// only when the daemon is proven to be down.

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

type fakeSchedDaemon struct {
	mu     sync.Mutex
	posts  []map[string]any
	status int
}

func (d *fakeSchedDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status != 0 {
		http.Error(w, "schedule refused: session \"zz\" has ended", d.status)
		return
	}
	switch r.Method {
	case http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		d.posts = append(d.posts, body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"d0d0d0d0","state":"pending"}`))
	case http.MethodGet:
		_, _ = w.Write([]byte(`[{"id":"daemon01","state":"pending","command":"from the daemon","session_id":"h-aa01"}]`))
	default:
		_, _ = w.Write([]byte(`{"status":"cancelled"}`))
	}
}

// cliCfg is a config whose daemon is at addr and whose data folder is empty.
func cliCfg(t *testing.T, addr string) *config.Config {
	t.Helper()
	oldPath, oldURL, oldSecure := cfgPath, serverURL, secureMode
	t.Cleanup(func() { cfgPath, serverURL, secureMode = oldPath, oldURL, oldSecure })
	dir := t.TempDir()
	cfgPath, serverURL, secureMode = filepath.Join(dir, "config.yaml"), "", false
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DataDir: dir}
	cfg.Server.Host = host
	cfg.Server.Port, _ = strconv.Atoi(port)
	return cfg
}

// deadAddr is a local address nothing listens on.
func deadAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func noScheduleFile(t *testing.T, cfg *config.Config) {
	t.Helper()
	if raw, err := os.ReadFile(schedStorePath(cfg)); err == nil {
		t.Fatalf("schedule.json was written: %s", raw)
	}
}

func TestScheduleCLIGoesThroughTheDaemon(t *testing.T) {
	d := &fakeSchedDaemon{}
	srv := httptest.NewServer(d)
	defer srv.Close()
	cfg := cliCfg(t, srv.Listener.Addr().String())
	out := captureStdout(t, func() {
		if err := runScheduleAdd(cfg, "", "supervisor", "wake up", "", "*/5 * * * *", "watch"); err != nil {
			t.Fatal(err)
		}
	})
	noScheduleFile(t, cfg)
	if len(d.posts) != 1 || d.posts[0]["session_name"] != "supervisor" || d.posts[0]["command"] != "wake up" ||
		d.posts[0]["cron_expr"] != "*/5 * * * *" || d.posts[0]["schedule_name"] != "watch" || d.posts[0]["run_at"] == "" {
		t.Fatalf("daemon received %+v", d.posts)
	}
	if !strings.Contains(out, "d0d0d0d0") {
		t.Errorf("output does not name the daemon's schedule: %s", out)
	}
	listed := captureStdout(t, func() {
		if err := runScheduleList(cfg); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(listed, "daemon01") {
		t.Errorf("list does not show the daemon's schedules: %s", listed)
	}
}

func TestScheduleCLIKeepsTheDaemonsRefusal(t *testing.T) {
	srv := httptest.NewServer(&fakeSchedDaemon{status: http.StatusInternalServerError})
	defer srv.Close()
	cfg := cliCfg(t, srv.Listener.Addr().String())
	err := runScheduleAdd(cfg, "zz", "", "x", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "has ended") {
		t.Fatalf("err = %v, want the daemon's reason", err)
	}
	noScheduleFile(t, cfg)
}

func TestScheduleCLIWritesNothingWhileTheLockIsHeld(t *testing.T) {
	cfg := cliCfg(t, deadAddr(t))
	release, ok, err := session.TryDaemonLock(cfg.DataDir)
	if err != nil || !ok {
		t.Skipf("no lock here: %v", err)
	}
	defer release()
	for label, run := range map[string]func() error{
		"add":    func() error { return runScheduleAdd(cfg, "aa01", "", "x", "", "", "") },
		"list":   func() error { return runScheduleList(cfg) },
		"cancel": func() error { return runScheduleCancel(cfg, "aa01") },
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), "running") {
			t.Errorf("%s: err = %v, want one saying the daemon is running", label, err)
		}
	}
	noScheduleFile(t, cfg)
}

func TestScheduleCLIWithTheDaemonDownUsesTheFileAndSaysSo(t *testing.T) {
	cfg := cliCfg(t, deadAddr(t))
	out := captureStdout(t, func() {
		if err := runScheduleAdd(cfg, "aa01", "", "later", "", "", "n1"); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "not running") {
		t.Errorf("output does not say the daemon is down: %s", out)
	}
	store, err := session.NewScheduleStore(schedStorePath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if got := store.List(session.SchedPending); len(got) != 1 || got[0].Command != "later" {
		t.Fatalf("file holds %+v", got)
	}
	listed := captureStdout(t, func() {
		if err := runScheduleList(cfg); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(listed, "n1") || !strings.Contains(listed, "not running") {
		t.Errorf("list: %s", listed)
	}
	captureStdout(t, func() {
		if err := runScheduleCancel(cfg, "n1"); err != nil {
			t.Fatal(err)
		}
	})
	again, _ := session.NewScheduleStore(schedStorePath(cfg))
	if got := again.List(session.SchedPending); len(got) != 0 {
		t.Fatalf("not cancelled: %+v", got)
	}
}

func TestScheduleCLIOnASecureInstallNeedsTheDaemon(t *testing.T) {
	cases := map[string]func(cfg *config.Config){
		"--secure": func(*config.Config) { secureMode = true },
		"encrypted config": func(*config.Config) {
			_ = os.WriteFile(cfgPath, []byte("DWATCH3\nxxxx"), 0o600)
		},
		"data key salt": func(cfg *config.Config) {
			_ = os.WriteFile(filepath.Join(cfg.DataDir, "enc.salt"), []byte("salt"), 0o600)
		},
	}
	for label, secure := range cases {
		t.Run(label, func(t *testing.T) {
			cfg := cliCfg(t, deadAddr(t))
			secure(cfg)
			err := runScheduleAdd(cfg, "aa01", "", "secret text", "", "", "")
			if err == nil || !strings.Contains(err.Error(), "secure") {
				t.Fatalf("err = %v, want one naming the secure install", err)
			}
			noScheduleFile(t, cfg)
		})
	}
}

// A --url target is another daemon: its file is not on this host.
func TestScheduleCLIWithARemoteTargetNeverWritesLocally(t *testing.T) {
	cfg := cliCfg(t, deadAddr(t))
	serverURL = "http://" + deadAddr(t)
	if err := runScheduleAdd(cfg, "aa01", "", "x", "", "", ""); err == nil {
		t.Fatal("reported success")
	}
	noScheduleFile(t, cfg)
}
