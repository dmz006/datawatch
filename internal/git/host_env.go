package git

// host_env.go builds the environment for one command run against a configured
// git host. The token for a vault credential is placed only in the returned
// slice: it is never written to the process environment, the config, or an
// error message. The resolver is a parameter so tests need no live vault; the
// production caller passes a lookup over the secrets provider.

import (
	"context"
	"fmt"
	"os"
)

// Credential sources a git host can use. They mirror the config package
// constants (git.hosts[].credential_source); this package cannot import config
// (see GitHostConfig).
const (
	hostCredentialHostLogin = "host_login"
	hostCredentialVault     = "vault"
	hostCredentialBroker    = "broker"
)

// Token environment variables read by each host's CLI or by git.
const (
	hostTokenGitHub = "GH_TOKEN"
	hostTokenGitLab = "GITLAB_TOKEN"
	hostTokenGit    = "DATAWATCH_GIT_TOKEN"
)

// HostCommandEnv returns the environment slice for one command run against
// host, the credential source it used, and an error.
//
//   - host_login (the default): the process environment, unchanged, with no
//     token added.
//   - vault: the process environment plus one token variable whose value is
//     the secret named by host.VaultSecret, read through resolve. The variable
//     is GH_TOKEN for github, GITLAB_TOKEN for gitlab, and DATAWATCH_GIT_TOKEN
//     for gitea, bitbucket, git and any other kind.
//   - broker: the process environment, unchanged. The token broker supplies
//     tokens to worker sessions, so no host-side token is added here. The
//     source is still reported so the caller can log it.
//
// An error names the host and the secret, never a secret value. ctx is checked
// before any secret is resolved.
func HostCommandEnv(ctx context.Context, host GitHostConfig, resolve func(name string) (string, error)) ([]string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	source := host.CredentialSource
	if source == "" {
		source = hostCredentialHostLogin
	}
	switch source {
	case hostCredentialHostLogin, hostCredentialBroker:
		return os.Environ(), source, nil
	case hostCredentialVault:
	default:
		return nil, source, fmt.Errorf("git host %q: unknown credential source %q", host.Name, source)
	}

	env, _, err := vaultTokenEnv(host, resolve)
	if err != nil {
		return nil, source, err
	}
	return env, source, nil
}

// tokenEnviron returns the process environment plus the token variable for
// kind. The token exists only in the returned slice.
func tokenEnviron(kind, token string) []string {
	return append(os.Environ(), hostTokenEnvName(kind)+"="+token)
}

// hostTokenEnvName returns the variable that carries a vault token for kind.
func hostTokenEnvName(kind string) string {
	switch kind {
	case "github":
		return hostTokenGitHub
	case "gitlab":
		return hostTokenGitLab
	default:
		return hostTokenGit
	}
}
