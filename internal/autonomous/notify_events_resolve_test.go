package autonomous

import (
	"encoding/json"
	"slices"
	"testing"
)

// TestResolveNotifyEvents_Precedence — one table covering
// Automaton-list × profile-list × global-list combinations:
//
//	Automaton: inherit (empty list) | own set
//	profile:   none | with events | empty list | name missing in store
//	global:    nil (unset → built-in default) | custom list | explicit empty
//
// Expected: the first non-empty tier at automaton → profile → global wins;
// a nil global resolves to the built-in DefaultNotifyEvents set and an
// explicit empty global resolves to no events; Source names the tier that
// produced the events. The profile store is real (NewManager + SaveGuardrailProfile).
func TestResolveNotifyEvents_Precedence(t *testing.T) {
	const profileName = "resolve-notify-profile"

	type autoMode struct {
		name string
		ev   []string
	}
	autos := []autoMode{
		{name: "auto_inherit", ev: nil},
		{name: "auto_set", ev: []string{"prd.blocked"}},
	}
	type profileMode struct {
		name string
		id   string
		ev   []string
		save bool
	}
	profiles := []profileMode{
		{name: "profile_none", id: ""},
		{name: "profile_events", id: profileName, ev: []string{"prd.failed"}, save: true},
		{name: "profile_empty", id: profileName, ev: []string{}, save: true},
		{name: "profile_missing", id: "no-such-profile"},
	}
	type globalMode struct {
		name string
		val  *[]string // nil pointer = unset field
	}
	globals := []globalMode{
		{name: "global_nil"},
		{name: "global_custom", val: ptrSlice([]string{"task.session_started"})},
		{name: "global_empty", val: ptrSlice([]string{})},
	}

	// expect is the spec-level computation of the expected outcome; it is
	// intentionally written separately from the resolver implementation.
	expect := func(auto []string, pm profileMode, gm globalMode) (ev []string, src string) {
		switch {
		case len(auto) > 0:
			return slices.Clone(auto), NotifySourceAutomaton
		case pm.save && len(pm.ev) > 0:
			return slices.Clone(pm.ev), NotifySourceProfile
		case gm.val == nil:
			return append([]string{}, DefaultNotifyEvents()...), NotifySourceGlobal
		case len(*gm.val) > 0:
			return append([]string{}, *gm.val...), NotifySourceGlobal
		default:
			return []string{}, NotifySourceGlobal // explicit empty global
		}
	}

	testcases := []struct {
		name    string
		auto    []string
		profile profileMode
		global  globalMode
		wantEv  []string
		wantSrc string
	}{}
	for _, am := range autos {
		for _, pm := range profiles {
			// profile_none/profile_missing do not depend on the profile store.
			if pm.name == "profile_events" || pm.name == "profile_empty" {
				am, pm := am, pm
				_ = am
				_ = pm
			}
			for _, gm := range globals {
				ev, src := expect(am.ev, pm, gm)
				testcases = append(testcases, struct {
					name    string
					auto    []string
					profile profileMode
					global  globalMode
					wantEv  []string
					wantSrc string
				}{
					name:    am.name + "_" + pm.name + "_" + gm.name,
					auto:    slices.Clone(am.ev),
					profile: pm,
					global:  gm,
					wantEv:  ev,
					wantSrc: src,
				})
			}
		}
	}
	if len(testcases) != len(autos)*len(profiles)*len(globals) {
		t.Fatalf("expected %d table rows, got %d", len(autos)*len(profiles)*len(globals), len(testcases))
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			if tc.profile.save {
				if err := m.Store().SaveGuardrailProfile(&GuardrailProfile{
					ID:           tc.profile.id,
					Name:         tc.profile.id,
					NotifyEvents: slices.Clone(tc.profile.ev),
				}); err != nil {
					t.Fatalf("SaveGuardrailProfile: %v", err)
				}
			}
			if tc.global.val != nil {
				cfg := m.Config()
				// slices.Clone preserves the nil vs explicit-empty distinction.
				cfg.NotifyDefaultEvents = slices.Clone(*tc.global.val)
				m.SetConfig(cfg)
			}
			prd := &PRD{
				ID:               "auto-resolve",
				NotifyEvents:     slices.Clone(tc.auto),
				GuardrailProfile: tc.profile.id,
			}
			got := m.ResolveNotifyEvents(prd)
			if got.Source != tc.wantSrc {
				t.Fatalf("Source = %q, want %q (events=%v)", got.Source, tc.wantSrc, got.Events)
			}
			if !slices.Equal(got.Events, tc.wantEv) {
				t.Fatalf("Events = %v, want %v", got.Events, tc.wantEv)
			}
		})
	}
}

func ptrSlice(s []string) *[]string { return &s }

// TestResolveNotifyEvents_NilAndCopies — a nil automaton resolves against
// the global tier, and every resolution hands out a fresh copy: mutating
// a result never affects the automaton's stored list, the profile's stored
// list, the global default, or a later (or earlier) resolution.
func TestResolveNotifyEvents_NilAndCopies(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Nil automaton → global tier (nil config → the built-in default set).
	got := m.ResolveNotifyEvents(nil)
	if got.Source != NotifySourceGlobal {
		t.Fatalf("nil automaton Source = %q, want %q", got.Source, NotifySourceGlobal)
	}
	if !slices.Equal(got.Events, DefaultNotifyEvents()) {
		t.Fatalf("nil automaton Events = %v, want %v", got.Events, DefaultNotifyEvents())
	}

	// Copy: mutating a result must not leak into the next resolution.
	got.Events[0] = "mutated"
	if second := m.ResolveNotifyEvents(nil); second.Events[0] != "prd.needs_review" {
		t.Fatalf("mutating a result leaked into a later resolution: %v", second.Events)
	}

	// Copy: the automaton tier never hands out the stored slice.
	if err := m.Store().SavePRD(&PRD{ID: "n1", NotifyEvents: []string{"prd.blocked", "prd.failed"}}); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	r1, ok := m.ResolveNotifyEventsFor("n1")
	if !ok {
		t.Fatalf("ResolveNotifyEventsFor(n1) not found")
	}
	if r1.Source != NotifySourceAutomaton {
		t.Fatalf("n1 Source = %q, want %q", r1.Source, NotifySourceAutomaton)
	}
	r1.Events[0] = "mutated"
	stored, ok := m.Store().GetPRD("n1")
	if !ok {
		t.Fatalf("GetPRD(n1) not found")
	}
	if stored.NotifyEvents[0] != "prd.blocked" {
		t.Fatalf("resolver aliased the automaton's stored list: %v", stored.NotifyEvents)
	}
	// Two resolutions do not share a backing slice either: r1 has already
	// been mutated above, so check that r2 is pristine and that mutating r2
	// does not reach into r1's backing array.
	r2, ok := m.ResolveNotifyEventsFor("n1")
	if !ok {
		t.Fatalf("ResolveNotifyEventsFor(n1) not found (2nd)")
	}
	if r2.Events[0] != "prd.blocked" || r2.Events[1] != "prd.failed" {
		t.Fatalf("r1's mutation leaked into a later resolution: %v", r2.Events)
	}
	back1 := append([]string{}, r1.Events...)
	r2.Events[0] = "mutated"
	if !slices.Equal(r1.Events, back1) {
		t.Fatalf("resolutions shared a backing slice: %v", r1.Events)
	}

	// Copy: the profile tier never hands out the stored profile slice.
	if err := m.Store().SaveGuardrailProfile(&GuardrailProfile{
		ID: "copy-check", Name: "copy-check", NotifyEvents: []string{"prd.completed"},
	}); err != nil {
		t.Fatalf("SaveGuardrailProfile: %v", err)
	}
	if err := m.Store().SavePRD(&PRD{ID: "n2", GuardrailProfile: "copy-check"}); err != nil {
		t.Fatalf("SavePRD(n2): %v", err)
	}
	rp, ok := m.ResolveNotifyEventsFor("n2")
	if !ok {
		t.Fatalf("ResolveNotifyEventsFor(n2) not found")
	}
	if rp.Source != NotifySourceProfile {
		t.Fatalf("n2 Source = %q, want %q", rp.Source, NotifySourceProfile)
	}
	rp.Events[0] = "mutated"
	pstored, ok := m.Store().GetGuardrailProfile("copy-check")
	if !ok {
		t.Fatalf("GetGuardrailProfile(copy-check) not found")
	}
	if pstored.NotifyEvents[0] != "prd.completed" {
		t.Fatalf("resolver aliased the profile's stored list: %v", pstored.NotifyEvents)
	}

	// Missing automaton.
	if _, ok := m.ResolveNotifyEventsFor("no-such-automaton"); ok {
		t.Fatalf("ResolveNotifyEventsFor(no-such) = found, want not found")
	}
}

// TestResolveNotifyEvents_LiveConfigChange — the global tier follows
// Manager.SetConfig live (Manager.SetConfig and the live PUT /api/config
// path, both), and the explicit-empty / custom / restore sequence holds.
func TestResolveNotifyEvents_LiveConfigChange(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Baseline (nil field → built-in default).
	if got := m.ResolveNotifyEvents(nil); !slices.Equal(got.Events, DefaultNotifyEvents()) || got.Source != NotifySourceGlobal {
		t.Fatalf("baseline = %v (source %q), want %v (global)", got.Events, got.Source, DefaultNotifyEvents())
	}

	// Live custom list through the Manager API.
	cfg := m.Config()
	cfg.NotifyDefaultEvents = []string{"task.session_started", "prd.completed"}
	m.SetConfig(cfg)
	got := m.ResolveNotifyEvents(nil)
	if !slices.Equal(got.Events, []string{"task.session_started", "prd.completed"}) || got.Source != NotifySourceGlobal {
		t.Fatalf("after live custom = %v (source %q), want the custom list (global)", got.Events, got.Source)
	}

	// Explicit empty through the live PUT /api/config shape
	// (autonomous.API.SetConfig with the nested "notify" object lifted to
	// flat NotifyDefaultEvents).
	a := &API{M: m}
	if err := a.SetConfig(json.RawMessage(`{"notify":{"default_events":[]}}`)); err != nil {
		t.Fatalf("API.SetConfig(empty): %v", err)
	}
	if got := m.ResolveNotifyEvents(nil); got.Source != NotifySourceGlobal || len(got.Events) != 0 {
		t.Fatalf("after live empty = %v (source %q), want no events (global)", got.Events, got.Source)
	}
	if v := m.Config().NotifyDefaultEvents; v == nil {
		t.Fatalf("live empty collapsed to nil in the manager config")
	}

	// Live custom list through the same nested-object shape.
	if err := a.SetConfig(json.RawMessage(`{"notify":{"default_events":["prd.failed"],"pending_events_max":100}}`)); err != nil {
		t.Fatalf("API.SetConfig(custom): %v", err)
	}
	if got := m.ResolveNotifyEvents(nil); !slices.Equal(got.Events, []string{"prd.failed"}) || got.Source != NotifySourceGlobal {
		t.Fatalf("after live custom via API = %v (source %q), want [prd.failed] (global)", got.Events, got.Source)
	}

	// Restoring the unset state (nil) returns to the built-in default.
	cfg = m.Config()
	cfg.NotifyDefaultEvents = nil
	m.SetConfig(cfg)
	if got := m.ResolveNotifyEvents(nil); !slices.Equal(got.Events, DefaultNotifyEvents()) || got.Source != NotifySourceGlobal {
		t.Fatalf("after restore = %v (source %q), want the built-in default (global)", got.Events, got.Source)
	}
}
