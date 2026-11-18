package autonomous

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// sweepSourceFake is a TagSweepSessionSource — with the archived lister and
// the any-writer — the store tests drive against: it lists real
// session.Session values live and archived (the same data the session
// manager would hand out) and records every SystemTags write. The operator
// tags are a separate field that the write never touches — asserting on it
// is what proves the store replaces only the derived system tags.
type sweepSourceFake struct {
	sessions []*session.Session
	archived []*session.Session
	writes   map[string]int
}

func newSweepSourceFake() *sweepSourceFake {
	return &sweepSourceFake{writes: map[string]int{}}
}

func (f *sweepSourceFake) session(id string) *session.Session {
	for _, s := range f.sessions {
		if s != nil && s.ID == id {
			return s
		}
	}
	for _, s := range f.archived {
		if s != nil && s.ID == id {
			return s
		}
	}
	return nil
}

func (f *sweepSourceFake) ListSessions() []*session.Session {
	return f.sessions
}

func (f *sweepSourceFake) ListArchivedSessions(flt session.SessionFilter, extra func(*session.Session) bool, text string, before time.Time, limit int) []*session.Session {
	if flt.Name != "" || flt.State != "" || flt.Backend != "" || flt.Alive != "" || len(flt.Tags) > 0 || extra != nil || text != "" || !before.IsZero() || limit > 0 {
		return []*session.Session{}
	}
	return f.archived
}

func (f *sweepSourceFake) SetSystemTags(id string, tags []string) error {
	s := f.session(id)
	if s == nil {
		return ErrQuestionNotFound
	}
	s.SystemTags = append([]string(nil), tags...)
	f.writes[id]++
	return nil
}

// SetSystemTagsAny is the any-writer the fake's session manager would be:
// the write lands on the record — live or archived — under that id.
func (f *sweepSourceFake) SetSystemTagsAny(id string, tags []string) error {
	return f.SetSystemTags(id, tags)
}

// TestTagSweepStore_RebuildsPlacementFromStoredFacts covers the spec's core
// requirement: the production store rebuilds each session's placement from
// the session's own stored facts (its compute node, backend, model, project
// directory; role and kind from the name) plus the Automaton's stored facts
// (its ID, the Automation it is member of, the stage of that membership) —
// it does not consume anyone's pre-built placement.
func TestTagSweepStore_RebuildsPlacementFromStoredFacts(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.Store().CreatePRD("spec", "/work/alpha", "pi", "gpt", EffortMedium)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	auto := &Automation{ID: "auto-1", Name: "build-automation", Members: []AutomationMember{{PRDID: prd.ID, Stage: "build"}}}
	if _, err := m.automations.Create(auto); err != nil {
		t.Fatalf("automation create: %v", err)
	}

	src := newSweepSourceFake()
	src.sessions = []*session.Session{
		{
			ID:             "s1",
			Name:           "autonomous:build-the-thing",
			PRDID:          prd.ID,
			ComputeNodeRef: "node-1",
			BackendFamily:  "pi",
			Model:          "gpt-5",
			ProjectDir:     "/work/alpha",
			SystemTags:     []string{"stale:tag"},
		},
	}
	st := NewSessionTagSweepStore(m, src)
	out := st.ListTagSweepSessions()
	if len(out) != 1 {
		t.Fatalf("listed %d sessions, want the one owned by the PRD: %+v", len(out), out)
	}
	got := out[0].Placement
	want := Placement{
		Node:       "node-1",
		Backend:    "pi",
		Model:      "gpt-5",
		Lane:       "gpu",
		Project:    "alpha",
		Automaton:  prd.ID,
		Automation: "auto-1",
		Stage:      "build",
		Role:       "worker",
		Kind:       "task",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuilt placement = %+v, want %+v", got, want)
	}
	// A sweep run under the store writes exactly the set that placement
	// derives — the whole "rebuild placement, then derive, then replace" path.
	m.SetTagSweepSessions(st)
	if res := m.SweepTags(false); res.Sessions != (SweepCount{Examined: 1, Changed: 1}) {
		t.Fatalf("sweep = %+v, want one examined, one changed", res)
	}
	if wantTags := DerivePlacementTags(want, config.DefaultPlacementConfig()); !reflect.DeepEqual(src.session("s1").SystemTags, wantTags) {
		t.Fatalf("session tags after sweep = %v, want %v", src.session("s1").SystemTags, wantTags)
	}
}

// TestTagSweepStore_OwnershipSelection covers the selection: the sweep is the
// Automata's — it lists a session that belongs to an Automaton (PRD ID) and
// a session the manager already carries system tags for, and it leaves an
// ordinary session (no PRD ID, no system tags) entirely out of the sweep so
// the sweep never touches an operator session it was not asked to.
func TestTagSweepStore_OwnershipSelection(t *testing.T) {
	src := newSweepSourceFake()
	src.sessions = []*session.Session{
		{ID: "s-prd", PRDID: "pr-1"},
		{ID: "s-system", SystemTags: []string{"role:worker"}},
		{ID: "s-plain", Name: "my-ordinary-session"},
	}
	st := NewSessionTagSweepStore(nil, src)
	var ids []string
	for _, s := range st.ListTagSweepSessions() {
		ids = append(ids, s.ID)
	}
	want := []string{"s-prd", "s-system"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("listed %v, want exactly the Automata-owned %v (the plain session must not be swept)", ids, want)
	}
}

// TestTagSweepStore_OperatorTagsSurvive covers the operator-tags guarantee
// through the production store: the write pass replaces only the SystemTags
// field; the operator's Tags stay exactly what they were, untouched, through
// both a dry run and a real run.
func TestTagSweepStore_OperatorTagsSurvive(t *testing.T) {
	src := newSweepSourceFake()
	src.sessions = []*session.Session{
		{
			ID:            "s1",
			PRDID:         "pr-1",
			Name:          "autonomous:work",
			BackendFamily: "pi",
			Tags:          []string{"mine-1", "mine-2"},
		},
	}
	st := NewSessionTagSweepStore(nil, src)
	m2, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m := m2
	m.SetTagSweepSessions(st)
	before := []string{"mine-1", "mine-2"}
	if res := m.SweepTags(true); res.Sessions.Failed != 0 {
		t.Fatalf("dry run failed: %+v", res)
	}
	if !reflect.DeepEqual(src.session("s1").Tags, before) {
		t.Fatalf("dry-run touched the operator tags: %v", src.session("s1").Tags)
	}
	if res := m.SweepTags(false); res.Sessions.Failed != 0 {
		t.Fatalf("write run failed: %+v", res)
	}
	if !reflect.DeepEqual(src.session("s1").Tags, before) {
		t.Fatalf("write run replaced the operator tags: %v", src.session("s1").Tags)
	}
}

// TestTagSweepStore_WritesNothingForNilSource covers the store with no
// session source wired (a partially built Manager): it lists nothing and
// writes nothing rather than failing the sweep.
func TestTagSweepStore_WritesNothingForNilSource(t *testing.T) {
	st := NewSessionTagSweepStore(nil, nil)
	if got := st.ListTagSweepSessions(); len(got) != 0 {
		t.Fatalf("store with no source listed %d sessions, want none", len(got))
	}
	if err := st.SetSystemTags("s1", []string{"x"}); err != nil {
		t.Fatalf("SetSystemTags with no source = %v, want a nil error (nothing to write)", err)
	}
}

// TestTagSweepStore_ArchivedSessionsSwept covers the spec's "live and
// Archived" session pass through the production store: an Archived, Automata-
// owned session is listed with its placement rebuilt the same way a live
// one's is, gets re-derived under the current settings, is written through
// the any-writer the session manager provides, and on the second run is
// already at the derived set — a zero-changed no-op. the operator tags on
// the Archived record survive both runs.
func TestTagSweepStore_ArchivedSessionsSwept(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.Store().CreatePRD("spec", "/work/beta", "pi", "gpt", EffortMedium)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	src := newSweepSourceFake()
	src.archived = []*session.Session{
		{
			ID:             "a1",
			Name:           "autonomous:verify-round-1",
			PRDID:          prd.ID,
			ComputeNodeRef: "node-2",
			BackendFamily:  "pi",
			Model:          "gpt-5",
			ProjectDir:     "/work/beta",
			SystemTags: []string{
				"node:node-2",
				"backend:pi",
			},
			Tags: []string{"mine"},
		},
	}
	st := NewSessionTagSweepStore(m, src)
	listed := st.ListTagSweepSessions()
	if len(listed) != 1 || listed[0].ID != "a1" {
		t.Fatalf("listed %d sessions %+v, want the archived Automata-owned one", len(listed), listed)
	}
	m2, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m2.SetTagSweepSessions(st)
	if res := m2.SweepTags(false); res.Sessions != (SweepCount{Examined: 1, Changed: 1}) {
		t.Fatalf("first run = %+v, want one examined, one changed, no failed", res)
	}
	wantTags := DerivePlacementTags(listed[0].Placement, config.DefaultPlacementConfig())
	if !reflect.DeepEqual(src.session("a1").SystemTags, wantTags) {
		t.Fatalf("archived tags after sweep = %v, want %v", src.session("a1").SystemTags, wantTags)
	}
	if !reflect.DeepEqual(src.session("a1").Tags, []string{"mine"}) {
		t.Fatalf("archived operator tags after sweep = %v, want [mine]", src.session("a1").Tags)
	}
	if res := m2.SweepTags(false); res.Sessions != (SweepCount{Examined: 1, Unchanged: 1}) {
		t.Fatalf("second run = %+v, want a zero-changed no-op", res)
	}
	if n := len(src.writes); n != 1 || src.writes["a1"] != 1 {
		t.Fatalf("writes = %v, want exactly one for a1", src.writes)
	}
}

// TestTagSweepStore_NameRoleKind covers the role and kind the sweep reads
// from a session's name, in the launch vocabulary: the non-task roles a
// session can be run as (a review round, a decompose, a judge round, an
// auto-fix, a supervisor) each take their own word, a task worker's name
// takes the worker/task default, a name outside the launch vocabulary takes
// neither, and a name with no "autonomous:" prefix is not a session the
// sweep roles at all.
func TestTagSweepStore_NameRoleKind(t *testing.T) {
	cases := []struct {
		name     string
		wantRole string
		wantKind string
	}{
		{"autonomous:work", "worker", "task"},
		{"autonomous:verify-round-1", "verifier", "review"},
		{"autonomous:decompose", "planner", "decompose"},
		{"autonomous:plan-2", "planner", "decompose"},
		{"autonomous:judge-pass", "judge", "rejudge"},
		{"autonomous:rejudge-1", "judge", "rejudge"},
		{"autonomous:auto-fix-7", "worker", autoFixTaskKind},
		{"autonomous:supervisor-1", "supervisor", ""},
		{"autonomous:guardrail-check", "", ""},
		{"autonomous:council-1", "", ""},
		{"autonomous:eval-set", "", ""},
		{"my-ordinary-session", "", ""},
	}
	for _, tc := range cases {
		role, kind := sessionNameRoleKind(tc.name)
		if role != tc.wantRole || kind != tc.wantKind {
			t.Errorf("sessionNameRoleKind(%q) = (%q, %q), want (%q, %q)", tc.name, role, kind, tc.wantRole, tc.wantKind)
		}
	}
}

// placementSweepFixture is a manager holding one PRD inside one Automation at
// stage "build" — the stored facts a launch-derived or legacy session's
// Automaton axis is read from.
func placementSweepFixture(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.Store().CreatePRD("spec", "/work/alpha", "pi", "gpt", EffortMedium)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	auto := &Automation{ID: "auto-1", Name: "build-automation", Members: []AutomationMember{{PRDID: prd.ID, Stage: "build"}}}
	if _, err := m.automations.Create(auto); err != nil {
		t.Fatalf("automation create: %v", err)
	}
	return m, prd
}

// hasTagWithPrefix reports whether any tag in tags starts with prefix.
func hasTagWithPrefix(tags []string, prefix string) bool {
	for _, tg := range tags {
		if strings.HasPrefix(tg, prefix) {
			return true
		}
	}
	return false
}

// TestTagSweepStore_LaunchFactsSweepIsIdempotent covers a session whose tags
// were already launch-derived: the first sweep may settle them, and a second
// sweep over the same records changes nothing.
func TestTagSweepStore_LaunchFactsSweepIsIdempotent(t *testing.T) {
	m, prd := placementSweepFixture(t)
	src := newSweepSourceFake()
	src.sessions = []*session.Session{{
		ID:             "s1",
		Name:           "autonomous:build-the-thing",
		PRDID:          prd.ID,
		ComputeNodeRef: "node-1",
		BackendFamily:  "pi",
		Model:          "gpt-5",
		ProjectDir:     "/work/alpha",
		LaunchRole:     "worker",
		LaunchKind:     "task",
		LaunchStage:    "build",
	}}
	m.SetTagSweepSessions(NewSessionTagSweepStore(m, src))
	if res := m.SweepTags(false); res.Sessions.Failed != 0 {
		t.Fatalf("first sweep failed: %+v", res)
	}
	settled := append([]string(nil), src.session("s1").SystemTags...)
	if res := m.SweepTags(false); res.Sessions.Changed != 0 || res.Sessions.Failed != 0 || res.Sessions.Examined != 1 {
		t.Fatalf("second sweep = %+v, want one examined, none changed", res.Sessions)
	}
	if !reflect.DeepEqual(src.session("s1").SystemTags, settled) {
		t.Fatalf("second sweep changed launch-derived tags: %v, was %v", src.session("s1").SystemTags, settled)
	}
}

// TestTagSweepStore_NoPRDIDGetsNoAutomatonTag covers a launch-fact session
// that belongs to no Automaton: after a sweep it carries no automaton or
// automation tag, though its other launch-derived tags are written.
func TestTagSweepStore_NoPRDIDGetsNoAutomatonTag(t *testing.T) {
	m, _ := placementSweepFixture(t)
	src := newSweepSourceFake()
	src.sessions = []*session.Session{{
		ID:             "s1",
		Name:           "autonomous:verify-the-thing",
		SystemTags:     []string{"role:worker"},
		ComputeNodeRef: "node-1",
		BackendFamily:  "claude",
		LaunchRole:     "verifier",
		LaunchKind:     "review",
	}}
	m.SetTagSweepSessions(NewSessionTagSweepStore(m, src))
	if res := m.SweepTags(false); res.Sessions.Failed != 0 {
		t.Fatalf("sweep failed: %+v", res)
	}
	tags := src.session("s1").SystemTags
	if hasTagWithPrefix(tags, "automaton:") || hasTagWithPrefix(tags, "automation:") {
		t.Fatalf("session with no PRDID got an automaton/automation tag: %v", tags)
	}
	if !reflect.DeepEqual(tags, []string{"backend:claude", "compute:node-1", "kind:review", "lane:claude", "role:verifier"}) {
		t.Fatalf("launch-fact tags = %v", tags)
	}
}

// TestTagSweepStore_LaunchFactsKeepComputeTag covers a session with recorded
// launch facts: its compute tag (from its stored node) survives a sweep, and a
// session with no stored node takes the local node name instead of none.
func TestTagSweepStore_LaunchFactsKeepComputeTag(t *testing.T) {
	m, _ := placementSweepFixture(t)
	src := newSweepSourceFake()
	src.sessions = []*session.Session{
		{ID: "remote", Name: "autonomous:a", ComputeNodeRef: "node-1", BackendFamily: "pi", SystemTags: []string{"role:worker"}, LaunchRole: "worker", LaunchKind: "task"},
		{ID: "local", Name: "autonomous:b", BackendFamily: "pi", SystemTags: []string{"role:worker"}, LaunchRole: "worker", LaunchKind: "task"},
	}
	m.SetTagSweepSessions(NewSessionTagSweepStore(m, src))
	if res := m.SweepTags(false); res.Sessions.Failed != 0 {
		t.Fatalf("sweep failed: %+v", res)
	}
	if got := src.session("remote").SystemTags; !containsString(got, "compute:node-1") {
		t.Fatalf("remote launch-fact session lost its compute tag: %v", got)
	}
	if got := src.session("local").SystemTags; !containsString(got, "compute:local") {
		t.Fatalf("local launch-fact session got no compute:local tag: %v", got)
	}
}

// TestTagSweepStore_LegacySessionTagsUnchanged covers the compatibility rule:
// a session with no recorded launch facts (LaunchRole, LaunchKind and
// LaunchStage all empty) keeps the derivation the sweep gave before launch
// facts existed — its role and kind come from its name, its stage from its
// Automaton's membership, and it takes no local-node fallback.
func TestTagSweepStore_LegacySessionTagsUnchanged(t *testing.T) {
	m, prd := placementSweepFixture(t)
	src := newSweepSourceFake()
	src.sessions = []*session.Session{{
		ID:             "s1",
		Name:           "autonomous:build-the-thing",
		PRDID:          prd.ID,
		ComputeNodeRef: "node-1",
		BackendFamily:  "pi",
		Model:          "gpt-5",
		ProjectDir:     "/work/alpha",
	}}
	m.SetTagSweepSessions(NewSessionTagSweepStore(m, src))
	if res := m.SweepTags(false); res.Sessions.Failed != 0 {
		t.Fatalf("sweep failed: %+v", res)
	}
	want := DerivePlacementTags(Placement{
		Node:       "node-1",
		Backend:    "pi",
		Model:      "gpt-5",
		Lane:       "gpu",
		Project:    "alpha",
		Automaton:  prd.ID,
		Automation: "auto-1",
		Stage:      "build",
		Role:       "worker",
		Kind:       "task",
	}, config.DefaultPlacementConfig())
	if got := src.session("s1").SystemTags; !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy session tags = %v, want the pre-change derivation %v", got, want)
	}
}
