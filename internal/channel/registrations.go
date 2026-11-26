package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// claude's user-scope MCP list is shared by everything that runs as this user:
// other daemons, also ones with the same hostname, included. The startup
// cleanup therefore removes an entry only when
//
//   - this daemon's data folder recorded registering it, and
//   - nothing in this run has registered that name.
//
// An entry no record names stays, also a stale one from a build that kept no
// record.

// Time limits for the claude calls of the startup cleanup. They bound
// datawatch's own housekeeping: a call cut off here is tried again at the next
// start.
var (
	// `claude mcp list` connects to every configured server, and claude gives
	// one server 30 seconds to connect (31 s measured for a list with one
	// unreachable server). Four times that means the command itself is stuck.
	mcpListTimeout = 2 * time.Minute
	// `claude mcp remove` only edits a file (about 0.25 s measured). A launch
	// that registers a server can wait this long at most.
	mcpRemoveTimeout = 30 * time.Second
)

// registrationRecordFile is kept in the daemon's data folder.
const registrationRecordFile = "channel-mcp-registrations.json"

type registrationRecord struct {
	Names []string `json:"names"`
}

var (
	// regMu makes "may this entry be removed" and its removal one step, and no
	// registration can claim a name in the middle of it. It guards the two
	// variables below and the record file.
	regMu             sync.Mutex
	registeredThisRun = map[string]bool{}
	recordPath        string
)

// SetRegistrationRecord names the data folder that records the registrations
// this daemon makes. Call it once at startup, before the first registration and
// before CleanupStaleRegistrations. Without it the cleanup removes nothing.
func SetRegistrationRecord(dataDir string) {
	regMu.Lock()
	defer regMu.Unlock()
	recordPath = filepath.Join(dataDir, registrationRecordFile)
	registeredThisRun = map[string]bool{}
}

// claimRegistration marks name as registered in this run and records it. It
// runs before the first claude call of a registration: from here on the cleanup
// leaves the name alone, and a removal already under way has ended.
func claimRegistration(name string) {
	regMu.Lock()
	defer regMu.Unlock()
	registeredThisRun[name] = true
	names := recordedLocked()
	if !slices.Contains(names, name) {
		writeRecordLocked(append(names, name))
	}
}

func forgetRegistrationLocked(name string) {
	names := recordedLocked()
	if i := slices.Index(names, name); i >= 0 {
		writeRecordLocked(slices.Delete(names, i, i+1))
	}
}

func recordedLocked() []string {
	if recordPath == "" {
		return nil
	}
	raw, err := os.ReadFile(recordPath)
	if err != nil {
		return nil
	}
	var r registrationRecord
	if err := json.Unmarshal(raw, &r); err != nil {
		fmt.Printf("[channel] MCP registration record %s is unreadable: %v\n", recordPath, err)
		return nil
	}
	return r.Names
}

// writeRecordLocked never fails a registration: a name that cannot be recorded
// is only never removed by a later cleanup.
func writeRecordLocked(names []string) {
	if recordPath == "" {
		return
	}
	raw, err := json.Marshal(registrationRecord{Names: names})
	if err == nil {
		err = os.MkdirAll(filepath.Dir(recordPath), 0o700)
	}
	if err == nil {
		err = os.WriteFile(recordPath+".tmp", raw, 0o600)
	}
	if err == nil {
		err = os.Rename(recordPath+".tmp", recordPath)
	}
	if err != nil {
		fmt.Printf("[channel] could not write the MCP registration record: %v\n", err)
	}
}

// CleanupStaleRegistrations removes this data folder's old entries from
// claude's MCP list. Every claude call has a time limit and ends, with the
// processes it started, when ctx is cancelled.
//
// With the Go bridge, entries that still point at node + channel.js go first.
// Then the legacy global "datawatch" entry and every "datawatch-<session>"
// entry whose session sessionExists does not know.
func CleanupStaleRegistrations(ctx context.Context, usingGoBridge bool, sessionExists func(string) bool) {
	if usingGoBridge {
		if cleaned := cleanupStaleJS(ctx); len(cleaned) > 0 {
			fmt.Printf("[channel] removed stale JS-bridge MCP registration(s): %s\n", strings.Join(cleaned, ", "))
		}
	}
	removeIfStale(ctx, "datawatch", "user")
	cleanupStaleSessions(ctx, sessionExists)
}

// cleanupStaleJS returns what it removed as name@scope.
func cleanupStaleJS(ctx context.Context) []string {
	var removed []string
	out, err := cleanupOutput(ctx, mcpListTimeout, "mcp", "list")
	if err != nil {
		return removed
	}
	for _, line := range strings.Split(string(out), "\n") {
		// Format: `<name>: <command...> - <status>`
		name, rest, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || !strings.HasPrefix(name, "datawatch") || !strings.Contains(rest, "channel.js") {
			continue
		}
		if scope := removeIfStale(ctx, name, "user", "local", "project"); scope != "" {
			removed = append(removed, name+"@"+scope)
		}
	}
	return removed
}

func cleanupStaleSessions(ctx context.Context, sessionExists func(string) bool) {
	out, err := cleanupOutput(ctx, mcpListTimeout, "mcp", "list")
	if err != nil {
		return
	}
	removed := 0
	for _, line := range strings.Split(string(out), "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), ":")
		sessionID, ok := strings.CutPrefix(name, "datawatch-")
		if !ok || sessionID == "" || sessionExists(sessionID) {
			continue
		}
		if removeIfStale(ctx, name, "user") != "" {
			removed++
		}
	}
	if removed > 0 {
		fmt.Printf("[channel] cleaned up %d stale MCP registration(s)\n", removed)
	}
}

// removeIfStale removes name from the first scope that has it, if the cleanup
// may remove it, and returns that scope ("" = nothing removed). A removal that
// fails keeps its record, so a later start tries again.
func removeIfStale(ctx context.Context, name string, scopes ...string) string {
	regMu.Lock()
	defer regMu.Unlock()
	if registeredThisRun[name] || !slices.Contains(recordedLocked(), name) {
		return ""
	}
	for _, scope := range scopes {
		if _, err := cleanupOutput(ctx, mcpRemoveTimeout, "mcp", "remove", name, "-s", scope); err == nil {
			forgetRegistrationLocked(name)
			return scope
		}
	}
	return ""
}

// cleanupOutput runs one claude call of the cleanup under limit and ctx.
func cleanupOutput(ctx context.Context, limit time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	base := claudeCmd(args...)
	cmd := exec.CommandContext(ctx, claudeCliBin, args...) // #nosec G204 -- the configured claude binary, argv list
	cmd.Env = base.Env
	endProcessGroupOnCancel(cmd)
	// Do not wait on output pipes something it started still holds.
	cmd.WaitDelay = 5 * time.Second
	return cmd.Output()
}
