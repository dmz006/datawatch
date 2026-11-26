// Who holds a merge lock, and when that holder is provably gone. A lock a
// killed run left behind is taken over only then: its process no longer
// exists (or the machine restarted since), and nothing the run started is
// still working in its scratch folder. Anything that cannot be proven, such as
// on a system whose processes cannot be listed, leaves the lock held.
package autonomous

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrMergeLockLeftovers means what the holding run started is still running,
// or cannot be shown to have ended.
var ErrMergeLockLeftovers = errors.New("the run that held the merge lock is not proven to have ended")

// mergeScratchMark is the start of a run's scratch folder name, followed by
// the run id and a dash.
const mergeScratchMark = "/dw-merge-"

// MergeLockOwner identifies the process that holds a lock.
type MergeLockOwner struct {
	PID int `json:"pid"`
	// Started and Boot tell this process from a later one with the same pid;
	// empty where the system does not say.
	Started string `json:"started,omitempty"`
	Boot    string `json:"boot,omitempty"`
	// Instance is one run of the daemon's program. The daemon restarts by
	// replacing its program, which keeps the pid and the start time.
	Instance string `json:"instance,omitempty"`
}

// processInstance is new each time the program starts.
var processInstance = func() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}()

// processTable is where processes are looked up; a variable so a test can
// stand in its own.
var processTable = "/proc"

func bootID() string {
	b, err := os.ReadFile(filepath.Join(processTable, "sys", "kernel", "random", "boot_id"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// processStat returns the state letter and start time of pid.
func processStat(pid int) (state, started string, ok bool) {
	b, err := os.ReadFile(filepath.Join(processTable, strconv.Itoa(pid), "stat")) // #nosec G304 -- a number under the process table
	if err != nil {
		return "", "", false
	}
	// The command name is in parentheses and may hold spaces; the fields
	// after it start with the state, and the start time is the 20th of them.
	at := bytes.LastIndexByte(b, ')')
	if at < 0 {
		return "", "", false
	}
	f := strings.Fields(string(b[at+1:]))
	if len(f) < 20 {
		return "", "", false
	}
	return f[0], f[19], true
}

func currentMergeLockOwner() *MergeLockOwner {
	o := &MergeLockOwner{PID: os.Getpid(), Boot: bootID(), Instance: processInstance}
	_, o.Started, _ = processStat(o.PID)
	return o
}

// ownerGone reports whether the owning process provably no longer exists.
// rebooted says nothing from that time can still be running.
func ownerGone(o *MergeLockOwner) (gone, rebooted bool) {
	if o == nil || o.PID <= 0 {
		return false, false
	}
	if now := bootID(); o.Boot != "" && now != "" && now != o.Boot {
		return true, true
	}
	if o.PID == os.Getpid() && o.Instance != "" && o.Instance != processInstance {
		return true, false // this process, before it replaced its program
	}
	if !processExists(o.PID) {
		return true, false
	}
	state, started, ok := processStat(o.PID)
	if !ok {
		return false, false
	}
	return state == "Z" || (o.Started != "" && started != o.Started), false
}

// processesOfRun lists the processes still working in the scratch folder of
// run runID: by their working folder, or by the environment a check was given.
// checked is false where processes cannot be listed.
func processesOfRun(runID string) (found []int, checked bool) {
	if runID == "" {
		return nil, false
	}
	mark := mergeScratchMark + runID + "-"
	entries, err := os.ReadDir(processTable)
	if err != nil {
		return nil, false
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		dir := filepath.Join(processTable, e.Name())
		cwd, _ := os.Readlink(filepath.Join(dir, "cwd"))
		if strings.Contains(cwd, mark) {
			out = append(out, pid)
			continue
		}
		env, _ := os.ReadFile(filepath.Join(dir, "environ")) // #nosec G304 -- a number under the process table
		if bytes.Contains(env, []byte(mark)) {
			out = append(out, pid)
		}
	}
	return out, true
}
