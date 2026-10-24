package autonomous

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

// TestNotifyEventsEffective_Sources — the read-time copy of a PRD carries
// notify_events_effective for each of the three sources (automaton list,
// guardrail-profile list, global tier — unset and explicitly empty), and
// filling never mutates the stored PRD.
func TestNotifyEventsEffective_Sources(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Store().SaveGuardrailProfile(&GuardrailProfile{
		ID: "eff-pf", Name: "eff-pf", NotifyEvents: []string{"prd.failed"},
	}); err != nil {
		t.Fatalf("SaveGuardrailProfile: %v", err)
	}

	tests := []struct {
		name string
		prd  *PRD
		want NotifyEventSet
	}{
		{
			name: "automaton_source",
			prd:  &PRD{ID: "eff-auto", NotifyEvents: []string{"prd.blocked"}},
			want: NotifyEventSet{Events: []string{"prd.blocked"}, Source: NotifySourceAutomaton},
		},
		{
			name: "profile_source",
			prd:  &PRD{ID: "eff-profile", GuardrailProfile: "eff-pf"},
			want: NotifyEventSet{Events: []string{"prd.failed"}, Source: NotifySourceProfile},
		},
		{
			// unset global tier → the built-in default set.
			name: "global_source_unset",
			prd:  &PRD{ID: "eff-global"},
			want: NotifyEventSet{Events: DefaultNotifyEvents(), Source: NotifySourceGlobal},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.prd.NotifyEventsEffective != nil {
				t.Fatalf("input PRD already carries the fill: %+v", tc.prd.NotifyEventsEffective)
			}
			got := m.PRDWithNotifyEffective(tc.prd)
			if got == tc.prd {
				t.Fatalf("PRDWithNotifyEffective must return a copy, got the input pointer")
			}
			eff := got.NotifyEventsEffective
			if eff == nil {
				t.Fatalf("copy has a nil notify_events_effective")
			}
			if eff.Source != tc.want.Source {
				t.Fatalf("source = %q, want %q (events=%v)", eff.Source, tc.want.Source, eff.Events)
			}
			if !slices.Equal(eff.Events, tc.want.Events) {
				t.Fatalf("events = %v, want %v", eff.Events, tc.want.Events)
			}
			if tc.prd.NotifyEventsEffective != nil {
				t.Fatalf("input PRD was mutated by the fill: %+v", tc.prd.NotifyEventsEffective)
			}
		})
	}

	t.Run("global_source_explicit_empty", func(t *testing.T) {
		cfg := m.Config()
		cfg.NotifyDefaultEvents = []string{}
		m.SetConfig(cfg)
		got := m.PRDWithNotifyEffective(&PRD{ID: "eff-empty"})
		eff := got.NotifyEventsEffective
		if eff == nil {
			t.Fatalf("copy has a nil notify_events_effective")
		}
		if eff.Source != NotifySourceGlobal {
			t.Fatalf("source = %q, want %q", eff.Source, NotifySourceGlobal)
		}
		if eff.Events == nil {
			t.Fatalf("explicit-empty global must resolve to an empty list, not nil")
		}
		if len(eff.Events) != 0 {
			t.Fatalf("explicit-empty global resolved to %v, want []", eff.Events)
		}
	})
}

// TestNotifyEventsEffective_NotPersisted — the fill only exists on
// read-time copies: a PRD saved (or re-read from disk) never carries the
// field, even when the object saved came in with the field filled.
func TestNotifyEventsEffective_NotPersisted(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Store().SavePRD(&PRD{ID: "np", Status: PRDRunning, NotifyEvents: []string{"prd.blocked"}}); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	api := &API{M: m}
	read, ok := api.GetPRD("np")
	if !ok {
		t.Fatalf("GetPRD(np) not found")
	}
	filled, ok := read.(*PRD)
	if !ok {
		t.Fatalf("GetPRD returned %T, want *PRD", read)
	}
	if filled.NotifyEventsEffective == nil || filled.NotifyEventsEffective.Source != NotifySourceAutomaton {
		t.Fatalf("read copy missing the fill: %+v", filled.NotifyEventsEffective)
	}

	// Saving the read-time copy must drop the fill before persist.
	if err := m.Store().SavePRD(filled); err != nil {
		t.Fatalf("SavePRD(filled): %v", err)
	}
	stored, ok := m.Store().GetPRD("np")
	if !ok {
		t.Fatalf("stored np vanished after save")
	}
	if stored.NotifyEventsEffective != nil {
		t.Fatalf("store returned a PRD carrying the computed field: %+v", stored.NotifyEventsEffective)
	}
	b, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("json.Marshal(stored): %v", err)
	}
	if bytes.Contains(b, []byte("notify_events_effective")) {
		t.Fatalf("persisted record JSON carries the key: %s", b)
	}

	// Re-open the store from disk: a freshly loaded PRD must be clean too.
	m2, err := NewManager(dir, DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager(reopen): %v", err)
	}
	loaded, ok := m2.Store().GetPRD("np")
	if !ok {
		t.Fatalf("reopened store lost np")
	}
	if loaded.NotifyEventsEffective != nil {
		t.Fatalf("reopened store returned the computed field: %+v", loaded.NotifyEventsEffective)
	}
	// The list endpoint's fill is additive and must not leak into the store.
	list := m2.PRDsWithNotifyEffective(m2.Store().ListPRDs())
	if len(list) != 1 {
		t.Fatalf("list = %d rows, want 1", len(list))
	}
	if row, ok := list[0].(*PRD); !ok || row.NotifyEventsEffective == nil {
		t.Fatalf("list row %T missing the fill", list[0])
	}
	recheck, ok := m2.Store().GetPRD("np")
	if !ok {
		t.Fatalf("reopened store lost np after listing")
	}
	if recheck.NotifyEventsEffective != nil {
		t.Fatalf("listing with the fill leaked into the store")
	}
}

// TestNotifyStatus_ListsOverrides — the status loop's notify block lists
// each non-finished Automaton whose events come from the automaton or
// profile tier (sorted by ID), with DefaultEvents as the resolved global
// tier; global-sourced and finished Automata are omitted. Both keys are
// always lists, never null.
func TestNotifyStatus_ListsOverrides(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := m.Store().SaveGuardrailProfile(&GuardrailProfile{
		ID: "ns-pf", Name: "ns-pf", NotifyEvents: []string{"prd.failed"},
	}); err != nil {
		t.Fatalf("SaveGuardrailProfile: %v", err)
	}
	for _, p := range []*PRD{
		{ID: "ns-auto", Status: PRDRunning, NotifyEvents: []string{"prd.blocked"}},
		{ID: "ns-profile", Status: PRDRunning, GuardrailProfile: "ns-pf"},
		{ID: "ns-inherit", Status: PRDRunning},                                            // resolves to the global tier → not an override
		{ID: "ns-completed", Status: PRDCompleted, NotifyEvents: []string{"prd.blocked"}}, // finished → omitted
		{ID: "ns-cancelled", Status: PRDCancelled, GuardrailProfile: "ns-pf"},
		{ID: "ns-failed", Status: PRDFailed, NotifyEvents: []string{"prd.blocked"}},
		{ID: "ns-rejected", Status: PRDRejected, GuardrailProfile: "ns-pf"},
		{ID: "ns-archived", Status: PRDArchived, GuardrailProfile: "ns-pf"},
	} {
		if err := m.Store().SavePRD(p); err != nil {
			t.Fatalf("SavePRD(%s): %v", p.ID, err)
		}
	}

	st := m.Status()
	if st.Notify == nil {
		t.Fatalf("status missing the notify block")
	}
	if st.Notify.DefaultEvents == nil {
		t.Fatalf("default_events must be a list, never null")
	}
	if !slices.Equal(st.Notify.DefaultEvents, DefaultNotifyEvents()) {
		t.Fatalf("default_events = %v, want the built-in default %v", st.Notify.DefaultEvents, DefaultNotifyEvents())
	}
	if len(st.Notify.Overrides) != 2 {
		t.Fatalf("overrides = %d, want 2 (ns-auto, ns-profile): %+v", len(st.Notify.Overrides), st.Notify.Overrides)
	}
	if st.Notify.Overrides[0].ID != "ns-auto" || st.Notify.Overrides[0].Source != NotifySourceAutomaton {
		t.Fatalf("override[0] = %+v, want ns-auto (automaton)", st.Notify.Overrides[0])
	}
	if !slices.Equal(st.Notify.Overrides[0].Events, []string{"prd.blocked"}) {
		t.Fatalf("override[0].events = %v, want [prd.blocked]", st.Notify.Overrides[0].Events)
	}
	if st.Notify.Overrides[1].ID != "ns-profile" || st.Notify.Overrides[1].Source != NotifySourceProfile {
		t.Fatalf("override[1] = %+v, want ns-profile (profile)", st.Notify.Overrides[1])
	}
	if !slices.Equal(st.Notify.Overrides[1].Events, []string{"prd.failed"}) {
		t.Fatalf("override[1].events = %v, want [prd.failed]", st.Notify.Overrides[1].Events)
	}
	for _, ov := range st.Notify.Overrides {
		if ov.Events == nil {
			t.Fatalf("override %s has nil events; must be a list", ov.ID)
		}
	}

	// Explicit-empty global tier: default_events stays [] (a list, never
	// null), and the overrides are unchanged.
	cfg := m.Config()
	cfg.NotifyDefaultEvents = []string{}
	m.SetConfig(cfg)
	st = m.Status()
	if st.Notify == nil {
		t.Fatalf("status missing the notify block (2nd)")
	}
	if st.Notify.DefaultEvents == nil || len(st.Notify.DefaultEvents) != 0 {
		t.Fatalf("explicit-empty global rendered default_events as %v; want an empty list", st.Notify.DefaultEvents)
	}
	if len(st.Notify.Overrides) != 2 {
		t.Fatalf("overrides after empty global = %d, want 2", len(st.Notify.Overrides))
	}
}

// TestNotifyStatus_Cap — the override list is capped at
// NotifyStatusMaxOverrides entries, keeping the first entries in sorted ID
// order.
func TestNotifyStatus_Cap(t *testing.T) {
	const n = NotifyStatusMaxOverrides + 50
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("cap-%03d", i)
		if err := m.Store().SavePRD(&PRD{ID: id, Status: PRDRunning, NotifyEvents: []string{"prd.blocked"}}); err != nil {
			t.Fatalf("SavePRD(%s): %v", id, err)
		}
	}
	st := m.Status()
	if st.Notify == nil {
		t.Fatalf("status missing the notify block")
	}
	if len(st.Notify.Overrides) != NotifyStatusMaxOverrides {
		t.Fatalf("overrides = %d, want the cap %d", len(st.Notify.Overrides), NotifyStatusMaxOverrides)
	}
	if st.Notify.Overrides[0].ID != "cap-000" {
		t.Fatalf("overrides[0] = %q, want cap-000 (sorted ID order)", st.Notify.Overrides[0].ID)
	}
	if st.Notify.Overrides[NotifyStatusMaxOverrides-1].ID != "cap-099" {
		t.Fatalf("last override = %q, want cap-099", st.Notify.Overrides[NotifyStatusMaxOverrides-1].ID)
	}
}
