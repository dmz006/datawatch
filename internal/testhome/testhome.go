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
)

// Run points HOME at a throwaway directory, runs the package's tests and
// removes the directory again, whatever the exit code. It returns the exit
// code for os.Exit. Setup failures print "home isolation: <err>" and return 1.
func Run(m *testing.M) int {
	realHome := os.Getenv("HOME")
	home, err := os.MkdirTemp("", "dw-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "home isolation:", err)
		return 1
	}
	// Deferred so the removal also runs if m.Run panics.
	defer func() {
		if err := Remove(home); err != nil {
			fmt.Fprintf(os.Stderr, "home isolation: WARNING: could not remove %s: %v\n", home, err)
		}
	}()
	PinGoEnv(realHome)
	if err := os.Setenv("HOME", home); err != nil {
		fmt.Fprintln(os.Stderr, "home isolation:", err)
		return 1
	}
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

// pinIfExists sets the env var to dir only if dir is an existing directory.
func pinIfExists(env, dir string) {
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		os.Setenv(env, dir) //nolint:errcheck
	}
}
