// BL403 — PluginTokenRegistry tests.

package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginTokenRegistry_RegisterVerify(t *testing.T) {
	r, err := NewPluginTokenRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Register(""); err == nil {
		t.Fatal("empty plugin name accepted")
	}
	tok, err := r.Register("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 40 {
		t.Fatalf("token %q looks too short", tok)
	}
	if err := r.Verify("p", tok); err != nil {
		t.Fatalf("Verify(own token) = %v", err)
	}
	if err := r.Verify("p", tok+"x"); !errors.Is(err, ErrInvalidPluginToken) {
		t.Fatalf("Verify(wrong token) = %v, want ErrInvalidPluginToken", err)
	}
	if err := r.Verify("other", tok); !errors.Is(err, ErrUnknownPluginToken) {
		t.Fatalf("Verify(other plugin) = %v, want ErrUnknownPluginToken", err)
	}
}

func TestPluginTokenRegistry_TokensArePerPlugin(t *testing.T) {
	r, _ := NewPluginTokenRegistry("")
	a, _ := r.Register("a")
	b, _ := r.Register("b")
	if err := r.Verify("a", b); err == nil {
		t.Fatal("plugin b's token verified for plugin a")
	}
	if err := r.Verify("b", a); err == nil {
		t.Fatal("plugin a's token verified for plugin b")
	}
}

func TestPluginTokenRegistry_RotateAndRevoke(t *testing.T) {
	r, _ := NewPluginTokenRegistry("")
	old, _ := r.Register("p")
	cur, _ := r.Register("p")
	if old == cur {
		t.Fatal("rotation returned the same token")
	}
	if err := r.Verify("p", old); err == nil {
		t.Fatal("old token still verifies after rotation")
	}
	if err := r.Verify("p", cur); err != nil {
		t.Fatalf("new token: %v", err)
	}
	if err := r.Revoke("p"); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify("p", cur); !errors.Is(err, ErrUnknownPluginToken) {
		t.Fatalf("Verify after Revoke = %v", err)
	}
	if err := r.Revoke("p"); err != nil {
		t.Fatalf("Revoke of unknown plugin: %v", err)
	}
}

func TestPluginTokenRegistry_Persistence(t *testing.T) {
	dir := t.TempDir()
	r, err := NewPluginTokenRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := r.Register("p")
	if err != nil {
		t.Fatal(err)
	}
	gone, _ := r.Register("gone")
	if err := r.Revoke("gone"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "plugins", "widget_tokens.json")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("widget_tokens.json mode = %o, want 600", perm)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), tok) {
		t.Fatal("plaintext token persisted")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file left behind: %v", err)
	}

	r2, err := NewPluginTokenRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := r2.Verify("p", tok); err != nil {
		t.Fatalf("token did not survive reload: %v", err)
	}
	if err := r2.Verify("gone", gone); !errors.Is(err, ErrUnknownPluginToken) {
		t.Fatalf("revoked token survived reload: %v", err)
	}
}

func TestPluginTokenRegistry_LoadEdgeCases(t *testing.T) {
	dir := t.TempDir()
	pdir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pdir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(pdir, "widget_tokens.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPluginTokenRegistry(dir); err != nil {
		t.Fatalf("empty file: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPluginTokenRegistry(dir); err == nil {
		t.Fatal("corrupt file accepted")
	}
}
