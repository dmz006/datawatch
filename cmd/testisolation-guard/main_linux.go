//go:build linux

// Command testisolation-guard runs one Go test binary in a sandbox and fails it
// when a test reads or writes outside its own temporary area and the
// repository. It is meant for `go test -exec`; use
// scripts/check-test-isolation.sh rather than calling it directly.
//
// It needs bubblewrap (bwrap) and fails when it cannot build the sandbox: it
// never runs a test binary unguarded. See internal/testisolation for what is
// checked.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/dmz006/datawatch/internal/testisolation"
)

const (
	innerFlag = "-testisolation-inner"
	runDir    = "/run/testisolation"
	tempRoot  = "/tmp"
	sharedDir = "/tmp/shared"
	maxReport = 25
)

// plan is what the outer process tells the inner one.
type plan struct {
	Home    string
	Repo    string
	Allowed []string
	Name    string // package folder relative to the repository, for the report
}

func main() {
	if len(os.Args) > 3 && os.Args[1] == innerFlag {
		os.Exit(inner(os.Args[2], os.Args[3], os.Args[4:]))
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: testisolation-guard <test binary> [test flags]  (use scripts/check-test-isolation.sh)")
		os.Exit(2)
	}
	os.Exit(outer(os.Args[1], os.Args[2:]))
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "test-isolation: cannot run the guard:", err)
	return 1
}

func outer(bin string, args []string) int {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return fail(errors.New("bwrap (bubblewrap) is not installed; tests are never run unguarded"))
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	repo, err := moduleRoot(cwd)
	if err != nil {
		return fail(err)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return fail(fmt.Errorf("no usable home folder (%q): %v", home, err))
	}
	if filepath.Clean(repo) == filepath.Clean(home) {
		return fail(errors.New("the repository must not be the home folder itself"))
	}
	self, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return fail(err)
	}
	goEnv, err := goFolders()
	if err != nil {
		return fail(err)
	}

	p := plan{Home: home, Repo: repo}
	p.Name, _ = filepath.Rel(repo, cwd)
	inBin := filepath.Join(runDir, filepath.Base(bin))
	inSelf := filepath.Join(runDir, "guard")

	a := []string{
		"--die-with-parent", "--unshare-pid", "--unshare-net", "--unshare-ipc",
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc",
		"--tmpfs", tempRoot, "--tmpfs", "/var/tmp", "--tmpfs", "/run", "--tmpfs", home,
		"--dir", runDir, "--ro-bind", bin, inBin, "--ro-bind", self, inSelf,
	}
	bind := func(flag, dir string) {
		if dir == "" {
			return
		}
		if _, err := os.Stat(dir); err != nil {
			return
		}
		a = append(a, flag, dir, dir)
		p.Allowed = append(p.Allowed, dir)
	}
	// Command folders under the home (a Go or Node install there) stay
	// readable, or the tools the tests run would be missing.
	for _, d := range commandFolders(os.Getenv("PATH"), home) {
		bind("--ro-bind", d)
	}
	bind("--ro-bind", goEnv.GOROOT)
	bind("--ro-bind", goEnv.GOMODCACHE)
	bind("--bind", goEnv.GOCACHE)
	bind("--ro-bind", gitCommonDir(repo))
	bind("--bind", repo) // last: the repository wins over any folder above it

	raw, err := json.Marshal(p)
	if err != nil {
		return fail(err)
	}
	// A datawatch session's own variables (session id, daemon address, token)
	// must not reach the tests: they change results and point at a live daemon.
	var drop []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "DATAWATCH_") {
			drop = append(drop, k)
		}
	}
	drop = append(drop, "TMUX", "TMUX_TMPDIR", "GOTMPDIR", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR")
	env := replaceEnv(os.Environ(), map[string]string{
		"TMPDIR": sharedDir, "HOME": home,
		"GOCACHE": goEnv.GOCACHE, "GOMODCACHE": goEnv.GOMODCACHE,
		"GOPROXY": "off", "GOTOOLCHAIN": "local",
	}, drop...)

	a = append(a, "--chdir", cwd, "--", inSelf, innerFlag, string(raw), inBin)
	a = append(a, args...)
	cmd := exec.Command(bwrap, a...) // #nosec G204 -- fixed tool, arguments built above
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return exitCode(cmd.Run())
}

func inner(rawPlan, bin string, args []string) int {
	var p plan
	if err := json.Unmarshal([]byte(rawPlan), &p); err != nil {
		return fail(err)
	}
	l := testisolation.Layout{TempRoot: tempRoot, Shared: sharedDir, Home: p.Home}
	// The Go tool writes its telemetry mode under the home; keeping it off is
	// toolchain bookkeeping, not a test reaching outside.
	telemetry := filepath.Join(p.Home, ".config", "go", "telemetry")
	if err := os.MkdirAll(telemetry, 0o700); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(telemetry, "mode"), []byte("off 2000-01-01\n"), 0o600); err != nil {
		return fail(err)
	}
	l.Allowed = append(p.Allowed, telemetry)
	if err := l.Plant(); err != nil {
		return fail(err)
	}
	snap, err := l.Snapshot()
	if err != nil {
		return fail(err)
	}

	logPath := filepath.Join(runDir, "testlog.txt")
	run := []string{"-test.testlogfile=" + logPath}
	for _, a := range args {
		if !strings.HasPrefix(a, "-test.testlogfile=") {
			run = append(run, a)
		}
	}
	cmd := exec.Command(bin, run...) // #nosec G204 -- the test binary go test handed us
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	code := exitCode(cmd.Run())
	events := snap.Check()

	cwd, _ := os.Getwd()
	var fromLog []testisolation.Violation
	f, err := os.Open(logPath)
	switch {
	case err == nil:
		fromLog, err = l.AnalyzeLog(f, cwd)
		_ = f.Close()
		if err != nil {
			return fail(fmt.Errorf("read the test log: %w", err))
		}
	case code == 0:
		// A binary that passed without writing a log was not checked.
		return fail(fmt.Errorf("%s wrote no test log: %w", p.Name, err))
	}
	if keep := os.Getenv("TESTISOLATION_KEEP_LOG"); keep != "" {
		// Debugging aid: the folder must be inside the repository.
		keepLog(logPath, filepath.Join(keep, strings.ReplaceAll(p.Name, "/", "__")+".testlog"))
	}
	all := append(events, fromLog...)
	if len(all) == 0 {
		return code
	}
	limit := maxReport
	if n, err := strconv.Atoi(os.Getenv("TESTISOLATION_MAX_LINES")); err == nil && n > 0 {
		limit = n
	}
	_, _ = fmt.Fprint(os.Stdout, testisolation.Report(p.Name, all, limit))
	if code == 0 {
		code = 1
	}
	return code
}

func keepLog(from, to string) {
	in, err := os.Open(from)
	if err != nil {
		return
	}
	defer in.Close() //nolint:errcheck
	out, err := os.Create(to)
	if err != nil {
		fmt.Fprintln(os.Stderr, "test-isolation: keep log:", err)
		return
	}
	defer out.Close() //nolint:errcheck
	if _, err := io.Copy(out, in); err != nil {
		fmt.Fprintln(os.Stderr, "test-isolation: keep log:", err)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	fmt.Fprintln(os.Stderr, "test-isolation:", err)
	return 1
}

func moduleRoot(dir string) (string, error) {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if d == filepath.Dir(d) {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
	}
}

type goFolderSet struct{ GOROOT, GOCACHE, GOMODCACHE string }

func goFolders() (goFolderSet, error) {
	var g goFolderSet
	out, err := exec.Command("go", "env", "-json", "GOROOT", "GOCACHE", "GOMODCACHE").Output()
	if err != nil {
		return g, fmt.Errorf("go env: %w", err)
	}
	return g, json.Unmarshal(out, &g)
}

// commandFolders returns what must stay readable for each PATH entry under the
// home: the entry itself, or the install it belongs to when it is the bin
// folder of a versioned install.
func commandFolders(path, home string) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range filepath.SplitList(path) {
		d = filepath.Clean(d)
		if !strings.HasPrefix(d, home+"/") {
			continue
		}
		parent := filepath.Dir(d)
		if filepath.Base(d) == "bin" && strings.Count(strings.TrimPrefix(parent, home+"/"), "/") >= 2 {
			d = parent
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// gitCommonDir returns the shared .git folder of a linked work tree, or "".
func gitCommonDir(repo string) string {
	raw, err := os.ReadFile(filepath.Join(repo, ".git"))
	if err != nil {
		return "" // a normal clone: .git is inside the repository
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(string(raw), "gitdir:"))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repo, gitDir)
	}
	common, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	c := strings.TrimSpace(string(common))
	if !filepath.IsAbs(c) {
		c = filepath.Join(gitDir, c)
	}
	return filepath.Clean(c)
}

func replaceEnv(env []string, set map[string]string, drop ...string) []string {
	skip := map[string]bool{}
	for _, k := range drop {
		skip[k] = true
	}
	for k := range set {
		skip[k] = true
	}
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !skip[k] {
			out = append(out, kv)
		}
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}
