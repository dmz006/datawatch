// Claude Code version canary. A CLI update can redraw the screen (a new
// suggestion text, a notice, a moved input row) and silently break the daemon's
// prompt-ready and task-delivery checks. When the installed CLI's version text
// differs from the one last checked, this replays the recorded real pane
// captures (testdata/delivery, embedded) through those checks and raises ONE
// alert if any recorded pane no longer reads the way it was recorded. It never
// updates the agent and never touches a session.

package session

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

//go:embed testdata/delivery/*.txt
var canaryFixtureFS embed.FS

// canaryFile is the data-dir entry holding the CLI version text last checked.
const canaryFile = "claude_cli_canary_version"

// canaryExpect is how a recorded pane must read: the delivery classification
// of its input box, and whether the main-prompt check calls it ready.
type canaryExpect struct {
	kind      inputBoxKind
	mainReady bool
}

// canaryExpectations lists every recorded pane (see testdata/delivery/README.md)
// with the reading it was recorded to have. A pane without an entry fails the
// replay, so a new capture cannot be added without saying what it must read as.
var canaryExpectations = map[string]canaryExpect{
	"claude-2.1.331-startup-idle.txt":             {inputBoxEmpty, true},
	"claude-2.1.330-startup-idle-placeholder.txt": {inputBoxEmpty, true},
	"claude-2.1.330-unsubmitted-paste.txt":        {inputBoxHoldsText, true},
	"claude-2.1.331-dev-channels-dialog.txt":      {inputBoxDialog, false},
	"claude-2.1.331-working.txt":                  {inputBoxBusy, true},
}

// canaryFixtureSource returns the recorded panes by file name; a test swaps it.
var canaryFixtureSource = func() map[string]string {
	out := map[string]string{}
	entries, _ := canaryFixtureFS.ReadDir("testdata/delivery")
	for _, e := range entries {
		if b, err := canaryFixtureFS.ReadFile("testdata/delivery/" + e.Name()); err == nil {
			out[e.Name()] = string(b)
		}
	}
	return out
}

// canaryFixtureOverride makes the replay use panes instead of the embedded set
// and returns the restore function. Test seam only.
func canaryFixtureOverride(panes map[string]string) func() {
	prev := canaryFixtureSource
	canaryFixtureSource = func() map[string]string { return panes }
	return func() { canaryFixtureSource = prev }
}

var canaryMu sync.Mutex

func inputBoxKindName(k inputBoxKind) string {
	switch k {
	case inputBoxEmpty:
		return "empty box"
	case inputBoxHoldsText:
		return "box holds text"
	case inputBoxDialog:
		return "dialog"
	case inputBoxBusy:
		return "busy"
	}
	return "no input row"
}

// replayCanaryPanes runs every recorded pane through the delivery and
// main-prompt checks and returns one line per pane that no longer reads as
// recorded, in file-name order.
func replayCanaryPanes() []string {
	panes := canaryFixtureSource()
	names := make([]string, 0, len(panes))
	for n := range panes {
		names = append(names, n)
	}
	sort.Strings(names)
	var failures []string
	for _, n := range names {
		want, ok := canaryExpectations[n]
		if !ok {
			failures = append(failures, n+": no recorded expectation")
			continue
		}
		kind, _ := classifyInputBox(panes[n], "")
		if kind != want.kind {
			failures = append(failures, fmt.Sprintf("%s: reads as %s, recorded as %s", n, inputBoxKindName(kind), inputBoxKindName(want.kind)))
			continue
		}
		if got := panePromptReady(panes[n]); got != want.mainReady {
			failures = append(failures, fmt.Sprintf("%s: main prompt ready=%v, recorded as %v", n, got, want.mainReady))
		}
	}
	return failures
}

// ClaudeCLICanaryResult is what one canary run did.
type ClaudeCLICanaryResult struct {
	// Replayed is true when the version text was new and the panes were replayed.
	Replayed bool
	Version  string
	Failures []string
}

// RunClaudeCLICanary reads the installed CLI's version text; when it is known
// and differs from the last one checked (or none was recorded) it replays the
// recorded panes, records the version, and raises ONE operator alert when any
// pane fails. The version is recorded either way, so a failure alerts once per
// version change, not on every run. An unknown version does nothing.
func (m *Manager) RunClaudeCLICanary() ClaudeCLICanaryResult {
	canaryMu.Lock()
	defer canaryMu.Unlock()
	tool := m.RefreshClaudeCLIVersion()
	res := ClaudeCLICanaryResult{Version: tool}
	if !knownToolText(tool) {
		return res
	}
	path := filepath.Join(m.dataDir, canaryFile)
	if prev, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(prev)) == tool {
		return res
	}
	res.Replayed = true
	res.Failures = replayCanaryPanes()
	if err := os.WriteFile(path, []byte(tool+"\n"), 0o600); err != nil {
		m.debugf("claude cli canary: record version: %v", err)
	}
	if len(res.Failures) > 0 && m.pushAlertFn != nil {
		m.pushAlertFn("warn", "Claude Code "+tool+": recorded screens no longer read as before",
			"The installed Claude Code version text changed to \""+tool+"\" and "+fmt.Sprint(len(res.Failures))+
				" recorded pane capture(s) no longer read the way they were recorded, so prompt-ready and task delivery may misread this version's screen: "+
				strings.Join(res.Failures, "; ")+". The agent was not updated or touched.")
	}
	return res
}
