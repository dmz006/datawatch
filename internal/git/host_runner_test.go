package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// hostTestToken is the vault secret value used by these tests. Every test
// asserts it never reaches an error, a log line, an audit entry or a response.
const hostTestToken = "ghp_FAKEvaultTOKEN0123456789abcdefABCDEF"

// fakeHostRunner is a HostCmdRunner that records every call and answers with a
// scripted result, so no process is started.
type fakeHostRunner struct {
	mu     sync.Mutex
	calls  []HostCmd
	handle func(c HostCmd) (stdout, stderr string, err error)
}

func (f *fakeHostRunner) Run(_ context.Context, c HostCmd) ([]byte, []byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	h := f.handle
	f.mu.Unlock()
	if h == nil {
		return nil, nil, nil
	}
	so, se, err := h(c)
	return []byte(so), []byte(se), err
}

func (f *fakeHostRunner) recorded() []HostCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]HostCmd(nil), f.calls...)
}

// fakeVault is an in-memory secrets lookup that records the names asked for.
type fakeVault struct {
	mu      sync.Mutex
	secrets map[string]string
	asked   []string
}

func (v *fakeVault) resolve(name string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.asked = append(v.asked, name)
	val, ok := v.secrets[name]
	if !ok {
		return "", fmt.Errorf("secret not found: %q", name)
	}
	return val, nil
}

func (v *fakeVault) askedNames() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.asked...)
}

// useFakeHostEnv installs the fake runner and the fake vault resolver for the
// test, clears the token variables from the process environment, and captures
// the standard logger so a test can assert nothing was logged with a token.
func useFakeHostEnv(t *testing.T, r *fakeHostRunner, v *fakeVault) *bytes.Buffer {
	t.Helper()
	clearTokenVars(t)
	restore := setHostCmdRunner(r)
	if v != nil {
		SetHostSecretResolver(v.resolve)
	} else {
		SetHostSecretResolver(nil)
	}
	var logBuf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&logBuf)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		restore()
		SetHostSecretResolver(nil)
	})
	return &logBuf
}

// isAuthStatus reports whether c is a gh/glab auth status call.
func isAuthStatus(c HostCmd) bool {
	return len(c.Args) >= 2 && c.Args[0] == "auth" && c.Args[1] == "status"
}

// assertTokenNotOnCommandLine fails when the token is in the binary name, the
// arguments or the working directory of any recorded call.
func assertTokenNotOnCommandLine(t *testing.T, calls []HostCmd, token string) {
	t.Helper()
	for i, c := range calls {
		all := append([]string{c.Name, c.Dir}, c.Args...)
		for _, a := range all {
			if strings.Contains(a, token) {
				t.Errorf("call %d: token on the command line: %q", i, a)
			}
		}
	}
}

// assertNoToken fails when any text contains the token.
func assertNoToken(t *testing.T, token string, texts ...string) {
	t.Helper()
	for i, s := range texts {
		if strings.Contains(s, token) {
			t.Errorf("text %d contains the token: %q", i, s)
		}
	}
}

// loggedInRunner answers every gh/glab call as a logged-in host.
func loggedInRunner() *fakeHostRunner {
	return &fakeHostRunner{handle: func(c HostCmd) (string, string, error) {
		switch {
		case isAuthStatus(c) && c.Name == "gh":
			return "Logged in to github.com account octo (keyring)\n- Token scopes: 'repo', 'workflow'\n", "", nil
		case isAuthStatus(c) && c.Name == "glab":
			return "Logged in to gitlab.com as octo\nToken scopes: api\n", "", nil
		case c.Name == "gh" && len(c.Args) > 0 && c.Args[0] == "api":
			return `{"login":"octo"}`, "", nil
		}
		return "", "", nil
	}}
}

func TestVaultSourcedHostGetsTokenInChildEnvironmentOnly(t *testing.T) {
	cases := []struct {
		kind, envName string
	}{
		{"github", "GH_TOKEN"},
		{"gitlab", "GITLAB_TOKEN"},
		{"gitea", "DATAWATCH_GIT_TOKEN"},
		{"bitbucket", "DATAWATCH_GIT_TOKEN"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			r := loggedInRunner()
			v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
			logBuf := useFakeHostEnv(t, r, v)
			host := GitHostConfig{Name: "h", Kind: tc.kind, BaseURL: "https://git.example.invalid", CredentialSource: "vault", VaultSecret: "host-pat"}

			readiness := CheckHostReadiness(context.Background(), host)
			conn := TestHostConnection(context.Background(), host)

			calls := r.recorded()
			if len(calls) == 0 {
				t.Fatal("no command was run")
			}
			for i, c := range calls {
				got, ok := envValue(c.Env, tc.envName)
				if !ok || got != hostTestToken {
					t.Errorf("call %d (%s %v): %s = %q, %v; want the token", i, c.Name, c.Args, tc.envName, got, ok)
				}
			}
			if names := v.askedNames(); len(names) == 0 || names[0] != "host-pat" {
				t.Errorf("vault asked for %v, want the secret name host-pat", names)
			}
			assertTokenNotOnCommandLine(t, calls, hostTestToken)
			assertNoToken(t, hostTestToken, logBuf.String(), readiness.Problem, readiness.Account, conn.Message, conn.Account, conn.FixCommand)
			if os.Getenv(tc.envName) != "" {
				t.Errorf("%s leaked into the daemon's own environment", tc.envName)
			}
		})
	}
}

func TestHostLoginPathLeavesEnvironmentUntouched(t *testing.T) {
	t.Run("no vault secret name", func(t *testing.T) {
		r := loggedInRunner()
		v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)
		for _, host := range []GitHostConfig{
			{Name: "a", Kind: "github"},
			{Name: "b", Kind: "github", CredentialSource: "host_login"},
			{Name: "c", Kind: "gitlab", CredentialSource: "broker"},
		} {
			CheckHostReadiness(context.Background(), host)
			TestHostConnection(context.Background(), host)
		}
		for i, c := range r.recorded() {
			if c.Env != nil {
				t.Errorf("call %d (%s %v): environment replaced, want untouched (nil)", i, c.Name, c.Args)
			}
		}
		if names := v.askedNames(); len(names) != 0 {
			t.Errorf("vault read for a host-login host: %v", names)
		}
	})

	t.Run("fallback secret named but login works", func(t *testing.T) {
		r := loggedInRunner()
		v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)
		host := GitHostConfig{Name: "a", Kind: "github", VaultSecret: "host-pat"}

		res := TestHostConnection(context.Background(), host)
		if !res.Ok {
			t.Fatalf("connection not ok: %+v", res)
		}
		for i, c := range r.recorded() {
			if c.Env != nil {
				t.Errorf("call %d: environment replaced although the login works", i)
			}
		}
		if names := v.askedNames(); len(names) != 0 {
			t.Errorf("vault read although the host login works: %v", names)
		}
	})

	t.Run("daemon environment is never modified", func(t *testing.T) {
		r := loggedInRunner()
		v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)
		before := strings.Join(os.Environ(), "\x00")
		CheckHostReadiness(context.Background(), GitHostConfig{Name: "v", Kind: "github", CredentialSource: "vault", VaultSecret: "host-pat"})
		if after := strings.Join(os.Environ(), "\x00"); after != before {
			t.Error("the daemon's own environment changed")
		}
	})
}

func TestFallbackReadsVaultOnlyWhenHostLoginIsMissing(t *testing.T) {
	t.Run("readiness retries the login check with the token", func(t *testing.T) {
		r := &fakeHostRunner{handle: func(c HostCmd) (string, string, error) {
			if isAuthStatus(c) {
				if _, ok := envValue(c.Env, "GH_TOKEN"); ok {
					return "Logged in to github.com account octo (GH_TOKEN)\n- Token scopes: 'repo', 'workflow'\n", "", nil
				}
				return "", "You are not logged into any GitHub hosts.", errors.New("exit status 1")
			}
			return "", "", nil
		}}
		v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)

		got := CheckHostReadiness(context.Background(), GitHostConfig{Name: "a", Kind: "github", VaultSecret: "host-pat"})
		if !got.LoggedIn || got.Account != "octo" || got.Problem != "" {
			t.Fatalf("readiness = %+v, want logged in as octo", got)
		}
		calls := r.recorded()
		if len(calls) != 2 {
			t.Fatalf("calls = %d, want 2 (login check, then retry)", len(calls))
		}
		if calls[0].Env != nil {
			t.Error("first attempt must leave the environment untouched")
		}
		if tok, _ := envValue(calls[1].Env, "GH_TOKEN"); tok != hostTestToken {
			t.Error("retry did not carry the token")
		}
		assertTokenNotOnCommandLine(t, calls, hostTestToken)
	})

	t.Run("an operation retries once the login check fails", func(t *testing.T) {
		r := &fakeHostRunner{handle: func(c HostCmd) (string, string, error) {
			_, hasTok := envValue(c.Env, "GITLAB_TOKEN")
			switch {
			case isAuthStatus(c):
				return "", "not logged in", errors.New("exit status 1")
			case !hasTok:
				return "", "401 Unauthorized", errors.New("exit status 1")
			}
			return `[{"id":7,"status":"success","sha":"abc","web_url":"https://gitlab.example.invalid/p/1"}]`, "", nil
		}}
		v := &fakeVault{secrets: map[string]string{"gl-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)

		runs, err := LatestRuns(context.Background(), GitHostConfig{Name: "gl", Kind: "gitlab", VaultSecret: "gl-pat"}, "g/p", "main")
		if err != nil || len(runs) != 1 || runs[0].ID != "7" {
			t.Fatalf("runs = %+v, err = %v", runs, err)
		}
		if names := v.askedNames(); len(names) != 1 || names[0] != "gl-pat" {
			t.Errorf("vault asked for %v, want exactly gl-pat", names)
		}
	})

	t.Run("a failure with a working login does not read the vault", func(t *testing.T) {
		r := &fakeHostRunner{handle: func(c HostCmd) (string, string, error) {
			if isAuthStatus(c) {
				return "Logged in to github.com account octo\n", "", nil
			}
			return "", "no such repo", errors.New("exit status 1")
		}}
		v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)

		_, err := LatestRuns(context.Background(), GitHostConfig{Name: "a", Kind: "github", VaultSecret: "host-pat"}, "o/r", "main")
		if err == nil {
			t.Fatal("want the command error")
		}
		if names := v.askedNames(); len(names) != 0 {
			t.Errorf("vault read although the login works: %v", names)
		}
		for i, c := range r.recorded() {
			if c.Env != nil {
				t.Errorf("call %d: environment replaced", i)
			}
		}
	})

	t.Run("a missing binary or a timeout never reads the vault", func(t *testing.T) {
		for _, failure := range []error{exec.ErrNotFound, context.DeadlineExceeded} {
			r := &fakeHostRunner{handle: func(c HostCmd) (string, string, error) { return "", "", failure }}
			v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
			useFakeHostEnv(t, r, v)

			got := CheckHostReadiness(context.Background(), GitHostConfig{Name: "a", Kind: "github", VaultSecret: "host-pat"})
			if got.LoggedIn {
				t.Errorf("%v: logged in", failure)
			}
			if names := v.askedNames(); len(names) != 0 {
				t.Errorf("%v: vault read: %v", failure, names)
			}
		}
	})

	t.Run("broker source never falls back", func(t *testing.T) {
		r := &fakeHostRunner{handle: func(c HostCmd) (string, string, error) { return "", "", errors.New("exit status 1") }}
		v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)

		CheckHostReadiness(context.Background(), GitHostConfig{Name: "a", Kind: "github", CredentialSource: "broker", VaultSecret: "host-pat"})
		if names := v.askedNames(); len(names) != 0 {
			t.Errorf("vault read for a broker host: %v", names)
		}
	})
}

func TestFailingCommandNeverLeaksTheToken(t *testing.T) {
	// The command echoes the token everywhere a CLI might: stdout, stderr and
	// the error text.
	echo := func(c HostCmd) (string, string, error) {
		return "out " + hostTestToken, "bad credentials " + hostTestToken, errors.New("exit status 1: " + hostTestToken)
	}
	vaultHost := func(kind string) GitHostConfig {
		return GitHostConfig{Name: "h", Kind: kind, BaseURL: "https://git.example.invalid", CredentialSource: "vault", VaultSecret: "host-pat"}
	}

	t.Run("readiness, connection test and CI watch", func(t *testing.T) {
		r := &fakeHostRunner{handle: echo}
		v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
		logBuf := useFakeHostEnv(t, r, v)

		var texts []string
		for _, kind := range []string{"github", "gitlab", "gitea"} {
			rd := CheckHostReadiness(context.Background(), vaultHost(kind))
			texts = append(texts, rd.Problem, rd.Account, rd.FixCommand)
			cr := TestHostConnection(context.Background(), vaultHost(kind))
			texts = append(texts, cr.Message, cr.Account, cr.FixCommand)
			if kind != "gitea" {
				_, err := LatestRuns(context.Background(), vaultHost(kind), "o/r", "main")
				if err == nil {
					t.Fatalf("%s: want an error", kind)
				}
				texts = append(texts, err.Error())
			}
		}
		texts = append(texts, logBuf.String())
		assertNoToken(t, hostTestToken, texts...)
		if len(r.recorded()) == 0 {
			t.Fatal("no command ran")
		}
	})

	t.Run("release, issue and merge request providers", func(t *testing.T) {
		r := &fakeHostRunner{handle: echo}
		v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
		logBuf := useFakeHostEnv(t, r, v)
		l, path := hostAuditFixture(t)
		ctx := context.Background()

		var texts []string
		for _, kind := range []string{"github", "gitlab"} {
			h := vaultHost(kind)
			_, err := PublishRelease(ctx, h, ReleaseOptions{Repo: "o/r", Tag: "v1", Title: "t", Notes: "n"})
			texts = appendErr(texts, err)
			_, err = FileIssue(ctx, h, IssueOptions{Repo: "o/r", Title: "t", Body: "b"})
			texts = appendErr(texts, err)
			_, err = OpenChangeRequest(ctx, h, PROptions{Repo: "o/r", HeadBranch: "x", Title: "t", Body: "b"})
			texts = appendErr(texts, err)
		}
		texts = append(texts, logBuf.String(), auditText(t, l, path))
		assertNoToken(t, hostTestToken, texts...)
		if len(texts) < 6 {
			t.Fatalf("want an error from each operation, got %d texts", len(texts))
		}
	})

	t.Run("clone, push and transport setup", func(t *testing.T) {
		r := &fakeHostRunner{handle: echo}
		v := &fakeVault{secrets: map[string]string{"host-pat": hostTestToken}}
		logBuf := useFakeHostEnv(t, r, v)
		ctx := context.Background()

		var texts []string
		texts = appendErr(texts, CloneGitLabRepo(ctx, vaultHost("gitlab"), "g/p", t.TempDir()))
		texts = appendErr(texts, ConfigureGitLabTransport(ctx, t.TempDir(), vaultHost("gitlab")))
		texts = appendErr(texts, NewGenericGitFor(vaultHost("gitea")).Push(ctx, t.TempDir(), "origin", "main"))
		texts = append(texts, logBuf.String())
		assertNoToken(t, hostTestToken, texts...)
		for i, txt := range texts[:3] {
			if txt == "" {
				t.Errorf("operation %d returned no error", i)
			}
		}
	})
}

func appendErr(texts []string, err error) []string {
	if err == nil {
		return append(texts, "")
	}
	return append(texts, err.Error())
}

// auditText returns the full text of the audit log file.
func auditText(t *testing.T, l interface{ Close() error }, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// NewGenericGitFor builds the plain git provider for a configured host the way
// the facade does.
func NewGenericGitFor(host GitHostConfig) *GenericGit {
	g := NewGenericGit(host.Kind, host.BaseURL)
	g.Host = host
	return g
}

func TestMissingVaultSecretGivesClearErrorNamingTheSecret(t *testing.T) {
	const name = "gh-missing-secret"
	host := GitHostConfig{Name: "prod", Kind: "github", CredentialSource: "vault", VaultSecret: name}

	t.Run("vault source", func(t *testing.T) {
		r := loggedInRunner()
		v := &fakeVault{secrets: map[string]string{"other-secret": hostTestToken}}
		logBuf := useFakeHostEnv(t, r, v)

		rd := CheckHostReadiness(context.Background(), host)
		conn := TestHostConnection(context.Background(), host)
		_, relErr := PublishRelease(context.Background(), host, ReleaseOptions{Repo: "o/r", Tag: "v1"})
		_, ciErr := LatestRuns(context.Background(), host, "o/r", "main")
		var texts []string
		texts = append(texts, rd.Problem, conn.Message)
		texts = appendErr(texts, relErr)
		texts = appendErr(texts, ciErr)

		for i, txt := range texts {
			if !strings.Contains(txt, name) {
				t.Errorf("text %d does not name the secret %q: %q", i, name, txt)
			}
		}
		assertNoToken(t, hostTestToken, append(texts, logBuf.String())...)
		if rd.LoggedIn || conn.Ok {
			t.Error("a host with a missing secret must not read as connected")
		}
		if n := len(r.recorded()); n != 0 {
			t.Errorf("%d commands ran although the secret is missing", n)
		}
		var ce *HostCredentialError
		if !errors.As(relErr, &ce) || ce.Secret != name || ce.Host != "prod" {
			t.Errorf("release error = %v, want a HostCredentialError for %q", relErr, name)
		}
	})

	t.Run("fallback secret missing while the login is missing", func(t *testing.T) {
		r := &fakeHostRunner{handle: func(c HostCmd) (string, string, error) {
			return "", "not logged in", errors.New("exit status 1")
		}}
		v := &fakeVault{secrets: map[string]string{}}
		useFakeHostEnv(t, r, v)

		rd := CheckHostReadiness(context.Background(), GitHostConfig{Name: "prod", Kind: "github", VaultSecret: name})
		if !strings.Contains(rd.Problem, name) || !strings.Contains(rd.Problem, "login is not available") {
			t.Errorf("problem = %q, want it to name the fallback secret and the missing login", rd.Problem)
		}
	})

	t.Run("no resolver installed", func(t *testing.T) {
		r := loggedInRunner()
		useFakeHostEnv(t, r, nil)
		_, err := PublishRelease(context.Background(), host, ReleaseOptions{Repo: "o/r", Tag: "v1"})
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("error = %v, want one naming %q", err, name)
		}
	})

	t.Run("empty secret value", func(t *testing.T) {
		r := loggedInRunner()
		v := &fakeVault{secrets: map[string]string{name: ""}}
		useFakeHostEnv(t, r, v)
		_, err := LatestRuns(context.Background(), host, "o/r", "main")
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "empty") {
			t.Errorf("error = %v, want one naming %q as empty", err, name)
		}
	})
}

func TestProvidersPassVaultTokenToChildEnvironment(t *testing.T) {
	ctx := context.Background()

	t.Run("github release", func(t *testing.T) {
		r := &fakeHostRunner{handle: func(c HostCmd) (string, string, error) {
			return "https://github.example.invalid/o/r/releases/tag/v1\n", "", nil
		}}
		v := &fakeVault{secrets: map[string]string{"gh-pat": hostTestToken}}
		logBuf := useFakeHostEnv(t, r, v)
		l, path := hostAuditFixture(t)

		host := GitHostConfig{Name: "prod", Kind: "github", CredentialSource: "vault", VaultSecret: "gh-pat"}
		res, err := PublishRelease(ctx, host, ReleaseOptions{Repo: "o/r", Tag: "v1", Title: "t", Notes: "n"})
		if err != nil || res.URL == "" {
			t.Fatalf("PublishRelease = %+v, %v", res, err)
		}
		calls := r.recorded()
		if len(calls) != 1 || calls[0].Name != "gh" {
			t.Fatalf("calls = %+v", calls)
		}
		if tok, _ := envValue(calls[0].Env, "GH_TOKEN"); tok != hostTestToken {
			t.Error("gh did not get GH_TOKEN")
		}
		assertTokenNotOnCommandLine(t, calls, hostTestToken)
		assertNoToken(t, hostTestToken, logBuf.String(), auditText(t, l, path), res.URL)
	})

	t.Run("gitlab issue", func(t *testing.T) {
		r := &fakeHostRunner{handle: func(c HostCmd) (string, string, error) {
			return "https://gitlab.example.invalid/g/p/-/issues/3\n", "", nil
		}}
		v := &fakeVault{secrets: map[string]string{"gl-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)

		host := GitHostConfig{Name: "gl", Kind: "gitlab", CredentialSource: "vault", VaultSecret: "gl-pat"}
		if _, err := FileIssue(ctx, host, IssueOptions{Repo: "g/p", Title: "t", Body: "b"}); err != nil {
			t.Fatal(err)
		}
		calls := r.recorded()
		if len(calls) != 1 || calls[0].Name != "glab" {
			t.Fatalf("calls = %+v", calls)
		}
		if tok, _ := envValue(calls[0].Env, "GITLAB_TOKEN"); tok != hostTestToken {
			t.Error("glab did not get GITLAB_TOKEN")
		}
	})

	t.Run("plain git push and clone", func(t *testing.T) {
		r := &fakeHostRunner{}
		v := &fakeVault{secrets: map[string]string{"gt-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)

		host := GitHostConfig{Name: "gt", Kind: "gitea", BaseURL: "https://git.example.invalid", CredentialSource: "vault", VaultSecret: "gt-pat"}
		if err := NewGenericGitFor(host).Push(ctx, t.TempDir(), "origin", "main"); err != nil {
			t.Fatal(err)
		}
		if err := CloneGitLabRepo(ctx, host, "g/p", t.TempDir()); err != nil {
			t.Fatal(err)
		}
		calls := r.recorded()
		if len(calls) != 2 {
			t.Fatalf("calls = %d, want 2", len(calls))
		}
		for i, c := range calls {
			if tok, _ := envValue(c.Env, "DATAWATCH_GIT_TOKEN"); tok != hostTestToken {
				t.Errorf("call %d: DATAWATCH_GIT_TOKEN missing", i)
			}
		}
		assertTokenNotOnCommandLine(t, calls, hostTestToken)
	})

	t.Run("host login provider leaves environment untouched", func(t *testing.T) {
		r := &fakeHostRunner{handle: func(c HostCmd) (string, string, error) {
			return "https://github.example.invalid/o/r/releases/tag/v1\n", "", nil
		}}
		v := &fakeVault{secrets: map[string]string{"gh-pat": hostTestToken}}
		useFakeHostEnv(t, r, v)

		host := GitHostConfig{Name: "prod", Kind: "github"}
		if _, err := PublishRelease(ctx, host, ReleaseOptions{Repo: "o/r", Tag: "v1"}); err != nil {
			t.Fatal(err)
		}
		if err := NewGenericGitFor(GitHostConfig{Name: "gt", Kind: "gitea", BaseURL: "https://git.example.invalid"}).Push(ctx, t.TempDir(), "origin", "main"); err != nil {
			t.Fatal(err)
		}
		for i, c := range r.recorded() {
			if c.Env != nil {
				t.Errorf("call %d: environment replaced for a host-login host", i)
			}
		}
		if names := v.askedNames(); len(names) != 0 {
			t.Errorf("vault read for a host-login host: %v", names)
		}
	})
}

func TestScrubTokenAndError(t *testing.T) {
	if got := scrubToken("a "+hostTestToken+" b "+hostTestToken, hostTestToken); strings.Contains(got, hostTestToken) || strings.Count(got, "[redacted]") != 2 {
		t.Errorf("scrubToken = %q", got)
	}
	if got := scrubToken("keep", ""); got != "keep" {
		t.Errorf("scrubToken with empty token = %q", got)
	}
	orig := fmt.Errorf("wrapped: %w", context.Canceled)
	if got := scrubErr(orig, hostTestToken); got != orig {
		t.Error("an error without the token must be returned unchanged")
	}
	if got := scrubErr(errors.New("boom "+hostTestToken), hostTestToken); strings.Contains(got.Error(), hostTestToken) {
		t.Errorf("scrubErr = %v", got)
	}
	if scrubErr(nil, hostTestToken) != nil {
		t.Error("nil error must stay nil")
	}
}

func TestExecHostRunnerRunsRealProcess(t *testing.T) {
	var r execHostRunner
	so, se, err := r.Run(context.Background(), HostCmd{Name: "sh", Args: []string{"-c", "echo out; echo err >&2; echo $DW_RUNNER_VAR"}, Env: []string{"DW_RUNNER_VAR=from-env", "PATH=" + os.Getenv("PATH")}})
	if err != nil {
		t.Fatal(err)
	}
	if string(so) != "out\nfrom-env\n" || string(se) != "err\n" {
		t.Errorf("stdout = %q, stderr = %q", so, se)
	}

	_, _, err = r.Run(context.Background(), HostCmd{Name: "sh", Args: []string{"-c", "exec sleep 5"}, Timeout: 50 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout error = %v, want deadline exceeded", err)
	}

	_, _, err = r.Run(context.Background(), HostCmd{Name: "dw-no-such-binary-xyz"})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("missing binary error = %v, want exec.ErrNotFound", err)
	}

	dir := t.TempDir()
	so, _, err = r.Run(context.Background(), HostCmd{Name: "pwd", Dir: dir})
	if err != nil || !strings.Contains(string(so), dir) {
		t.Errorf("pwd in dir = %q, %v", so, err)
	}
}
