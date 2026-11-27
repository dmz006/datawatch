//go:build linux

package proclimit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Scope runs each command in a systemd scope of its own: one control group
// with a hard memory limit and no swap. The processes the command starts
// belong to it, whatever process group or session they move to, unless
// something moves them or runs the work elsewhere (see outside.go).
type Scope struct {
	// MemoryBytes is the limit of the whole tree.
	MemoryBytes int64
}

// Host is the limit a shadow runs under on this system.
func Host() Starter { return Scope{MemoryBytes: ShadowTreeBytes} }

// UnitPrefix starts the name of every scope made here.
const UnitPrefix = "datawatch-shadow-"

// scopeMaxRun ends a scope whose command was given no time limit.
const scopeMaxRun = 2 * time.Hour

// scopeSlack is added to a command's own time limit for the time systemd
// ends the scope, so that datawatch's own stop comes first.
const scopeSlack = 30 * time.Second

// scopeScript checks that the limit is in force before it starts the command,
// so that a command never runs without it. After the command it reports the
// processes still running and what the kernel stopped for memory, and stops
// what is left. Only shell builtins run beside the command.
var scopeScript = `status=$1 want=$2 refuse=$3
shift 3
cg=
while IFS= read -r line; do
	case $line in 0::*) cg=/sys/fs/cgroup${line#0::} ;; esac
done </proc/self/cgroup
max= swap=
[ -n "$cg" ] && read -r max <"$cg/memory.max" && read -r swap <"$cg/memory.swap.max"
if [ "$max" != "$want" ] || [ "$swap" != 0 ]; then
	echo "datawatch: the memory limit is not in force, so the command was not started" >&2
	exit 125
fi
printf 'cgroup %s\n' "$cg" >"$status"
# systemd-run needed the manager's address; the command is not given it.
unset ` + strings.Join(outsideVars, " ") + `
# With no address, a container engine's tools use the system's own.
DOCKER_HOST=` + noEngine + ` CONTAINER_HOST=` + noEngine + `
export DOCKER_HOST CONTAINER_HOST
[ -n "$refuse" ] && PATH=$refuse${PATH:+:$PATH} && export PATH
"$@"
rc=$?
others() {
	n=0 pids=
	while read -r p; do
		[ "$p" = "$$" ] || { n=$((n + 1)); pids="$pids $p"; }
	done <"$cg/cgroup.procs"
}
others
if [ "$n" -gt 0 ]; then
	/bin/sleep 1
	others
fi
stopped=0
while read -r k v; do
	[ "$k" = oom_kill ] && stopped=$v
done <"$cg/memory.events"
printf 'left %s\noom %s\ndone\n' "$n" "$stopped" >>"$status"
[ "$n" -gt 0 ] && kill -9 $pids 2>/dev/null
exit "$rc"`

// CeilingBytes is the memory one command may take.
func (s Scope) CeilingBytes() int64 { return s.MemoryBytes }

// Describe says what the limit covers.
func (s Scope) Describe() string {
	return fmt.Sprintf("%s of memory and no swap for the processes of each command that stay in its systemd scope; "+
		"%s are refused when found by name on the command's search path, and container engines are not given their usual address; "+
		"this is not a sandbox", size(s.MemoryBytes), strings.Join(refusedTools, ", "))
}

func size(b int64) string {
	if b >= 1<<30 && b%(1<<30) == 0 {
		return strconv.FormatInt(b>>30, 10) + " GiB"
	}
	return strconv.FormatInt(b>>20, 10) + " MiB"
}

// Available starts a trivial command under the limit.
func (s Scope) Available(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/true")
	finish, err := s.Prepare(cmd, 20*time.Second)
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	r := finish()
	switch {
	case r.Failed != "":
		return fmt.Errorf("%s: %s", r.Failed, strings.TrimSpace(string(out)))
	case err != nil:
		return fmt.Errorf("a command could not be run under the memory limit: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Prepare makes cmd run as the command of a new scope.
func (s Scope) Prepare(cmd *exec.Cmd, maxRun time.Duration) (func() Result, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	// Code that makes limits of its own, such as datawatch's own tests run
	// by a check, may itself be under one. A scope it made would not be part
	// of that one. The control group says so whatever the environment holds.
	own, err := os.ReadFile(ownCgroup)
	if err != nil {
		return nil, fmt.Errorf("it cannot be told whether this process already runs under a command's limit: %w", err)
	}
	if unit := limitScopeOf(string(own)); unit != "" {
		return nil, fmt.Errorf("this process already runs under the limit of a command (%s): a scope made here would be outside that limit, so none is made", unit)
	}
	page := int64(os.Getpagesize())
	limit := s.MemoryBytes / page * page
	if limit <= 0 {
		return nil, errors.New("no memory limit is set")
	}
	systemdRun, err := exec.LookPath("systemd-run")
	if err != nil {
		return nil, fmt.Errorf("a hard memory limit over a whole process tree needs systemd-run: %w", err)
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "dw-scope-*")
	if err != nil {
		return nil, err
	}
	status := f.Name()
	if err := f.Close(); err != nil {
		return nil, err
	}
	if maxRun <= 0 {
		maxRun = scopeMaxRun
	}

	// A tmux the command starts or reaches without naming a socket must be
	// one of its own, started inside the scope, not a server already running.
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	kept := Inside(env)
	tmuxDir := ""
	if own := envValue(kept, "TMUX_TMPDIR"); own == "" || own == os.Getenv("TMUX_TMPDIR") {
		// A short path: tmux's socket lies below it.
		if tmuxDir, err = os.MkdirTemp(shortTempDir(), "dwt-"); err != nil {
			_ = os.Remove(status)
			return nil, err
		}
		kept = append(withoutVar(kept, "TMUX_TMPDIR"), "TMUX_TMPDIR="+tmuxDir)
	}
	refuseDir, err := makeRefusals(envValue(kept, "PATH"))
	if err != nil {
		_ = os.Remove(status)
		if tmuxDir != "" {
			_ = os.RemoveAll(tmuxDir)
		}
		return nil, fmt.Errorf("make what refuses work outside the limit: %w", err)
	}
	// systemd-run needs the user manager's address. The script takes it away
	// again before it starts the command.
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	}
	kept = append(kept, "XDG_RUNTIME_DIR="+runtimeDir)
	if bus := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); bus != "" {
		kept = append(kept, "DBUS_SESSION_BUS_ADDRESS="+bus)
	}

	args := []string{"systemd-run"}
	if os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	args = append(args, "--scope", "--quiet", "--collect", "--expand-environment=no",
		"--unit="+UnitPrefix+hex.EncodeToString(id[:]),
		"-p", "MemoryMax="+strconv.FormatInt(limit, 10), "-p", "MemorySwapMax=0",
		"-p", "OOMPolicy=continue",
		"-p", "RuntimeMaxSec="+strconv.FormatInt(int64((maxRun+scopeSlack)/time.Second), 10),
		"--", "/bin/sh", "-c", scopeScript, "datawatch-scope",
		status, strconv.FormatInt(limit, 10), refuseDir, cmd.Path)
	if len(cmd.Args) > 1 {
		args = append(args, cmd.Args[1:]...)
	}
	cmd.Path, cmd.Args, cmd.Env = systemdRun, args, kept
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = 10 * time.Second
	}
	// A stop, such as at the time limit, ends the whole tree at once.
	if inner := cmd.Cancel; inner != nil {
		cmd.Cancel = func() error {
			killGroup(readStatus(status)["cgroup"])
			return inner()
		}
	}

	return func() Result {
		defer func() {
			_ = os.Remove(status)
			if tmuxDir != "" {
				_ = os.RemoveAll(tmuxDir)
			}
			if refuseDir != "" {
				_ = os.RemoveAll(refuseDir)
			}
		}()
		st := readStatus(status)
		cg := st["cgroup"]
		killGroup(cg)
		if cg == "" {
			return Result{Failed: "the command was not run: the hard memory limit of " + size(limit) + " could not be put in force"}
		}
		if _, ok := st["done"]; !ok {
			r := Result{Failed: "the processes of the command's scope were stopped before it ended: its time limit, or its memory limit of " + size(limit), Refused: asked(refuseDir)}
			if len(r.Refused) > 0 {
				r.Failed = refusedWhy(r.Refused) + "; " + r.Failed
			}
			return r
		}
		r := Result{Refused: asked(refuseDir)}
		r.Left, _ = strconv.Atoi(st["left"])
		var why []string
		// A command that was refused is read as failed even when it went on
		// and reported success: what it meant to check did not run.
		if len(r.Refused) > 0 {
			why = append(why, refusedWhy(r.Refused))
		}
		if n, _ := strconv.Atoi(st["oom"]); n > 0 {
			why = append(why, fmt.Sprintf("stopped by the memory limit: the command's processes passed %s of memory, and the kernel stopped %d of them", size(limit), n))
		}
		r.Failed = strings.Join(why, "; ")
		return r
	}, nil
}

// readStatus reads the "name value" lines the scope's script wrote.
func readStatus(path string) map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(path) // #nosec G304 -- the file made above
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, _ := strings.Cut(line, " "); k != "" {
			m[k] = v
		}
	}
	return m
}

// killGroup stops every process of a scope's control group. A group that has
// ended is already gone.
func killGroup(cg string) {
	if !strings.HasPrefix(cg, "/sys/fs/cgroup/") || !strings.Contains(filepath.Base(cg), UnitPrefix) {
		return
	}
	_ = os.WriteFile(filepath.Join(cg, "cgroup.kill"), []byte("1"), 0o200) // #nosec G306 -- an existing kernel control file
}

func envValue(env []string, name string) string {
	value := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			value = v
		}
	}
	return value
}

func withoutVar(env []string, name string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); k != name {
			out = append(out, kv)
		}
	}
	return out
}

// shortTempDir is where a folder that must have a short path is made.
func shortTempDir() string {
	if st, err := os.Stat("/tmp"); err == nil && st.IsDir() {
		return "/tmp"
	}
	return ""
}
