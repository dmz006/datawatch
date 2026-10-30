package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/testhome"
)

// watchedLeakPrefixes are the datawatch test-harness temp prefixes that have
// historically leaked into the session temp dir and accumulated across runs:
// the TestMain homes, the supervisor helper dir, the schedule scratch roots,
// the pi-agent task/status/out channels, the claudecode/opencode/aider/goose
// task scratch dirs, tmux scratch, and this guard's own work dir. They are
// listed so failures can say which harness an orphan belongs to, and so a
// future regression in the guard itself (dw-tmp-leak-guard-) is caught too.
var watchedLeakPrefixes = []string{
	"dw-test-home-",
	"svchelper-",
	"sched-test-",
	"datawatch-pi-task-",
	"datawatch-pi-status-",
	"datawatch-pi-out-",
	"dw-tmp-leak-guard-",
	"dw-test-manifest",
	"dw-test-tmux-",
	"dw-leak-",
	"datawatch-claudecode-task-",
	"datawatch-opencode-task-",
	"datawatch-aider-task-",
	"datawatch-goose-task-",
}

// TestNoNewTmpLeakEntries is the story-s3 regression guard against temp-dir
// leaks from the test suites that share the session temp dir.
//
// Approach: the inner `go test` run AND this test process operate with
// TMPDIR pointed at a fresh, private directory (os.TempDir() honours $TMPDIR),
// so the assertion is absolute: after a clean inner run, that exclusive temp
// dir must contain zero entries. Any survivor — a TestMain home (dw-test-home-*)
// whose TestMain self-cleanup was bypassed by an os.Exit, a supervisor helper
// dir (svchelper-*), a schedule scratch root (sched-test-*), or a pi scratch
// channel (datawatch-pi-task-*/-status-*/-out-*) — is a leak, whether it
// appeared during the run or was left behind by a killed run.
//
// An exclusive TMPDIR also keeps the guard honest on busy hosts: unrelated
// concurrent activity (other sessions, the daemon) writing to the shared
// system temp dir can no longer produce false positives here, because it
// cannot see this private directory.
//
// The packages run one at a time and the private dir is checked and cleaned
// after each, so every survivor is attributed to the package that left it;
// the failure message names the leaked entry, the watched harness prefix it
// matches, and the responsible package.
//
// The inner runs operate with TMUX and TMUX_TMPDIR removed from the
// environment (the equivalent of `env -u TMUX -u TMUX_TMPDIR go test ...`)
// and never issue tmux server commands.
//
// The inner run's throwaway homes are removed even when the go tool created
// read-only module cache files in them (testhome.Remove plus -modcacherw), so
// the exclusive dir stays empty after a clean run.
//
// Package selection: by default the guard covers the packages that use the
// shared temp dir (mcp, server, plugins) and deliberately does not include
// ./internal/session/ so this test never runs a copy of itself. With
// DW_LEAK_GUARD_FULL=1 (set by `make test-tmp-guard`) the list is
// `go list ./...` minus ./internal/session/, i.e. every other package in the
// repo.
func TestNoNewTmpLeakEntries(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("skipping temp-leak guard: go toolchain not found on PATH: %v", err)
	}

	// Fresh private temp workspace for the inner runs. Created inside the
	// shared system temp dir (which on this host is /tmp), then made the
	// active temp dir for this process and, through inheritance, for the
	// inner `go test` runs. t.Cleanup removes it so the guard itself never
	// contributes to the accumulation it is watching for.
	fresh, err := os.MkdirTemp("", "dw-tmp-leak-guard-")
	if err != nil {
		t.Fatalf("create exclusive temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fresh) }) //nolint:errcheck
	t.Setenv("TMPDIR", fresh)
	if got, want := os.TempDir(), fresh; got != filepath.Clean(want) { //nolint:staticcheck
		t.Fatalf("TMPDIR not in effect: os.TempDir()=%q want %q", got, want)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	pkgs, err := guardPackageList(t, root)
	if err != nil {
		t.Fatalf("build inner package list: %v", err)
	}
	t.Logf("temp-leak guard: %d package(s): %s", len(pkgs), strings.Join(pkgs, " "))

	env := guardChildEnv()

	var offenders []string
	for _, pkg := range pkgs {
		out, err := innerGoTest(t, root, env, pkg)
		if err != nil {
			t.Fatalf("inner go test %s failed (fix the suite first; the leak guard cannot run on top of it):\n%s", pkg, out)
		}
		// Check the directory while pkg is the one under the gun, name the
		// offenders, then clean it so the next package starts from an empty
		// private dir.
		if left := collectAndClean(fresh); len(left) > 0 {
			t.Logf("temp-leak guard: package %s left %s in %s", pkg, strings.Join(left, ", "), fresh)
			offenders = append(offenders, "package "+pkg+" left "+strings.Join(left, ", "))
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("temp-dir leak: %d package(s) left entries in the test temp dir %s; "+
			"known watched harness prefixes are %s: [%s]",
			len(offenders),
			fresh,
			strings.Join(watchedLeakPrefixes, ", "),
			strings.Join(offenders, " | "))
	}
}

// guardPackageList returns the inner `go test` targets. In normal mode the
// small default list; with DW_LEAK_GUARD_FULL=1 the full repo list
// (`go list ./...` minus ./internal/session/ — the guard must never run a
// copy of itself).
func guardPackageList(t *testing.T, root string) ([]string, error) {
	t.Helper()
	if os.Getenv("DW_LEAK_GUARD_FULL") != "1" {
		return []string{"./internal/mcp/", "./internal/server/", "./internal/plugins/"}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list ./... in %s: %w", root, err)
	}
	var pkgs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "github.com/dmz006/datawatch") {
			continue // external or tool packages are not part of this repo's suites
		}
		if strings.HasSuffix(line, "/internal/session") {
			continue // never run a copy of the guard
		}
		pkgs = append(pkgs, line)
	}
	if len(pkgs) == 0 {
		return nil, errors.New("go list ./... produced no internal packages")
	}
	return pkgs, nil
}

// guardChildEnv is the environment for the inner `go test` processes:
// this process's environment minus TMUX/TMUX_TMPDIR (the equivalent of
// `env -u TMUX -u TMUX_TMPDIR`), with the real HOME restored. This process
// runs with the package's throwaway HOME (see TestMain); the child go
// toolchain must use the real one, otherwise it downloads the whole module
// cache and builds from scratch inside the throwaway HOME, which is created
// before TMPDIR is redirected and cannot be removed (the module cache is
// read-only). TMPDIR carries over, pointing at the private dir.
func guardChildEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			if k := kv[:eq]; k == "TMUX" || k == "TMUX_TMPDIR" {
				continue
			}
		}
		env = append(env, kv)
	}
	if realHome != "" {
		env = append(env, "HOME="+realHome)
	}
	return env
}

// innerGoTest runs `go test -count=1 <pkg>` with a 30-minute budget and
// returns combined output; it fails the test on timeout or a non-zero exit.
func innerGoTest(t *testing.T, root string, env []string, pkg string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", pkg)
	cmd.Dir = root
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	t.Logf("inner go test %s output:\n%s", pkg, out)
	if ctx.Err() != nil {
		t.Fatalf("inner go test %s exceeded the 30m budget:\n%s", pkg, out)
	}
	return out, err
}

// collectAndClean lists whatever the just-finished inner run left in the
// private temp dir, human-readable (naming the matching watched harness
// prefix when one matches), and removes the entries so the next package
// starts from an empty private dir.
func collectAndClean(dir string) []string {
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil
	}
	left := make([]string, 0, len(entries))
	for _, e := range entries {
		name := filepath.Base(e)
		desc := name + " (no watched harness prefix match)"
		for _, p := range watchedLeakPrefixes {
			if strings.HasPrefix(name, p) {
				desc = name + " (watched harness prefix " + p + ")"
				break
			}
		}
		left = append(left, desc)
		if err := os.RemoveAll(e); err != nil { //nolint:errcheck
			// Leave it: attribution is already recorded above.
		}
	}
	return left
}

// buildReadOnlyModTree creates a throwaway home under the current temp dir
// shaped like what the go tool leaves behind: go/pkg/mod/example.com/mod@v1.0.0/sub/file.go
// with the file mode 0444 and every directory below the root mode 0555. All
// content is created first and the directories are chmod'ed last, deepest first.
func buildReadOnlyModTree(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "dw-test-home-")
	if err != nil {
		t.Fatalf("create test home: %v", err)
	}
	// Registered first so it runs last: a failed test must not leave a
	// read-only tree behind.
	t.Cleanup(func() { _ = testhome.Remove(root) }) //nolint:errcheck

	rel := filepath.Join("go", "pkg", "mod", "example.com", "mod@v1.0.0", "sub")
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		t.Fatalf("create module tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, rel, "file.go"), []byte("package sub\n"), 0o444); err != nil {
		t.Fatalf("write module file: %v", err)
	}
	if err := os.Chmod(filepath.Join(root, rel, "file.go"), 0o444); err != nil {
		t.Fatalf("chmod module file: %v", err)
	}
	// Deepest first: a 0555 parent would not stop chmod of its child, but this
	// mirrors the order in which the go tool seals the tree.
	dirs := []string{rel}
	for d := filepath.Dir(rel); d != "."; d = filepath.Dir(d) {
		dirs = append(dirs, d)
	}
	for _, d := range dirs {
		if err := os.Chmod(filepath.Join(root, d), 0o555); err != nil {
			t.Fatalf("chmod %s: %v", d, err)
		}
	}
	return root
}

// TestHomeCleanupRemovesReadOnlyTree proves testhome.Remove deletes a
// TestMain-style home holding a read-only module cache tree, which a plain
// os.RemoveAll cannot.
func TestHomeCleanupRemovesReadOnlyTree(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not block root, so the read-only tree proves nothing")
	}

	// Private temp dir so the dw-test-home-* check sees only this test's homes
	// and not the package's own TestMain home or other concurrent runs.
	private := t.TempDir()
	// Runs before TempDir's own removal, which could not delete a read-only tree.
	t.Cleanup(func() { _ = testhome.Remove(private) }) //nolint:errcheck
	t.Setenv("TMPDIR", private)

	root := buildReadOnlyModTree(t)
	control := buildReadOnlyModTree(t)

	// Control: the bug. A plain RemoveAll cannot unlink files from a 0555 directory.
	if err := os.RemoveAll(control); err == nil {
		if _, lerr := os.Lstat(control); errors.Is(lerr, fs.ErrNotExist) {
			t.Fatalf("control: os.RemoveAll removed the read-only tree; the fixture is not read-only")
		}
	}
	if _, err := os.Lstat(control); err != nil {
		t.Fatalf("control: expected the read-only tree to survive os.RemoveAll: %v", err)
	}

	if err := testhome.Remove(root); err != nil {
		t.Fatalf("testhome.Remove(%s): %v", root, err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("home %s still exists after testhome.Remove: lstat err=%v", root, err)
	}

	left, err := filepath.Glob(filepath.Join(private, "dw-test-home-*"))
	if err != nil {
		t.Fatalf("glob %s: %v", private, err)
	}
	for _, e := range left {
		if e != control {
			t.Errorf("leftover test home %s after testhome.Remove", e)
		}
	}
}

// TestHomeEnvPinsGoModuleCache checks the live process: TestMain applied
// testhome.PinGoEnv, so go children must write a writable module cache and,
// when one exists, reuse the real module cache instead of the throwaway HOME.
func TestHomeEnvPinsGoModuleCache(t *testing.T) {
	if !hasModcacherw(os.Getenv("GOFLAGS")) {
		t.Errorf("GOFLAGS=%q does not contain -modcacherw", os.Getenv("GOFLAGS"))
	}

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain not found on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "env", "GOFLAGS", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOFLAGS GOMODCACHE: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("go env printed %d lines, want 2: %q", len(lines), out)
	}
	goflags, modcache := lines[0], lines[1]
	if !hasModcacherw(goflags) {
		t.Errorf("go env GOFLAGS=%q does not contain -modcacherw", goflags)
	}

	// Same derivation as testhome.PinGoEnv: GOPATH's first entry, else <realHome>/go.
	gopath := filepath.Join(realHome, "go")
	if gp := filepath.SplitList(os.Getenv("GOPATH")); len(gp) > 0 && gp[0] != "" {
		gopath = gp[0]
	}
	realModCache := filepath.Join(gopath, "pkg", "mod")
	if fi, err := os.Stat(realModCache); err != nil || !fi.IsDir() {
		t.Logf("real module cache %s does not exist; nothing to pin", realModCache)
		return
	}
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME is empty in the test process")
	}
	if rel, err := filepath.Rel(home, modcache); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Errorf("GOMODCACHE %s is under the throwaway HOME %s; want the real cache %s", modcache, home, realModCache)
	}
}

func hasModcacherw(goflags string) bool {
	for _, f := range strings.Fields(goflags) {
		if f == "-modcacherw" {
			return true
		}
	}
	return false
}
