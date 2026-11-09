package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitHostConfig covers the git.hosts model: a valid host, every
// validation failure reported in one pass, the load-time defaults, and the
// dotted key helpers. No test uses or prints a secret value — vault_secret is
// a vault entry name only.
func TestGitHostConfig(t *testing.T) {
	t.Run("valid host passes", func(t *testing.T) {
		hosts := []GitHostConfig{
			{Name: "work-gitea", Kind: GitHostKindGitea, BaseURL: "https://git.example.test", CredentialSource: GitCredentialVault, VaultSecret: "gitea-token", SignTags: true, SigningKey: "ABCD1234"},
			{Name: "gh", Kind: GitHostKindGitHub},
			{Name: "gl-cloud", Kind: GitHostKindGitLab},
		}
		if err := ValidateGitHosts(hosts); err != nil {
			t.Fatalf("valid hosts: unexpected error: %v", err)
		}
	})

	t.Run("validation failures are all reported", func(t *testing.T) {
		hosts := []GitHostConfig{
			{Name: "dup", Kind: GitHostKindGitHub},
			{Name: "dup", Kind: GitHostKindGitHub},                            // duplicate name
			{Name: "bad", Kind: "svn"},                                        // unknown kind
			{Name: "src", Kind: GitHostKindGitHub, CredentialSource: "magic"}, // unknown credential source
			{Name: "nosecret", Kind: GitHostKindGitHub, CredentialSource: GitCredentialVault, VaultSecret: "  "}, // vault without secret name
			{Name: "nourl", Kind: GitHostKindGitea},     // self-hosted without base URL
			{Name: "Bad Name", Kind: GitHostKindGitHub}, // not a slug
		}
		err := ValidateGitHosts(hosts)
		if err == nil {
			t.Fatal("expected validation errors, got nil")
		}
		msg := err.Error()
		for _, want := range []string{
			`duplicate name "dup"`,
			`unknown kind "svn"`,
			`unknown credential_source "magic"`,
			"requires vault_secret",
			"requires base_url",
			"must be a lowercase slug",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("error missing %q; got:\n%s", want, msg)
			}
		}
	})

	t.Run("gitlab and github need no base URL", func(t *testing.T) {
		hosts := []GitHostConfig{
			{Name: "gl", Kind: GitHostKindGitLab},
			{Name: "gh", Kind: GitHostKindGitHub},
		}
		if err := ValidateGitHosts(hosts); err != nil {
			t.Fatalf("gitlab/github without base_url must be valid: %v", err)
		}
	})

	t.Run("defaults applied at load", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		yaml := "git:\n  hosts:\n    - name: gh\n      kind: github\n"
		if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(cfg.Git.Hosts) != 1 {
			t.Fatalf("hosts: got %d entries, want 1", len(cfg.Git.Hosts))
		}
		h := cfg.Git.Hosts[0]
		if h.CredentialSource != GitCredentialHostLogin {
			t.Errorf("credential_source default: got %q, want %q", h.CredentialSource, GitCredentialHostLogin)
		}
		if h.SignTags {
			t.Error("sign_tags default: want false")
		}
		if h.BaseURL != "" {
			t.Errorf("base_url default: got %q, want empty (github means github.com)", h.BaseURL)
		}
	})

	t.Run("load rejects a malformed host list", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		yaml := "git:\n  hosts:\n    - name: gt\n      kind: gitea\n"
		if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "requires base_url") {
			t.Fatalf("Load: want base_url error, got %v", err)
		}
	})

	t.Run("dotted key get", func(t *testing.T) {
		g := GitConfig{Hosts: []GitHostConfig{{
			Name: "work", Kind: GitHostKindGitea, BaseURL: "https://git.example.test",
			CredentialSource: GitCredentialVault, VaultSecret: "gitea-token", SignTags: true, SigningKey: "KEY1",
		}}}
		cases := map[string]string{
			"git.hosts.work.name":              "work",
			"git.hosts.work.kind":              "gitea",
			"git.hosts.work.base_url":          "https://git.example.test",
			"git.hosts.work.credential_source": "vault",
			"git.hosts.work.vault_secret":      "gitea-token",
			"git.hosts.work.sign_tags":         "true",
			"git.hosts.work.signing_key":       "KEY1",
		}
		for key, want := range cases {
			got, err := GetGitHostField(g, key)
			if err != nil {
				t.Errorf("get %s: %v", key, err)
				continue
			}
			if got != want {
				t.Errorf("get %s: got %q, want %q", key, got, want)
			}
		}
		for _, bad := range []string{"git.hosts.nope.kind", "git.hosts.work.bogus", "git.hosts.work", "hosts.work.kind"} {
			if _, err := GetGitHostField(g, bad); err == nil {
				t.Errorf("get %s: want error", bad)
			}
		}
	})

	t.Run("dotted key set", func(t *testing.T) {
		g := &GitConfig{Hosts: []GitHostConfig{{Name: "work", Kind: GitHostKindGit, BaseURL: "https://x.example.test"}}}
		if err := SetGitHostField(g, "git.hosts.work.sign_tags", "true"); err != nil {
			t.Fatalf("set sign_tags: %v", err)
		}
		if !g.Hosts[0].SignTags {
			t.Error("sign_tags not set")
		}
		if err := SetGitHostField(g, "git.hosts.work.credential_source", ""); err != nil {
			t.Fatalf("reset credential_source: %v", err)
		}
		if g.Hosts[0].CredentialSource != GitCredentialHostLogin {
			t.Errorf("empty credential_source should reset to host_login, got %q", g.Hosts[0].CredentialSource)
		}

		// A value that makes the list invalid is refused and g is unchanged.
		before := g.Hosts[0]
		if err := SetGitHostField(g, "git.hosts.work.credential_source", "vault"); err == nil {
			t.Fatal("vault without vault_secret: want error")
		}
		if g.Hosts[0] != before {
			t.Errorf("failed set mutated the host: %+v", g.Hosts[0])
		}
		if err := SetGitHostField(g, "git.hosts.work.base_url", ""); err == nil {
			t.Fatal("self-hosted git with empty base_url: want error")
		}

		if err := SetGitHostField(g, "git.hosts.work.name", "renamed"); err == nil {
			t.Error("setting name: want error")
		}
		if err := SetGitHostField(g, "git.hosts.work.sign_tags", "maybe"); err == nil {
			t.Error("sign_tags=maybe: want error")
		}
		if err := SetGitHostField(g, "git.hosts.nope.kind", "github"); err == nil {
			t.Error("unknown host: want error")
		}
		if err := SetGitHostField(g, "git.hosts.work.unknown", "x"); err == nil {
			t.Error("unknown field: want error")
		}
	})
}
