package mergeshadow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LintToolUnavailableCode is the finding code raised when the pinned
// golangci-lint could not be installed, so the gate reports the install
// failure rather than a version mismatch.
const LintToolUnavailableCode = "lint_tool_unavailable"

// Sources of a resolved golangci-lint binary, recorded in the gate Detail.
const (
	LintSourceToolsDir  = "tools folder"
	LintSourcePath      = "PATH"
	LintSourceInstalled = "installed"
)

// DefaultLintInstallTimeout bounds one install of the pinned golangci-lint.
const DefaultLintInstallTimeout = 10 * time.Minute

const golangciLintModule = "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"

// LintToolError is returned when the pinned golangci-lint cannot be provided.
type LintToolError struct {
	Pinned string
	Reason string
	Err    error
}

func (e *LintToolError) Error() string {
	return fmt.Sprintf("golangci-lint %s unavailable: %s", e.Pinned, e.Reason)
}

func (e *LintToolError) Unwrap() error { return e.Err }

// LintToolOptions makes ResolveLintTool testable. Zero fields use defaults.
type LintToolOptions struct {
	// ReadVersion returns the version output of the binary at bin.
	ReadVersion func(ctx context.Context, bin string) (string, error)
	// Install installs golangci-lint at version into toolsDir (GOBIN).
	Install func(ctx context.Context, toolsDir, version string) error
	// LookPath resolves a command name. nil means exec.LookPath.
	LookPath func(name string) (string, error)
	// InstallTimeout bounds Install. 0 means DefaultLintInstallTimeout.
	InstallTimeout time.Duration
}

// LintTool is a resolved golangci-lint binary.
type LintTool struct {
	Path    string // absolute binary path ("" when there is none)
	Version string // normalized version ("" when unknown)
	Source  string // one of the LintSource* values
}

// Dir returns the directory holding the binary.
func (t LintTool) Dir() string { return filepath.Dir(t.Path) }

var lintInstallMu sync.Mutex

func (o LintToolOptions) readVersion(ctx context.Context, bin string) (string, error) {
	if o.ReadVersion != nil {
		return o.ReadVersion(ctx, bin)
	}
	vctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(vctx, bin, "version").CombinedOutput() // #nosec G204 -- resolved binary path
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(firstLine(string(out))), nil
}

func (o LintToolOptions) lookPath(name string) (string, error) {
	if o.LookPath != nil {
		return o.LookPath(name)
	}
	return exec.LookPath(name)
}

func (o LintToolOptions) install(ctx context.Context, toolsDir, version string) error {
	if o.Install != nil {
		return o.Install(ctx, toolsDir, version)
	}
	goBin, err := o.lookPath("go")
	if err != nil {
		return fmt.Errorf("go not found on PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, goBin, "install", golangciLintModule+"@v"+version) // #nosec G204 -- fixed module, validated version
	cmd.Env = append(os.Environ(), "GOBIN="+toolsDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go install: %w: %s", err, tailLines(string(out), 5))
	}
	return nil
}

// matches reports whether the binary at bin reports version want.
func (o LintToolOptions) matches(ctx context.Context, bin, want string) (string, bool) {
	out, err := o.readVersion(ctx, bin)
	if err != nil {
		return "", false
	}
	v := NormalizeLintVersion(out)
	return v, v != "" && v == want
}

// ResolveLintTool returns a golangci-lint whose version equals pinned: the
// tools-folder binary, else the PATH binary when it already matches, else a
// freshly installed one. With no pin it returns the PATH binary unchanged
// (an error when there is none is a *LintToolError with no pin). An install
// failure is a *LintToolError.
func ResolveLintTool(ctx context.Context, dataDir, pinned string, opts LintToolOptions) (LintTool, error) {
	want := NormalizeLintVersion(pinned)
	if want == "" {
		p, err := opts.lookPath("golangci-lint")
		if err != nil {
			return LintTool{}, &LintToolError{Reason: "golangci-lint not found on PATH", Err: err}
		}
		v, _ := opts.readVersion(ctx, p)
		return LintTool{Path: p, Version: NormalizeLintVersion(v), Source: LintSourcePath}, nil
	}
	if dataDir == "" {
		return LintTool{}, &LintToolError{Pinned: want, Reason: "no data directory for the tools folder"}
	}
	toolsDir := filepath.Join(dataDir, "tools", "golangci-lint", want)
	bin := filepath.Join(toolsDir, "golangci-lint")

	if _, ok := opts.matches(ctx, bin, want); ok {
		return LintTool{Path: bin, Version: want, Source: LintSourceToolsDir}, nil
	}
	if p, err := opts.lookPath("golangci-lint"); err == nil {
		if _, ok := opts.matches(ctx, p, want); ok {
			return LintTool{Path: p, Version: want, Source: LintSourcePath}, nil
		}
	}

	timeout := opts.InstallTimeout
	if timeout <= 0 {
		timeout = DefaultLintInstallTimeout
	}
	ictx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	lintInstallMu.Lock()
	defer lintInstallMu.Unlock()
	if err := os.MkdirAll(toolsDir, 0o750); err != nil {
		return LintTool{}, &LintToolError{Pinned: want, Reason: err.Error(), Err: err}
	}
	lf, err := os.OpenFile(filepath.Join(toolsDir, ".install.lock"), os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- under the tools folder
	if err != nil {
		return LintTool{}, &LintToolError{Pinned: want, Reason: err.Error(), Err: err}
	}
	defer func() { _ = lf.Close() }()
	if err := lockFile(lf); err != nil {
		return LintTool{}, &LintToolError{Pinned: want, Reason: "lock: " + err.Error(), Err: err}
	}
	defer unlockFile(lf)

	// Another resolver may have installed while this one waited for the lock.
	if _, ok := opts.matches(ctx, bin, want); ok {
		return LintTool{Path: bin, Version: want, Source: LintSourceToolsDir}, nil
	}
	if err := opts.install(ictx, toolsDir, want); err != nil {
		reason := err.Error()
		if errors.Is(ictx.Err(), context.DeadlineExceeded) {
			reason = fmt.Sprintf("install timed out after %s: %s", timeout, reason)
		}
		return LintTool{}, &LintToolError{Pinned: want, Reason: reason, Err: err}
	}
	if _, ok := opts.matches(ctx, bin, want); !ok {
		return LintTool{}, &LintToolError{Pinned: want, Reason: "installed binary does not report version " + want}
	}
	return LintTool{Path: bin, Version: want, Source: LintSourceInstalled}, nil
}
