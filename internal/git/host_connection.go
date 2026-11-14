package git

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
)

// ConnectionResult is the outcome of TestHostConnection for one git host.
// Message is plain language and never carries a token or raw command output.
type ConnectionResult struct {
	Ok         bool   `json:"ok"`
	Account    string `json:"account,omitempty"`
	Message    string `json:"message"`
	FixCommand string `json:"fix_command,omitempty"`
}

// hostLoginName returns the host name shown to the operator for a github or
// gitlab login, falling back to the public default when no base URL is set.
func hostLoginName(host GitHostConfig, defaultHost string) string {
	if base := hostBaseName(host.BaseURL); base != "" {
		return base
	}
	return defaultHost
}

// TestHostConnection checks one configured git host end to end. It first runs
// CheckHostReadiness; when that reports a problem it returns Ok false with the
// readiness problem and fix command and does not probe. When the host is ready
// it runs one read-only probe (gh api user, glab api user, or git ls-remote for
// the plain kinds) under the same 15 second deadline as the readiness check.
func TestHostConnection(ctx context.Context, host GitHostConfig) ConnectionResult {
	r := CheckHostReadiness(ctx, host)
	if r.Problem != "" || !r.LoggedIn {
		return ConnectionResult{
			Ok:         false,
			Account:    r.Account,
			Message:    r.Problem,
			FixCommand: r.FixCommand,
		}
	}

	switch host.Kind {
	case "github":
		return probeGitHub(ctx, host, r)
	case "gitlab":
		return probeGitLab(ctx, host, r)
	default:
		return probePlainRemote(ctx, host)
	}
}

// probeGitHub runs gh api user against the configured hostname.
func probeGitHub(ctx context.Context, host GitHostConfig, r HostReadiness) ConnectionResult {
	base := hostBaseName(host.BaseURL)
	loginHost := hostLoginName(host, "github.com")
	args := []string{"api", "user"}
	if base != "" {
		args = append(args, "--hostname", base)
	}
	out, err := runHostCmd(ctx, host, "gh", args...)
	fix := "gh auth login --hostname " + loginHost
	if err != nil {
		return ConnectionResult{
			Ok:         false,
			Account:    r.Account,
			Message:    probeFailureMessage(err, "GitHub", loginHost, "gh"),
			FixCommand: fix,
		}
	}
	account := redactHostText(parseProbeAccount(out, "login"))
	if account == "" {
		account = r.Account
	}
	return ConnectionResult{
		Ok:      true,
		Account: account,
		Message: connectedMessage(account, loginHost),
	}
}

// probeGitLab runs glab api user against the configured hostname.
func probeGitLab(ctx context.Context, host GitHostConfig, r HostReadiness) ConnectionResult {
	loginHost := hostLoginName(host, "gitlab.com")
	out, err := runHostCmd(ctx, host, "glab", "api", "user", "--hostname", loginHost)
	fix := "glab auth login --hostname " + loginHost
	if err != nil {
		return ConnectionResult{
			Ok:         false,
			Account:    r.Account,
			Message:    probeFailureMessage(err, "GitLab", loginHost, "glab"),
			FixCommand: fix,
		}
	}
	account := redactHostText(parseProbeAccount(out, "username"))
	if account == "" {
		account = r.Account
	}
	return ConnectionResult{
		Ok:      true,
		Account: account,
		Message: connectedMessage(account, loginHost),
	}
}

// probePlainRemote runs git ls-remote against the base URL of a plain host.
// The "--" stops the URL being read as an option.
func probePlainRemote(ctx context.Context, host GitHostConfig) ConnectionResult {
	target := redactHostText(strings.TrimSpace(host.BaseURL))
	_, err := runHostCmd(ctx, host, "git", "ls-remote", "--heads", "--", host.BaseURL)
	if err != nil {
		return ConnectionResult{
			Ok:      false,
			Message: probeFailureMessage(err, "", target, "git"),
		}
	}
	return ConnectionResult{
		Ok:      true,
		Message: "Reached " + target + ".",
	}
}

// parseProbeAccount reads the account name from a JSON user response. It
// returns "" when the output is not JSON or the field is missing.
func parseProbeAccount(out, field string) string {
	var user map[string]any
	if err := json.Unmarshal([]byte(out), &user); err != nil {
		return ""
	}
	name, _ := user[field].(string)
	return strings.TrimSpace(name)
}

// probeFailureMessage turns a probe error into plain language. The provider
// label is empty for plain remotes, which name the target directly.
func probeFailureMessage(err error, provider, target, tool string) string {
	var ce *HostCredentialError
	if errors.As(err, &ce) {
		return redactHostText(ce.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The check on " + target + " timed out after 15 seconds."
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "The " + tool + " command is not installed, so " + target + " cannot be checked."
	}
	if provider == "" {
		return "Could not reach " + target + "."
	}
	return "Could not read the account on " + target + " with " + tool + ", so the login may have expired."
}

// connectedMessage describes a successful probe for a github or gitlab host.
func connectedMessage(account, loginHost string) string {
	if account == "" {
		return "Connected to " + loginHost + "."
	}
	return "Connected to " + loginHost + " as " + account + "."
}
