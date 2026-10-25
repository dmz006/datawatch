package autonomous

import (
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"
)

// tagEventLog records the tag events a Manager delivers through
// SetTagEventFn. The callback runs synchronously, so no locking is needed.
type tagEventLog struct{ events []TagEvent }

func newTagEventLog(m *Manager) *tagEventLog {
	l := &tagEventLog{}
	m.SetTagEventFn(func(ev TagEvent) { l.events = append(l.events, ev) })
	return l
}

// take returns the events recorded since the last call and clears the log.
func (l *tagEventLog) take() []TagEvent {
	out := l.events
	l.events = nil
	return out
}

func requireTagEvents(t *testing.T, got []TagEvent, want ...TagEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("tag events = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tag event[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// jsonKeys returns the sorted JSON field names of v.
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestTagEventRegistryChangesEmitOncePerOperation(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	log := newTagEventLog(m)

	parent, err := m.CreateTag("work", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := m.CreateTag("client", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	requireTagEvents(t, log.take(),
		TagEvent{Kind: TagEventCreated, TagID: parent.ID},
		TagEvent{Kind: TagEventCreated, TagID: child.ID})

	if _, err := m.RenameTag(parent.ID, "office"); err != nil {
		t.Fatal(err)
	}
	requireTagEvents(t, log.take(), TagEvent{Kind: TagEventRenamed, TagID: parent.ID})

	if _, err := m.SetTagColor(parent.ID, "#336699"); err != nil {
		t.Fatal(err)
	}
	requireTagEvents(t, log.take(), TagEvent{Kind: TagEventRecolored, TagID: parent.ID})

	if _, err := m.SetTagParent(child.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	requireTagEvents(t, log.take(), TagEvent{Kind: TagEventReparented, TagID: child.ID})

	if err := m.DeleteTag(child.ID); err != nil {
		t.Fatal(err)
	}
	requireTagEvents(t, log.take(), TagEvent{Kind: TagEventDeleted, TagID: child.ID})
}

func TestTagEventDescriptionChangeEmitsNothing(t *testing.T) {
	// A description never changes a count, so it does not refresh counts.
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	tag, _ := m.CreateTag("work", "", "", "")
	log := newTagEventLog(m)

	if _, err := m.SetTagDescription(tag.ID, "day job"); err != nil {
		t.Fatal(err)
	}
	requireTagEvents(t, log.take())
}

func TestTagEventRefusedOperationsEmitNothing(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	parent, _ := m.CreateTag("work", "", "", "")
	child, _ := m.CreateTag("client", parent.ID, "", "")
	prd, err := m.CreatePRD("p", "/p", "claude", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	log := newTagEventLog(m)

	refusals := map[string]func() error{
		"cycle: parent under its child": func() error {
			_, err := m.SetTagParent(parent.ID, child.ID)
			return err
		},
		"cycle: tag under itself": func() error {
			_, err := m.SetTagParent(parent.ID, parent.ID)
			return err
		},
		"delete with child tags": func() error { return m.DeleteTag(parent.ID) },
		"rename a missing tag": func() error {
			_, err := m.RenameTag("no-such-tag", "x")
			return err
		},
		"create with an empty name": func() error {
			_, err := m.CreateTag("  ", "", "", "")
			return err
		},
		"assign a missing tag": func() error { return m.SetPRDTags(prd.ID, []string{"no-such-tag"}) },
	}
	for name, op := range refusals {
		if err := op(); err == nil {
			t.Fatalf("%s: operation succeeded, want a refusal", name)
		}
		if got := log.take(); len(got) != 0 {
			t.Errorf("%s: emitted %+v, want nothing", name, got)
		}
	}

	if err := m.SetPRDTags(prd.ID, []string{child.ID}); err != nil {
		t.Fatal(err)
	}
	log.take()
	if err := m.DeleteTag(child.ID); !errors.Is(err, ErrTagInUse) {
		t.Fatalf("delete an assigned tag: err = %v, want ErrTagInUse", err)
	}
	if got := log.take(); len(got) != 0 {
		t.Errorf("delete refused as in use: emitted %+v, want nothing", got)
	}
}

func TestTagEventAssignmentEmitsAssignedForTheAutomaton(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	tag, _ := m.CreateTag("work", "", "", "")
	prd, _ := m.CreatePRD("p", "/p", "claude", "", EffortNormal)
	log := newTagEventLog(m)

	if err := m.SetPRDTags(prd.ID, []string{tag.ID}); err != nil {
		t.Fatal(err)
	}
	requireTagEvents(t, log.take(), TagEvent{Kind: TagEventAssigned, PRDID: prd.ID})

	primary := tag.ID
	if err := m.SetPRDTagsAndPrimary(prd.ID, []string{tag.ID}, &primary); err != nil {
		t.Fatal(err)
	}
	requireTagEvents(t, log.take(), TagEvent{Kind: TagEventAssigned, PRDID: prd.ID})
}

func TestTagEventPayloadCarriesIDsOnly(t *testing.T) {
	cases := []struct {
		ev   TagEvent
		keys []string
	}{
		{TagEvent{Kind: TagEventCreated, TagID: "t1"}, []string{"kind", "tag_id"}},
		{TagEvent{Kind: TagEventRenamed, TagID: "t1"}, []string{"kind", "tag_id"}},
		{TagEvent{Kind: TagEventReparented, TagID: "t1"}, []string{"kind", "tag_id"}},
		{TagEvent{Kind: TagEventRecolored, TagID: "t1"}, []string{"kind", "tag_id"}},
		{TagEvent{Kind: TagEventDeleted, TagID: "t1"}, []string{"kind", "tag_id"}},
		{TagEvent{Kind: TagEventAssigned, PRDID: "p1"}, []string{"kind", "prd_id"}},
		{TagEvent{Kind: TagEventSessionAssigned, SessionID: "host-s1"}, []string{"kind", "session_id"}},
	}
	for _, tc := range cases {
		got := jsonKeys(t, tc.ev)
		if len(got) != len(tc.keys) {
			t.Errorf("%s: keys = %v, want %v", tc.ev.Kind, got, tc.keys)
			continue
		}
		for i := range got {
			if got[i] != tc.keys[i] {
				t.Errorf("%s: keys = %v, want %v", tc.ev.Kind, got, tc.keys)
				break
			}
		}
	}
}

func TestTagEventIsNotALifecycleEvent(t *testing.T) {
	// Tag changes go through the tag callback only. A wildcard lifecycle
	// subscriber must not receive them.
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	c := &collector{}
	cancel := m.Subscribe(LifecycleFilter{}, c.add)
	t.Cleanup(cancel)
	newTagEventLog(m)

	tag, _ := m.CreateTag("work", "", "", "")
	if _, err := m.RenameTag(tag.ID, "office"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, ev := range c.snapshot() {
		if ev.Kind == KindPRDUpdated {
			t.Errorf("tag change produced a prd.updated lifecycle event: %+v", ev)
		}
	}
	if n := len(c.snapshot()); n != 0 {
		t.Errorf("wildcard lifecycle subscriber saw %d events from tag changes, want 0", n)
	}
}

func TestTagEventWithoutCallbackIsSafe(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	if _, err := m.CreateTag("work", "", "", ""); err != nil {
		t.Fatalf("create without a tag callback: %v", err)
	}
}

// waitForPRDUpdates waits until the prd.updated count reaches want, then
// checks it stays there, so a duplicate frame would fail the test.
func waitForPRDUpdates(t *testing.T, c *collector, want int) []LifecycleEvent {
	t.Helper()
	waitFor(t, "prd.updated", func() bool { return len(c.snapshot()) >= want })
	time.Sleep(50 * time.Millisecond)
	got := c.snapshot()
	if len(got) != want {
		t.Fatalf("prd.updated count = %d, want %d: %+v", len(got), want, got)
	}
	return got
}

func TestTagEventStatusChangesEmitPRDUpdate(t *testing.T) {
	// Each status is driven through the code path that writes it, so the
	// test covers the frame each operator or internal transition sends.
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	api := NewAPI(m)
	c := prdUpdated(t, m)

	// paused: the operator Pause transition, on a running Automaton.
	prd, err := m.CreatePRD("p", "/p", "claude", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	prd.Status = PRDRunning
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Pause(prd.ID, "test"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got := waitForPRDUpdates(t, c, 1)
	if got[0].PRDID != prd.ID || got[0].NewStatus != string(PRDPaused) {
		t.Errorf("paused: prd.updated = %+v, want prd %s status paused", got[0], prd.ID)
	}

	// blocked: the failure-escalation hold that parks a task for review.
	held, err := m.CreatePRD("held", "/p", "claude", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	m.blockForTaskReview(held, "task-1")
	got = waitForPRDUpdates(t, c, 2)
	if got[1].PRDID != held.ID || got[1].NewStatus != string(PRDBlocked) {
		t.Errorf("blocked: prd.updated = %+v, want prd %s status blocked", got[1], held.ID)
	}

	// needs_review: the operator Decompose transition.
	draft, err := m.CreatePRD("draft", "/p", "claude", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Decompose(draft.ID); err != nil {
		t.Fatalf("decompose: %v", err)
	}
	got = waitForPRDUpdates(t, c, 3)
	if got[2].PRDID != draft.ID || got[2].NewStatus != string(PRDNeedsReview) {
		t.Errorf("needs_review: prd.updated = %+v, want prd %s status needs_review", got[2], draft.ID)
	}
}

func TestTagEventBlockedTransitionSendsOneUpdate(t *testing.T) {
	// One blocked transition sends one prd.updated: the status event and
	// the WS frame are separate, and the frame is sent once.
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.CreatePRD("p", "/p", "claude", "", EffortNormal)
	c := prdUpdated(t, m)

	m.blockForTaskReview(prd, "task-1")
	waitForPRDUpdates(t, c, 1)
}
