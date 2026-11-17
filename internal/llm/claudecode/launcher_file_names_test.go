package claudecode

import (
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/channel"
)

// TestLauncherFileNamesMatchWriterConstants pins the daemon-side launcher
// file exclusion set (internal/autonomous) to the two names the writers
// actually create: channel.WorkerMCPConfigFileName and this package's
// WorkerStartingContextFileName. The exclusion set cannot import the writer
// packages (this package already imports autonomous), so the literals are
// pinned here from the writer side and can never drift.
func TestLauncherFileNamesMatchWriterConstants(t *testing.T) {
	names := autonomous.LauncherToolingFileNames()
	want := map[string]bool{
		channel.WorkerMCPConfigFileName: true,
		WorkerStartingContextFileName:   true,
	}
	if len(names) != len(want) {
		t.Fatalf("exclusion set = %v; want exactly the two launcher file names %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("exclusion set contains %q, which is not a launcher file written by the daemon", n)
		}
		delete(want, n)
	}
	if len(want) != 0 {
		t.Fatalf("exclusion set misses the launcher file names written by the daemon: %v", want)
	}
}
