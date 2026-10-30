package autonomous

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The real host and worktree adapters (the production probes the
// supervisor watch checks sit on). The decision paths are covered over
// fakes elsewhere; these tests run the adapters themselves against a
// stand-in systemctl on PATH, a stub node model server, real git on a
// temporary repository and a temporary directory tree. Nothing leaves
// the test's own temporary directories.

// fakeSystemctlScript puts a systemctl stand-in first on PATH whose body
// is the given shell script.
func fakeSystemctlScript(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// emptyPath leaves nothing on PATH, so every tool lookup fails.
func emptyPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestShellProbeConstructorDefaults(t *testing.T) {
	p := NewShellSupervisorWatchHostProbe(nil, "")
	if p.localNode != "local" {
		t.Errorf("blank local node = %q, want local", p.localNode)
	}
	if p.httpc == nil || p.shell == "" {
		t.Errorf("constructor left the client or shell unset: %+v", p)
	}
	if got := NewShellSupervisorWatchHostProbe(map[string]string{"a": "x"}, "a"); got.localNode != "a" || got.nodeAddresses["a"] != "x" {
		t.Errorf("constructor dropped its arguments: %+v", got)
	}
}

func TestShellProbeRunCmd(t *testing.T) {
	p := NewShellSupervisorWatchHostProbe(nil, "")
	out, err := p.runCmd(context.Background(), "sh", "-c", "echo hello")
	if err != nil || strings.TrimSpace(out) != "hello" {
		t.Fatalf("runCmd success = %q, %v", out, err)
	}
	if _, err := p.runCmd(context.Background(), "sh", "-c", "exit 3"); err == nil || !strings.Contains(err.Error(), "supervisor watch host") {
		t.Fatalf("a failing command must be an error naming the command, got %v", err)
	}
	if _, err := p.runCmd(context.Background(), "no-such-tool-for-the-watch"); err == nil {
		t.Fatal("a missing command must be an error")
	}
}

func TestShellProbeResolveTool(t *testing.T) {
	p := NewShellSupervisorWatchHostProbe(nil, "")
	if _, err := p.resolveTool("sh"); err != nil {
		t.Errorf("sh must resolve: %v", err)
	}
	emptyPath(t)
	if _, err := p.resolveTool("systemctl"); err == nil || !strings.Contains(err.Error(), "command -v") {
		t.Errorf("a missing tool must be an error naming command -v, got %v", err)
	}
}

func TestShellProbeServiceRestartCount(t *testing.T) {
	p := NewShellSupervisorWatchHostProbe(nil, "")

	fakeSystemctlScript(t, "echo NRestarts=2")
	if n, err := p.ServiceRestartCount(); err != nil || n != 2 {
		t.Fatalf("NRestarts=2: got %d, %v", n, err)
	}
	fakeSystemctlScript(t, "echo NRestarts=abc")
	if _, err := p.ServiceRestartCount(); err == nil || !strings.Contains(err.Error(), "cannot parse") {
		t.Errorf("an unparseable count must be an error, got %v", err)
	}
	fakeSystemctlScript(t, "echo something else")
	if _, err := p.ServiceRestartCount(); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("no NRestarts line must be an error, got %v", err)
	}
	fakeSystemctlScript(t, "exit 1")
	if _, err := p.ServiceRestartCount(); err == nil {
		t.Error("a failing systemctl must be an error")
	}
	emptyPath(t)
	if _, err := p.ServiceRestartCount(); err == nil {
		t.Error("a missing systemctl must be an error")
	}
}

func TestShellProbeActiveSince(t *testing.T) {
	p := NewShellSupervisorWatchHostProbe(nil, "")

	fakeSystemctlScript(t, "echo 'ActiveEnterTimestamp=Fri 2026-10-30 09:00:00 UTC'")
	if got, err := p.ActiveSince(); err != nil || got != "Fri 2026-10-30 09:00:00 UTC" {
		t.Fatalf("ActiveSince = %q, %v", got, err)
	}
	fakeSystemctlScript(t, "echo ActiveEnterTimestamp=n/a")
	if _, err := p.ActiveSince(); err == nil || !strings.Contains(err.Error(), "no ActiveEnterTimestamp") {
		t.Errorf("n/a must be an error, got %v", err)
	}
	fakeSystemctlScript(t, "echo nothing")
	if _, err := p.ActiveSince(); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("no timestamp line must be an error, got %v", err)
	}
	fakeSystemctlScript(t, "exit 1")
	if _, err := p.ActiveSince(); err == nil {
		t.Error("a failing systemctl must be an error")
	}
	emptyPath(t)
	if _, err := p.ActiveSince(); err == nil {
		t.Error("a missing systemctl must be an error")
	}
}

func TestShellProbeDaemonActiveToolFailures(t *testing.T) {
	p := NewShellSupervisorWatchHostProbe(nil, "node-a")
	emptyPath(t)
	if _, err := p.DaemonActive("node-a"); err == nil {
		t.Error("a missing systemctl must read as unknown")
	}

	// A systemctl that cannot be started at all (a bad interpreter) is not
	// an exit status: it must be an error, never a "not active" answer.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte("#!/nonexistent/interpreter\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	active, err := p.DaemonActive("node-a")
	if err == nil || active {
		t.Errorf("an unstartable systemctl must be an error and not active, got %v, %v", active, err)
	}
}

func TestShellProbeResidentModels(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		switch r.Header.Get("X-Case") {
		case "":
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.8:27b"},{"name":"nomic-embed"}]}`))
	}))
	defer srv.Close()

	p := NewShellSupervisorWatchHostProbe(map[string]string{
		"with-scheme":    srv.URL + "/",
		"without-scheme": strings.TrimPrefix(srv.URL, "http://"),
		"blank":          "  ",
	}, "local")

	for _, node := range []string{"with-scheme", "without-scheme"} {
		got, err := p.ResidentModels(node)
		if err != nil || len(got) != 2 || got[0] != "qwen3.8:27b" || got[1] != "nomic-embed" {
			t.Fatalf("%s: ResidentModels = %v, %v", node, got, err)
		}
		if gotPath != "/api/ps" {
			t.Errorf("%s: asked %q, want /api/ps", node, gotPath)
		}
	}
	if _, err := p.ResidentModels("blank"); err == nil || !strings.Contains(err.Error(), "no model-server address") {
		t.Errorf("a blank address must be an error, got %v", err)
	}
	if _, err := p.ResidentModels("unknown"); err == nil {
		t.Error("a node without an address must be an error")
	}
}

func TestShellProbeResidentModelsFailures(t *testing.T) {
	status := http.StatusOK
	body := `{"models":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	p := NewShellSupervisorWatchHostProbe(map[string]string{"n": srv.URL}, "local")

	if got, err := p.ResidentModels("n"); err != nil || got == nil || len(got) != 0 {
		t.Errorf("an empty report is an empty list, not an error: %v, %v", got, err)
	}
	status = http.StatusInternalServerError
	if _, err := p.ResidentModels("n"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("a 500 must be an error naming the status, got %v", err)
	}
	status, body = http.StatusOK, `not json`
	if _, err := p.ResidentModels("n"); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("a garbage report must be an error, got %v", err)
	}
	srv.Close()
	if _, err := p.ResidentModels("n"); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("a closed server must be an error naming it unreachable, got %v", err)
	}

	// An address that is not a URL at all fails before any request is made.
	bad := NewShellSupervisorWatchHostProbe(map[string]string{"n": "http://bad host"}, "local")
	if _, err := bad.ResidentModels("n"); err == nil {
		t.Error("an invalid address must be an error")
	}
	// A probe built without its client falls back to the default one.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"m"}]}`))
	}))
	defer srv2.Close()
	noClient := &ShellSupervisorWatchHostProbe{nodeAddresses: map[string]string{"n": srv2.URL}}
	if got, err := noClient.ResidentModels("n"); err != nil || len(got) != 1 {
		t.Errorf("default client: %v, %v", got, err)
	}
}

func TestShellProbeGitStatus(t *testing.T) {
	p := NewShellSupervisorWatchHostProbe(nil, "")
	dir := snapTestRepo(t)

	if lines, err := p.GitStatus(dir); err != nil || len(lines) != 0 {
		t.Fatalf("clean checkout: %v, %v", lines, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := p.GitStatus(dir)
	if err != nil || len(lines) != 2 {
		t.Fatalf("dirty checkout: %v, %v", lines, err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, " M tracked.txt") || !strings.Contains(joined, "?? scratch.md") {
		t.Errorf("status lines = %q", joined)
	}

	if _, err := p.GitStatus(t.TempDir()); err == nil {
		t.Error("a directory that is not a repository must be an error")
	}
	emptyPath(t)
	if _, err := p.GitStatus(dir); err == nil {
		t.Error("a missing git must be an error")
	}
}

func TestShellProbeFileMode(t *testing.T) {
	p := NewShellSupervisorWatchHostProbe(nil, "")
	path := filepath.Join(t.TempDir(), "verify.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mode, err := p.FileMode(path)
	if err != nil || mode&0o111 == 0 {
		t.Fatalf("FileMode = %v, %v; want an executable mode", mode, err)
	}
	_, err = p.FileMode(filepath.Join(t.TempDir(), "absent"))
	if err == nil || !os.IsNotExist(err) {
		t.Errorf("a missing file must keep os.IsNotExist, got %v", err)
	}
}

func TestShellWorktreeFSGoModAt(t *testing.T) {
	f := NewShellSupervisorWatchWorktreeFS()
	dir := t.TempDir()
	if ok, err := f.GoModAt(dir); err != nil || ok {
		t.Errorf("no go.mod: %v, %v", ok, err)
	}
	if ok, err := f.GoModAt(filepath.Join(dir, "absent")); err != nil || ok {
		t.Errorf("a missing directory is (false, nil): %v, %v", ok, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.GoModAt(dir); err != nil || !ok {
		t.Errorf("go.mod present: %v, %v", ok, err)
	}
	other := t.TempDir()
	if err := os.Mkdir(filepath.Join(other, "go.mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.GoModAt(other); err != nil || ok {
		t.Errorf("a directory named go.mod is not a go.mod: %v, %v", ok, err)
	}
	if os.Geteuid() != 0 {
		locked := t.TempDir()
		if err := os.WriteFile(filepath.Join(locked, "go.mod"), []byte("module x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		if _, err := f.GoModAt(locked); err == nil {
			t.Error("an unreadable directory must be an error, not (false, nil)")
		}
	}
}

func TestShellWorktreeFSRegisteredWorktrees(t *testing.T) {
	f := NewShellSupervisorWatchWorktreeFS()
	repo := snapTestRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	snapTestGit(t, repo, "worktree", "add", "-q", "-b", "side", linked)

	got, err := f.RegisteredWorktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	// git may print resolved paths; compare by base name.
	var bases []string
	for _, p := range got {
		bases = append(bases, filepath.Base(p))
	}
	if len(got) != 2 || !swAdaptersHas(bases, filepath.Base(repo)) || !swAdaptersHas(bases, "linked") {
		t.Errorf("registered worktrees = %v, want the repo and the linked worktree", got)
	}

	if _, err := f.RegisteredWorktrees(t.TempDir()); err == nil || !strings.Contains(err.Error(), "git worktree list") {
		t.Errorf("a non-repository must be an error naming the command, got %v", err)
	}
	emptyPath(t)
	if _, err := f.RegisteredWorktrees(repo); err == nil || !strings.Contains(err.Error(), "git not found") {
		t.Errorf("a missing git must be an error, got %v", err)
	}
}

func swAdaptersHas(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestShellWorktreeFSNewestNonGitChange(t *testing.T) {
	f := NewShellSupervisorWatchWorktreeFS()
	dir := t.TempDir()
	old := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	mid := old.Add(2 * time.Hour)
	newest := old.Add(5 * time.Hour)

	write := func(rel string, mt time.Time) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	// an empty directory holds no file at all
	if _, ok, err := f.NewestNonGitChange(dir); err != nil || ok {
		t.Fatalf("empty dir: ok=%v err=%v", ok, err)
	}
	write("a.go", old)
	write("sub/b.go", mid)
	// .git contents (and a gitlink file) are newer but must be ignored
	write(".git/HEAD", newest)
	write("sub/.git", newest)
	if err := os.Symlink("a.go", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	got, ok, err := f.NewestNonGitChange(dir)
	if err != nil || !ok || !got.Equal(mid) {
		t.Fatalf("newest = %v ok=%v err=%v, want %v (the .git entries must be skipped)", got, ok, err, mid)
	}

	if _, ok, err := f.NewestNonGitChange(filepath.Join(dir, "absent")); err != nil || ok {
		t.Errorf("a missing dir is (zero, false, nil): ok=%v err=%v", ok, err)
	}
	if _, _, err := f.NewestNonGitChange(filepath.Join(dir, "a.go")); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("a file given as the dir must be an error, got %v", err)
	}
	if os.Geteuid() != 0 {
		locked := t.TempDir()
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		if _, _, err := f.NewestNonGitChange(locked); err == nil {
			t.Error("an unreadable root must be an error")
		}
	}
}

func TestShellWorktreeFSMtime(t *testing.T) {
	f := NewShellSupervisorWatchWorktreeFS()
	p := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(p, want, want); err != nil {
		t.Fatal(err)
	}
	got, err := f.Mtime(p)
	if err != nil || !got.Equal(want) {
		t.Errorf("Mtime = %v, %v; want %v", got, err, want)
	}
	if _, err := f.Mtime(filepath.Join(filepath.Dir(p), "absent")); err == nil || !os.IsNotExist(err) {
		t.Errorf("a missing file must keep os.IsNotExist, got %v", err)
	}
}

func TestRealSupervisorWatchClockIsNow(t *testing.T) {
	before := time.Now()
	got := realSupervisorWatchClock{}.Now()
	if got.Before(before) || got.After(time.Now().Add(time.Second)) {
		t.Errorf("clock = %v, not near now", got)
	}
}
