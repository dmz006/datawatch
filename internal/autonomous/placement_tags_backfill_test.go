package autonomous

import (
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// backfillLedger is the fake capacity ledger for the startup backfill tests:
// it hands out a fixed lease list, the shape the real ledger's Leases() has.
type backfillLedger struct {
	leases []capacity.Lease
}

func (f *backfillLedger) Leases() []capacity.Lease {
	return append([]capacity.Lease(nil), f.leases...)
}

// backfillStore is the fake session store for the startup backfill tests.
// GetSession returns the exact *session.Session the fake holds, and
// SetSystemTags replaces SystemTags on it — the same split session.Manager
// uses (its operator Tags list is left alone here on purpose: the backfill
// must never touch it).
type backfillStore struct {
	mu       sync.Mutex
	sessions map[string]*session.Session
	writes   []string // session IDs the backfill wrote, in call order
}

func (f *backfillStore) GetSession(id string) (*session.Session, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	return s, ok
}

func (f *backfillStore) SetSystemTags(id string, tags []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[id]; ok {
		s.SystemTags = append([]string(nil), tags...)
	}
	f.writes = append(f.writes, id)
	return nil
}

// put registers a session (full ID) in the fake store.
func (f *backfillStore) put(id string, st session.State, systemTags []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions == nil {
		f.sessions = map[string]*session.Session{}
	}
	f.sessions[id] = &session.Session{FullID: id, State: st, SystemTags: append([]string(nil), systemTags...)}
}

func (f *backfillStore) systemTagsOf(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[id]; ok {
		return append([]string(nil), s.SystemTags...)
	}
	return nil
}

func (f *backfillStore) writeIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

// backfillPlacementConfig is the live tags.placement block the tests run
// under: the defaults (compute on) plus two custom axes that expose the two
// lease facts a backfill knows — the model and the PRD — so the derived set
// is not a single tag.
func backfillPlacementConfig() config.PlacementConfig {
	pc := config.DefaultPlacementConfig()
	pc.Custom = []config.PlacementCustomAxis{
		{Name: "model", Field: "Model", Template: "model:{value}", Enabled: true},
		{Name: "prd", Field: "PRDID", Template: "prd:{value}", Enabled: true},
	}
	return pc
}

var leaseForSess1 = capacity.Lease{
	Holder:    "task-prd1-t1",
	PRDID:     "prd1",
	Node:      "node-a",
	Model:     "qwen3:8b",
	SessionID: "sess1",
	Pools:     []string{"node:node-a"},
}

func TestPlacementTags_StartupBackfill_FillsMissing(t *testing.T) {
	store := &backfillStore{}
	store.put("sess1", session.StateRunning, nil) // started before the placement-tag feature; no system tags

	led := &backfillLedger{leases: []capacity.Lease{leaseForSess1}}
	res := BackfillPlacementTags(led, store, backfillPlacementConfig())

	// The default axes now carry the lease's facts too: the automaton id the
	// lease holds (the backfill knows no Automation, so that axis is empty)
	// joins the model and PRD custom axes.
	want := []string{"compute:node-a", "model:qwen3:8b", "prd:prd1",
		"automaton:prd1"}
	if !reflect.DeepEqual(res, BackfillResult{Leases: 1, Backfilled: 1, Skipped: 0, Failed: 0}) {
		t.Fatalf("result = %+v, want {Leases:1 Backfilled:1}", res)
	}
	if got := store.systemTagsOf("sess1"); !reflect.DeepEqual(sortedTags(got), sortedTags(want)) {
		t.Fatalf("sess1 system tags = %v, want %v", got, want)
	}
	if got := store.writeIDs(); len(got) != 1 || got[0] != "sess1" {
		t.Fatalf("writes = %v, want [sess1]", got)
	}
}

// TestPlacementTags_StartupBackfill_Idempotent: a second run over the same
// leases finds the tags already in place and writes nothing, so restarting
// the daemon (or re-running the backfill) is a no-op for healthy sessions.
func TestPlacementTags_StartupBackfill_Idempotent(t *testing.T) {
	store := &backfillStore{}
	store.put("sess1", session.StateRunning, nil)
	led := &backfillLedger{leases: []capacity.Lease{leaseForSess1}}
	pc := backfillPlacementConfig()

	first := BackfillPlacementTags(led, store, pc)
	if first.Backfilled != 1 {
		t.Fatalf("first run backfilled = %d, want 1 (res %+v)", first.Backfilled, first)
	}
	afterFirst := store.systemTagsOf("sess1")

	second := BackfillPlacementTags(led, store, pc)
	if second != (BackfillResult{Leases: 1, Skipped: 1}) {
		t.Fatalf("second run = %+v, want everything skipped", second)
	}
	if got := store.writeIDs(); len(got) != 1 {
		t.Fatalf("total writes = %v, want exactly the one from the first run", got)
	}
	if after := store.systemTagsOf("sess1"); !reflect.DeepEqual(sortedTags(after), sortedTags(afterFirst)) {
		t.Fatalf("tags changed across the second run: %v -> %v", afterFirst, after)
	}
}

// TestPlacementTags_StartupBackfill_NoLeaseUntouched: a running session
// that holds no lease is not part of the lease iteration, so its system
// tags — whatever they are — are left exactly as they were, and no write
// reaches it.
func TestPlacementTags_StartupBackfill_NoLeaseUntouched(t *testing.T) {
	store := &backfillStore{}
	// sess1 holds the only lease; sess2 is running but lease-less.
	store.put("sess1", session.StateRunning, nil)
	store.put("sess2", session.StateRunning, []string{"legacy:tag"})
	led := &backfillLedger{leases: []capacity.Lease{leaseForSess1}}

	res := BackfillPlacementTags(led, store, backfillPlacementConfig())

	if res.Backfilled != 1 {
		t.Fatalf("backfilled = %d, want 1 (res %+v)", res.Backfilled, res)
	}
	if got := store.systemTagsOf("sess2"); !reflect.DeepEqual(got, []string{"legacy:tag"}) {
		t.Fatalf("sess2 system tags = %v, want the untouched [legacy:tag]", got)
	}
	for _, id := range store.writeIDs() {
		if id == "sess2" {
			t.Fatalf("backfill wrote the lease-less session sess2: %v", store.writeIDs())
		}
	}
}

// TestPlacementTags_StartupBackfill_UpdatesDiffering: a session whose
// stored tags no longer match the lease's derived set (a node or model
// change, or a half-written state) is brought back in line.
func TestPlacementTags_StartupBackfill_UpdatesDiffering(t *testing.T) {
	store := &backfillStore{}
	store.put("sess1", session.StateRunning, []string{"compute:node-b"})
	led := &backfillLedger{leases: []capacity.Lease{leaseForSess1}}

	res := BackfillPlacementTags(led, store, backfillPlacementConfig())

	if res.Backfilled != 1 || res.Failed != 0 {
		t.Fatalf("res = %+v, want exactly one backfill", res)
	}
	want := []string{"compute:node-a", "model:qwen3:8b", "prd:prd1",
		"automaton:prd1"}
	if got := store.systemTagsOf("sess1"); !reflect.DeepEqual(sortedTags(got), sortedTags(want)) {
		t.Fatalf("sess1 system tags = %v, want %v", got, want)
	}
}

// TestPlacementTags_StartupBackfill_FinishedSessionSkipped: a lease whose
// bound session has finished is left alone — its tags are a historical
// record of where the finished work ran, and the session is dead anyway.
func TestPlacementTags_StartupBackfill_FinishedSessionSkipped(t *testing.T) {
	for _, st := range []session.State{session.StateComplete, session.StateFailed, session.StateKilled} {
		store := &backfillStore{}
		store.put("sess1", st, []string{"compute:node-b"})
		led := &backfillLedger{leases: []capacity.Lease{leaseForSess1}}

		res := BackfillPlacementTags(led, store, backfillPlacementConfig())
		if res.Backfilled != 0 || res.Skipped != 1 {
			t.Fatalf("state %s: res = %+v, want skipped", st, res)
		}
		if got := store.systemTagsOf("sess1"); !reflect.DeepEqual(got, []string{"compute:node-b"}) {
			t.Fatalf("state %s: tags = %v, want the finished session left as-is", st, got)
		}
	}
}

func sortedTags(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
