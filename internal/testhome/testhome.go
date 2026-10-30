// Package testhome owns the throwaway-HOME lifecycle for TestMain functions.
//
// It is a normal (non _test) package because _test.go files cannot be
// imported across packages. A package's TestMain calls testhome.Run(m).
package testhome

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run points HOME at a throwaway directory, runs the package's tests and
// removes the directory again, whatever the exit code. It returns the exit
// code for os.Exit. Setup failures print "home isolation: <err>" and return 1.
func Run(m *testing.M) int {
	// Reclaim strays from an earlier run that died before cleanup. Warn
	// and continue: sweeping is bookkeeping, never a gate.
	if err := SweepStale(os.TempDir(), time.Hour, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "home isolation: WARNING: sweep: %v\n", err)
	}
	realHome := os.Getenv("HOME")
	home, err := os.MkdirTemp("", "dw-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "home isolation:", err)
		return 1
	}
	Record(home) // so a later sweep can reclaim it if we die hard
	// Deferred so the removal also runs if m.Run panics.
	defer func() {
		if err := Remove(home); err != nil {
			fmt.Fprintf(os.Stderr, "home isolation: WARNING: could not remove %s: %v\n", home, err)
		}
	}()
	PinGoEnv(realHome)
	DisableGoTelemetry(home)
	if err := os.Setenv("HOME", home); err != nil {
		fmt.Fprintln(os.Stderr, "home isolation:", err)
		return 1
	}
	// Isolate the multiplexer too: without this, a test that spawns tmux
	// inherits TMUX / TMUX_TMPDIR from the operator's shell and would talk
	// to their live server. Deferred like the HOME cleanup above.
	restoreTmux, err := TmuxIsolation()
	if err != nil {
		fmt.Fprintln(os.Stderr, "home isolation:", err)
		return 1
	}
	defer restoreTmux()
	return m.Run()
}

// Remove deletes dir and everything under it, including trees the go tool
// made read-only (module cache). It first makes every directory readable and
// writable and every regular file writable, without following symlinks.
// Removing a path that does not exist returns nil.
func Remove(dir string) error {
	//nolint:errcheck // entries may vanish mid-walk; RemoveAll reports what matters
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		switch {
		case d.IsDir():
			// Pre-visit: WalkDir lists the directory only after this returns.
			if info, ierr := d.Info(); ierr == nil {
				os.Chmod(p, info.Mode().Perm()|0o700) //nolint:errcheck
			}
		case d.Type().IsRegular():
			if info, ierr := d.Info(); ierr == nil {
				os.Chmod(p, info.Mode().Perm()|0o600) //nolint:errcheck
			}
		}
		return nil
	})
	return os.RemoveAll(dir)
}

// PinGoEnv prepares the environment for go tool children that run under the
// throwaway HOME. realHome is the HOME the process started with. Call it
// before HOME is changed and before any child process starts.
//
// GOFLAGS gains -modcacherw, so a module cache the go tool still creates under
// the throwaway HOME is writable and Remove always succeeds.
//
// GOMODCACHE and GOCACHE are pinned to the real locations, but only when they
// are not already set and the directory already exists. This is safe because
// the go tool locks the shared caches and only ever adds to them, which is
// what every go invocation outside the tests does anyway. When the shared
// directory does not exist it is never created: the per-run cache under the
// throwaway HOME is left alone and -modcacherw plus Remove handle it.
func PinGoEnv(realHome string) {
	flags := strings.Fields(os.Getenv("GOFLAGS"))
	has := false
	for _, f := range flags {
		if f == "-modcacherw" {
			has = true
			break
		}
	}
	if !has {
		os.Setenv("GOFLAGS", strings.Join(append(flags, "-modcacherw"), " ")) //nolint:errcheck
	}

	if os.Getenv("GOMODCACHE") == "" {
		gopath := filepath.Join(realHome, "go")
		if gp := filepath.SplitList(os.Getenv("GOPATH")); len(gp) > 0 && gp[0] != "" {
			gopath = gp[0]
		}
		pinIfExists("GOMODCACHE", filepath.Join(gopath, "pkg", "mod"))
	}
	if os.Getenv("GOCACHE") == "" {
		cache := filepath.Join(realHome, ".cache")
		if x := os.Getenv("XDG_CACHE_HOME"); filepath.IsAbs(x) {
			cache = x
		}
		pinIfExists("GOCACHE", filepath.Join(cache, "go-build"))
	}
}

// DisableGoTelemetry marks Go telemetry as off for a go tool that runs with
// home as its HOME. With the default mode the go command writes counter files
// under <home>/.config/go/telemetry and may leave a detached process behind
// that writes there after the test process has already removed home, which
// recreates a stray dw-test-home-* directory. The mode file is the go
// tool's own switch (what "go telemetry off" writes); a failure here only
// loses that protection, so it is ignored.
func DisableGoTelemetry(home string) {
	dir := filepath.Join(home, ".config", "go", "telemetry")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	os.WriteFile(filepath.Join(dir, "mode"), []byte("off 2000-01-01\n"), 0o600) //nolint:errcheck,gosec
}

// pinIfExists sets the env var to dir only if dir is an existing directory.
func pinIfExists(env, dir string) {
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		os.Setenv(env, dir) //nolint:errcheck
	}
}

// PrivateTmp points TMPDIR at a test-scoped directory for the duration of
// t's test. Scratch files the code under test creates via os.CreateTemp or
// os.MkdirTemp (the backend task scratch files are the canonical example)
// then land in a tree t.TempDir already guarantees will be removed even when
// the test fails partway, instead of orphaning in the operator's $TMPDIR.
// Must not be called from a parallel test (t.Setenv restriction).
// Returns the private directory.
func PrivateTmp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

// TmuxDirMarker is a file TmuxIsolation writes inside the throwaway
// TMUX_TMPDIR, so a test can prove a candidate TMUX_TMPDIR was created for
// this test run (and is therefore safe) rather than pointing at a live
// socket directory.
const TmuxDirMarker = "testhome-tmux-owned"

// TmuxIsolation points the process at a throwaway tmux socket directory:
// TMUX is unset (so tmux sees no attached client) and TMUX_TMPDIR is set
// to a fresh os.MkdirTemp directory carrying TmuxDirMarker. Any tmux a test
// spawns only ever binds its socket in that directory and can never reach
// the operator's multiplexer server. It returns a cleanup func that deletes
// the directory and restores both variables; call it before m.Run and
// defer the cleanup. Setup failures print nothing themselves; the caller
// reports the error.
func TmuxIsolation() (func(), error) {
	realTmux := os.Getenv("TMUX")
	realTmp := os.Getenv("TMUX_TMPDIR")
	dir, err := os.MkdirTemp("", "dw-test-tmux-")
	if err != nil {
		return nil, err
	}
	Record(dir) // so a later sweep can reclaim it if we die hard
	fail := func(err error) (func(), error) {
		if remErr := os.RemoveAll(dir); remErr != nil {
			fmt.Fprintf(os.Stderr, "home isolation: WARNING: could not remove %s: %v\n", dir, remErr)
		}
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, TmuxDirMarker),
		[]byte("created by testhome for a single test run\n"), 0o600); err != nil {
		return fail(err)
	}
	if err := os.Unsetenv("TMUX"); err != nil {
		return fail(err)
	}
	if err := os.Setenv("TMUX_TMPDIR", dir); err != nil {
		return fail(err)
	}
	return func() {
		// Restore the ambient values: the test binary exits straight away,
		// so this is belt-and-braces for anyone who reuses the process.
		if realTmux == "" {
			_ = os.Unsetenv("TMUX")
		} else {
			os.Setenv("TMUX", realTmux) //nolint:errcheck
		}
		if realTmp == "" {
			_ = os.Unsetenv("TMUX_TMPDIR")
		} else {
			os.Setenv("TMUX_TMPDIR", realTmp) //nolint:errcheck
		}
		if err := Remove(dir); err != nil {
			fmt.Fprintf(os.Stderr, "home isolation: WARNING: could not remove %s: %v\n", dir, err)
		}
	}, nil
}

// TmuxDirOwned reports whether dir carries TmuxDirMarker, i.e. it was
// created by TmuxIsolation for this test run.
func TmuxDirOwned(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, TmuxDirMarker))
	return err == nil && fi.Mode().IsRegular()
}
