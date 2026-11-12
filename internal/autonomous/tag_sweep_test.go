package autonomous

import (
	"reflect"
	"sort"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// fakeSweepSessionStore is a TagSweepSessionStore for the tag-sweep tests:
// the fake's operatorTags is a separate list the fake never touches (the
// session's operator Tags field, which the real store also holds apart from
// the derived system tags), and SetSystemTags replaces only systemTags and
// records the write. If the sweep writes, stored.systemTags is changed
// while stored.operatorTags survives.
type fakeSweepSessionStore struct {
	sessions map[string]*fakeSweepSession
}

type fakeSweepSession struct {
	id           string
	operatorTags []string
	systemTags   []string
	placement    Placement
	writes       int  // how many times SetSystemTags wrote it
	fail         bool // force every SetSystemTags write to fail
	failWrites   int  // number of failed writes so far
}

func newFakeSweepSessionStore() *fakeSweepSessionStore {
	return &fakeSweepSessionStore{sessions: map[string]*fakeSweepSession{}}
}

func (f *fakeSweepSessionStore) add(id string, placement Placement, systemTags, operatorTags []string) *fakeSweepSession {
	s := &fakeSweepSession{id: id, placement: placement, systemTags: copyTags(systemTags), operatorTags: copyTags(operatorTags)}
	f.sessions[id] = s
	return s
}

func (f *fakeSweepSessionStore) get(id string) *fakeSweepSession {
	return f.sessions[id]
}

func (f *fakeSweepSessionStore) ListTagSweepSessions() []TagSweepSession {
	ids := make([]string, 0, len(f.sessions))
	for id := range f.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic order for the counts the tests assert
	out := make([]TagSweepSession, 0, len(f.sessions))
	for _, id := range ids {
		s := f.sessions[id]
		out = append(out, TagSweepSession{ID: id, SystemTags: copyTags(s.systemTags), Placement: s.placement})
	}
	return out
}

func (f *fakeSweepSessionStore) SetSystemTags(sessionID string, tags []string) error {
	s := f.sessions[sessionID]
	if s == nil {
		return ErrQuestionNotFound
	}
	if s.fail {
		s.failWrites++
		return ErrQuestionNotOpen
	}
	s.systemTags = copyTags(tags)
	s.writes++
	return nil
}

func copyTags(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func sessionPlacement() Placement {
	return Placement{Node: "node-a", Backend: "pi", Lane: "gpu", Role: "worker", Automaton: "pr-1"}
}

// TestTagSweep_AddsAxesToOldSessions covers the sweep applying the current
// placement settings to a session that was started before those axes (or
// before the sweep existed) and therefore carries no derived tags: after a
// write run the session holds the full derived placement tag set.
func TestTagSweep_AddsAxesToOldSessions(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	fake := newFakeSweepSessionStore()
	m.SetTagSweepSessions(fake)
	// systemTags empty: an old session that has never been tagged.
	pl := sessionPlacement()
	fake.add("s1", pl, nil, nil)

	res := m.SweepTags(false)
	if res.Sessions != (SweepCount{Examined: 1, Changed: 1, Unchanged: 0, Failed: 0}) {
		t.Fatalf("session count = %+v, want one examined, one changed", res.Sessions)
	}
	want := DerivePlacementTags(pl, config.DefaultPlacementConfig())
	if len(want) == 0 {
		t.Fatal("a non-empty placement must derive a non-empty tag set")
	}
	got := fake.get("s1").systemTags
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("session tags after sweep = %v, want %v", got, want)
	}
	for _, axis := range []string{"compute:node-a", "backend:pi", "lane:gpu", "role:worker", "automaton:pr-1"} {
		if !containsTag(got, axis) {
			t.Fatalf("session tags %v are missing %q", got, axis)
		}
	}
	if fake.get("s1").writes != 1 {
		t.Fatalf("the session was written %d times, want exactly one", fake.get("s1").writes)
	}
}

// TestTagSweep_DisabledAxisRemoved covers flipping an axis off after a
// session already carries its tag: the sweep re-derives under the new block
// and drops the axis's tag while keeping the ones that survive.
func TestTagSweep_DisabledAxisRemoved(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	pc := config.DefaultPlacementConfig()
	pc.Lane.Enabled = false
	m.SetPlacementTagSettings(func() config.PlacementConfig { return pc })

	fake := newFakeSweepSessionStore()
	m.SetTagSweepSessions(fake)
	pl := Placement{Node: "node-a", Backend: "pi", Lane: "gpu", Role: "worker"}
	// The session was tagged with the lane still on; it carries the lane tag.
	fake.add("s1", pl, []string{"compute:node-a", "backend:pi", "lane:gpu", "role:worker"}, nil)

	res := m.SweepTags(false)
	if res.Sessions != (SweepCount{Examined: 1, Changed: 1, Unchanged: 0, Failed: 0}) {
		t.Fatalf("session count = %+v, want one examined, one changed", res.Sessions)
	}
	got := fake.get("s1").systemTags
	if !sweepTagSetEqual(got, []string{"compute:node-a", "backend:pi", "role:worker"}) {
		t.Fatalf("sweep kept or added the disabled lane tag: %v", got)
	}
	if containsTag(got, "lane:gpu") {
		t.Fatalf("disabled axis tag still present: %v", got)
	}
}

// TestTagSweep_SecondRunZeroChanged covers idempotence: a run whose every
// object already holds the derived set changes nothing and writes nothing,
// so a second run against a just-swept session is a no-op.
func TestTagSweep_SecondRunZeroChanged(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	fake := newFakeSweepSessionStore()
	m.SetTagSweepSessions(fake)
	pl := sessionPlacement()
	fake.add("s1", pl, nil, nil)
	first := m.SweepTags(false)
	if first.Sessions != (SweepCount{Examined: 1, Changed: 1, Unchanged: 0, Failed: 0}) {
		t.Fatalf("first run = %+v, want one changed", first.Sessions)
	}
	writesAfterFirst := fake.get("s1").writes

	second := m.SweepTags(false)
	if second.Sessions != (SweepCount{Examined: 1, Changed: 0, Unchanged: 1, Failed: 0}) {
		t.Fatalf("second run = %+v, want zero changed, one unchanged", second.Sessions)
	}
	if fake.get("s1").writes != writesAfterFirst {
		t.Fatalf("second run wrote the session (%d -> %d writes); it should have written nothing", writesAfterFirst, fake.get("s1").writes)
	}
}

// TestTagSweep_DryRunWritesNothing covers dryRun=true: it reports the
// would-be counts but performs no writes at all — a stale and a matching
// session are both read, the stale one is counted changed, and neither
// session's stored tags or write counter are touched.
func TestTagSweep_DryRunWritesNothing(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	fake := newFakeSweepSessionStore()
	m.SetTagSweepSessions(fake)
	pl := sessionPlacement()
	stale := fake.add("s-stale", pl, []string{"role:worker"}, nil)
	matching := fake.add("s-match", pl, DerivePlacementTags(pl, config.DefaultPlacementConfig()), nil)

	res := m.SweepTags(true)
	if res.Sessions != (SweepCount{Examined: 2, Changed: 1, Unchanged: 1, Failed: 0}) {
		t.Fatalf("dry-run session count = %+v, want 2 examined, 1 changed (the stale one), 1 unchanged", res.Sessions)
	}
	if stale.writes != 0 {
		t.Fatalf("dry run wrote the stale session %d times", stale.writes)
	}
	if matching.writes != 0 {
		t.Fatalf("dry run wrote the matching session %d times", matching.writes)
	}
	if !reflect.DeepEqual(stale.systemTags, []string{"role:worker"}) {
		t.Fatalf("dry run modified the stale session's tags: %v", stale.systemTags)
	}
	wantMatch := DerivePlacementTags(pl, config.DefaultPlacementConfig())
	if !reflect.DeepEqual(matching.systemTags, wantMatch) {
		t.Fatalf("dry run modified the matching session: %v", matching.systemTags)
	}
}

// TestTagSweep_DryRunWritesNothing_ObjectPasses extends the dry-run no-write
// guarantee to the object passes: a store's existing Automaton, task and
// Automation each have their derived tags out of date, and a dry run counts
// them all as changed without writing a single object.
func TestTagSweep_DryRunWritesNothing_ObjectPasses(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("stale objects", "project/example", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	auto, err := m.CreateAutomation(AutomationInput{Name: "stale arc", Members: []AutomationMember{{PRDID: prd.ID, Stage: "build"}}}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if auto.ID == "" {
		t.Fatal("CreateAutomation returned no id")
	}
	if err := m.Store().SaveTask(&Task{ID: "t1", PRDID: prd.ID}); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	// Under the default block both objects carry the automaton tag.
	fresh, _ := m.Store().GetPRD(prd.ID)
	if !containsTag(fresh.SystemTags, "automaton:"+prd.ID) {
		t.Fatalf("a consistent Automaton must carry its automaton tag, got %v", fresh.SystemTags)
	}

	// The operator switches off the Automaton axis: every stored object that
	// carries the automaton tag is now out of date, so a write run would drop
	// it and a dry run must count it as the would-be change without writing.
	pc := config.DefaultPlacementConfig()
	pc.Automaton.Enabled = false
	m.SetPlacementTagSettings(func() config.PlacementConfig { return pc })

	res := m.SweepTags(true)
	if res.Automata != (SweepCount{Examined: 1, Changed: 1, Unchanged: 0, Failed: 0}) {
		t.Fatalf("automata dry-run count = %+v, want one changed", res.Automata)
	}
	if res.Tasks != (SweepCount{Examined: 1, Changed: 1, Unchanged: 0, Failed: 0}) {
		t.Fatalf("tasks dry-run count = %+v, want one changed", res.Tasks)
	}
	if res.Automations != (SweepCount{Examined: 1, Changed: 0, Unchanged: 1, Failed: 0}) {
		t.Fatalf("automations dry-run count = %+v, want one unchanged", res.Automations)
	}
	// The dry run wrote nothing back: the Automaton and task still carry the
	// now-orphan automaton tag.
	if after, _ := m.Store().GetPRD(prd.ID); !containsTag(after.SystemTags, "automaton:"+prd.ID) {
		t.Fatalf("dry run rewrote the Automaton's tags: %v", after.SystemTags)
	}
	if afterT, _ := m.Store().GetTask("t1"); !containsTag(afterT.SystemTags, "automaton:"+prd.ID) {
		t.Fatalf("dry run rewrote the task's tags: %v", afterT.SystemTags)
	}
}

// TestTagSweep_OperatorTagsSurvive covers the guarantee the sweep never
// touches the operator's own tags: the sweep only ever writes the derived
// (system) tags, so after writing the session holds the newly derived tag
// set while the operator tags it started with are untouched by name and
// order.
func TestTagSweep_OperatorTagsSurvive(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	fake := newFakeSweepSessionStore()
	m.SetTagSweepSessions(fake)
	pl := sessionPlacement()
	operator := []string{"my-operator-tag", "another-operator-tag"}
	fake.add("s1", pl, []string{"role:worker"}, operator) // stale system tag + operator tags

	if res := m.SweepTags(false); res.Sessions != (SweepCount{Examined: 1, Changed: 1, Unchanged: 0, Failed: 0}) {
		t.Fatalf("session count = %+v, want one examined, one changed", res.Sessions)
	}
	want := DerivePlacementTags(pl, config.DefaultPlacementConfig())
	if !reflect.DeepEqual(fake.get("s1").systemTags, want) {
		t.Fatalf("system tags after sweep = %v, want %v", fake.get("s1").systemTags, want)
	}
	// The operator tags survive the sweep entirely.
	if !reflect.DeepEqual(fake.get("s1").operatorTags, operator) {
		t.Fatalf("operator tags = %v, want %v (the sweep must not touch them)", fake.get("s1").operatorTags, operator)
	}
}

// TestTagSweep_FailedWriteCountsFailed covers the failed bucket: a write
// that errors out is counted as failed (not changed) and the object keeps
// its prior tags.
func TestTagSweep_FailedWriteCountsFailed(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	fake := newFakeSweepSessionStore()
	m.SetTagSweepSessions(fake)
	pl := sessionPlacement()
	s := fake.add("s1", pl, []string{"role:worker"}, nil)
	s.fail = true

	res := m.SweepTags(false)
	if res.Sessions != (SweepCount{Examined: 1, Changed: 0, Unchanged: 0, Failed: 1}) {
		t.Fatalf("session count = %+v, want the failed write counted as failed", res.Sessions)
	}
	if s.systemTags[0] != "role:worker" {
		t.Fatalf("a failed write must leave the prior tags in place, got %v", s.systemTags)
	}
}

// containsTag reports whether tags contains the named derived tag.
func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// sweepTagSetEqual compares two derived tag lists as sets: the sweep derives
// a sorted, de-duplicated list, so order is not part of equality.
func sweepTagSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := make([]string, len(a))
	copy(aa, a)
	sort.Strings(aa)
	bb := make([]string, len(b))
	copy(bb, b)
	sort.Strings(bb)
	return reflect.DeepEqual(aa, bb)
}
