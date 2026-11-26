//go:build !windows

package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeClaude stands in for `claude mcp` over a folder holding one file per
// entry. `mcp list` takes its snapshot when it starts. A hold-list or
// hold-remove file makes the call wait until the matching go- file appears;
// only the first remove is held. Every wait ends on its own after 60 seconds.
type fakeClaude struct {
	t   *testing.T
	dir string
}

func newFakeClaude(t *testing.T) *fakeClaude {
	t.Helper()
	f := &fakeClaude{t: t, dir: t.TempDir()}
	if err := os.MkdirAll(f.path("entries"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
d='%s'
hold() { i=0; while [ ! -e "$d/go-$1" ] && [ "$i" -lt 1200 ]; do sleep 0.05; i=$((i+1)); done; }
case "$2" in
list)
  snap=$(for f in "$d"/entries/*; do [ -e "$f" ] && printf '%%s: %%s - ok\n' "${f##*/}" "$(cat "$f")"; done)
  echo $$ > "$d/list-started"
  [ -e "$d/hold-list" ] && hold list
  printf '%%s\n' "$snap" ;;
add)
  printf '%%s' "$6" > "$d/entries/$5" ;;
remove)
  if [ -e "$d/hold-remove" ] && mkdir "$d/remove-held" 2>/dev/null; then hold remove; fi
  [ -e "$d/entries/$3" ] || exit 1
  rm -f "$d/entries/$3" ;;
esac
`, f.dir)
	bin := f.path("claude")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	oldBin, oldBridge := claudeCliBin, channelBinPathForReg
	claudeCliBin, channelBinPathForReg = bin, "/opt/datawatch-channel"
	t.Cleanup(func() {
		f.touch("go-list")
		f.touch("go-remove")
		claudeCliBin, channelBinPathForReg = oldBin, oldBridge
	})
	return f
}

func (f *fakeClaude) path(name string) string { return filepath.Join(f.dir, name) }

func (f *fakeClaude) touch(name string) { _ = os.WriteFile(f.path(name), nil, 0o600) }

func (f *fakeClaude) exists(name string) bool {
	_, err := os.Stat(f.path(name))
	return err == nil
}

func (f *fakeClaude) entry(name, command string) {
	f.t.Helper()
	if err := os.WriteFile(f.path(filepath.Join("entries", name)), []byte(command), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeClaude) has(name string) bool { return f.exists(filepath.Join("entries", name)) }

// recorded writes the record an earlier daemon run on dataDir left behind.
func recorded(t *testing.T, dataDir string, names ...string) {
	t.Helper()
	b, err := json.Marshal(map[string][]string{"names": names})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "channel-mcp-registrations.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func startCleanup(usingGoBridge bool, sessionExists func(string) bool) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		CleanupStaleRegistrations(context.Background(), usingGoBridge, sessionExists)
	}()
	return done
}

func finished(t *testing.T, what string, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("%s did not finish", what)
	}
}

func noSession(string) bool { return false }

// A session is registered before it is saved, so the cleanup can find its entry
// and no session for it.
func TestCleanupKeepsAnEntryRegisteredInThisRun(t *testing.T) {
	f, data := newFakeClaude(t), t.TempDir()
	recorded(t, data, "datawatch-host-dead")
	f.entry("datawatch-host-dead", "/opt/datawatch-channel")
	SetRegistrationRecord(data)
	if err := RegisterSessionMCP("host-ab12", "", nil); err != nil {
		t.Fatal(err)
	}
	finished(t, "cleanup", startCleanup(true, noSession))
	if !f.has("datawatch-host-ab12") {
		t.Error("cleanup removed the entry a launch registered in this run")
	}
	if f.has("datawatch-host-dead") {
		t.Error("cleanup kept the stale entry an earlier run recorded")
	}
}

// The list the channel.js pass reads may be older than a resume that has since
// registered the same name again.
func TestCleanupKeepsAnEntryReRegisteredWhileItWasListing(t *testing.T) {
	f, data := newFakeClaude(t), t.TempDir()
	recorded(t, data, "datawatch-host-ab12", "datawatch-host-cd34")
	f.entry("datawatch-host-ab12", "node /old/channel.js")
	f.entry("datawatch-host-cd34", "node /old/channel.js")
	SetRegistrationRecord(data)
	f.touch("hold-list")
	done := startCleanup(true, func(string) bool { return true })
	waitUntil(t, "the cleanup's list", func() bool { return f.exists("list-started") })
	if err := RegisterSessionMCP("host-ab12", "", nil); err != nil {
		t.Fatal(err)
	}
	f.touch("go-list")
	finished(t, "cleanup", done)
	if !f.has("datawatch-host-ab12") {
		t.Error("cleanup removed the entry a resume had just registered")
	}
	if f.has("datawatch-host-cd34") {
		t.Error("cleanup kept the recorded channel.js entry nothing re-registered")
	}
}

// A removal already under way must not take away a registration made meanwhile.
func TestRegistrationDuringARemovalSurvivesIt(t *testing.T) {
	f, data := newFakeClaude(t), t.TempDir()
	recorded(t, data, "datawatch-host-ab12")
	f.entry("datawatch-host-ab12", "/opt/datawatch-channel")
	SetRegistrationRecord(data)
	f.touch("hold-remove")
	done := startCleanup(false, noSession)
	waitUntil(t, "the cleanup's removal", func() bool { return f.exists("remove-held") })
	registered := make(chan struct{})
	var regErr error
	go func() {
		defer close(registered)
		regErr = RegisterSessionMCP("host-ab12", "", nil)
	}()
	// A registration that does not wait for the removal ends within this time.
	// One that does wait is still waiting when it passes.
	select {
	case <-registered:
	case <-time.After(2 * time.Second):
	}
	f.touch("go-remove")
	finished(t, "cleanup", done)
	finished(t, "registration", registered)
	if regErr != nil {
		t.Fatal(regErr)
	}
	if !f.has("datawatch-host-ab12") {
		t.Error("the removal took away the registration made while it ran")
	}
}

// Two daemons with one hostname and their own data folders share claude's list.
// Neither may remove what the other's folder recorded, or what none did.
func TestCleanupRemovesOnlyWhatItsDataFolderRecorded(t *testing.T) {
	f := newFakeClaude(t)
	first, second := t.TempDir(), t.TempDir()
	SetRegistrationRecord(first)
	if err := RegisterSessionMCP("host-aaaa", "", nil); err != nil {
		t.Fatal(err)
	}
	f.entry("datawatch-host-bbbb", "/opt/datawatch-channel") // nobody's record
	f.entry("datawatch-host-cccc", "node /old/channel.js")   // nobody's record
	f.entry("datawatch", "/opt/datawatch-channel")           // nobody's record
	SetRegistrationRecord(second)
	finished(t, "the second daemon's cleanup", startCleanup(true, noSession))
	for _, name := range []string{"datawatch-host-aaaa", "datawatch-host-bbbb", "datawatch-host-cccc", "datawatch"} {
		if !f.has(name) {
			t.Errorf("a daemon removed %s, which its data folder never recorded", name)
		}
	}
	// The first daemon's next start removes its own stale entry, whatever the
	// hostname is by then.
	SetRegistrationRecord(first)
	finished(t, "the first daemon's next cleanup", startCleanup(true, noSession))
	if f.has("datawatch-host-aaaa") {
		t.Error("the recorded stale entry stayed after a new run of its own daemon")
	}
}

// A claude call that never answers ends at the limit, and its process with it.
func TestCleanupCallEndsAtItsTimeLimit(t *testing.T) {
	f := newFakeClaude(t)
	SetRegistrationRecord(t.TempDir())
	old := mcpListTimeout
	mcpListTimeout = 300 * time.Millisecond
	defer func() { mcpListTimeout = old }()
	f.touch("hold-list")
	done := startCleanup(true, noSession)
	finished(t, "cleanup against a list that never answers", done)
	raw, err := os.ReadFile(f.path("list-started"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the end of the listing process", func() bool { return syscall.Kill(pid, 0) != nil })
}
