// F10 sprint 8 (S8.1) — secret-provider tests.

package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve_RoutesByKind(t *testing.T) {
	cases := map[string]string{
		"":           "file", // empty defaults to file
		"file":       "file",
		"env":        "env",
		"envvar":     "env",
		"k8s-secret": "k8s-secret",
		"vault":      "vault",
		"csi":        "csi",
		"unknown":    "unknown", // stub echoes
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got := Resolve(in, t.TempDir()).Kind()
			if got != want {
				t.Errorf("Resolve(%q).Kind()=%q want %q", in, got, want)
			}
		})
	}
}

func TestStubProvider_AllOpsErrNotImplemented(t *testing.T) {
	for _, kind := range []string{"k8s-secret", "vault", "csi", "weird"} {
		t.Run(kind, func(t *testing.T) {
			p := Resolve(kind, "")
			if _, err := p.Get("x"); !errors.Is(err, ErrNotImplemented) {
				t.Errorf("Get err=%v want ErrNotImplemented", err)
			}
			if err := p.Put("x", "y"); !errors.Is(err, ErrNotImplemented) {
				t.Errorf("Put err=%v want ErrNotImplemented", err)
			}
			// Error message names the requested provider so operators
			// see exactly which backend is missing.
			_, err := p.Get("x")
			if !strings.Contains(err.Error(), kind) {
				t.Errorf("error should name kind %q: %v", kind, err)
			}
		})
	}
}

// FileProvider round-trip: Put then Get returns the value.
func TestFileProvider_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := NewFileProvider(dir)
	if err := p.Put("api-token", "secret-value"); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get("api-token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret-value" {
		t.Errorf("got=%q want secret-value", got)
	}
	// File should be 0600.
	st, _ := os.Stat(filepath.Join(dir, "api-token"))
	if st.Mode().Perm() != 0600 {
		t.Errorf("perms=%o want 0600", st.Mode().Perm())
	}
}

// FileProvider supports namespace-style keys (slashes = subdirs).
func TestFileProvider_NestedKey(t *testing.T) {
	dir := t.TempDir()
	p := NewFileProvider(dir)
	if err := p.Put("git/github/dmz006", "ghp_xxx"); err != nil {
		t.Fatal(err)
	}
	got, _ := p.Get("git/github/dmz006")
	if got != "ghp_xxx" {
		t.Errorf("nested key round-trip lost: %q", got)
	}
}

// FileProvider trims trailing newline (operators editing in $EDITOR
// typically add one).
func TestFileProvider_TrimsTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	p := NewFileProvider(dir)
	path := filepath.Join(dir, "k")
	_ = os.WriteFile(path, []byte("with-newline\n"), 0600)
	got, _ := p.Get("k")
	if got != "with-newline" {
		t.Errorf("got=%q want with-newline", got)
	}
}

// FileProvider returns ErrNotFound for missing keys + carries the
// path in the message so operators can debug.
func TestFileProvider_GetMissing(t *testing.T) {
	p := NewFileProvider(t.TempDir())
	_, err := p.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err=%v want ErrNotFound", err)
	}
}

// FileProvider rejects path-traversal attempts.
func TestFileProvider_RefusesPathTraversal(t *testing.T) {
	p := NewFileProvider(t.TempDir())
	cases := []string{"../escape", "/abs/path", "valid/../escape"}
	for _, k := range cases {
		t.Run(k, func(t *testing.T) {
			if _, err := p.Get(k); err == nil {
				t.Errorf("Get(%q) should refuse", k)
			}
			if err := p.Put(k, "v"); err == nil {
				t.Errorf("Put(%q) should refuse", k)
			}
		})
	}
}

// FileProvider with empty BaseDir defaults to ~/.datawatch/secrets/.
func TestFileProvider_DefaultBaseDir(t *testing.T) {
	p := NewFileProvider("")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".datawatch", "secrets")
	if p.BaseDir != want {
		t.Errorf("BaseDir=%q want %q", p.BaseDir, want)
	}
}

// Atomic write: a partial-content .tmp file shouldn't break Get.
func TestFileProvider_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	p := NewFileProvider(dir)
	// Pre-seed a stale .tmp file (simulating a crashed write).
	_ = os.WriteFile(filepath.Join(dir, "k.tmp"), []byte("partial"), 0600)
	if err := p.Put("k", "real-value"); err != nil {
		t.Fatal(err)
	}
	got, _ := p.Get("k")
	if got != "real-value" {
		t.Errorf("got=%q want real-value", got)
	}
}

// EnvVarProvider reads existing env + Put exports.
func TestEnvVarProvider_RoundTrip(t *testing.T) {
	t.Setenv("MY_TOKEN", "")
	p := NewEnvVarProvider()
	if err := p.Put("MY_TOKEN", "v"); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get("MY_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v" {
		t.Errorf("got=%q want v", got)
	}
}

// EnvVarProvider with prefix joins on read + write.
func TestEnvVarProvider_Prefix(t *testing.T) {
	t.Setenv("DATAWATCH_SECRET_FOO", "")
	p := NewEnvVarProvider()
	p.Prefix = "DATAWATCH_SECRET_"
	if err := p.Put("FOO", "bar"); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DATAWATCH_SECRET_FOO"); got != "bar" {
		t.Errorf("env after Put=%q want bar", got)
	}
	got, _ := p.Get("FOO")
	if got != "bar" {
		t.Errorf("Get got=%q want bar", got)
	}
}

// EnvVarProvider treats unset + empty as ErrNotFound (security
// hardening — empty secret is never a valid secret).
func TestEnvVarProvider_EmptyIsNotFound(t *testing.T) {
	t.Setenv("EMPTY_TOKEN", "")
	p := NewEnvVarProvider()
	if _, err := p.Get("EMPTY_TOKEN"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err=%v want ErrNotFound for empty value", err)
	}
}

// EnvVarProvider rejects path-like keys.
func TestEnvVarProvider_RefusesSlashKey(t *testing.T) {
	p := NewEnvVarProvider()
	if err := p.Put("git/token", "v"); err == nil {
		t.Error("Put with slash key should refuse")
	}
}
