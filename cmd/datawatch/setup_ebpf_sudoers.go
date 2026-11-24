package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/ebpfsetup"
)

// ebpfSetupEnv holds every host-touching seam of the setup ebpf sudoers and
// re-apply modes, so tests run them with fakes and never call sudo.
type ebpfSetupEnv struct {
	runner ebpfsetup.Runner
	user   func() (string, error)
	setcap func() (string, error)
	binary func(target string) (string, error)
	daemon func() ebpfsetup.DaemonView
	// ebpfEnabled reports whether the config asks for eBPF.
	ebpfEnabled func() bool
	tmpDir      string
	out         io.Writer
}

func realEBPFSetupEnv(out io.Writer) ebpfSetupEnv {
	return ebpfSetupEnv{
		runner: ebpfsetup.ExecRunner{},
		user: func() (string, error) {
			u, err := user.Current()
			if err != nil {
				return "", err
			}
			return u.Username, nil
		},
		setcap: ebpfsetup.FindSetcap,
		binary: ebpfBinaryPath,
		daemon: daemonEBPFView,
		ebpfEnabled: func() bool {
			cfg, err := loadConfig()
			return err == nil && (cfg.Stats.EBPFEnabled || cfg.Observer.EBPFEnabled == "true")
		},
		tmpDir: os.TempDir(),
		out:    out,
	}
}

// ebpfBinaryPath is the binary `setup ebpf` patches: this one, or with
// target "stats" the standalone observer daemon.
func ebpfBinaryPath(target string) (string, error) {
	if target == "stats" {
		p, err := exec.LookPath("datawatch-stats")
		if err != nil {
			return "", fmt.Errorf("--target stats requested but datawatch-stats not found in PATH")
		}
		return filepath.EvalSymlinks(p)
	}
	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine binary path: %w", err)
	}
	return filepath.EvalSymlinks(p)
}

// daemonEBPFView asks the running daemon (read only) what it holds.
func daemonEBPFView() ebpfsetup.DaemonView {
	body, err := daemonJSONBody("GET", "/api/observer/stats?v=2", nil)
	if err != nil {
		return ebpfsetup.DaemonView{}
	}
	var resp struct {
		Host struct {
			EBPF struct {
				Capability    bool   `json:"capability"`
				KprobesLoaded bool   `json:"kprobes_loaded"`
				Message       string `json:"message"`
			} `json:"ebpf"`
		} `json:"host"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return ebpfsetup.DaemonView{}
	}
	e := resp.Host.EBPF
	return ebpfsetup.DaemonView{Reachable: true, Capability: e.Capability, KprobesLoaded: e.KprobesLoaded, Message: e.Message}
}

func (e ebpfSetupEnv) plan(target string) (*ebpfsetup.Plan, error) {
	u, err := e.user()
	if err != nil {
		return nil, fmt.Errorf("cannot determine the user name: %w", err)
	}
	sc, err := e.setcap()
	if err != nil {
		return nil, err
	}
	bin, err := e.binary(target)
	if err != nil {
		return nil, err
	}
	return ebpfsetup.NewPlan(u, sc, bin, target)
}

// runEBPFHostMode runs one of the host modes of `datawatch setup ebpf`:
// sudoers (print), install-sudoers, remove-sudoers, status or reapply.
func runEBPFHostMode(ctx context.Context, e ebpfSetupEnv, mode, target string) error {
	p, err := e.plan(target)
	if err != nil {
		return err
	}
	switch mode {
	case "sudoers":
		_, _ = fmt.Fprint(e.out, p.Describe())
	case "install-sudoers":
		_, _ = fmt.Fprintf(e.out, "Installing %s (sudo asks for your password once)...\n", p.DropIn)
		if err := p.Install(ctx, e.runner, e.tmpDir); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(e.out, "Installed. Rule:\n  %s\n", p.Rule())
		_, _ = fmt.Fprintln(e.out, "From now on `datawatch update` and `make install` restore the eBPF capabilities after replacing the binary.")
		_, _ = fmt.Fprintln(e.out, "Keep the binary owned by you and not writable by anyone else: anyone who can write to it can run code with those capabilities.")
		_, _ = fmt.Fprintln(e.out, "Remove the rule with: datawatch setup ebpf --remove-sudoers")
	case "remove-sudoers":
		removed, err := p.Remove(ctx, e.runner)
		if err != nil {
			return err
		}
		if !removed {
			_, _ = fmt.Fprintf(e.out, "%s does not exist; nothing to remove.\n", p.DropIn)
			return nil
		}
		_, _ = fmt.Fprintf(e.out, "Removed %s. Releases will no longer restore the eBPF capabilities on their own.\n", p.DropIn)
	case "status":
		_, _ = fmt.Fprint(e.out, p.Format(p.ReadStatus(ctx, e.runner, e.daemon)))
	case "reapply":
		p.Reapply(ctx, e.runner, e.ebpfEnabled(), e.out)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
	return nil
}

// reapplyEBPFCapsAfterReplace is called right after a release replaced the
// binary at path. hadCaps says whether the old file carried the
// capabilities. It never prompts for a password. A variable so tests can
// observe it.
var reapplyEBPFCapsAfterReplace = func(path string, hadCaps bool) {
	e := realEBPFSetupEnv(os.Stdout)
	sc, err := e.setcap()
	if err != nil {
		return
	}
	u, err := e.user()
	if err != nil {
		return
	}
	p, err := ebpfsetup.NewPlan(u, sc, path, "")
	if err != nil {
		if hadCaps {
			_, _ = fmt.Fprintf(os.Stdout, "[update] This release removed the eBPF capabilities from %s. Run `datawatch setup ebpf` and restart the daemon.\n", path)
		}
		return
	}
	p.Reapply(context.Background(), e.runner, hadCaps, os.Stdout)
}

// ebpfCapsBeforeReplace reports whether the binary at path carries the eBPF
// capabilities right now (call it before replacing the file).
var ebpfCapsBeforeReplace = func(path string) bool {
	return ebpfsetup.FileHasCaps(context.Background(), ebpfsetup.ExecRunner{}, path)
}
