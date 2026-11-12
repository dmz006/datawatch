// Durable run record for a verification script. The script is started in
// its own session, writes its combined output and exit code to files under
// the Automaton's scratch area, and the daemon waits on that record — so a
// daemon restart no longer ends the script, and the record survives for a
// restarted daemon to read.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
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
// script ends. Delivered is set once the run's result has reached a caller
// (in-process, or consumed by a restarted daemon), so a finished record is
// consumed at most once and a later attempt never picks up an old verdict.
type verifyRunRecord struct {
	ScriptPath string    `json:"script_path"`
	StartedAt  time.Time `json:"started_at"`
	PID        int       `json:"pid"`
	OutputFile string    `json:"output_file"`
	ExitFile   string    `json:"exit_file"`
	ExitCode   *int      `json:"exit_code,omitempty"`
	TimedOut   bool      `json:"timed_out,omitempty"`
	Delivered  bool      `json:"delivered,omitempty"`
	EndedAt    time.Time `json:"ended_at,omitempty"`
}

// verifyRunDirFor is the one run directory for scriptPath under root. It is
// keyed by the script's absolute path so a restarted daemon can find the
// record of a run it did not start, without any task field naming it.
func verifyRunDirFor(root, scriptPath string) string {
	sum := sha256.Sum256([]byte(scriptPath))
	return filepath.Join(root, "run-"+hex.EncodeToString(sum[:8]))
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
