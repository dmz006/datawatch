// Oversized themes-layout requests must be refused before their lengths can
// size an allocation. The bodies stay under the 256 KiB decode cap, but the
// lists they name are past the layout bounds (maxPlanThemes /
// maxPlanThemeItems), so a handler without the bounds would happily hand
// them to make().

package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/planrecords"
)

func wantTooMany(t *testing.T, code int, out map[string]any, what string) {
	t.Helper()
	if code != http.StatusBadRequest {
		t.Fatalf("%s: want 400, got %d %v", what, code, out)
	}
	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "at most") {
		t.Fatalf("%s: error must name the bound, got %q", what, msg)
	}
}

func TestThemesPutRejectsOversizedLayout(t *testing.T) {
	rr := newRelRig(t)
	id := rr.releasePlan(t, "12.0", "a-sec")
	tooMany := make([]map[string]any, maxPlanThemes+1)
	for i := range tooMany {
		tooMany[i] = map[string]any{"name": fmt.Sprintf("t-%05d", i)}
	}
	code, out := rr.send(t, http.MethodPut, "/api/plans/"+id+"/themes", map[string]any{"themes": tooMany})
	wantTooMany(t, code, out, "too many themes")
	if p, _ := rr.store.Get(id); len(p.Themes) != 0 {
		t.Fatalf("a refused layout wrote %d themes", len(p.Themes))
	}
}

func TestThemesPutRejectsOversizedThemeItems(t *testing.T) {
	rr := newRelRig(t)
	id := rr.releasePlan(t, "12.0", "a-sec")
	ids := make([]string, maxPlanThemeItems+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("a-%05d", i)
	}
	code, out := rr.send(t, http.MethodPut, "/api/plans/"+id+"/themes",
		map[string]any{"themes": []map[string]any{{"name": "Everything", "automata": ids}}})
	wantTooMany(t, code, out, "too many items in one theme")
	if p, _ := rr.store.Get(id); len(p.Themes) != 0 {
		t.Fatalf("a refused layout wrote %d themes", len(p.Themes))
	}
	// The same refusal comes from the checked-step layer directly, for either
	// kind of item.
	rec, _ := rr.store.Get(id)
	for kind, entries := range map[string][]planThemeSetEntry{
		"automata":    {{Name: "Everything", Automata: ids}},
		"automations": {{Name: "Everything", Automations: ids}},
	} {
		_, err := planThemeSetSteps(rec, entries)
		if err == nil {
			t.Fatalf("planThemeSetSteps accepted %d %s", len(ids), kind)
		}
		var inv *planrecords.ErrInvalid
		if !errors.As(err, &inv) {
			t.Fatalf("%s: error type = %T, want *planrecords.ErrInvalid", kind, err)
		}
		if !strings.Contains(inv.Reason, "at most") {
			t.Fatalf("%s: reason must name the bound: %q", kind, inv.Reason)
		}
	}
}

func TestThemesActionsRejectOversizedLists(t *testing.T) {
	rr := newRelRig(t)
	id := rr.releasePlan(t, "12.0", "a-sec")
	rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/themes", map[string]any{"action": "create", "name": "First"})
	rr.must(t, 200, http.MethodPost, "/api/plans/"+id+"/attach", map[string]any{"kind": "automaton", "id": "a-sec", "theme": "First"})

	themeIDs := make([]string, maxPlanThemes+1)
	for i := range themeIDs {
		themeIDs[i] = fmt.Sprintf("th-%05d", i)
	}
	if code, out := rr.send(t, http.MethodPost, "/api/plans/"+id+"/themes",
		map[string]any{"action": "reorder", "themes": themeIDs}); code != http.StatusBadRequest {
		t.Fatalf("reorder with %d themes: %d %v", len(themeIDs), code, out)
	}

	itemIDs := make([]string, maxPlanThemeItems+1)
	for i := range itemIDs {
		itemIDs[i] = fmt.Sprintf("a-%05d", i)
	}
	if code, out := rr.send(t, http.MethodPost, "/api/plans/"+id+"/themes",
		map[string]any{"action": "reorder_items", "theme": "First", "kind": "automaton", "items": itemIDs}); code != http.StatusBadRequest {
		t.Fatalf("reorder_items with %d items: %d %v", len(itemIDs), code, out)
	}
}

// factsPreallocHint is the overflow-safe sum the facts list is preallocated
// with: a real-sized hint passes through, anything near the int limit is
// clamped instead of wrapping negative.
func TestFactsPreallocHintClamps(t *testing.T) {
	cases := []struct {
		prds, autos int
		want        int
	}{
		{0, 0, 0},
		{5, 7, 12},
		{3, 4096, 4099},
		{1 << 62, 0, maxFactsPrealloc},
		{0, 1 << 62, maxFactsPrealloc},
		{1 << 62, 1 << 62, maxFactsPrealloc},
		{maxFactsPrealloc, maxFactsPrealloc, maxFactsPrealloc},
		{maxFactsPrealloc, maxFactsPrealloc + 1, maxFactsPrealloc},
		{-1, 5, 0},
		{5, -1, 0},
	}
	for _, tc := range cases {
		if got := factsPreallocHint(tc.prds, tc.autos); got != tc.want {
			t.Errorf("factsPreallocHint(%d, %d) = %d, want %d", tc.prds, tc.autos, got, tc.want)
		}
	}
}
