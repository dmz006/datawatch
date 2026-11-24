package git

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// hostCheckTimeout bounds every external command the readiness check runs.
const hostCheckTimeout = 15 * time.Second

// GitHostConfig is the subset of config.GitHostConfig the readiness check and
// the host command environment read. It is mirrored here because package config
// imports trace, which imports this package, so importing config would be an
// import cycle. Callers convert with
// GitHostConfig{Name: h.Name, Kind: h.Kind, BaseURL: h.BaseURL,
// CredentialSource: h.CredentialSource, VaultSecret: h.VaultSecret}.
type GitHostConfig struct {
	Name    string
	Kind    string
	BaseURL string
	// CredentialSource is host_login, vault or broker (empty reads as host_login).
	CredentialSource string
	// VaultSecret is the secrets vault entry NAME holding the token when
	// CredentialSource is vault. It never holds the secret value.
	VaultSecret string
	// SignTags makes CreateTagForHost create signed (git tag -s) tags.
	SignTags bool
	// SigningKey is the key id passed as git tag -u when SignTags is set.
	// Empty uses git's default signing key.
	SigningKey string
}

// HostReadiness reports whether datawatch can use one configured git host.
// It never carries a token: only the account name, scope names and a
// plain-language problem.
type HostReadiness struct {
	Host             string   `json:"host"`
	Kind             string   `json:"kind"`
	LoggedIn         bool     `json:"logged_in"`
	Account          string   `json:"account,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	HasRepoScope     bool     `json:"has_repo_scope,omitempty"`
	HasWorkflowScope bool     `json:"has_workflow_scope,omitempty"`
	HasAPIScope      bool     `json:"has_api_scope,omitempty"`
	FixCommand       string   `json:"fix_command,omitempty"`
	Problem          string   `json:"problem,omitempty"`
}

var (
	reHostAccount = regexp.MustCompile(`(?i)logged in to \S+ (?:as|account) ([^\s(]+)`)
	reHostScopes  = regexp.MustCompile(`(?i)token scopes:\s*(.+)`)
	reHostToken   = regexp.MustCompile(`(gh[pousr]_[A-Za-z0-9_]+|github_pat_[A-Za-z0-9_]+|glpat-[A-Za-z0-9_\-]+|://[^/@\s]+:[^/@\s]+@)`)
)

// redactHostText removes anything token-shaped from text.
func redactHostText(s string) string {
	return reHostToken.ReplaceAllString(s, "[redacted]")
}

// hostBaseName returns the host part of a base URL ("" when unset/invalid).
func hostBaseName(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return u.Host
}

// parseHostAuth extracts the account and scope names from gh/glab status text.
func parseHostAuth(out string) (account string, scopes []string) {
	if m := reHostAccount.FindStringSubmatch(out); m != nil {
		account = m[1]
	}
	if m := reHostScopes.FindStringSubmatch(out); m != nil {
		line := strings.TrimSpace(strings.SplitN(m[1], "\n", 2)[0])
		for _, s := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\'' || r == '"'
		}) {
			if s != "" && s != "none" {
				scopes = append(scopes, s)
			}
		}
	}
	return account, scopes
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// runHostCmd runs a command for host with a 15s deadline per attempt and
// returns its output (stdout then stderr). The host's credential source decides
// the child's environment (see runHostCommand): a vault-sourced host, or a
// host_login host with a fallback vault secret whose login is not available,
// gets its token in that one child's environment and nowhere else.
func runHostCmd(ctx context.Context, host GitHostConfig, name string, args ...string) (string, error) {
	// Every command run through here is the host's login check or a read-only
	// probe; only the auth status calls are login checks.
	loginCheck := len(args) >= 2 && args[0] == "auth" && args[1] == "status"
	so, se, err := runHostCommand(ctx, host, HostCmd{Name: name, Args: args, Timeout: hostCheckTimeout}, loginCheck)
	return so + se, err
}

// hostProblemMessage returns the readiness problem for err when it is a vault
// credential error (so the operator sees the secret name), or fallback.
func hostProblemMessage(err error, fallback string) string {
	var ce *HostCredentialError
	if errors.As(err, &ce) {
		return redactHostText(ce.Error())
	}
	return fallback
}

// CheckHostReadiness checks whether the host's CLI login (github, gitlab) or
// remote (gitea, bitbucket, git) is usable. It never returns a token.
func CheckHostReadiness(ctx context.Context, host GitHostConfig) HostReadiness {
	r := HostReadiness{Host: host.Name, Kind: host.Kind}
	base := hostBaseName(host.BaseURL)

	switch host.Kind {
	case "github":
		args := []string{"auth", "status"}
		if base != "" {
			args = append(args, "--hostname", base)
		}
		out, err := runHostCmd(ctx, host, "gh", args...)
		loginHost := base
		if loginHost == "" {
			loginHost = "github.com"
		}
		if err != nil && errors.Is(err, exec.ErrNotFound) {
			r.Problem = "The gh command is not installed, so GitHub access cannot be checked."
			r.FixCommand = "gh auth login --hostname " + loginHost
			return r
		}
		if err != nil {
			r.Problem = hostProblemMessage(err, "Not logged in to "+loginHost+" with gh.")
			r.FixCommand = "gh auth login --hostname " + loginHost
			return r
		}
		r.LoggedIn = true
		r.Account, r.Scopes = parseHostAuth(out)
		r.HasRepoScope = hasScope(r.Scopes, "repo")
		r.HasWorkflowScope = hasScope(r.Scopes, "workflow")
		if !r.HasRepoScope || !r.HasWorkflowScope {
			var miss []string
			if !r.HasRepoScope {
				miss = append(miss, "repo")
			}
			if !r.HasWorkflowScope {
				miss = append(miss, "workflow")
			}
			r.Problem = "The gh login is missing the " + strings.Join(miss, " and ") + " scope."
			fix := "gh auth refresh"
			if loginHost != "github.com" {
				fix += " -h " + loginHost
			}
			r.FixCommand = fix + " -s repo,workflow"
		}
	case "gitlab":
		if base == "" {
			base = "gitlab.com"
		}
		out, err := runHostCmd(ctx, host, "glab", "auth", "status", "--hostname", base)
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				r.Problem = "The glab command is not installed, so GitLab access cannot be checked."
			} else {
				r.Problem = hostProblemMessage(err, "Not logged in to "+base+" with glab.")
			}
			r.FixCommand = "glab auth login --hostname " + base
			return r
		}
		r.LoggedIn = true
		r.Account, r.Scopes = parseHostAuth(out)
		r.HasAPIScope = hasScope(r.Scopes, "api")
		if !r.HasAPIScope {
			r.Problem = "The glab login is missing the api scope."
			r.FixCommand = "glab auth login --hostname " + base
		}
	default:
		if strings.TrimSpace(host.BaseURL) == "" {
			r.Problem = "No base URL is set for this host."
			return r
		}
		_, err := runHostCmd(ctx, host, "git", "ls-remote", "--heads", host.BaseURL)
		if err != nil {
			r.Problem = hostProblemMessage(err, "Could not reach "+redactHostText(host.BaseURL)+".")
			return r
		}
		r.LoggedIn = true
	}
	r.Problem = redactHostText(r.Problem)
	r.Account = redactHostText(r.Account)
	return r
}
