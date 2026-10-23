package shutdowncause

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// PreviousShutdownLine reads the most recently persisted shutdown cause
// (dir/last-shutdown.json, via ReadLast) and renders it as the single
// line the daemon prints at startup. The line is exactly one of:
//
//   - "previous shutdown: signal <name> from <command> <pid> at <time>"
//     for a signal-caused stop whose sender was attributed (command and
//     pid parts are omitted when they are empty);
//   - "previous shutdown: <via> at <time>"
//     for a stop initiated through the daemon's own API
//     (MarkAPIInitiated);
//   - "previous shutdown: unknown/crash"
//     when the record is missing, unreadable, or carries no usable
//     information.
//
// <time> is RFC3339. The package never invents data: fields that were
// not captured are simply absent from the line. Once the line is
// produced the record is consumed (renamed to last-shutdown.json.consumed
// so a forensic copy is kept, removed if the rename fails) so that a
// crash after a clean start is not misreported next startup as the
// cause of the previous, already-shown stop: each record is displayed
// at most once.
func PreviousShutdownLine(dir string) string {
	path := filepath.Join(dir, FileName)

	c, ok := ReadLast(dir)
	if !ok {
		// Either there is no record (nothing to consume) or the record
		// exists but is unreadable as a Cause (consume it so it does not
		// linger and re-trigger on the next startup).
		if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() {
			consumeRecord(path)
		}
		return "previous shutdown: unknown/crash"
	}
	line := formatPreviousShutdown(c)
	consumeRecord(path)
	return line
}

// consumeRecord marks the record consumed by renaming it aside; if the
// rename fails it removes the file. Best effort: the record is stale
// either way, since the line has already been produced.
func consumeRecord(path string) {
	if err := os.Rename(path, path+".consumed"); err == nil {
		return
	}
	_ = os.Remove(path)
}

// formatPreviousShutdown renders c in the three forms documented on
// PreviousShutdownLine.
func formatPreviousShutdown(c Cause) string {
	if c.Via != "" {
		line := "previous shutdown: " + c.Via
		if !c.Time.IsZero() {
			line += " at " + c.Time.Format(time.RFC3339)
		}
		return line
	}
	if c.Signal != "" {
		line := "previous shutdown: signal " + c.Signal
		if c.Cmdline != "" {
			line += " from " + c.Cmdline
			if c.SenderPID != 0 {
				line += " " + strconv.Itoa(c.SenderPID)
			}
		}
		if !c.Time.IsZero() {
			line += " at " + c.Time.Format(time.RFC3339)
		}
		return line
	}
	return "previous shutdown: unknown/crash"
}
