//go:build !unix

package git

import (
	"context"
	"os/exec"
)

// limitedCommand has no memory limit to set here: only the time limit holds.
func limitedCommand(ctx context.Context, _ string, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed binary, arguments built by the caller
}
