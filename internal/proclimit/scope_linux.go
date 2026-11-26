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
// with a hard memory limit and no swap, which every process the command
// starts belongs to, whatever process group or session it moves to.
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
const scopeScript = `status=$1 want=$2 xdg=$3 bus=$4
shift 4
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
case $xdg in -) unset XDG_RUNTIME_DIR ;; *) XDG_RUNTIME_DIR=${xdg#=}; export XDG_RUNTIME_DIR ;; esac
case $bus in -) unset DBUS_SESSION_BUS_ADDRESS ;; *) DBUS_SESSION_BUS_ADDRESS=${bus#=}; export DBUS_SESSION_BUS_ADDRESS ;; esac
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

// Describe says what the limit covers.
func (s Scope) Describe() string {
	return fmt.Sprintf("%s of memory and no swap for each command together with everything it starts (a systemd scope)", size(s.MemoryBytes))
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

	// systemd-run needs the user manager's address; the command gets back
	// exactly what it had.
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	}
	own := map[string]string{"XDG_RUNTIME_DIR": runtimeDir, "DBUS_SESSION_BUS_ADDRESS": os.Getenv("DBUS_SESSION_BUS_ADDRESS")}
	had := map[string]string{"XDG_RUNTIME_DIR": "-", "DBUS_SESSION_BUS_ADDRESS": "-"}
	var kept []string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if _, ok := own[k]; ok {
			had[k] = "=" + v
			continue
		}
		kept = append(kept, kv)
	}
	for k, v := range own {
		if v != "" {
			kept = append(kept, k+"="+v)
		}
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
		status, strconv.FormatInt(limit, 10), had["XDG_RUNTIME_DIR"], had["DBUS_SESSION_BUS_ADDRESS"], cmd.Path)
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
		defer func() { _ = os.Remove(status) }()
		st := readStatus(status)
		cg := st["cgroup"]
		killGroup(cg)
		if cg == "" {
			return Result{Failed: "the command was not run: the hard memory limit of " + size(limit) + " could not be put in force"}
		}
		if _, ok := st["done"]; !ok {
			return Result{Failed: "the command and everything it started were stopped before it ended: its time limit, or its memory limit of " + size(limit)}
		}
		r := Result{}
		r.Left, _ = strconv.Atoi(st["left"])
		if n, _ := strconv.Atoi(st["oom"]); n > 0 {
			r.Failed = fmt.Sprintf("stopped by the memory limit: the command and everything it started passed %s of memory, and the kernel stopped %d of its processes", size(limit), n)
		}
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
