// Package shutdowncause captures what caused the daemon to be stopped,
// so tooling can distinguish (for example) an operator running
// `systemctl --user stop datawatch` from a crash or an API-initiated
// restart.
//
// On Linux, Capture prefers siginfo (si_pid/si_uid) captured via a
// signalfd when the caller opted in with Install. Note that the Go
// runtime installs its own trap handlers and kills the process on
// unhandled signals, and per-thread sigmask cannot outrun it in an
// already multithreaded program, so siginfo capture only takes effect
// if the signals were blocked process-wide before the runtime became
// multithreaded (for example from a small C shim started first).
// Capture transparently falls back when no pending siginfo is
// available: it takes a snapshot of the /proc table at the moment of
// the signal, recording the daemon's own parent chain plus any live
// process whose cmdline looks like a service-control invocation naming
// the datawatch unit or the daemon pid, with Source=procscan. A
// systemd stop job is recorded when `systemctl` is on the PATH (user
// scope, then system scope). Other platforms report only the signal
// name with Source=unknown.
//
// MarkAPIInitiated lets the daemon's own restart/stop API handlers
// declare that the stop went through the API before Capture runs. The
// last captured cause can be persisted with Write (atomic temp file +
// rename to last-shutdown.json) and read back with ReadLast.
package shutdowncause

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Source identifies how the cause was determined.
const (
	SourceSignalInfo     = "signalinfo"
	SourceProcScan       = "procscan"
	SourceServiceManager = "service-manager"
	SourceAPIRestart     = "api-restart"
	SourceAPIStop        = "api-stop"
	SourceUnknown        = "unknown"
)

// FileName is the JSON file name used under the daemon data dir.
const FileName = "last-shutdown.json"

// ProcInfo is one process in the ParentChain trace.
type ProcInfo struct {
	PID     int    `json:"pid"`
	Cmdline string `json:"cmdline,omitempty"`
	User    string `json:"user,omitempty"`
}

// Cause describes what caused the most recent shutdown of the daemon.
// Fields that could not be determined are left at their zero values;
// the package never invents data.
type Cause struct {
	Signal         string     `json:"signal"`
	SenderPID      int        `json:"sender_pid"`
	SenderUID      string     `json:"sender_uid,omitempty"`
	SenderUser     string     `json:"sender_user,omitempty"`
	Cmdline        string     `json:"cmdline,omitempty"`
	ParentChain    []ProcInfo `json:"parent_chain,omitempty"`
	Source         string     `json:"source"`
	ServiceStopJob string     `json:"service_stop_job,omitempty"`
	Time           time.Time  `json:"time"`
	Via            string     `json:"via,omitempty"`
}

var apiVia atomic.Value // string

// Candidate is one process a procscan-based fallback may attribute the
// stop to.
type Candidate struct {
	PID     int
	Cmdline string
}

// MarkAPIInitiated declares that the current shutdown is being initiated
// through the daemon's own API. Pass a short descriptor such as "restart"
// or "stop" (any text works; "restart" yields Source api-restart,
// anything else api-stop). Capture picks the flag up until it is reset
// with MarkAPIInitiated("").
func MarkAPIInitiated(via string) { apiVia.Store(via) }

func apiMarked() (string, bool) {
	via, _ := apiVia.Load().(string)
	if via == "" {
		return "", false
	}
	return via, true
}

func apiSource(via string) string {
	if strings.Contains(strings.ToLower(via), "restart") {
		return SourceAPIRestart
	}
	return SourceAPIStop
}

// Capture records the signal the daemon is shutting down on and
// determines (best effort) who or what sent it. Call it from the
// os/signal handler, or from the reader of the channel returned by
// Install, passing the signal that was received.
func Capture(sig os.Signal) Cause {
	now := time.Now()
	name := ""
	if sig != nil {
		name = sig.String()
	}
	if via, marked := apiMarked(); marked {
		return Cause{Signal: name, Source: apiSource(via), Via: via, Time: now}
	}
	c := capturePlatform(sig)
	if c.Signal == "" {
		c.Signal = name
	}
	if c.Time.IsZero() {
		c.Time = now
	}
	return c
}

// Write persists c as last-shutdown.json under dir, creating the
// directory if needed. The write is atomic: the JSON goes to a temp
// file in the same directory which is renamed over the final path.
func Write(dir string, c Cause) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("shutdowncause: Write: data dir must not be empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("shutdowncause: Write: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("shutdowncause: Write: encode: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".last-shutdown-*.tmp")
	if err != nil {
		return fmt.Errorf("shutdowncause: Write: %w", err)
	}
	tmpName := tmp.Name()
	fail := func(cause string, inner error) error {
		os.Remove(tmpName)
		return fmt.Errorf("shutdowncause: Write: %s: %w", cause, inner)
	}
	if _, werr := tmp.Write(data); werr != nil {
		tmp.Close()
		return fail("write", werr)
	}
	if serr := tmp.Sync(); serr != nil {
		tmp.Close()
		return fail("sync", serr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return fail("close", cerr)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fail("chmod", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, FileName)); err != nil {
		return fail("rename", err)
	}
	return nil
}

// ReadLast reads the most recently persisted cause from
// dir/last-shutdown.json. ok is false if the file is missing or
// unreadable as a Cause.
func ReadLast(dir string) (Cause, bool) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return Cause{}, false
	}
	var c Cause
	if err := json.Unmarshal(data, &c); err != nil {
		return Cause{}, false
	}
	return c, true
}

// String renders the cause as a single human-readable line.
func (c Cause) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "shutdown: signal=%s source=%s", c.Signal, c.Source)
	if c.SenderPID != 0 {
		fmt.Fprintf(&b, " sender_pid=%d", c.SenderPID)
	}
	if c.SenderUser != "" {
		fmt.Fprintf(&b, " sender_user=%s", c.SenderUser)
	} else if c.SenderUID != "" {
		fmt.Fprintf(&b, " sender_uid=%s", c.SenderUID)
	}
	if c.Cmdline != "" {
		fmt.Fprintf(&b, " cmdline=%q", c.Cmdline)
	}
	if c.ServiceStopJob != "" {
		fmt.Fprintf(&b, " service_stop_job=%s", c.ServiceStopJob)
	}
	if c.Via != "" {
		fmt.Fprintf(&b, " via=%q", c.Via)
	}
	if !c.Time.IsZero() {
		fmt.Fprintf(&b, " at %s", c.Time.Format(time.RFC3339))
	}
	return b.String()
}
