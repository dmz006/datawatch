package git

// host_runner.go is the one place a child process is started for a configured
// git host (gh, glab or git). It applies the host's credential source:
//
//   - vault: the secret named by host.VaultSecret is read from the secrets
//     vault and passed to that one child as the host's token variable.
//   - host_login (the default) with a vault secret name set: the command runs
//     with the process environment untouched. Only when it fails AND the
//     host's own login is not available is the secret read and the command run
//     once more with the token. This is the optional fallback for servers
//     and clusters.
//   - broker, or host_login with no secret name: the environment is untouched.
//
// The token lives only in the Env slice handed to the runner for one call. It
// is never put on the command line, logged, stored, or left in output or an
// error message: output and error text are scrubbed of the exact token value
// before they are returned. The vault lookup is a package-level resolver set at
// daemon start (SetHostSecretResolver), and the child process starter is an
// interface, so tests use a fake runner and a fake vault.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// HostCmd describes one child process run for a git host.
type HostCmd struct {
	// Name is the binary (gh, glab, git, or a path in tests).
	Name string
	// Args are the command arguments. They never carry a token.
	Args []string
	// Dir is the working directory ("" keeps the daemon's own).
	Dir string
	// Env is the child's environment. Nil means the child inherits the
	// daemon's environment unchanged; non-nil replaces it (a token variable
	// is only ever added here).
	Env []string
	// Timeout bounds one attempt (0 means only the caller's context bounds it).
	Timeout time.Duration
}

// HostCmdRunner starts one child process and returns its stdout and stderr.
type HostCmdRunner interface {
	Run(ctx context.Context, c HostCmd) (stdout, stderr []byte, err error)
}

// execHostRunner is the production runner: it starts the real process.
type execHostRunner struct{}

// Run implements HostCmdRunner with os/exec (no shell).
func (execHostRunner) Run(ctx context.Context, c HostCmd) ([]byte, []byte, error) {
	cctx := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(cctx, c.Name, c.Args...) // #nosec G204 -- fixed binary names, args built from validated config
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if cctx.Err() != nil {
		return stdout.Bytes(), stderr.Bytes(), cctx.Err()
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

var (
	hostRunMu          sync.RWMutex
	hostRunner         HostCmdRunner = execHostRunner{}
	hostSecretResolver func(name string) (string, error)
)

// SetHostSecretResolver installs the secrets vault lookup used to read a git
// host's token by secret name. The daemon calls it once the secrets store is
// ready. A nil resolver removes it, after which a vault-sourced host fails
// with an error that names the secret. The resolver returns the secret value;
// it must not put the value in an error.
func SetHostSecretResolver(resolve func(name string) (string, error)) {
	hostRunMu.Lock()
	defer hostRunMu.Unlock()
	hostSecretResolver = resolve
}

// currentHostSecretResolver returns the installed resolver (nil when unset).
func currentHostSecretResolver() func(name string) (string, error) {
	hostRunMu.RLock()
	defer hostRunMu.RUnlock()
	return hostSecretResolver
}

// setHostCmdRunner swaps the child process starter and returns a restore
// function. Tests use it to substitute a fake runner.
func setHostCmdRunner(r HostCmdRunner) (restore func()) {
	hostRunMu.Lock()
	prev := hostRunner
	hostRunner = r
	hostRunMu.Unlock()
	return func() {
		hostRunMu.Lock()
		hostRunner = prev
		hostRunMu.Unlock()
	}
}

func currentHostCmdRunner() HostCmdRunner {
	hostRunMu.RLock()
	defer hostRunMu.RUnlock()
	return hostRunner
}

// HostCredentialError reports that a host's token could not be obtained from
// the vault. It names the host and the secret NAME, never a value.
type HostCredentialError struct {
	Host   string
	Secret string
	// Fallback is true when the host's own login was not available and the
	// vault secret was read as the fallback.
	Fallback bool
	Err      error
}

// Error returns a plain-language message naming the host and the secret.
func (e *HostCredentialError) Error() string {
	if e.Fallback {
		return fmt.Sprintf("git host %q: the host login is not available and the fallback vault secret %q could not be used: %v", e.Host, e.Secret, e.Err)
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *HostCredentialError) Unwrap() error { return e.Err }

// vaultTokenEnv reads the secret named by host.VaultSecret through resolve and
// returns the process environment plus the host's token variable, and the
// token itself so the caller can scrub it from output. The error names the
// host and the secret, never the value.
func vaultTokenEnv(host GitHostConfig, resolve func(name string) (string, error)) (env []string, token string, err error) {
	if host.VaultSecret == "" {
		return nil, "", fmt.Errorf("git host %q: vault credential source has no vault_secret name", host.Name)
	}
	if resolve == nil {
		return nil, "", fmt.Errorf("git host %q: no secret resolver for vault secret %q", host.Name, host.VaultSecret)
	}
	// The value is ignored whenever err is set, so a partial result from a
	// failing lookup never reaches the environment or an error message.
	token, err = resolve(host.VaultSecret)
	if err != nil {
		return nil, "", fmt.Errorf("git host %q: cannot resolve vault secret %q: %w", host.Name, host.VaultSecret, err)
	}
	if token == "" {
		return nil, "", fmt.Errorf("git host %q: vault secret %q is empty", host.Name, host.VaultSecret)
	}
	return tokenEnviron(host.Kind, token), token, nil
}

// scrubToken removes every occurrence of token from s.
func scrubToken(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "[redacted]")
}

// scrubErr returns err with token removed from its text. An error that does
// not contain the token is returned unchanged so errors.Is and errors.As still
// work; one that does is replaced by a plain error with the token redacted.
func scrubErr(err error, token string) error {
	if err == nil || token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return errors.New(scrubToken(err.Error(), token))
}

// runHostCommand runs c for host and returns its stdout and stderr text.
//
// loginCheck is true when c is itself the host login check (gh auth status,
// glab auth status), so a failure of c already means the login is not
// available.
func runHostCommand(ctx context.Context, host GitHostConfig, c HostCmd, loginCheck bool) (stdout, stderr string, err error) {
	runner := currentHostCmdRunner()
	resolve := currentHostSecretResolver()

	if host.CredentialSource == hostCredentialVault {
		env, token, envErr := vaultTokenEnv(host, resolve)
		if envErr != nil {
			return "", "", &HostCredentialError{Host: host.Name, Secret: host.VaultSecret, Err: envErr}
		}
		return runWithToken(ctx, runner, c, env, token)
	}

	// host_login (or broker): the environment is left untouched.
	c.Env = nil
	so, se, runErr := runner.Run(ctx, c)
	if runErr == nil || !fallbackEligible(host, runErr) {
		return string(so), string(se), runErr
	}
	if !loginCheck && !hostLoginUnavailable(ctx, runner, host) {
		return string(so), string(se), runErr
	}
	env, token, envErr := vaultTokenEnv(host, resolve)
	if envErr != nil {
		return string(so), string(se), &HostCredentialError{Host: host.Name, Secret: host.VaultSecret, Fallback: true, Err: envErr}
	}
	return runWithToken(ctx, runner, c, env, token)
}

// runWithToken runs c once with env (which carries the token) and scrubs the
// token from everything it returns.
func runWithToken(ctx context.Context, runner HostCmdRunner, c HostCmd, env []string, token string) (string, string, error) {
	c.Env = env
	so, se, err := runner.Run(ctx, c)
	return scrubToken(string(so), token), scrubToken(string(se), token), scrubErr(err, token)
}

// fallbackEligible reports whether a failed host_login command may be retried
// with the vault token: the host names a fallback secret, the failure is not a
// missing binary, a timeout or a cancellation, and the source is host_login.
func fallbackEligible(host GitHostConfig, runErr error) bool {
	if host.VaultSecret == "" {
		return false
	}
	if host.CredentialSource != "" && host.CredentialSource != hostCredentialHostLogin {
		return false
	}
	if errors.Is(runErr, exec.ErrNotFound) || errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, context.Canceled) {
		return false
	}
	return true
}

// hostLoginUnavailable runs the host's own login check with the environment
// untouched. A github or gitlab host's login is unavailable when gh or glab
// auth status fails. The plain kinds have no login of their own, so a failed
// command there counts as unavailable.
func hostLoginUnavailable(ctx context.Context, runner HostCmdRunner, host GitHostConfig) bool {
	base := hostBaseName(host.BaseURL)
	var c HostCmd
	switch host.Kind {
	case hostKindGitHub:
		c = HostCmd{Name: "gh", Args: []string{"auth", "status"}, Timeout: hostCheckTimeout}
		if base != "" {
			c.Args = append(c.Args, "--hostname", base)
		}
	case hostKindGitLab:
		if base == "" {
			base = "gitlab.com"
		}
		c = HostCmd{Name: "glab", Args: []string{"auth", "status", "--hostname", base}, Timeout: hostCheckTimeout}
	default:
		return true
	}
	_, _, err := runner.Run(ctx, c)
	return err != nil
}
