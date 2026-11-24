package git

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// envValue returns the value of the last KEY=VALUE entry for name in env
// ("" and false when absent). Duplicate keys resolve last-wins, as in os/exec.
func envValue(env []string, name string) (string, bool) {
	val, found := "", false
	prefix := name + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			val, found = strings.TrimPrefix(kv, prefix), true
		}
	}
	return val, found
}

// countKey counts entries in env that set name.
func countKey(env []string, name string) int {
	n := 0
	prefix := name + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			n++
		}
	}
	return n
}

// clearTokenVars makes sure none of the token variables are set in the process
// environment for the duration of the test, restoring the originals afterwards.
func clearTokenVars(t *testing.T) {
	t.Helper()
	for _, name := range []string{hostTokenGitHub, hostTokenGitLab, hostTokenGit} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGitHostCredentialEnv(t *testing.T) {
	const secretValue = "tok-SECRET-value-7f3a"
	ctx := context.Background()

	t.Run("vault token appears only in returned slice", func(t *testing.T) {
		clearTokenVars(t)
		var asked []string
		resolve := func(name string) (string, error) {
			asked = append(asked, name)
			return secretValue, nil
		}
		host := GitHostConfig{Name: "work", Kind: "github", CredentialSource: "vault", VaultSecret: "gh-work-pat"}
		env, source, err := HostCommandEnv(ctx, host, resolve)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if source != "vault" {
			t.Fatalf("source = %q, want vault", source)
		}
		if len(asked) != 1 || asked[0] != "gh-work-pat" {
			t.Fatalf("resolver asked for %v, want [gh-work-pat]", asked)
		}
		if got, ok := envValue(env, "GH_TOKEN"); !ok || got != secretValue {
			t.Fatalf("GH_TOKEN in returned slice = %q (found %v), want the secret", got, ok)
		}
		// The token must not reach the process environment.
		if v, ok := os.LookupEnv("GH_TOKEN"); ok {
			t.Fatalf("GH_TOKEN leaked into the process environment: %q", v)
		}
		// Nor the other token variables.
		if _, ok := os.LookupEnv("GITLAB_TOKEN"); ok {
			t.Fatal("GITLAB_TOKEN set in the process environment")
		}
		if _, ok := os.LookupEnv("DATAWATCH_GIT_TOKEN"); ok {
			t.Fatal("DATAWATCH_GIT_TOKEN set in the process environment")
		}
		// The host config passed in never held the value and still does not.
		if strings.Contains(host.Name+host.Kind+host.BaseURL+host.CredentialSource+host.VaultSecret, secretValue) {
			t.Fatal("host config holds the token")
		}
	})

	t.Run("token variable per host kind", func(t *testing.T) {
		cases := []struct {
			kind    string
			varName string
		}{
			{"github", "GH_TOKEN"},
			{"gitlab", "GITLAB_TOKEN"},
			{"gitea", "DATAWATCH_GIT_TOKEN"},
			{"bitbucket", "DATAWATCH_GIT_TOKEN"},
			{"git", "DATAWATCH_GIT_TOKEN"},
		}
		for _, tc := range cases {
			t.Run(tc.kind, func(t *testing.T) {
				clearTokenVars(t)
				host := GitHostConfig{Name: "h", Kind: tc.kind, BaseURL: "https://git.example.com", CredentialSource: "vault", VaultSecret: "s"}
				env, _, err := HostCommandEnv(ctx, host, func(string) (string, error) { return secretValue, nil })
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got, ok := envValue(env, tc.varName); !ok || got != secretValue {
					t.Fatalf("%s = %q (found %v), want the secret", tc.varName, got, ok)
				}
				for _, other := range []string{"GH_TOKEN", "GITLAB_TOKEN", "DATAWATCH_GIT_TOKEN"} {
					if other != tc.varName && countKey(env, other) != 0 {
						t.Fatalf("%s set for kind %s, want only %s", other, tc.kind, tc.varName)
					}
				}
			})
		}
	})

	t.Run("unresolved secret errors without the value", func(t *testing.T) {
		clearTokenVars(t)
		// A failing lookup can return a partial value alongside its error.
		resolve := func(string) (string, error) {
			return secretValue, errors.New("secret not found")
		}
		host := GitHostConfig{Name: "work", Kind: "github", CredentialSource: "vault", VaultSecret: "gh-missing"}
		env, source, err := HostCommandEnv(ctx, host, resolve)
		if err == nil {
			t.Fatal("expected an error for an unresolved secret")
		}
		if env != nil {
			t.Fatal("env must be nil on error")
		}
		if source != "vault" {
			t.Fatalf("source = %q, want vault", source)
		}
		if !strings.Contains(err.Error(), "gh-missing") {
			t.Fatalf("error does not name the secret: %v", err)
		}
		if strings.Contains(err.Error(), secretValue) {
			t.Fatalf("error leaks the secret value: %v", err)
		}
		if !strings.Contains(err.Error(), "secret not found") {
			t.Fatalf("error lost the resolver cause: %v", err)
		}
	})

	t.Run("empty secret value errors without naming a value", func(t *testing.T) {
		clearTokenVars(t)
		host := GitHostConfig{Name: "work", Kind: "gitlab", CredentialSource: "vault", VaultSecret: "gl-empty"}
		_, _, err := HostCommandEnv(ctx, host, func(string) (string, error) { return "", nil })
		if err == nil || !strings.Contains(err.Error(), "gl-empty") {
			t.Fatalf("want error naming the secret, got %v", err)
		}
	})

	t.Run("vault source without secret name errors", func(t *testing.T) {
		clearTokenVars(t)
		host := GitHostConfig{Name: "work", Kind: "github", CredentialSource: "vault"}
		_, _, err := HostCommandEnv(ctx, host, func(string) (string, error) { return secretValue, nil })
		if err == nil {
			t.Fatal("expected an error when vault_secret is empty")
		}
	})

	t.Run("vault source with nil resolver errors", func(t *testing.T) {
		clearTokenVars(t)
		host := GitHostConfig{Name: "work", Kind: "github", CredentialSource: "vault", VaultSecret: "gh-work-pat"}
		_, _, err := HostCommandEnv(ctx, host, nil)
		if err == nil || !strings.Contains(err.Error(), "gh-work-pat") {
			t.Fatalf("want error naming the secret, got %v", err)
		}
	})

	t.Run("host_login adds no variable", func(t *testing.T) {
		clearTokenVars(t)
		resolve := func(string) (string, error) {
			t.Fatal("resolver must not be called for host_login")
			return "", nil
		}
		for _, source := range []string{"host_login", ""} {
			host := GitHostConfig{Name: "gh", Kind: "github", CredentialSource: source, VaultSecret: "gh-work-pat"}
			env, got, err := HostCommandEnv(ctx, host, resolve)
			if err != nil {
				t.Fatalf("source %q: unexpected error: %v", source, err)
			}
			if got != "host_login" {
				t.Fatalf("source %q: reported %q, want host_login", source, got)
			}
			if len(env) != len(os.Environ()) {
				t.Fatalf("source %q: env has %d entries, want the process env (%d)", source, len(env), len(os.Environ()))
			}
			for _, name := range []string{"GH_TOKEN", "GITLAB_TOKEN", "DATAWATCH_GIT_TOKEN"} {
				if countKey(env, name) != 0 {
					t.Fatalf("source %q: %s added, want none", source, name)
				}
			}
		}
	})

	t.Run("broker returns process env and reports source", func(t *testing.T) {
		clearTokenVars(t)
		resolve := func(string) (string, error) {
			t.Fatal("resolver must not be called for broker")
			return "", nil
		}
		host := GitHostConfig{Name: "gh", Kind: "github", CredentialSource: "broker"}
		env, source, err := HostCommandEnv(ctx, host, resolve)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if source != "broker" {
			t.Fatalf("source = %q, want broker", source)
		}
		if len(env) != len(os.Environ()) {
			t.Fatalf("env has %d entries, want the process env (%d)", len(env), len(os.Environ()))
		}
		if countKey(env, "GH_TOKEN") != 0 {
			t.Fatal("broker source added a token")
		}
	})

	t.Run("unknown credential source errors", func(t *testing.T) {
		clearTokenVars(t)
		host := GitHostConfig{Name: "odd", Kind: "github", CredentialSource: "magic"}
		_, source, err := HostCommandEnv(ctx, host, nil)
		if err == nil || source != "magic" {
			t.Fatalf("want error for unknown source, got source=%q err=%v", source, err)
		}
	})

	t.Run("cancelled context errors before resolving", func(t *testing.T) {
		clearTokenVars(t)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		resolve := func(string) (string, error) {
			t.Fatal("resolver must not run on a cancelled context")
			return "", nil
		}
		host := GitHostConfig{Name: "work", Kind: "github", CredentialSource: "vault", VaultSecret: "gh-work-pat"}
		if _, _, err := HostCommandEnv(cctx, host, resolve); !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})
}
