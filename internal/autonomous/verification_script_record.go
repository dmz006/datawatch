// Durable run record for a verification script. The script is started in
// its own session, writes its combined output and exit code to files under
// the Automaton's scratch area, and the daemon waits on that record — so a
// daemon restart no longer ends the script, and the record survives for a
// restarted daemon to read.
package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record file names inside one run's directory.
const (
	verifyRecordFile = "run.json"
	verifyOutputFile = "output.log"
	verifyExitFile   = "exit_code"
)

// verificationScriptTimeout is how long a script may run before the daemon
// ends its process group.
var verificationScriptTimeout = 30 * time.Minute

var (
	verifyRunRootMu sync.RWMutex
	verifyRunRoot   string
)

// SetVerificationRunRoot sets the durable directory holding verification
// script run records (normally under the scratch area). Empty keeps the
// direct-child run path.
func SetVerificationRunRoot(dir string) {
	verifyRunRootMu.Lock()
	verifyRunRoot = dir
	verifyRunRootMu.Unlock()
}

func verificationRunRoot() string {
	verifyRunRootMu.RLock()
	defer verifyRunRootMu.RUnlock()
	return verifyRunRoot
}

// verifyRunRecord is the run.json content. ExitCode is nil until the
// script ends.
type verifyRunRecord struct {
	ScriptPath string    `json:"script_path"`
	StartedAt  time.Time `json:"started_at"`
	PID        int       `json:"pid"`
	OutputFile string    `json:"output_file"`
	ExitFile   string    `json:"exit_file"`
	ExitCode   *int      `json:"exit_code,omitempty"`
	TimedOut   bool      `json:"timed_out,omitempty"`
	EndedAt    time.Time `json:"ended_at,omitempty"`
}

func writeVerifyRecord(dir string, rec *verifyRunRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, verifyRecordFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, verifyRecordFile))
}
