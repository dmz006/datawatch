// BL111 — ResolveCreds tests.

package agents

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
	"github.com/dmz006/datawatch/internal/secrets"
)

func TestResolveCreds_NilProvider_ReturnsLiteralKey(t *testing.T) {
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	m := NewManager(ps, cs)

	got, err := m.ResolveCreds(profile.CredsRef{Key: "/etc/datawatch/secret"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/etc/datawatch/secret" {
		t.Errorf("got %q want /etc/datawatch/secret (literal-key fallback)", got)
	}
}

func TestResolveCreds_EmptyKey_NoOp(t *testing.T) {
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	m := NewManager(ps, cs)
	m.SecretsProvider = secrets.NewEnvVarProvider()

	got, err := m.ResolveCreds(profile.CredsRef{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q want empty", got)
	}
}

func TestResolveCreds_FileProvider_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	m := NewManager(ps, cs)
	m.SecretsProvider = secrets.NewFileProvider(dir)

	if err := m.SecretsProvider.Put("github-pat", "ghp_secret"); err != nil {
		t.Fatal(err)
	}
	got, err := m.ResolveCreds(profile.CredsRef{Provider: "file", Key: "github-pat"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ghp_secret" {
		t.Errorf("got %q want ghp_secret", got)
	}
}

func TestResolveCreds_StubProvider_BubbleError(t *testing.T) {
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	m := NewManager(ps, cs)
	m.SecretsProvider = secrets.Resolve("vault", "")

	_, err := m.ResolveCreds(profile.CredsRef{Provider: "vault", Key: "anything"})
	if err == nil {
		t.Fatal("expected ErrNotImplemented from vault stub")
	}
	if !errors.Is(err, secrets.ErrNotImplemented) {
		t.Errorf("got %v want ErrNotImplemented", err)
	}
}
