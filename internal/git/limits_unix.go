//go:build unix

package git

import (
	"context"
	"os/exec"
)

// limitScript sets the data memory limit and becomes git. A lower limit that
// is already set is kept. If neither can be had, git is not started.
const limitScript = `ulimit -d "$0" 2>/dev/null
now=$(ulimit -d)
if [ "$now" = unlimited ] || [ "$now" -gt "$0" ]; then
	echo "datawatch: the memory limit for git could not be set" >&2
	exit 125
fi
exec git "$@"`

func limitedCommand(ctx context.Context, kib string, args []string) *exec.Cmd {
	var cmd *exec.Cmd
	if kib == "0" {
		cmd = exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed binary, arguments built by the caller
	} else {
		cmd = exec.CommandContext(ctx, "sh", append([]string{"-c", limitScript, kib}, args...)...) // #nosec G204 -- fixed script, the rest are positional arguments
	}
	// The time limit stops what git started as well.
	configureCheckProcessGroup(cmd)
	return cmd
}
