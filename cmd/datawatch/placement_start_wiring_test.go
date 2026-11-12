package main

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// recordingTagSink is the SystemTagsSink the wiring writes to in these tests.
// It keeps every write, so a test can see the last tag set a start produced.
type recordingTagSink struct {
	calls []tagWrite
	err   error
}

type tagWrite struct {
	sessionID string
	tags      []string
}

func (r *recordingTagSink) SetSystemTags(sessionID string, tags []string) error {
	r.calls = append(r.calls, tagWrite{sessionID: sessionID, tags: append([]string(nil), tags...)})
	return r.err
}

// last returns the most recent write; it fails the test when there is none.
func (r *recordingTagSink) last(t *testing.T) tagWrite {
	t.Helper()
	if len(r.calls) == 0 {
		t.Fatal("no SetSystemTags call recorded")
	}
	return r.calls[len(r.calls)-1]
}

// sortedTags returns a sorted copy so comparisons do not depend on the order
// the derivation emits.
func sortedTags(tags []string) []string {
	out := append([]string(nil), tags...)
	sort.Strings(out)
	return out
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// placementOn is the live config a stock daemon reads: the master switch on
// and every axis at its default.
func placementOn() config.PlacementConfig { return config.DefaultPlacementConfig() }

func TestPlacementStartFunc_LocalSessionWithoutNodeGetsLocalComputeTag(t *testing.T) {
	sink := &recordingTagSink{}
	hook := placementStartFunc(sink, placementOn, nil)

	// A plain local start: no compute node stored, a Claude Code backend and
	// no Automaton.
	s := &session.Session{FullID: "host-abc", BackendFamily: "claude-code"}
	hook(s)

	w := sink.last(t)
	if w.sessionID != "host-abc" {
		t.Fatalf("tags written to %q, want host-abc", w.sessionID)
	}
	if !hasTag(w.tags, "compute:local") {
		t.Errorf("local session with no compute node: tags %v, want compute:local", w.tags)
	}
	if !hasTag(w.tags, "backend:claude-code") {
		t.Errorf("tags %v, want backend:claude-code", w.tags)
	}
	// Claude Code is a lane family autonomous.laneForBackend does not list;
	// the wiring fills its lane the way the REST start path does.
	if !hasTag(w.tags, "lane:"+string(autonomous.LaneTypeClaude)) {
		t.Errorf("tags %v, want the claude lane tag", w.tags)
	}
	for _, tag := range w.tags {
		if strings.HasPrefix(tag, "peer:") {
			t.Errorf("local work must carry no peer tag, got %q", tag)
		}
	}
}

func TestPlacementStartFunc_StoredComputeNodeWinsOverLocal(t *testing.T) {
	sink := &recordingTagSink{}
	hook := placementStartFunc(sink, placementOn, nil)

	hook(&session.Session{FullID: "host-x", BackendFamily: "pi-prompt", ComputeNodeRef: "node-a"})

	w := sink.last(t)
	if !hasTag(w.tags, "compute:node-a") {
		t.Errorf("tags %v, want compute:node-a", w.tags)
	}
	if hasTag(w.tags, "compute:local") {
		t.Errorf("tags %v: a stored node must not be replaced by local", w.tags)
	}
	if !hasTag(w.tags, "lane:"+string(autonomous.LaneTypeGPU)) {
		t.Errorf("tags %v, want the gpu lane tag for pi-prompt", w.tags)
	}
}

func TestPlacementStartFunc_SessionWithPRDIDGetsAutomatonTag(t *testing.T) {
	sink := &recordingTagSink{}
	var asked []string
	automationOf := func(prdID string) string {
		asked = append(asked, prdID)
		if prdID == "prd-1" {
			return "auto-9"
		}
		return ""
	}
	hook := placementStartFunc(sink, placementOn, automationOf)

	hook(&session.Session{FullID: "host-p", BackendFamily: "claude-code", PRDID: "prd-1"})

	w := sink.last(t)
	if !hasTag(w.tags, "automaton:prd-1") {
		t.Errorf("tags %v, want automaton:prd-1", w.tags)
	}
	if !hasTag(w.tags, "automation:auto-9") {
		t.Errorf("tags %v, want automation:auto-9 from the Automaton's Automation", w.tags)
	}
	if !reflect.DeepEqual(asked, []string{"prd-1"}) {
		t.Errorf("automationOf asked for %v, want [prd-1]", asked)
	}
}

func TestPlacementStartFunc_SessionWithoutPRDIDSkipsAutomationLookup(t *testing.T) {
	sink := &recordingTagSink{}
	called := false
	hook := placementStartFunc(sink, placementOn, func(string) string {
		called = true
		return "auto-x"
	})

	hook(&session.Session{FullID: "host-n", BackendFamily: "claude-code"})

	if called {
		t.Error("automationOf must not be asked for a session with no PRDID")
	}
	for _, tag := range sink.last(t).tags {
		if strings.HasPrefix(tag, "automaton:") {
			t.Errorf("session with no Automaton carried %q", tag)
		}
	}
}

func TestPlacementStartFunc_SwitchOffClearsTags(t *testing.T) {
	sink := &recordingTagSink{}
	// The master switch is off. The axis template is set, so
	// EffectivePlacement keeps the block as written instead of replacing it
	// with the defaults (which would switch the master back on).
	off := config.PlacementConfig{
		Enabled: false,
		Compute: config.PlacementAxis{Enabled: true, Template: "compute:{node}"},
	}
	hook := placementStartFunc(sink, func() config.PlacementConfig { return off }, nil)

	hook(&session.Session{FullID: "host-off", BackendFamily: "claude-code", ComputeNodeRef: "node-a", PRDID: "prd-1"})

	w := sink.last(t)
	if len(w.tags) != 0 {
		t.Errorf("switch off: tags %v written, want the system tags cleared", w.tags)
	}
	if w.sessionID != "host-off" {
		t.Errorf("cleared tags on %q, want host-off", w.sessionID)
	}
}

func TestPlacementStartFunc_LiveConfigReadOnEachStart(t *testing.T) {
	sink := &recordingTagSink{}
	pc := placementOn()
	hook := placementStartFunc(sink, func() config.PlacementConfig { return pc }, nil)

	hook(&session.Session{FullID: "s1", BackendFamily: "claude-code"})
	if !hasTag(sink.last(t).tags, "compute:local") {
		t.Fatalf("first start: tags %v, want compute:local", sink.last(t).tags)
	}

	// The operator turns the compute axis off between two starts.
	pc.Compute.Enabled = false
	hook(&session.Session{FullID: "s2", BackendFamily: "claude-code"})
	if hasTag(sink.last(t).tags, "compute:local") {
		t.Errorf("second start: tags %v still carry compute after the axis was turned off", sink.last(t).tags)
	}
}

func TestPlacementStartFunc_NilPlacementReadsDefaults(t *testing.T) {
	sink := &recordingTagSink{}
	hook := placementStartFunc(sink, nil, nil)

	hook(&session.Session{FullID: "s3", BackendFamily: "claude-code"})

	if !hasTag(sink.last(t).tags, "compute:local") {
		t.Errorf("nil placement: tags %v, want the defaults (compute:local)", sink.last(t).tags)
	}
}

func TestPlacementStartFunc_NilSessionAndNilSinkAreNoOps(t *testing.T) {
	sink := &recordingTagSink{}
	placementStartFunc(sink, placementOn, nil)(nil)
	if len(sink.calls) != 0 {
		t.Errorf("nil session wrote %d tag sets", len(sink.calls))
	}

	// A nil sink must not panic.
	placementStartFunc(nil, placementOn, nil)(&session.Session{FullID: "s4"})
}

func TestPlacementStartFunc_SinkErrorIsLoggedNotReturned(t *testing.T) {
	sink := &recordingTagSink{err: errors.New("disk full")}
	hook := placementStartFunc(sink, placementOn, nil)

	// The hook has no return value; the session must still be tagged and the
	// call must return. A failed write is only logged.
	hook(&session.Session{FullID: "s5", BackendFamily: "claude-code"})

	if len(sink.calls) != 1 {
		t.Fatalf("sink called %d times, want 1", len(sink.calls))
	}
}

func TestPlacementAutomationOf(t *testing.T) {
	// No manager wired: an unknown Automation, never a panic.
	none := placementAutomationOf(func() *autonomous.Manager { return nil })
	if got := none("prd-1"); got != "" {
		t.Errorf("nil manager: got %q, want empty", got)
	}
	// An empty PRDID never reaches the manager.
	if got := none(""); got != "" {
		t.Errorf("empty PRDID: got %q, want empty", got)
	}
	// A nil getter is also a no-op.
	if got := placementAutomationOf(nil)("prd-1"); got != "" {
		t.Errorf("nil getter: got %q, want empty", got)
	}
}

func TestPlacementStartLane(t *testing.T) {
	cases := map[string]string{
		"claude":      string(autonomous.LaneTypeClaude),
		"claude-code": string(autonomous.LaneTypeClaude),
		"pi":          string(autonomous.LaneTypeGPU),
		"pi-prompt":   string(autonomous.LaneTypeGPU),
		"ollama":      string(autonomous.LaneTypeGPU),
		"vllm":        string(autonomous.LaneTypeGPU),
		"subprocess":  "",
		"":            "",
	}
	for backend, want := range cases {
		if got := placementStartLane(backend); got != want {
			t.Errorf("placementStartLane(%q) = %q, want %q", backend, got, want)
		}
	}
}

func TestPlacementStartFunc_TagsAreSortedStable(t *testing.T) {
	// Two starts of the same record write the same set (order aside).
	sink := &recordingTagSink{}
	hook := placementStartFunc(sink, placementOn, nil)
	s := &session.Session{FullID: "s6", BackendFamily: "claude-code", ComputeNodeRef: "node-b"}
	hook(s)
	hook(s)
	a := sortedTags(sink.calls[0].tags)
	b := sortedTags(sink.calls[1].tags)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("repeat start changed tags: %v vs %v", a, b)
	}
}
