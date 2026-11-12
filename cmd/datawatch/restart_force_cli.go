// `datawatch restart` guard. Before the running daemon is stopped, the CLI reads
// the restart interruption report the daemon serves and refuses, with the
// report, when running Automata work would be interrupted and --force is not
// given. See docs/howto/restart-with-work-running.md.

package main

import (
	"fmt"
	"io"
	"net/url"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// restartCLIGuard returns nil when the restart may proceed. It prints the
// report and returns an error when the daemon says NeedsForce and force is not
// set. fetch reads one daemon GET route (progressGet in production). An
// unreachable daemon has no running work to interrupt, so the restart proceeds.
func restartCLIGuard(force bool, fetch func(path string, q url.Values) ([]byte, error), out io.Writer) error {
	if force {
		return nil
	}
	body, err := fetch("/api/autonomous/restart-interruptions", nil)
	if err != nil {
		return nil
	}
	rep, err := autonomous.ParseRestartInterruption(body)
	if err != nil {
		return fmt.Errorf("read the restart interruption report: %w", err)
	}
	if !rep.NeedsForce {
		return nil
	}
	_, _ = fmt.Fprint(out, restartGuardReportWith(rep, "datawatch restart --force"))
	return fmt.Errorf("restart refused: running work would be interrupted; pass --force to restart anyway")
}
