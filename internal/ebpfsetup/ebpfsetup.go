package ebpfsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// CapList is the capability argument `datawatch setup ebpf` applies:
	// cap_bpf to load programs, cap_perfmon to attach kprobes and
	// cap_sys_resource to lift the memlock limit. "=ep" replaces any
	// earlier set on the file.
	CapList = "cap_bpf,cap_perfmon,cap_sys_resource=ep"

	// DropInDir is the directory the sudoers drop-in is installed into.
	DropInDir = "/etc/sudoers.d"

	// Marker tags a drop-in this package wrote. Removal refuses a file
	// without it, so an operator's own rule is never deleted.
	Marker = "# datawatch-managed: ebpf-setcap (written by `datawatch setup ebpf --install-sudoers`; remove with `datawatch setup ebpf --remove-sudoers`)"

	markerPrefix = "# datawatch-managed: ebpf-setcap"
)

var (
	userRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	// pathRe is deliberately narrow: no space, no glob character and no
	// sudoers metacharacter, so a path can never widen the rule.
	pathRe = regexp.MustCompile(`^/[A-Za-z0-9_./+@-]*$`)
)

// Result is what a command run produced.
type Result struct {
	Stdout string
	Stderr string
}

// Runner runs one host command. The real one is [ExecRunner]; tests pass a
// fake so sudo and visudo are never called.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

// ExecRunner runs commands with os/exec. sudo reads its password prompt from
// the terminal, so stdin stays attached.
type ExecRunner struct{}

// Run implements [Runner].
func (ExecRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	return Result{Stdout: so.String(), Stderr: se.String()}, err
}

// Plan is the rule for one user, one setcap and one binary.
type Plan struct {
	User       string
	SetcapPath string
	BinaryPath string
	// DropIn is the full path of the sudoers drop-in.
	DropIn string
}

// FindSetcap returns the absolute path of setcap: PATH first, then the
// directories a non-root PATH often lacks.
func FindSetcap() (string, error) {
	if p, err := exec.LookPath("setcap"); err == nil {
		return filepath.Abs(p)
	}
	for _, p := range []string{"/usr/sbin/setcap", "/sbin/setcap", "/usr/bin/setcap"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("setcap not found (install libcap2-bin / libcap)")
}

// NewPlan validates the three values and names the drop-in. target is ""
// for the daemon and "stats" for the standalone observer, which is a
// different binary and so gets its own file.
func NewPlan(user, setcapPath, binaryPath, target string) (*Plan, error) {
	if !userRe.MatchString(user) {
		return nil, fmt.Errorf("user name %q cannot be written into a sudoers rule safely", user)
	}
	for label, p := range map[string]string{"setcap path": setcapPath, "binary path": binaryPath} {
		if !pathRe.MatchString(p) || filepath.Clean(p) != p {
			return nil, fmt.Errorf("%s %q has characters sudoers treats specially (a rule may not contain spaces, wildcards or relative parts); move the binary to a plain path", label, p)
		}
	}
	name := "datawatch-ebpf-" + user
	if target != "" {
		if !regexp.MustCompile(`^[a-z0-9]+$`).MatchString(target) {
			return nil, fmt.Errorf("target %q is not valid", target)
		}
		name += "-" + target
	}
	return &Plan{User: user, SetcapPath: setcapPath, BinaryPath: binaryPath, DropIn: filepath.Join(DropInDir, name)}, nil
}

// SetcapArgv is the command that grants the capabilities, without sudo.
func (p *Plan) SetcapArgv() []string {
	return []string{p.SetcapPath, CapList, p.BinaryPath}
}

// SudoArgv is SetcapArgv behind sudo, as the rule allows it. flags go
// between sudo and the command (for example "-n").
func (p *Plan) SudoArgv(flags ...string) []string {
	return append(append([]string{"sudo"}, flags...), p.SetcapArgv()...)
}

// sudoersEscape escapes the characters sudoers reads as separators inside a
// command's argument list.
func sudoersEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `,`, `\,`, `:`, `\:`, `=`, `\=`)
	return r.Replace(s)
}

// Rule is the one sudoers line: this user may run exactly this command as
// root with no password. No wildcard: sudoers matches the argument list
// exactly.
func (p *Plan) Rule() string {
	argv := p.SetcapArgv()
	return fmt.Sprintf("%s ALL=(root) NOPASSWD: %s %s %s", p.User, argv[0], sudoersEscape(argv[1]), sudoersEscape(argv[2]))
}

// FileContents is what the drop-in holds: the marker comment and the rule.
func (p *Plan) FileContents() string {
	return Marker + "\n" + p.Rule() + "\n"
}

// Describe is the print mode: the file, the rule and how to install it.
func (p *Plan) Describe() string {
	return fmt.Sprintf(`Sudoers rule for eBPF capabilities

File:  %s   (root-owned, mode 0440)
User:  %s
Allows: %s
        as root, without a password, with exactly this argument list and nothing else.

--- begin file ---
%s--- end file ---

To install it yourself:
  sudo visudo -f %s     (paste the two lines above, save, quit)
To let datawatch do it (one password prompt, validated with visudo first):
  datawatch setup ebpf --install-sudoers
To remove it later:
  datawatch setup ebpf --remove-sudoers

Security note: this rule lets %s set those three capabilities on %s
without a password. Anyone who can write to that file can then run code with
them, so keep the binary owned by you and not writable by anyone else.
`, p.DropIn, p.User, strings.Join(p.SudoArgv(), " "), p.FileContents(), p.DropIn, p.User, p.BinaryPath)
}

func run(ctx context.Context, r Runner, d time.Duration, name string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return r.Run(ctx, name, args...)
}

func failure(what string, res Result, err error) error {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	if msg != "" {
		return fmt.Errorf("%s: %v: %s", what, err, msg)
	}
	return fmt.Errorf("%s: %w", what, err)
}

// readDropIn reads the drop-in through sudo (the directory is usually not
// readable by the user). exists is false when the file is absent.
func (p *Plan) readDropIn(ctx context.Context, r Runner) (content string, exists bool, err error) {
	if res, terr := run(ctx, r, 2*time.Minute, "sudo", "test", "-e", p.DropIn); terr != nil {
		var ee *exec.ExitError
		if errors.As(terr, &ee) && ee.ExitCode() == 1 {
			return "", false, nil
		}
		return "", false, failure("checking "+p.DropIn, res, terr)
	}
	res, cerr := run(ctx, r, 2*time.Minute, "sudo", "cat", "--", p.DropIn)
	if cerr != nil {
		return "", true, failure("reading "+p.DropIn, res, cerr)
	}
	return res.Stdout, true, nil
}

func hasMarker(content string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), markerPrefix) {
			return true
		}
	}
	return false
}

// Install writes the rule to a temporary file, validates it with visudo -cf
// and only then installs it root-owned with mode 0440 through sudo. An
// invalid rule is never installed, and a drop-in of the same name that does
// not carry the marker is never overwritten.
func (p *Plan) Install(ctx context.Context, r Runner, tmpDir string) error {
	f, err := os.CreateTemp(tmpDir, "datawatch-sudoers-*")
	if err != nil {
		return fmt.Errorf("temporary file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp) //nolint:errcheck
	if _, err := f.WriteString(p.FileContents()); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if res, verr := run(ctx, r, 30*time.Second, "visudo", "-cf", tmp); verr != nil {
		return failure("the rule did not pass visudo -cf, nothing was installed", res, verr)
	}
	if content, exists, rerr := p.readDropIn(ctx, r); rerr != nil {
		return rerr
	} else if exists && !hasMarker(content) {
		return fmt.Errorf("%s exists and was not written by datawatch (no marker comment); not overwriting it", p.DropIn)
	}
	if res, ierr := run(ctx, r, 2*time.Minute, "sudo", "install", "-o", "root", "-g", "root", "-m", "0440", tmp, p.DropIn); ierr != nil {
		return failure("installing "+p.DropIn, res, ierr)
	}
	return nil
}

// Remove deletes the drop-in, only when it carries the marker comment.
// removed is false when there was nothing to remove.
func (p *Plan) Remove(ctx context.Context, r Runner) (removed bool, err error) {
	content, exists, rerr := p.readDropIn(ctx, r)
	if rerr != nil {
		return false, rerr
	}
	if !exists {
		return false, nil
	}
	if !hasMarker(content) {
		return false, fmt.Errorf("%s does not carry the datawatch marker comment; refusing to remove a file datawatch did not write", p.DropIn)
	}
	if res, derr := run(ctx, r, 2*time.Minute, "sudo", "rm", "-f", "--", p.DropIn); derr != nil {
		return false, failure("removing "+p.DropIn, res, derr)
	}
	return true, nil
}

// FileHasCaps reports whether getcap shows all three capabilities on the
// binary.
func FileHasCaps(ctx context.Context, r Runner, binaryPath string) bool {
	res, err := run(ctx, r, 10*time.Second, "getcap", binaryPath)
	if err != nil {
		return false
	}
	out := strings.ToLower(res.Stdout)
	for _, c := range []string{"cap_bpf", "cap_perfmon", "cap_sys_resource"} {
		if !strings.Contains(out, c) {
			return false
		}
	}
	return true
}

// RulePresent runs `sudo -n -l <exact command>`: exit 0 means sudo would run
// that command now without a password.
func (p *Plan) RulePresent(ctx context.Context, r Runner) bool {
	argv := p.SudoArgv("-n", "-l")
	_, err := run(ctx, r, 15*time.Second, argv[0], argv[1:]...)
	return err == nil
}

// DaemonView is what the running daemon reports about itself.
type DaemonView struct {
	Reachable     bool
	Capability    bool
	KprobesLoaded bool
	Message       string
}

// Status is the three-part read.
type Status struct {
	FileHasCaps bool
	Daemon      DaemonView
	RulePresent bool
}

// ReadStatus gathers the status. daemon is called once; pass nil when no
// daemon is to be asked.
func (p *Plan) ReadStatus(ctx context.Context, r Runner, daemon func() DaemonView) Status {
	st := Status{FileHasCaps: FileHasCaps(ctx, r, p.BinaryPath), RulePresent: p.RulePresent(ctx, r)}
	if daemon != nil {
		st.Daemon = daemon()
	}
	return st
}

// Format renders the status in plain words with the next step.
func (p *Plan) Format(st Status) string {
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "eBPF capabilities\n")
	fmt.Fprintf(&b, "  binary:                         %s\n", p.BinaryPath)
	fmt.Fprintf(&b, "  file has the capabilities:      %s\n", yn(st.FileHasCaps))
	switch {
	case !st.Daemon.Reachable:
		fmt.Fprintf(&b, "  running daemon holds them:      unknown (daemon not reachable)\n")
	default:
		fmt.Fprintf(&b, "  running daemon holds them:      %s\n", yn(st.Daemon.Capability))
		fmt.Fprintf(&b, "  kernel probes loaded:           %s\n", yn(st.Daemon.KprobesLoaded))
		if st.Daemon.Message != "" {
			fmt.Fprintf(&b, "  daemon message:                 %s\n", st.Daemon.Message)
		}
	}
	fmt.Fprintf(&b, "  passwordless rule present:      %s   (sudo -n -l %s)\n", yn(st.RulePresent), strings.Join(p.SetcapArgv(), " "))
	switch {
	case !st.FileHasCaps:
		fmt.Fprintf(&b, "Next: run `datawatch setup ebpf` (asks for your sudo password), then restart the daemon.\n")
	case st.Daemon.Reachable && !st.Daemon.Capability:
		fmt.Fprintf(&b, "Next: the file has the capabilities but the running daemon started before they were set. Restart the daemon.\n")
	case !st.RulePresent:
		fmt.Fprintf(&b, "Note: without the rule, every release removes the capabilities again. `datawatch setup ebpf --sudoers` prints it; `--install-sudoers` installs it.\n")
	}
	return b.String()
}

// ManualCommand is the one command to run when nothing could be done
// without a password.
func (p *Plan) ManualCommand() string { return strings.Join(p.SudoArgv(), " ") }

// Reapply restores the capabilities after the binary was replaced. If the
// passwordless rule exists it runs `sudo -n` setcap itself. If it does not,
// or sudo refuses, and wanted is true (the file had the capabilities before
// the replace, or eBPF is enabled), it prints the one command to run.
// Nothing here ever prompts for a password.
func (p *Plan) Reapply(ctx context.Context, r Runner, wanted bool, out interface{ Write([]byte) (int, error) }) {
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	if p.RulePresent(ctx, r) {
		argv := p.SudoArgv("-n")
		if res, err := run(ctx, r, 30*time.Second, argv[0], argv[1:]...); err == nil {
			say("[update] eBPF capabilities re-applied to %s. Restart the daemon to load the kernel probes.\n", p.BinaryPath)
			return
		} else {
			say("[update] eBPF capabilities could not be re-applied without a password: %v\n", failure("sudo -n", res, err))
		}
	} else if !wanted {
		return
	} else {
		say("[update] This release replaced %s and removed its eBPF capabilities.\n", p.BinaryPath)
	}
	say("[update] Restore them with:  %s\n", p.ManualCommand())
	say("[update] To make this automatic: datawatch setup ebpf --install-sudoers   (then restart the daemon)\n")
}
