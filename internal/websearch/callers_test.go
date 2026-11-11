package websearch

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchCallers(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	rec := func(caller, sid string, n int) {
		for i := 0; i < n; i++ {
			if err := st.Record(ctx, Event{Time: time.Now(), ProviderName: "p", Caller: caller, SessionID: sid, Success: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	rec(CallerSession, "s-plain", 2)
	rec(CallerSession, "s-auto", 3)
	rec(CallerREST, "", 1)
	rec("", "legacy-sid", 1) // row from before callers were recorded
	rec("", "", 1)

	got, err := st.Callers(ctx, func(id string) bool { return id == "s-auto" })
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{CallerAutomaton: 3, CallerSession: 3, CallerREST: 1, CallerMCP: 1}
	if len(got) != len(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	for _, c := range got {
		if want[c.Caller] != c.Total {
			t.Errorf("caller %s = %d want %d", c.Caller, c.Total, want[c.Caller])
		}
	}
	if got[0].Total < got[len(got)-1].Total {
		t.Errorf("not sorted largest first: %+v", got)
	}
	empty, _ := st.Callers(ctx, nil)
	if len(empty) == 0 {
		t.Fatal("nil resolver should still count")
	}
}

func TestSearchCallersRegistryDefault(t *testing.T) {
	var r *Registry
	got, err := r.Callers(context.Background(), nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil registry: %v %v", got, err)
	}
}
